package command

import (
	"bytes"
	"context"
	"encoding/json"
	"io"
	"os"
	"strings"
	"testing"

	initmcp "github.com/akira-init1/ChannelTerm/internal/init/mcp"
)

func TestDesktopDiscoveryWithNoInstalledClientsCreatesNothing(t *testing.T) {
	root := t.TempDir()
	t.Setenv("PATH", root)
	locations := initmcp.RepairLocations{Options: initmcp.Options{Home: root, ConfigDir: root, WorkDir: root}}
	for _, id := range []string{"codex", "claude", "opencode", "zoo"} {
		client := inspectDesktopInitClient(context.Background(), locations, id)
		if client.Detected || client.Version != "" || client.Official != "unavailable" {
			t.Fatalf("absent client: %+v", client)
		}
	}
	entries, err := os.ReadDir(root)
	if err != nil || len(entries) != 0 {
		t.Fatalf("discovery modified empty profile: %v, %v", entries, err)
	}
}

func TestDesktopInitRejectsMalformedAndUnboundedRequests(t *testing.T) {
	for _, data := range []string{`{`, `{"action":"apply","arbitrary_command":"secret"}`, strings.Repeat(" ", 16385), `{"action":"preview","target":{"client":"codex","scope":"user"},"endpoint":"https://example.test"}`} {
		var output bytes.Buffer
		if err := runDesktopInit(context.Background(), nil, strings.NewReader(data), &output); err != nil {
			t.Fatal(err)
		}
		var response desktopInitReply
		if err := json.Unmarshal(output.Bytes(), &response); err != nil || response.OK {
			t.Fatalf("accepted malformed request: %s", output.String())
		}
		if strings.Contains(output.String(), "secret") {
			t.Fatal("request echoed in error")
		}
	}
}

func TestDesktopCapabilityHelpRemainsBoundedThroughIOCopy(t *testing.T) {
	var output boundedDesktopHelp
	if _, err := io.Copy(&output, strings.NewReader(strings.Repeat("a", 65536))); err == nil {
		t.Fatal("unbounded help accepted")
	}
	if len(output.String()) > 32768 {
		t.Fatal("help limit bypassed")
	}
}
