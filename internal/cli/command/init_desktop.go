package command

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"runtime"
	"strings"
	"sync/atomic"
	"time"

	"github.com/akira-init1/ChannelTerm/internal/core/config"
	initmcp "github.com/akira-init1/ChannelTerm/internal/init/mcp"
	protocol "github.com/modelcontextprotocol/go-sdk/mcp"
)

type desktopInitRequest struct {
	Action       string               `json:"action"`
	Target       initmcp.RepairTarget `json:"target"`
	Endpoint     string               `json:"endpoint,omitempty"`
	Fingerprint  string               `json:"fingerprint,omitempty"`
	Replace      bool                 `json:"replace,omitempty"`
	FileFallback bool                 `json:"file_fallback,omitempty"`
}

type desktopInitReply struct {
	OK    bool   `json:"ok"`
	Code  string `json:"code,omitempty"`
	Value any    `json:"value,omitempty"`
}

type desktopInitClient struct {
	ID         string `json:"id"`
	Detected   bool   `json:"detected"`
	Version    string `json:"version,omitempty"`
	Official   string `json:"official"`
	OpenCodeV1 bool   `json:"opencode_v1,omitempty"`
}

// runDesktopInit is a fixed local JSON bridge, not an HTTP administration route.
// It reads one bounded request and emits one secret-free result. Exact paths and
// sanitized entry previews are intended for Main's native confirmation only.
func runDesktopInit(ctx context.Context, args []string, input io.Reader, output io.Writer) error {
	if len(args) != 0 {
		return errors.New("init --mcp-desktop accepts one JSON request on stdin and no other arguments")
	}
	ctx, cancel := context.WithTimeout(ctx, 45*time.Second)
	defer cancel()
	write := func(value any, code string) error {
		return json.NewEncoder(output).Encode(desktopInitReply{OK: code == "", Code: code, Value: value})
	}
	data, err := io.ReadAll(io.LimitReader(input, 16385))
	if err != nil || len(data) > 16384 {
		return write(nil, "invalid_request")
	}
	var request desktopInitRequest
	decoder := json.NewDecoder(bytes.NewReader(data))
	decoder.DisallowUnknownFields()
	if decoder.Decode(&request) != nil || !json.Valid(data) {
		return write(nil, "invalid_request")
	}
	locations, err := desktopRepairLocations()
	if err != nil {
		return write(nil, "configuration_scope")
	}
	if request.Action == "discover" {
		var clients []desktopInitClient
		for _, id := range []string{"codex", "claude", "opencode", "zoo"} {
			clients = append(clients, inspectDesktopInitClient(ctx, locations, id))
		}
		return write(clients, "")
	}
	journal, err := desktopRepairJournalPath()
	if err != nil {
		return write(nil, "local_authority")
	}
	if request.Action == "recover" {
		if _, err := os.Lstat(journal); errors.Is(err, os.ErrNotExist) {
			return write(desktopRepairResult{State: "none"}, "")
		}
		store, err := config.DefaultClientStore()
		if err != nil {
			return write(nil, "local_authority")
		}
		result, err := (&desktopRepairCoordinator{store: store, journal: journal}).recover(ctx)
		if err != nil {
			return write(result, "recovery_required")
		}
		return write(result, "")
	}
	if request.Action != "preview" && request.Action != "apply" && request.Action != "verify" {
		return write(nil, "invalid_request")
	}
	if !initmcp.SafeRepairEndpoint(request.Endpoint) {
		return write(nil, "invalid_endpoint")
	}
	if request.Target.Client == "claude" && os.Getenv("CLAUDE_CONFIG_DIR") != "" {
		return write(nil, "configuration_scope")
	}
	if request.Target.Client == "opencode" {
		for _, key := range []string{"OPENCODE_CONFIG", "OPENCODE_CONFIG_CONTENT", "OPENCODE_CONFIG_DIR"} {
			if os.Getenv(key) != "" {
				return write(nil, "configuration_scope")
			}
		}
	}
	client := inspectDesktopInitClient(ctx, locations, request.Target.Client)
	request.Target.OpenCodeV1 = client.OpenCodeV1
	if request.Target.Client == "opencode" && client.Version == "" {
		return write(nil, "unsupported_client_version")
	}
	plan, err := initmcp.PreviewRepair(locations, request.Target, initmcp.Endpoint{Transport: initmcp.TransportStreamableHTTP, URL: request.Endpoint, Headers: map[string]string{"Authorization": "Bearer preview-placeholder"}})
	if err != nil {
		return write(nil, "configuration_unavailable")
	}
	if request.Action == "preview" {
		return write(struct {
			Plan   *initmcp.RepairPlan `json:"plan"`
			Client desktopInitClient   `json:"client"`
		}{plan, client}, "")
	}
	if request.Fingerprint != plan.Fingerprint {
		return write(nil, "configuration_changed")
	}
	if request.Action == "verify" {
		if !plan.Existing || plan.ExistingURL() != request.Endpoint || plan.ExistingCredential() == "" {
			return write(nil, "credential_unavailable")
		}
		if err := probeDesktopRepair(ctx, request.Endpoint, plan.ExistingCredential()); err != nil {
			return write(nil, desktopProbeCode(err))
		}
		return write(desktopRepairResult{State: "verified_existing", Permissions: "unknown"}, "")
	}
	if !request.FileFallback {
		return write(nil, "file_fallback_confirmation_required")
	}
	if plan.Existing && !request.Replace {
		return write(desktopRepairResult{State: "preserved"}, "")
	}
	store, err := config.DefaultClientStore()
	if err != nil {
		return write(nil, "local_authority")
	}
	coordinator := desktopRepairCoordinator{locations: locations, store: store, journal: journal, probe: probeDesktopRepair}
	result, err := coordinator.apply(ctx, request.Target, request.Endpoint, request.Fingerprint, request.Replace)
	if err != nil {
		code := "apply_failed"
		if result.State == "recovery_required" {
			code = "recovery_required"
		} else if probeCode := desktopProbeCode(err); probeCode != "apply_failed" {
			code = probeCode
		}
		return write(result, code)
	}
	return write(result, "")
}

