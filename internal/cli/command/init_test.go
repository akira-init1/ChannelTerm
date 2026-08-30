package command

import (
	"bytes"
	"errors"
	"strings"
	"testing"

	initmcp "github.com/akira-init1/ChannelTerm/internal/init/mcp"
)

func TestRunInitShowAllAndOneClient(t *testing.T) {
	adapters := []initmcp.Adapter{
		&fakeInitAdapter{id: "codex", name: "Codex", example: "[mcp_servers.channelterm]\n"},
		&fakeInitAdapter{id: "claude", name: "Claude Code", example: "{\"mcpServers\":{}}\n"},
		&fakeInitAdapter{id: "opencode", name: "OpenCode", example: "{\"mcp\":{}}\n"},
		&fakeInitAdapter{id: "zoo", name: "Zoo Code", example: "{\"mcpServers\":{}}\n"},
	}
	var output bytes.Buffer
	if err := runInitWithAdapters([]string{"--mcp-show"}, &output, adapters); err != nil {
		t.Fatalf("runInitWithAdapters(--mcp-show) error = %v", err)
	}
	for _, name := range []string{"Codex:", "Claude Code:", "OpenCode:", "Zoo Code:"} {
		if !strings.Contains(output.String(), name) {
			t.Errorf("all examples = %q, want %s", output.String(), name)
		}
	}

	output.Reset()
	if err := runInitWithAdapters([]string{"--mcp-show", "codex"}, &output, adapters); err != nil {
		t.Fatalf("runInitWithAdapters(--mcp-show codex) error = %v", err)
	}
	if !strings.Contains(output.String(), "Codex:") {
		t.Errorf("Codex example = %q, want Codex", output.String())
	}
	for _, omitted := range []string{"Claude Code:", "OpenCode:", "Zoo Code:"} {
		if strings.Contains(output.String(), omitted) {
			t.Errorf("Codex example = %q, must not contain %s", output.String(), omitted)
		}
	}
}

func TestRunInitInstallsOnlyDetectedClients(t *testing.T) {
	codex := &fakeInitAdapter{id: "codex", name: "Codex", detected: true, install: initmcp.InstallResult{Path: "codex.toml"}}
	claude := &fakeInitAdapter{id: "claude", name: "Claude Code", detected: true, install: initmcp.InstallResult{Path: "claude.json", AlreadyPresent: true}}
	opencode := &fakeInitAdapter{id: "opencode", name: "OpenCode"}
	var output bytes.Buffer
	if err := runInitWithAdapters([]string{"--mcp"}, &output, []initmcp.Adapter{codex, claude, opencode}); err != nil {
		t.Fatalf("runInitWithAdapters(--mcp) error = %v", err)
	}
	if codex.installs != 1 || claude.installs != 1 || opencode.installs != 0 {
		t.Errorf("install calls = %d/%d/%d, want 1/1/0", codex.installs, claude.installs, opencode.installs)
	}
	if !strings.Contains(output.String(), "Codex: installed") || !strings.Contains(output.String(), "Claude Code: ChannelTerm MCP configuration already exists") {
		t.Errorf("install output = %q, want installed and existing messages", output.String())
	}
}

func TestRunInitRejectsInvalidSelection(t *testing.T) {
	adapters := []initmcp.Adapter{&fakeInitAdapter{id: "codex", name: "Codex"}}
	for _, args := range [][]string{{}, {"--mcp", "--mcp-show"}, {"--mcp-show", "unknown"}, {"--mcp"}} {
		err := runInitWithAdapters(args, &bytes.Buffer{}, adapters)
		if err == nil {
			t.Errorf("runInitWithAdapters(%q) succeeded, want error", args)
		}
	}
}

type fakeInitAdapter struct {
	id       string
	name     string
	detected bool
	example  string
	install  initmcp.InstallResult
	err      error
	installs int
}

func (a *fakeInitAdapter) ID() string { return a.id }

func (a *fakeInitAdapter) Name() string { return a.name }

func (a *fakeInitAdapter) Detect() (bool, error) { return a.detected, a.err }

func (a *fakeInitAdapter) HasChannelTerm() (bool, error) { return a.install.AlreadyPresent, a.err }

func (a *fakeInitAdapter) Example(initmcp.Endpoint) (string, error) { return a.example, a.err }

func (a *fakeInitAdapter) Install(initmcp.Endpoint) (initmcp.InstallResult, error) {
	a.installs++
	if a.err != nil {
		return initmcp.InstallResult{}, errors.New("install failed")
	}
	return a.install, nil
}
