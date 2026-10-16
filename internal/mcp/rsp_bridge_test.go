package mcp

import (
	"context"
	"encoding/json"
	"strings"
	"testing"
	"time"

	"github.com/akira-init1/ChannelTerm/internal/core/app"
	"github.com/akira-init1/ChannelTerm/internal/core/debugger"
	"github.com/akira-init1/ChannelTerm/internal/core/session"
	"github.com/akira-init1/ChannelTerm/internal/core/tool"
	"github.com/akira-init1/ChannelTerm/internal/mcp/terminal"
)

func TestExternalBridgeMCPLifecycle(t *testing.T) {
	m := session.NewManager()
	defer m.Close()
	s, err := session.New("bridge-board", newFakeTransport())
	if err != nil {
		t.Fatal(err)
	}
	if err := s.Connect(context.Background()); err != nil {
		t.Fatal(err)
	}
	if err := m.RegisterWithMetadata(s, session.SessionMetadata{Transport: "serial"}); err != nil {
		t.Fatal(err)
	}
	a, err := app.New(app.Dependencies{Manager: m})
	if err != nil {
		t.Fatal(err)
	}
	registered, err := terminal.NewTools(a)
	if err != nil {
		t.Fatal(err)
	}
	registry := tool.NewRegistry()
	for _, current := range registered {
		if err := registry.Register(current); err != nil {
			t.Fatal(err)
		}
	}
	client, closeClient := connectTestClient(t, registry)
	defer closeClient()
	owner := "EXAMPLE-MCP-BRIDGE-OWNER-NOT-A-REAL-SECRET"
	q := map[string]any{"session_id": "SER-1", "owner": owner, "request_id": "start", "acknowledge_unverified_target": false}
	result := callTool(t, client, "debug_bridge_start", q)
	if !result.IsError || !strings.Contains(resultText(t, result), "unverified") {
		t.Fatal("readiness risk missing")
	}
	q["acknowledge_unverified_target"] = true
	result = callTool(t, client, "debug_bridge_start", q)
	if result.IsError {
		t.Fatal(resultText(t, result))
	}
	var start app.BridgeStartResult
	if err := json.Unmarshal([]byte(resultText(t, result)), &start); err != nil {
		t.Fatal(err)
	}
	defer a.CloseRSPBridge(start.Status.BridgeID, owner)
	control := map[string]any{"bridge_id": start.Status.BridgeID, "owner": owner}
	if r := callTool(t, client, "debug_bridge_renew", control); r.IsError {
		t.Fatal(resultText(t, r))
	}
	if r := callTool(t, client, "terminal_write", map[string]any{"session_id": "SER-1", "data": "bad"}); !r.IsError {
		t.Fatal("MCP write bypassed lease")
	}
	conn, err := debugger.DialBridge(context.Background(), *start.Connection)
	if err != nil {
		t.Fatal(err)
	}
	conn.Close()
	deadline := time.Now().Add(time.Second)
	for {
		result = callTool(t, client, "debug_bridge_status", control)
		text := resultText(t, result)
		if strings.Contains(text, start.Connection.Token) || strings.Contains(text, owner) {
			t.Fatal("MCP status leaked capability")
		}
		if strings.Contains(text, "disconnected") {
			break
		}
		if time.Now().After(deadline) {
			t.Fatal(text)
		}
		time.Sleep(time.Millisecond)
	}
	if r := callTool(t, client, "debug_bridge_close", map[string]any{"bridge_id": start.Status.BridgeID, "owner": "wrong"}); !r.IsError {
		t.Fatal("wrong owner closed bridge")
	}
	result = callTool(t, client, "debug_bridge_close", control)
	if result.IsError || s.State() != session.StateClosed {
		t.Fatal(resultText(t, result))
	}
}
