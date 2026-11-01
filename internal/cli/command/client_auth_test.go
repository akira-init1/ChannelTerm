package command

import (
	"bytes"
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"testing"

	"github.com/akira-init1/ChannelTerm/internal/core/app"
	"github.com/akira-init1/ChannelTerm/internal/core/config"
	"github.com/akira-init1/ChannelTerm/internal/core/session"
	protocol "github.com/modelcontextprotocol/go-sdk/mcp"
)

func TestClientAuthenticationRejectsForgeryAndRevocation(t *testing.T) {
	store := config.ClientStore{Path: filepath.Join(t.TempDir(), "clients.json")}
	identity, token, err := store.Register("registered agent", "agent")
	if err != nil {
		t.Fatal(err)
	}
	var observed session.ClientIdentity
	handler := requireClientToken(store, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		observed = app.ClientIdentityFromContext(r.Context())
		w.WriteHeader(http.StatusNoContent)
	}))
	for _, test := range []struct {
		token string
		code  int
		id    string
	}{{token, 204, identity.ID}, {"legacy", 401, ""}, {"wrong", 401, ""}, {identity.ID, 401, ""}} {
		observed = session.ClientIdentity{}
		r := httptest.NewRequest("POST", "/mcp", nil)
		r.Header.Set("Authorization", "Bearer "+test.token)
		r.Header.Set("X-Client-ID", "forged")
		w := httptest.NewRecorder()
		handler.ServeHTTP(w, r)
		if w.Code != test.code || observed.ID != test.id {
			t.Fatalf("code %d identity %+v", w.Code, observed)
		}
	}
	if err := store.Remove(identity.ID); err != nil {
		t.Fatal(err)
	}
	r := httptest.NewRequest("POST", "/mcp", nil)
	r.Header.Set("Authorization", "Bearer "+token)
	w := httptest.NewRecorder()
	handler.ServeHTTP(w, r)
	if w.Code != 401 {
		t.Fatal("revocation not effective")
	}
	if err := os.WriteFile(store.Path, []byte("broken"), 0o600); err != nil {
		t.Fatal(err)
	}
	w = httptest.NewRecorder()
	handler.ServeHTTP(w, r)
	if w.Code != 503 {
		t.Fatal("corrupt store did not fail closed")
	}
}

func TestRegisteredMCPClientProvenanceSurvivesCLIActorAndLease(t *testing.T) {
	host := newAttachTestHost(t)
	defer host.close()
	store := config.ClientStore{Path: filepath.Join(t.TempDir(), "clients.json")}
	identity, token, err := store.Register("AI via CLI", "agent")
	if err != nil {
		t.Fatal(err)
	}
	if err := store.SetWriteAllowed(identity.ID, true); err != nil {
		t.Fatal(err)
	}
	server := httptest.NewServer(requireClientToken(store, host.server.Config.Handler))
	defer server.Close()
	transport, err := newBearerTokenTransport(server.URL, token)
	if err != nil {
		t.Fatal(err)
	}
	client := protocol.NewClient(&protocol.Implementation{Name: "test", Version: "test"}, nil)
	connected, err := client.Connect(context.Background(), &protocol.StreamableClientTransport{Endpoint: server.URL, DisableStandaloneSSE: true, HTTPClient: &http.Client{Transport: transport}}, nil)
	if err != nil {
		t.Fatal(err)
	}
	defer connected.Close()
	call := func(name string, args map[string]any) *protocol.CallToolResult {
		t.Helper()
		result, err := connected.CallTool(context.Background(), &protocol.CallToolParams{Name: name, Arguments: args})
		if err != nil || result.IsError {
			t.Fatalf("%s: %v %v", name, result, err)
		}
		return result
	}
	call("terminal_session_attach", map[string]any{"session_id": "board", "actor": "user"})
	call("terminal_write", map[string]any{"session_id": "board", "actor": "user", "data": "hello\n"})
	call("terminal_acquire_lease", map[string]any{"session_id": "board", "owner": "example-owner", "type": "terminal"})
	call("terminal_write_leased", map[string]any{"session_id": "board", "owner": "example-owner", "actor": "system", "data": "leased\n"})
	call("terminal_release_lease", map[string]any{"session_id": "board", "owner": "example-owner"})
	result := call("terminal_read_activity", map[string]any{"session_id": "board"})
	raw, err := json.Marshal(result.StructuredContent)
	if err != nil {
		t.Fatal(err)
	}
	var activity struct {
		Events []struct {
			Client session.ClientIdentity `json:"client"`
			Actor  string                 `json:"actor"`
		} `json:"events"`
	}
	if err := json.Unmarshal(raw, &activity); err != nil {
		t.Fatal(err)
	}
	if len(activity.Events) != 2 || activity.Events[0].Actor != "agent" {
		t.Fatalf("activity: %s", raw)
	}
	for _, event := range activity.Events {
		if event.Client.ID != identity.ID {
			t.Fatalf("lost client: %s", raw)
		}
	}
	result = call("terminal_session_events", map[string]any{"session_id": "board"})
	raw, _ = json.Marshal(result.StructuredContent)
	if !bytes.Contains(raw, []byte(identity.ID)) || bytes.Contains(raw, []byte(token)) {
		t.Fatal("invalid public provenance")
	}
}

func TestClientsCommandDoesNotOverwriteOrPrintSecrets(t *testing.T) {
	t.Setenv(config.HostConfigDirectoryEnv, t.TempDir())
	t.Setenv("XDG_CONFIG_HOME", t.TempDir())
	t.Setenv(httpAuthTokenEnvVar, "fixture-host-token")
	destination := filepath.Join(t.TempDir(), "client-token")
	var output bytes.Buffer
	args := []string{"add", "--name", "AI test", "--credential-file", destination}
	if err := runClients(args, &output); err != nil {
		t.Fatal(err)
	}
	token, err := os.ReadFile(destination)
	if err != nil {
		t.Fatal(err)
	}
	if bytes.Contains(output.Bytes(), bytes.TrimSpace(token)) {
		t.Fatal("secret printed")
	}
	if err := runClients(args, &output); err == nil {
		t.Fatal("overwrote credential file")
	}
	after, err := os.ReadFile(destination)
	if err != nil || !bytes.Equal(after, token) {
		t.Fatal("credential changed")
	}
}

// registeredTestHandler exercises production authentication with disposable clients.
func registeredTestHandler(t *testing.T, next http.Handler) (http.Handler, string) {
	t.Helper()
	store := config.ClientStore{Path: filepath.Join(t.TempDir(), "clients.json")}
	_, token, err := store.Register("test management client", "user")
	if err != nil {
		t.Fatal(err)
	}
	return requireClientToken(store, next), token
}

func TestMCPInitRequiresExplicitClientCredential(t *testing.T) {
	t.Setenv(config.HostConfigDirectoryEnv, t.TempDir())
	t.Setenv(httpAuthTokenEnvVar, "")
	if _, err := loadHTTPClientToken(); err != nil {
		t.Fatal(err)
	}
	if _, err := explicitHTTPClientToken(); err == nil {
		t.Fatal("init implicitly exported local management")
	}
	t.Setenv(httpAuthTokenEnvVar, "independent-ai-token")
	if got, err := explicitHTTPClientToken(); err != nil || got != "independent-ai-token" {
		t.Fatal("explicit configuration changed")
	}
}
