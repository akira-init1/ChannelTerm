// Package guiapi adapts narrowly scoped desktop HTTP requests to Application.
// It owns no Sessions or SSH implementation. The embedding Host must mount it
// behind the same authentication middleware as its existing MCP endpoint.
package guiapi

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"io"
	"mime"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/akira-init1/ChannelTerm/internal/core/app"
	sshtransport "github.com/akira-init1/ChannelTerm/internal/transport/ssh"
)

// SSHPath is separate from MCP; no MCP tool or parameter is added by this API.
const SSHPath = "/gui/v1/ssh"

type sshOpener interface {
	OpenSSH(context.Context, app.OpenSSHRequest) (app.OpenSSHResult, error)
}

type sshRequest struct {
	ConfirmHostKey bool         `json:"confirm_host_key"`
	Host           string       `json:"host"`
	User           string       `json:"user"`
	Port           int          `json:"port"`
	Label          string       `json:"label"`
	IdentityFiles  []string     `json:"identity_files"`
	KnownHostsFile string       `json:"known_hosts_file"`
	Password       *string      `json:"password"`
	KeyPassphrase  *string      `json:"key_passphrase"`
	Advanced       *sshAdvanced `json:"advanced"`
}

// sshAdvanced bounds optional setup/liveness settings without granting resize ownership.
type sshAdvanced struct {
	TimeoutSeconds      int    `json:"timeout_seconds"`
	KeepAliveSeconds    int    `json:"keepalive_seconds"`
	KeepAliveCount      int    `json:"keepalive_count"`
	WriteTimeoutSeconds int    `json:"write_timeout_seconds"`
	Term                string `json:"term"`
	Columns             int    `json:"columns"`
	Rows                int    `json:"rows"`
}

func (a sshAdvanced) valid() bool {
	return a.TimeoutSeconds >= 1 && a.TimeoutSeconds <= 120 &&
		a.KeepAliveSeconds >= 0 && a.KeepAliveSeconds <= 3600 &&
		a.KeepAliveCount >= 1 && a.KeepAliveCount <= 10 &&
		a.WriteTimeoutSeconds >= 0 && a.WriteTimeoutSeconds <= 3600 &&
		a.Term != "" && len(a.Term) <= 64 && !strings.ContainsFunc(a.Term, func(r rune) bool {
		return !(r >= 'a' && r <= 'z' || r >= 'A' && r <= 'Z' || r >= '0' && r <= '9' || r == '-' || r == '_' || r == '.' || r == '+')
	}) && a.Columns >= 1 && a.Columns <= 1000 && a.Rows >= 1 && a.Rows <= 1000
}

// NewSSHHandler exposes capability discovery and explicit SSH creation. Callers
// must authenticate before reaching this handler. Browser origins are rejected;
// secrets and underlying errors never enter responses, logs or Session metadata.
// Unknown keys require an opt-in, handshake-bound confirmation. Changed and revoked
// keys always fail closed against the Host trust file.
func NewSSHHandler(application *app.Application) http.Handler {
	return newSSHHandler(application)
}

func newSSHHandler(application sshOpener) http.Handler {
	active := make(chan struct{}, 2)
	trust := &sshTrust{pending: make(map[string]sshTrustDecision), decisions: make(chan struct{}, 2)}
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Cache-Control", "no-store")
		w.Header().Set("Content-Type", "application/json")
		if (r.URL.Path != SSHPath && r.URL.Path != SSHConfirmPath) || r.URL.RawQuery != "" {
			reply(w, 404, map[string]string{"error": "not_found"})
			return
		}
		if r.Header.Get("Origin") != "" {
			reply(w, 403, map[string]string{"error": "origin_forbidden"})
			return
		}
		if r.URL.Path == SSHConfirmPath {
			trust.serveDecision(w, r)
			return
		}
		if r.Method == http.MethodGet {
			reply(w, 200, map[string]bool{"available": true, "advanced": true, "trust_confirmation": true})
			return
		}
		if r.Method != http.MethodPost {
			w.Header().Set("Allow", "GET, POST")
			reply(w, 405, map[string]string{"error": "method_not_allowed"})
			return
		}
		media, _, err := mime.ParseMediaType(r.Header.Get("Content-Type"))
		if err != nil || media != "application/json" {
			reply(w, 415, map[string]string{"error": "invalid_content_type"})
			return
		}
		select {
		case active <- struct{}{}:
			defer func() { <-active }()
		default:
			reply(w, 429, map[string]string{"error": "busy"})
			return
		}
		// Bound a stalled body as well as setup. This affects only this request,
		// not subsequent MCP requests on the same HTTP/1 connection.
		started := time.Now()
		controller := http.NewResponseController(w)
		_ = controller.SetReadDeadline(time.Now().Add(5 * time.Second))
		defer controller.SetReadDeadline(time.Time{})
		body, err := io.ReadAll(http.MaxBytesReader(w, r.Body, 32768))
		// The body budget must not become a deadline for interactive confirmation
		// or the server's disconnect watcher once the complete body is consumed.
		_ = controller.SetReadDeadline(time.Time{})
		defer clear(body)
		var input sshRequest
		decoder := json.NewDecoder(bytes.NewReader(body))
		decoder.DisallowUnknownFields()
		if err != nil || decoder.Decode(&input) != nil || decoder.Decode(new(any)) != io.EOF || !validRequest(input) {
			reply(w, 400, map[string]string{"error": "invalid_input"})
			return
		}
		setupTimeout := 10 * time.Second
		if input.Advanced != nil {
			setupTimeout = time.Duration(input.Advanced.TimeoutSeconds) * time.Second
		}
		budget := setupTimeout + 5*time.Second
		if input.ConfirmHostKey {
			budget += 120 * time.Second
		}
		ctx, cancel := context.WithDeadline(r.Context(), started.Add(budget))
		defer cancel()
		config, cleanup, err := configuration(&input)
		if err != nil {
			reply(w, 400, map[string]string{"error": "configuration"})
			return
		}
		defer cleanup()
		if input.ConfirmHostKey {
			w.Header().Set("Content-Type", "application/x-ndjson")
			prompted := false
			config.ConfirmHostKey = func(ctx context.Context, key sshtransport.HostKey) (bool, error) {
				if prompted {
					return false, errDuplicateSSHTrust
				}
				prompted = true
				promptContext, stop := context.WithTimeout(ctx, 120*time.Second)
				defer stop()
				return trust.confirm(promptContext, w, key)
			}
		}
		opened, err := application.OpenSSH(ctx, app.OpenSSHRequest{Config: config, Label: input.Label})
		if err != nil {
			code := "connection_failed"
			var stage *sshtransport.StageError
			if errors.As(err, &stage) {
				switch stage.Stage {
				case "host-key":
					code = "host_key_rejected"
				case "identity", "authentication":
					code = "authentication_failed"
				}
			}
			if ctx.Err() != nil {
				code = "cancelled"
			}
			streamSSHResult(w, input.ConfirmHostKey, 422, map[string]any{"error": code})
			return
		}
		// Request completion/GUI detach never closes this Manager-owned Session.
		streamSSHResult(w, input.ConfirmHostKey, 200, map[string]any{"session_id": opened.Info.ID, "reused": opened.Reused})
	})
}