func desktopRepairJournalPath() (string, error) {
	path, err := config.DefaultHTTPHostPath()
	if err != nil {
		return "", err
	}
	return filepath.Join(filepath.Dir(path), "mcp-repair.json"), nil
}

func desktopRepairLocations() (initmcp.RepairLocations, error) {
	home, err := os.UserHomeDir()
	if err != nil {
		return initmcp.RepairLocations{}, err
	}
	xdg := os.Getenv("XDG_CONFIG_HOME")
	if xdg == "" {
		xdg = filepath.Join(home, ".config")
	}
	locations := initmcp.RepairLocations{Options: initmcp.Options{Home: home}, OpenCodeDirectory: filepath.Join(xdg, "opencode"), CodexDirectory: os.Getenv("CODEX_HOME")}
	for _, path := range []string{locations.OpenCodeDirectory, locations.CodexDirectory} {
		if path != "" && !filepath.IsAbs(path) {
			return locations, errors.New("client configuration override must be absolute")
		}
	}
	return locations, nil
}

func inspectDesktopInitClient(ctx context.Context, locations initmcp.RepairLocations, id string) desktopInitClient {
	result := desktopInitClient{ID: id, Official: "unavailable"}
	if id != "codex" && id != "claude" && id != "opencode" && id != "zoo" {
		return result
	}
	if id != "zoo" {
		version, err := desktopClientHelp(ctx, id, "--version")
		if err == nil {
			result.Detected = true
			result.Version = regexp.MustCompile(`\b\d+\.\d+\.\d+(?:[-+][A-Za-z0-9.-]+)?`).FindString(version)
			if _, err := desktopClientHelp(ctx, id, "mcp", "add", "--help"); err == nil {
				// Current commands cannot combine secret-free process arguments,
				// durable authentication and exact replacement preview/rollback.
				// Do not invoke add or silently fall back after a failed mutation.
				result.Official = "needs_explicit_file_fallback"
			}
		}
	}
	result.OpenCodeV1 = id == "opencode" && strings.HasPrefix(result.Version, "1.")
	if id == "opencode" && !result.OpenCodeV1 && !strings.HasPrefix(result.Version, "2.") {
		result.Version = ""
	}
	plan, err := initmcp.PreviewRepair(locations, initmcp.RepairTarget{Client: id, Scope: "user", OpenCodeV1: result.OpenCodeV1}, initmcp.DefaultEndpoint())
	if err == nil {
		if _, err := os.Lstat(plan.Path); err == nil {
			result.Detected = true
		}
	}
	return result
}

