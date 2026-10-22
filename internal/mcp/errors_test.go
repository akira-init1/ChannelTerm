package mcp

import "testing"

func TestInvalidUTF8StructuredErrorAllowsLosslessRetry(t *testing.T) {
	manager, registry, device := newTestRegistry(t)
	defer manager.Close()
	client, closeClient := connectTestClient(t, registry)
	defer closeClient()
	device.emit([]byte{0xd3, '\r', 'x'})
	r := callTool(t, client, "terminal_read", map[string]any{"session_id": "board", "cursor": 0, "timeout_ms": 1000})
	if !r.IsError {
		t.Fatal("invalid UTF-8 accepted")
	}
	structured := r.StructuredContent.(map[string]any)
	if structured["error"].(map[string]any)["code"] != "invalid_utf8" {
		t.Fatalf("error=%v", structured)
	}
	retry := callTool(t, client, "terminal_read", map[string]any{"session_id": "board", "cursor": 0, "encoding": "hex", "timeout_ms": 1000})
	if retry.IsError || resultString(t, retry, "data") != "d30d78" || resultNumber(t, retry, "next") != 3 {
		t.Fatalf("lossless retry=%v", retry)
	}
}