func reply(w http.ResponseWriter, status int, value any) {
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(value)
}

func cleanText(value string, max int) bool {
	return len(value) <= max && !strings.ContainsFunc(value, func(r rune) bool { return r < 32 || r == 127 })
}

func validRequest(r sshRequest) bool {
	if r.Advanced != nil && !r.Advanced.valid() {
		return false
	}
	if r.Host == "" || r.User == "" || !cleanText(r.Host, 253) || !cleanText(r.User, 128) || !cleanText(r.Label, 128) || r.Port < 1 || r.Port > 65535 || len(r.IdentityFiles) > 8 || !cleanText(r.KnownHostsFile, 4096) {
		return false
	}
	for _, path := range r.IdentityFiles {
		if path == "" || !cleanText(path, 4096) {
			return false
		}
	}
	return (r.Password == nil || len(*r.Password) <= 4096) && (r.KeyPassphrase == nil || len(*r.KeyPassphrase) <= 4096)
}

// configuration supplies the existing Transport's strict base configuration,
// never a parallel SSH stack or verification bypass. The handler may attach its
// bounded handshake confirmation callback afterward. Paths refer to the Host;
// this helper does not persist connection settings or trust records.
func configuration(r *sshRequest) (sshtransport.Config, func(), error) {
	c := sshtransport.Config{Host: r.Host, User: r.User, Port: r.Port, PrivateKeyPaths: r.IdentityFiles, KnownHostsPath: r.KnownHostsFile, Timeout: 10 * time.Second}
	if a := r.Advanced; a != nil {
		c.Timeout = time.Duration(a.TimeoutSeconds) * time.Second
		c.KeepAliveInterval = time.Duration(a.KeepAliveSeconds) * time.Second
		c.KeepAliveCountMax = a.KeepAliveCount
		c.WriteTimeout = time.Duration(a.WriteTimeoutSeconds) * time.Second
		c.Term, c.Columns, c.Rows = a.Term, a.Columns, a.Rows
	}
	if c.KnownHostsPath == "" || len(c.PrivateKeyPaths) == 0 && r.Password == nil {
		home, err := os.UserHomeDir()
		if err != nil {
			return c, nil, err
		}
		if c.KnownHostsPath == "" {
			c.KnownHostsPath = filepath.Join(home, ".ssh", "known_hosts")
		}
		if len(c.PrivateKeyPaths) == 0 && r.Password == nil {
			for _, name := range []string{"id_ed25519", "id_rsa"} {
				path := filepath.Join(home, ".ssh", name)
				if _, err := os.Stat(path); !errors.Is(err, os.ErrNotExist) {
					c.PrivateKeyPaths = append(c.PrivateKeyPaths, path)
				}
			}
		}
	}
	var password, passphrase []byte
	if r.Password != nil {
		password = []byte(*r.Password)
		*r.Password = ""
		r.Password = nil
		used := false
		c.Password = func(ctx context.Context) ([]byte, error) {
			if err := ctx.Err(); err != nil {
				return nil, err
			}
			if used {
				return nil, errors.New("credential already attempted")
			}
			used = true
			return bytes.Clone(password), nil
		}
	}
	if r.KeyPassphrase != nil {
		passphrase = []byte(*r.KeyPassphrase)
		*r.KeyPassphrase = ""
		r.KeyPassphrase = nil
		used := map[string]bool{}
		c.KeyPassphrase = func(ctx context.Context, path string) ([]byte, error) {
			if err := ctx.Err(); err != nil {
				return nil, err
			}
			if used[path] {
				return nil, errors.New("credential already attempted")
			}
			used[path] = true
			return bytes.Clone(passphrase), nil
		}
	}
	return c, func() { clear(password); clear(passphrase) }, nil
}
