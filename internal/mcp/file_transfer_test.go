package mcp

import (
	"context"
	"testing"

	"github.com/akira-init1/ChannelTerm/internal/core/app"
	"github.com/akira-init1/ChannelTerm/internal/core/config"
	"github.com/akira-init1/ChannelTerm/internal/core/session"
	"github.com/akira-init1/ChannelTerm/internal/core/tool"
	"github.com/akira-init1/ChannelTerm/internal/mcp/terminal"
)

func TestFileTransferMCPTaskStatusSchemaAndExactRetry(t *testing.T) {
	manager := session.NewManager()
	defer manager.Close()
	a, err := app.New(app.Dependencies{Manager: manager, FileTransfer: config.FileTransfer{Root: t.TempDir(), SSH: map[string]config.FileTransferSSH{"fixture": {}}}})
	if err != nil {
		t.Fatal(err)
	}
	defer a.CloseFileTransfers(context.Background())
	registry := tool.NewRegistry()
	all, err := terminal.NewTools(a)
	if err != nil {
		t.Fatal(err)
	}
	for _, item := range all {
		if err := registry.Register(item); err != nil {
			t.Fatal(err)
		}
	}
	client, closeClient := connectTestClient(t, registry)
	defer closeClient()
	args := map[string]any{"action": "start", "request_id": "same", "direction": "send", "transport": "sftp", "ssh_profile": "fixture", "sources": []string{"missing"}, "destination": "/out"}
	first := callTool(t, client, "file_transfer", args)
	if first.IsError {
		t.Fatal(resultText(t, first))
	}
	assertStructuredResultMatchesOutputSchema(t, "file_transfer", first)
	status := first.StructuredContent.(map[string]any)["status"].(map[string]any)
	id := status["transfer_id"].(string)
	again := callTool(t, client, "file_transfer", args)
	if again.IsError || again.StructuredContent.(map[string]any)["status"].(map[string]any)["transfer_id"] != id {
		t.Fatal("retry did not return same task")
	}
	final := callTool(t, client, "file_transfer", map[string]any{"action": "status", "transfer_id": id, "wait_ms": 1000})
	if final.IsError {
		t.Fatal(resultText(t, final))
	}
	assertStructuredResultMatchesOutputSchema(t, "file_transfer", final)
	status = final.StructuredContent.(map[string]any)["status"].(map[string]any)
	if status["state"] != "failed" || status["error"] == "" {
		t.Fatalf("missing-source result=%+v", status)
	}
}