// desktopClientHelp runs only fixed help/version arguments. No credential,
// renderer text, project path or install arguments can reach the command shell.
func desktopClientHelp(ctx context.Context, id string, args ...string) (string, error) {
	if id != "codex" && id != "claude" && id != "opencode" {
		return "", errors.New("unsupported client")
	}
	ctx, cancel := context.WithTimeout(ctx, 4*time.Second)
	defer cancel()
	path, err := exec.LookPath(id)
	if err != nil {
		return "", err
	}
	command := exec.CommandContext(ctx, path, args...)
	if runtime.GOOS == "windows" && strings.EqualFold(filepath.Ext(path), ".cmd") {
		// The fixed executable name is resolved by cmd for npm's Windows shims.
		command = exec.CommandContext(ctx, "cmd.exe", "/d", "/c", id+" "+strings.Join(args, " "))
	}
	hideDesktopHelper(command)
	command.WaitDelay = time.Second
	for _, entry := range os.Environ() {
		key, _, _ := strings.Cut(entry, "=")
		if !strings.EqualFold(key, httpAuthTokenEnvVar) {
			command.Env = append(command.Env, entry)
		}
	}
	var output boundedDesktopHelp
	command.Stdout = &output
	command.Stderr = io.Discard
	if err := command.Run(); err != nil {
		return "", err
	}
	return output.String(), nil
}

type boundedDesktopHelp struct{ buffer bytes.Buffer }

func (b *boundedDesktopHelp) String() string { return b.buffer.String() }

func (b *boundedDesktopHelp) Write(data []byte) (int, error) {
	if b.buffer.Len()+len(data) > 32768 {
		return 0, errors.New("client help exceeds limit")
	}
	return b.buffer.Write(data)
}

func probeDesktopRepair(parent context.Context, endpoint, token string) error {
	if !initmcp.SafeRepairEndpoint(endpoint) || token == "" || len(token) > 4096 || strings.ContainsAny(token, " \r\n\t") {
		return errors.New("invalid local probe")
	}
	ctx, cancel := context.WithTimeout(parent, 8*time.Second)
	defer cancel()
	transport, err := newBearerTokenTransport(endpoint, token)
	if err != nil {
		return err
	}
	client := protocol.NewClient(&protocol.Implementation{Name: "channelterm-client-setup", Version: version}, nil)
	observed := &desktopProbeTransport{base: &queryHTTPTransport{ctx: ctx, base: transport}}
	remote, err := client.Connect(ctx, &protocol.StreamableClientTransport{Endpoint: endpoint, DisableStandaloneSSE: true, HTTPClient: &http.Client{Transport: observed, CheckRedirect: func(*http.Request, []*http.Request) error { return http.ErrUseLastResponse }}}, nil)
	if err != nil {
		return observed.failure()
	}
	defer remote.Close()
	result, err := remote.CallTool(ctx, &protocol.CallToolParams{Name: "terminal_list_sessions", Arguments: map[string]any{}})
	if err != nil {
		return observed.failure()
	}
	if result.IsError {
		return &desktopProbeFailure{code: "permission_denied"}
	}
	return nil
}

type desktopProbeFailure struct{ code string }

func (e *desktopProbeFailure) Error() string { return "client setup probe: " + e.code }
func desktopProbeCode(err error) string {
	var failure *desktopProbeFailure
	if errors.As(err, &failure) {
		return failure.code
	}
	return "apply_failed"
}

// Retain only HTTP status classification, never server error bodies or headers.
type desktopProbeTransport struct {
	base   http.RoundTripper
	status atomic.Int64
}

func (t *desktopProbeTransport) RoundTrip(request *http.Request) (*http.Response, error) {
	response, err := t.base.RoundTrip(request)
	if response != nil && response.StatusCode >= 400 {
		t.status.Store(int64(response.StatusCode))
	}
	return response, err
}
func (t *desktopProbeTransport) failure() error {
	code := "host_unreachable"
	switch t.status.Load() {
	case 401:
		code = "authentication_failed"
	case 403:
		code = "permission_denied"
	case 503:
		code = "authentication_unavailable"
	}
	return &desktopProbeFailure{code: code}
}
