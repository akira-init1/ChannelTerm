package command

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"strings"
	"testing"
	"time"

	"github.com/akira-init1/ChannelTerm/internal/core/app"
	"github.com/akira-init1/ChannelTerm/internal/core/session"
	mcpadapter "github.com/akira-init1/ChannelTerm/internal/mcp"
)

func TestGDBListFormattingAndEmptyResults(t *testing.T) {
	item := app.BridgeSummary{BridgeID: "RSP-example", SessionID: "opaque-session", SessionRef: "SER-1", Target: "TEST\t\x1b[31m", Address: "[::1]:47123", Port: 47123, State: "disconnected"}
	for _, jsonOutput := range []bool{false, true} {
		for _, empty := range []bool{false, true} {
			var output bytes.Buffer
			args := []string{"--endpoint", " http://localhost:37099/mcp "}
			if jsonOutput {
				args = append(args, "--json")
			}
			err := runGDBList(context.Background(), args, &output, func(ctx context.Context, endpoint string) ([]app.BridgeSummary, error) {
				deadline, ok := ctx.Deadline()
				if endpoint != "http://localhost:37099/mcp" || !ok || time.Until(deadline) <= 0 || time.Until(deadline) > 10*time.Second {
					t.Fatalf("query endpoint/deadline: %q, %v", endpoint, deadline)
				}
				if empty {
					return nil, nil
				}
				return []app.BridgeSummary{item}, nil
			})
			if err != nil {
				t.Fatal(err)
			}
			if jsonOutput {
				var report struct {
					Endpoint string              `json:"endpoint"`
					Bridges  []app.BridgeSummary `json:"bridges"`
				}
				if err := json.Unmarshal(output.Bytes(), &report); err != nil {
					t.Fatal(err)
				}
				if report.Endpoint != "http://localhost:37099/mcp" || report.Bridges == nil {
					t.Fatalf("invalid JSON envelope: %s", output.String())
				}
				if empty && len(report.Bridges) != 0 || !empty && (len(report.Bridges) != 1 || report.Bridges[0] != item) {
					t.Fatalf("incorrect JSON inventory: %s", output.String())
				}
			} else if empty {
				if !strings.Contains(output.String(), "No active external GDB bridges.") {
					t.Fatal(output.String())
				}
			} else {
				for _, want := range []string{"PORT", "ADDRESS", "SESSION", "TARGET", "STATE", "BRIDGE", "47123", "[::1]:47123", "SER-1", "disconnected", "RSP-example", `TEST\t\x1b[31m`} {
					if !strings.Contains(output.String(), want) {
						t.Fatalf("missing %q in %s", want, output.String())
					}
				}
				if strings.Contains(output.String(), "\x1b") {
					t.Fatal("Host metadata injected terminal controls")
				}
			}
		}
	}
}

func TestGDBListHelpValidationAndQueryErrors(t *testing.T) {
	for _, args := range [][]string{{"--help"}, {"-h"}, {"--unknown"}, {"SER-1"}, {"--endpoint"}} {
		var output bytes.Buffer
		err := runGDBList(context.Background(), args, &output, func(context.Context, string) ([]app.BridgeSummary, error) {
			t.Fatal("help or invalid arguments queried a Host")
			return nil, nil
		})
		help := args[0] == "--help" || args[0] == "-h"
		if help && (err != nil || !strings.Contains(output.String(), "Usage: channelterm gdb-list")) || !help && err == nil {
			t.Fatalf("args %v: %v, %s", args, err, output.String())
		}
	}
	for _, cause := range []error{context.Canceled, context.DeadlineExceeded, errors.New("unknown tool: debug_bridge_list")} {
		var output bytes.Buffer
		err := runGDBList(context.Background(), []string{"--json"}, &output, func(_ context.Context, endpoint string) ([]app.BridgeSummary, error) {
			if endpoint != defaultMCPEndpoint {
				t.Fatal(endpoint)
			}
			return nil, cause
		})
		if !errors.Is(err, cause) || output.Len() != 0 {
			t.Fatalf("query failure presented as success: %v, %s", err, output.String())
		}
	}
}

