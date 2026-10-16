package terminal

import (
	"encoding/json"
	"reflect"
	"testing"

	"github.com/akira-init1/ChannelTerm/internal/core/app"
	"github.com/akira-init1/ChannelTerm/internal/core/debugger"
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
