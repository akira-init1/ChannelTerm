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
	protocol "github.com/modelcontextprotocol/go-sdk/mcp"
)

func TestExternalBridgeMCPLifecycle(t *testing.T) {
	m := session.NewManager()
	defer m.Close()
	s, err := session.New("bridge-board", newFakeTransport(), session.WithEventBufferCapacity(8))
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
	list := func(reader *protocol.ClientSession) []app.BridgeSummary {
		t.Helper()
		result := callTool(t, reader, "debug_bridge_list", map[string]any{})
		if result.IsError {
			t.Fatal(resultText(t, result))
		}
		var inventory struct {
			Bridges []app.BridgeSummary `json:"bridges"`
		}
		if err := json.Unmarshal([]byte(resultText(t, result)), &inventory); err != nil || inventory.Bridges == nil {
			t.Fatalf("invalid bridge inventory: %v, %s", err, resultText(t, result))
		}
		return inventory.Bridges
	}
	if items := list(client); len(items) != 0 {
		t.Fatalf("expected empty inventory: %+v", items)
	}
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
	if items := list(client); len(items) != 1 || items[0].BridgeID != start.Status.BridgeID || items[0].Address != start.Connection.Address || items[0].SessionRef != "SER-1" || !items[0].ExpiresAt.Equal(start.Status.ExpiresAt) {
		t.Fatalf("incorrect bridge inventory: %+v", items)
	}
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
	// Reports use MCP events only; errors must leave the authenticated connection
	// available to the external GDB client.
	report := map[string]any{"bridge_id": start.Status.BridgeID, "owner": owner, "sequence": 1, "kind": "command", "text": "break main"}
	for range 2 {
		if r := callTool(t, client, "debug_bridge_report", report); r.IsError || !strings.Contains(resultText(t, r), `"sequence":1`) {
			t.Fatal(resultText(t, r))
		}
	}
	report["text"] = start.Connection.Token
	if r := callTool(t, client, "debug_bridge_report", report); !r.IsError || strings.Contains(resultText(t, r), start.Connection.Token) {
		t.Fatal("report did not reject credential safely")
	}
	observer, closeObserver := connectTestClient(t, registry)
	defer closeObserver()
	items := list(observer)
	if len(items) != 1 || items[0].BridgeID != start.Status.BridgeID {
		t.Fatalf("independent observer could not list bridges: %+v", items)
	}
	encoded, err := json.Marshal(items)
	if err != nil {
		t.Fatal(err)
	}
	for _, secret := range []string{owner, start.Connection.Token, start.Connection.Fingerprint} {
		if strings.Contains(string(encoded), secret) {
			t.Fatal("bridge inventory disclosed credentials")
		}
	}
	for _, reader := range []*protocol.ClientSession{client, observer} {
		r := callTool(t, reader, "terminal_session_events", map[string]any{"session_id": "SER-1", "cursor": 0, "max_events": 100})
		var events struct {
			Events []session.Event `json:"events"`
		}
		if err := json.Unmarshal([]byte(resultText(t, r)), &events); err != nil {
			t.Fatal(err)
		}
		count := 0
		for _, e := range events.Events {
			if e.Type == session.EventDebugClientReport {
				count++
				if e.Metadata["text"] != "break main" || e.Metadata["reported_by"] != "codex" || e.Metadata["complete_audit"] != false {
					t.Fatal(e)
				}
			}
		}
		if count != 1 {
			t.Fatalf("report retry duplicated history: %d", count)
		}
	}
	for seq := 3; seq <= 12; seq++ {
		report["sequence"], report["text"], report["kind"] = seq, "stopped at main", "state"
		if r := callTool(t, client, "debug_bridge_report", report); r.IsError {
			t.Fatal(resultText(t, r))
		}
	}
	retained := callTool(t, observer, "terminal_session_events", map[string]any{"session_id": "SER-1", "cursor": 0, "max_events": 100})
	if !strings.Contains(resultText(t, retained), `"dropped":true`) || !strings.Contains(resultText(t, retained), `"sequence":12`) {
		t.Fatal("slow observer did not receive bounded retained history")
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
	if items := list(observer); len(items) != 1 || items[0].State != "disconnected" {
		t.Fatalf("disconnected bridge missing: %+v", items)
	}
	if r := callTool(t, client, "debug_bridge_close", map[string]any{"bridge_id": start.Status.BridgeID, "owner": "wrong"}); !r.IsError {
		t.Fatal("wrong owner closed bridge")
	}
	result = callTool(t, client, "debug_bridge_close", control)
	if result.IsError || s.State() != session.StateClosed {
		t.Fatal(resultText(t, result))
	}
	if items := list(observer); len(items) != 0 {
		t.Fatalf("closed bridge retained in inventory: %+v", items)
	}
}
