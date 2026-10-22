package mcp

import (
	"encoding/base64"
	"testing"
)

func TestDiagnosticToolsRoundTrip(t *testing.T) {
	manager, registry, _ := newTestRegistry(t)
	defer manager.Close()
	client, closeClient := connectTestClient(t, registry)
	defer closeClient()
	exported := callTool(t, client, "terminal_export_diagnostics", map[string]any{"session_id": "board", "include_raw": true})
	if exported.IsError {
		t.Fatal(resultText(t, exported))
	}
	assertStructuredResultMatchesOutputSchema(t, "terminal_export_diagnostics", exported)
	id := resultString(t, exported, "diagnostic_id")
	chunk := callTool(t, client, "terminal_read_diagnostic", map[string]any{"diagnostic_id": id, "offset": 0})
	if chunk.IsError {
		t.Fatal(resultText(t, chunk))
	}
	assertStructuredResultMatchesOutputSchema(t, "terminal_read_diagnostic", chunk)
	data, err := base64.StdEncoding.DecodeString(resultString(t, chunk, "data"))
	if err != nil || len(data) < 4 || string(data[:2]) != "PK" {
		t.Fatalf("not a ZIP: %v", err)
	}
}
