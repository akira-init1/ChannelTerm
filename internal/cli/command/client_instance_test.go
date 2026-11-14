package command

import (
	"context"
	"encoding/json"
	"net/http/httptest"
	"path/filepath"
	"strings"
	"testing"

	"github.com/akira-init1/ChannelTerm/internal/core/app"
	"github.com/akira-init1/ChannelTerm/internal/core/config"
	"github.com/akira-init1/ChannelTerm/internal/core/session"
	protocol "github.com/modelcontextprotocol/go-sdk/mcp"
)

func TestCLIInstanceDelegationPreservesAgentAndRejectsForgery(t *testing.T) {
	host := newAttachTestHost(t)
	defer host.close()
	store := config.ClientStore{Path: filepath.Join(t.TempDir(), "clients.json")}
	identity, token, err := store.Register("Agent test", "agent")
	if err != nil {
		t.Fatal(err)
	}
	if err = store.SetWriteAllowed(identity.ID, true); err != nil {
		t.Fatal(err)
	}
	server := httptest.NewServer(requireClientToken(store, host.server.Config.Handler))
	defer server.Close()
	t.Setenv(httpAuthTokenEnvVar, token)
	ctx := context.Background()
	parent, _, err := connectMCPClient(ctx, server.URL)
	if err != nil {
		t.Fatal(err)
	}
	defer parent.Close()
	if parent.proof.ID == "" {
		t.Fatal("new Host did not register CLI")
	}
	var use struct {
		Use app.InstanceUse `json:"use"`
	}
	if err = callMCPTool(ctx, parent.ClientSession, "client_instance", map[string]any{"action": "use", "credential": parent.proof, "key": "parent-operation", "session_id": "board", "kind": "operation"}, &use); err != nil {
		t.Fatal(err)
	}
	var delegation struct {
		Token string `json:"delegation"`
	}
	if err = callMCPTool(ctx, parent.ClientSession, "client_instance", map[string]any{"action": "delegate", "credential": parent.proof, "use_id": use.Use.ID}, &delegation); err != nil {
		t.Fatal(err)
	}
	t.Setenv("CHANNELTERM_CLIENT_DELEGATION", delegation.Token)
	t.Setenv("CHANNELTERM_CLIENT_DELEGATION_ENDPOINT", server.URL)
	child, _, err := connectMCPClient(ctx, server.URL)
	if err != nil {
		t.Fatal(err)
	}
	defer child.Close()
	if child.proof.ID == parent.proof.ID {
		t.Fatal("child reused parent instance")
	}
	if err = callMCPTool(ctx, child, "terminal_write", map[string]any{"session_id": "board", "actor": "user", "data": "test\n"}, nil); err != nil {
		t.Fatal(err)
	}
	forged := child.proof
	forged.Secret = "invalid"
	result, err := child.ClientSession.CallTool(ctx, &protocol.CallToolParams{Name: "terminal_write", Arguments: map[string]any{"session_id": "board", "data": "forged"}, Meta: protocol.Meta{"io.channelterm/instance": forged}})
	if err != nil || !result.IsError {
		t.Fatal("forged presence reached writer", err)
	}
	var activity struct {
		Events []struct {
			Client session.ClientIdentity `json:"client"`
			Actor  string                 `json:"actor"`
		} `json:"events"`
	}
	if err = callMCPTool(ctx, parent, "terminal_read_activity", map[string]any{"session_id": "board"}, &activity); err != nil {
		t.Fatal(err)
	}
	if len(activity.Events) != 1 {
		t.Fatalf("unexpected writes: %d", len(activity.Events))
	}
	c := activity.Events[0].Client
	if c.ID != identity.ID || c.Kind != "agent" || activity.Events[0].Actor != "agent" || c.InstanceID != child.proof.ID || c.ParentInstanceID != parent.proof.ID || c.OperationID != use.Use.ID || c.Entry != "mcp" || c.DeclaredEntry != "cli" {
		t.Fatalf("lost source: %+v", c)
	}
	var views struct {
		Instances []app.ClientInstance `json:"instances"`
	}
	if err = callMCPTool(ctx, parent, "client_instance", map[string]any{"action": "list"}, &views); err != nil {
		t.Fatal(err)
	}
	raw, _ := json.Marshal(views)
	for _, secret := range []string{token, parent.proof.Secret, child.proof.Secret, delegation.Token} {
		if strings.Contains(string(raw), secret) {
			t.Fatal("secret in public instance list")
		}
	}
	if err = child.Close(); err != nil {
		t.Fatal(err)
	}
	if err = callMCPTool(ctx, parent, "terminal_write", map[string]any{"session_id": "board", "data": "still-open\n"}, nil); err != nil {
		t.Fatal("child close closed Session", err)
	}
}

func TestCLICommandSharesOneInstanceAcrossInternalConnections(t *testing.T) {
	host := newAttachTestHost(t)
	defer host.close()
	scope := &cliInstances{clients: make(map[string]*instanceMCPClient)}
	defer scope.close()
	ctx := context.WithValue(context.Background(), cliInstancesKey{}, scope)
	first, _, err := connectMCPClient(ctx, host.server.URL)
	if err != nil {
		t.Fatal(err)
	}
	if err = first.Close(); err != nil {
		t.Fatal(err)
	}
	second, _, err := connectMCPClient(ctx, host.server.URL)
	if err != nil {
		t.Fatal(err)
	}
	if first != second || first.proof.ID == "" {
		t.Fatal("internal connection created another process instance")
	}
	if err = callMCPTool(ctx, second, "terminal_session_attach", map[string]any{"session_id": "board"}, nil); err != nil {
		t.Fatal(err)
	}
	var views struct {
		Instances []app.ClientInstance `json:"instances"`
	}
	if err = callMCPTool(ctx, second, "client_instance", map[string]any{"action": "list"}, &views); err != nil {
		t.Fatal(err)
	}
	if len(views.Instances) != 1 || len(views.Instances[0].Uses) != 1 || views.Instances[0].Uses[0].Kind != "monitor" {
		t.Fatalf("missing explicit monitor: %+v", views)
	}
	if err = callMCPTool(ctx, second, "terminal_session_detach", map[string]any{"session_id": "board"}, nil); err != nil {
		t.Fatal(err)
	}
	if err = callMCPTool(ctx, second, "client_instance", map[string]any{"action": "list"}, &views); err != nil {
		t.Fatal(err)
	}
	if len(views.Instances[0].Uses) != 0 {
		t.Fatal("detach retained monitoring")
	}
}
