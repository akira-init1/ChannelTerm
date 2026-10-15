package terminal

import (
	"encoding/json"
	"reflect"
	"testing"

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
