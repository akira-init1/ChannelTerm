package mcp

import (
	"context"
	"strings"
	"testing"

	"github.com/akira-init1/ChannelTerm/internal/core/session"
	protocol "github.com/modelcontextprotocol/go-sdk/mcp"
)

func TestUBootInvalidInputErrorClassification(t *testing.T) {
	manager, registry, _ := newTestRegistry(t)
	defer manager.Close()
	client, closeClient := connectTestClient(t, registry)
	defer closeClient()
	failed, err := client.CallTool(context.Background(), &protocol.CallToolParams{Name: "terminal_enter_uboot", Arguments: map[string]any{"session_id": "board", "mode": "wait_reset", "countdown_pattern": "Hit any key", "prompt_pattern": "Zynq> ", "boot_started_patterns": []string{"Starting kernel"}, "interrupt_data_hex": "xx"}})
	if err != nil || !failed.IsError {
		t.Fatalf("result=%v error=%v", failed, err)
	}
	structured, ok := failed.StructuredContent.(map[string]any)
	if !ok {
		t.Fatalf("missing structured failure: %#v", failed)
	}
	failure := structured["error"].(map[string]any)
	if failure["code"] != "invalid_input" || !strings.Contains(resultText(t, failed), "terminal_enter_uboot failed:") {
		t.Fatalf("failure=%v", failed)
	}
}

func TestUBootTimeoutReturnsPartialEvidenceOverMCP(t *testing.T) {
	manager, registry, _ := newTestRegistry(t)
	defer manager.Close()
	terminal, _ := manager.Remove("board")
	if err := manager.RegisterWithMetadata(terminal, session.SessionMetadata{Transport: "serial"}); err != nil {
		t.Fatal(err)
	}
	client, closeClient := connectTestClient(t, registry)
	defer closeClient()
	r := callTool(t, client, "terminal_enter_uboot", map[string]any{"session_id": "board", "mode": "wait_reset", "countdown_pattern": "Hit any key", "prompt_pattern": "Zynq> ", "boot_started_patterns": []string{"Starting kernel"}, "interrupt_data_hex": "20", "timeout_ms": 20, "diagnostics": "capture"})
	if !r.IsError {
		t.Fatal("timeout was reported as success")
	}
	structured := r.StructuredContent.(map[string]any)
	if structured["error"].(map[string]any)["code"] != "timeout" {
		t.Fatalf("error=%v", structured)
	}
	partial := structured["result"].(map[string]any)
	if partial["status"] != "timeout" || partial["diagnostic_id"] == "" {
		t.Fatalf("lost evidence: %v", partial)
	}
}