func TestGDBListQueriesAuthenticatedHost(t *testing.T) {
	const token = "EXAMPLE-HTTP-TOKEN-NOT-A-SECRET"
	t.Setenv(httpAuthTokenEnvVar, token)
	m := session.NewManager()
	defer m.Close()
	s, err := session.New("list-board", newAttachTestTransport())
	if err != nil {
		t.Fatal(err)
	}
	if err := s.Connect(context.Background()); err != nil {
		t.Fatal(err)
	}
	if err := m.RegisterWithMetadata(s, session.SessionMetadata{Transport: "serial", Endpoint: "TEST-NO-HARDWARE"}); err != nil {
		t.Fatal(err)
	}
	registry, err := newMCPRegistry(m, newTestDeviceRegistry(t))
	if err != nil {
		t.Fatal(err)
	}
	handler, err := mcpadapter.NewStreamableHTTPHandler(registry, "test")
	if err != nil {
		t.Fatal(err)
	}
	server := httptest.NewServer(requireBearerToken(token, handler))
	defer server.Close()
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	client, _, err := connectMCPClient(ctx, server.URL)
	if err != nil {
		t.Fatal(err)
	}
	defer client.Close()
	const owner = "EXAMPLE-BRIDGE-OWNER-NOT-A-SECRET"
	var start app.BridgeStartResult
	if err := callMCPTool(ctx, client, "debug_bridge_start", map[string]any{"session_id": "SER-1", "owner": owner, "request_id": "list", "listen_address": "127.0.0.1:0", "acknowledge_unverified_target": true}, &start); err != nil {
		t.Fatal(err)
	}
	defer func() {
		var result any
		if err := callMCPTool(context.Background(), client, "debug_bridge_close", map[string]any{"bridge_id": start.Status.BridgeID, "owner": owner}, &result); err != nil {
			t.Error(err)
		}
	}()
	for _, jsonOutput := range []bool{false, true} {
		args := []string{"gdb-list", "--endpoint", server.URL}
		if jsonOutput {
			args = append(args, "--json")
		}
		var output bytes.Buffer
		if err := Run(ctx, args, strings.NewReader(""), &output, io.Discard); err != nil {
			t.Fatal(err)
		}
		for _, want := range []string{start.Status.BridgeID, start.Status.Address, "SER-1", "TEST-NO-HARDWARE", "listening"} {
			if !strings.Contains(output.String(), want) {
				t.Fatalf("missing %q in %s", want, output.String())
			}
		}
		for _, secret := range []string{owner, token, start.Connection.Token, start.Connection.Fingerprint} {
			if strings.Contains(output.String(), secret) {
				t.Fatal("CLI output disclosed a credential")
			}
		}
	}
	t.Setenv(httpAuthTokenEnvVar, "EXAMPLE-WRONG-TOKEN")
	var output bytes.Buffer
	if err := Run(ctx, []string{"gdb-list", "--endpoint", server.URL, "--json"}, strings.NewReader(""), &output, io.Discard); err == nil || output.Len() != 0 {
		t.Fatalf("unauthorized query: %v, %s", err, output.String())
	}
	// The original authenticated client remains usable after an observer fails.
	var status struct {
		Status app.BridgeStatus `json:"status"`
	}
	if err := callMCPTool(ctx, client, "debug_bridge_status", map[string]any{"bridge_id": start.Status.BridgeID, "owner": owner}, &status); err != nil || status.Status.State != "listening" || !status.Status.ExpiresAt.Equal(start.Status.ExpiresAt) {
		t.Fatalf("observation affected bridge lifetime: %+v, %v", status, err)
	}
}

func TestGDBListInterruptCancelsHTTPQuery(t *testing.T) {
	t.Setenv(httpAuthTokenEnvVar, "EXAMPLE-HTTP-TOKEN-NOT-A-SECRET")
	entered := make(chan struct{}, 1)
	release := make(chan struct{})
	server := httptest.NewServer(http.HandlerFunc(func(_ http.ResponseWriter, r *http.Request) {
		_, _ = io.Copy(io.Discard, r.Body)
		select {
		case entered <- struct{}{}:
		default:
		}
		select {
		case <-r.Context().Done():
		case <-release:
		}
	}))
	defer server.Close()
	defer close(release)
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	interrupts := make(chan os.Signal, 1)
	done := make(chan error, 1)
	var output bytes.Buffer
	go func() {
		done <- RunWithInterrupts(ctx, []string{"gdb-list", "--endpoint", server.URL, "--json"}, strings.NewReader(""), &output, io.Discard, interrupts)
	}()
	select {
	case <-entered:
	case <-ctx.Done():
		t.Fatal("query did not reach HTTP server")
	}
	interrupts <- os.Interrupt
	select {
	case err := <-done:
		if !errors.Is(err, context.Canceled) || output.Len() != 0 {
			t.Fatalf("interrupt produced a success inventory: %v, %s", err, output.String())
		}
	case <-ctx.Done():
		t.Fatal("interrupt did not stop HTTP query")
	}
}

func TestGDBListQueryCancellationWhileReadingResponse(t *testing.T) {
	release := make(chan struct{})
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
		w.(http.Flusher).Flush()
		select {
		case <-r.Context().Done():
		case <-release:
		}
	}))
	defer server.Close()
	defer close(release)
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	client := &http.Client{Transport: &queryHTTPTransport{ctx: ctx, base: http.DefaultTransport}, Timeout: 5 * time.Second}
	response, err := client.Get(server.URL)
	if err != nil {
		t.Fatal(err)
	}
	defer response.Body.Close()
	cancel()
	if _, err := io.ReadAll(response.Body); !errors.Is(err, context.Canceled) {
		t.Fatalf("query cancellation did not stop response read: %v", err)
	}
	for range 2 {
		if err := response.Body.Close(); err != nil {
			t.Fatalf("repeated response close: %v", err)
		}
	}
}
