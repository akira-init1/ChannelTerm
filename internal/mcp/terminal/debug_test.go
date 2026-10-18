package terminal

import (
	"context"
	"encoding/json"
	"errors"
	"reflect"
	"testing"

	"github.com/akira-init1/ChannelTerm/internal/core/app"
	"github.com/akira-init1/ChannelTerm/internal/core/debugger"
	"github.com/akira-init1/ChannelTerm/internal/core/session"
)

func TestDebugMCPPreservesInvalidUTF8(t *testing.T) {
	r := debugRecord(debugger.Record{Kind: "~", Text: "\xff\x00", Results: map[string]any{"value": "\xfe", "nested": []any{"okay", "\xff"}}})
	if !reflect.DeepEqual(r["text"], map[string]any{"encoding": "base64", "data": "/wA="}) {
		t.Fatal(r)
	}
	encoded, e := json.Marshal(r)
	if e != nil || len(encoded) == 0 {
		t.Fatalf("JSON: %s %v", encoded, e)
	}
}

func TestBridgeListSchemaValidationAndCancellation(t *testing.T) {
	m := session.NewManager()
	defer m.Close()
	a, err := app.New(app.Dependencies{Manager: m})
	if err != nil {
		t.Fatal(err)
	}
	tool := &bridgeTool{name: "debug_bridge_list", application: a}
	schema := tool.InputSchema()
	if schema.Type != "object" || len(schema.Required) != 0 || len(schema.Properties) != 0 {
		t.Fatalf("list must not require owner or bridge ID: %+v", schema)
	}
	for _, input := range []string{`{"owner":"unnecessary"}`, `{"bridge_id":"RSP-example"}`, `[]`, `{} {}`} {
		if _, err := tool.Call(context.Background(), json.RawMessage(input)); err == nil {
			t.Fatalf("accepted invalid list arguments: %s", input)
		}
	}
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	if _, err := tool.Call(ctx, json.RawMessage(`{}`)); !errors.Is(err, context.Canceled) {
		t.Fatalf("canceled list: %v", err)
	}
}

func TestBridgePortPolicySchemaAndDecode(t *testing.T) {
	schema := (&bridgeTool{name: "debug_bridge_start"}).InputSchema()
	if schema.Properties["strict_port_pool"].Type != "boolean" {
		t.Fatal("missing strict pool boolean")
	}
	var q app.BridgeStartRequest
	if err := decodeDebug([]byte(`{"strict_port_pool":true,"listen_address":"127.0.0.1:37109"}`), &q); err != nil || !q.StrictPortPool || q.ListenAddress != "127.0.0.1:37109" {
		t.Fatalf("port policy decoding: %+v %v", q, err)
	}
}
func TestDebugMCPRejectsUnknownFieldsAndInvalidShapes(t *testing.T) {
	for _, input := range []string{`{"console":"shell"}`, `{"owner":"x"} {}`, `{"pid":"1"}`, `{"args":"a b"}`} {
		var q struct {
			Owner string   `json:"owner"`
			PID   int      `json:"pid"`
			Args  []string `json:"args"`
		}
		if e := decodeDebug([]byte(input), &q); e == nil {
			t.Fatalf("accepted %s", input)
		}
	}
}
