package command

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"testing"

	"github.com/akira-init1/ChannelTerm/internal/core/app"
	"github.com/akira-init1/ChannelTerm/internal/core/config"
	protocol "github.com/modelcontextprotocol/go-sdk/mcp"
)

func TestHTTPClientGrantsEnforceReadOnlyAndImmediateRevocation(t *testing.T) {
	host := newAttachTestHost(t)
	defer host.close()
	store := config.ClientStore{Path: filepath.Join(t.TempDir(), "clients.json")}
	agent, token, err := store.Register("agent", "agent")
	if err != nil {
		t.Fatal(err)
	}
	server := httptest.NewServer(requireClientToken(store, host.server.Config.Handler))
	defer server.Close()
	connect := func(credential string) *protocol.ClientSession {
		t.Helper()
		transport, err := newBearerTokenTransport(server.URL, credential)
		if err != nil {
			t.Fatal(err)
		}
		client := protocol.NewClient(&protocol.Implementation{Name: "permission-test", Version: "test"}, nil)
		connected, err := client.Connect(context.Background(), &protocol.StreamableClientTransport{Endpoint: server.URL, DisableStandaloneSSE: true, HTTPClient: &http.Client{Transport: transport}}, nil)
		if err != nil {
			t.Fatal(err)
		}
		t.Cleanup(func() { _ = connected.Close() })
		return connected
	}
	ai := connect(token)
	call := func(client *protocol.ClientSession, name string, args map[string]any, wantDenied bool) {
		t.Helper()
		result, err := client.CallTool(context.Background(), &protocol.CallToolParams{Name: name, Arguments: args})
		if err != nil {
			t.Fatalf("%s: %v", name, err)
		}
		if result.IsError != wantDenied {
			t.Fatalf("%s IsError=%t, want %t: %+v", name, result.IsError, wantDenied, result.StructuredContent)
		}
		if wantDenied {
			data, _ := json.Marshal(result.StructuredContent)
			var decoded struct{ Error struct{ Code string } }
			if err := json.Unmarshal(data, &decoded); err != nil || decoded.Error.Code != "permission_denied" {
				t.Fatalf("denial: %s", data)
			}
		}
	}
	write := map[string]any{"session_id": "board", "data": "test\n", "actor": "user"}
	for _, client := range []*protocol.ClientSession{ai} {
		call(client, "terminal_list_sessions", map[string]any{}, false)
		call(client, "terminal_read", map[string]any{"session_id": "board"}, false)
		call(client, "terminal_write", write, true)
		for _, name := range []string{"terminal_write_leased", "terminal_exec", "terminal_open_serial", "terminal_close", "terminal_acquire_lease", "terminal_renew_lease", "terminal_release_lease", "terminal_report_file_transfer", "terminal_file_transfer_checkpoint", "terminal_begin_file_transfer_cancel", "terminal_resolve_file_transfer_cancel", "terminal_enter_uboot", "terminal_export_diagnostics", "debug_start", "debug_command", "debug_events", "debug_stop", "debug_bridge_start", "debug_bridge_renew", "debug_bridge_close", "debug_bridge_restore", "debug_bridge_report"} {
			call(client, name, map[string]any{}, true)
		}
	}
	if err := store.SetWriteAllowed(agent.ID, true); err != nil {
		t.Fatal(err)
	}
	call(ai, "terminal_write", write, false)
	call(ai, "terminal_close", map[string]any{"session_id": "board"}, true)
	if err := store.SetWriteAllowed(agent.ID, false); err != nil {
		t.Fatal(err)
	}
	call(ai, "terminal_write", write, true)
	call(ai, "terminal_list_sessions", map[string]any{}, false)
	adminToken, err := store.LoadOrCreateLocalClientToken()
	if err != nil {
		t.Fatal(err)
	}
	admin := connect(adminToken)
	call(admin, "terminal_write", write, false)
	call(admin, "terminal_close", map[string]any{"session_id": "board"}, false)
}

func TestDesktopMutationsRequireManagementNotAgentWriteGrant(t *testing.T) {
	for _, test := range []struct {
		name, method string
		access       app.ClientAccess
		want         int
	}{
		{"agent read", http.MethodGet, app.ClientAccess{}, 204},
		{"agent read-only mutation", http.MethodPost, app.ClientAccess{}, 403},
		{"agent mutation", http.MethodPost, app.ClientAccess{Write: true}, 403},
		{"management", http.MethodPost, app.ClientAccess{Write: true, Manage: true}, 204},
	} {
		t.Run(test.name, func(t *testing.T) {
			handler := requireDesktopManagement(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) { w.WriteHeader(204) }))
			r := httptest.NewRequest(test.method, "/gui/v1/serial-profiles", nil)
			r = r.WithContext(app.WithClientAccess(r.Context(), test.access))
			w := httptest.NewRecorder()
			handler.ServeHTTP(w, r)
			if w.Code != test.want {
				t.Fatalf("status=%d", w.Code)
			}
		})
	}
}

func TestExplicitClientTokenNeverFallsBackToManagement(t *testing.T) {
	t.Setenv(config.HostConfigDirectoryEnv, t.TempDir())
	t.Setenv("XDG_CONFIG_HOME", t.TempDir())
	t.Setenv(httpAuthTokenEnvVar, "explicit-read-only-credential")
	got, err := loadHTTPClientToken()
	if err != nil || got != "explicit-read-only-credential" {
		t.Fatal("explicit credential replaced")
	}
	t.Setenv(httpAuthTokenEnvVar, "   ")
	if _, err := loadHTTPClientToken(); err == nil {
		t.Fatal("malformed explicit credential fell back to management")
	}
	t.Setenv(httpAuthTokenEnvVar, "")
	got, err = loadHTTPClientToken()
	if err != nil {
		t.Fatal("implicit local client still uses shared token")
	}
	store, err := config.DefaultClientStore()
	if err != nil {
		t.Fatal(err)
	}
	client, ok, err := store.Authenticate(got)
	if err != nil || !ok || client.Kind != "user" {
		t.Fatal("local client not registered")
	}
}
