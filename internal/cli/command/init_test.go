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
		&fakeInitAdapter{id: "codex", name: "Codex", example: "[mcp_servers.channelterm]\n", httpExample: "codex-http\n"},
		&fakeInitAdapter{id: "claude", name: "Claude Code", example: "{\"mcpServers\":{}}\n", httpExample: "claude-http\n"},
		&fakeInitAdapter{id: "opencode", name: "OpenCode", example: "{\"mcp\":{}}\n", httpExample: "opencode-http\n"},
		&fakeInitAdapter{id: "zoo", name: "Zoo Code", example: "{\"mcpServers\":{}}\n", httpExample: "zoo-http\n"},
	}
	var output bytes.Buffer
	if err := runInitWithAdapters([]string{"--mcp-show"}, strings.NewReader("\n"), &output, adapters, func() (string, error) { return "test-token", nil }); err != nil {
		t.Fatalf("runInitWithAdapters(--mcp-show) error = %v", err)
	}
	for _, heading := range []string{"Shared HTTP configurations", "Do not share or commit"} {
		if !strings.Contains(output.String(), heading) {
			t.Errorf("all examples = %q, want heading %q", output.String(), heading)
		}
	}
	if strings.Contains(output.String(), "Local stdio configurations") {
		t.Errorf("all examples = %q, must not repeat stdio configurations", output.String())
	}
	for _, name := range []string{"=== Codex ===", "=== Claude Code ===", "=== OpenCode ===", "=== Zoo Code ==="} {
		if strings.Count(output.String(), name) != 1 {
			t.Errorf("all examples = %q, want one %s HTTP section", output.String(), name)
		}
	}
	for _, adapter := range adapters {
		fake := adapter.(*fakeInitAdapter)
		if len(fake.examples) != 1 || fake.examples[0].Transport != initmcp.TransportStreamableHTTP {
			t.Errorf("%s example endpoints = %#v, want only HTTP", fake.name, fake.examples)
			continue
		}
		if fake.examples[0].URL != "http://127.0.0.1:37099/mcp" || fake.examples[0].Headers["Authorization"] != "Bearer test-token" {
			t.Errorf("%s HTTP endpoint = %#v, want authenticated default endpoint", fake.name, fake.examples[0])
		}
	}

	output.Reset()
	if err := runInitWithAdapters([]string{"--mcp-show", "codex"}, strings.NewReader("http\n"), &output, adapters, func() (string, error) { return "test-token", nil }); err != nil {
		t.Fatalf("runInitWithAdapters(--mcp-show codex) error = %v", err)
	}
	if strings.Count(output.String(), "=== Codex ===") != 1 {
		t.Errorf("Codex example = %q, want one HTTP Codex section", output.String())
	}
	for _, omitted := range []string{"=== Claude Code ===", "=== OpenCode ===", "=== Zoo Code ==="} {
		if strings.Contains(output.String(), omitted) {
			t.Errorf("Codex example = %q, must not contain %s", output.String(), omitted)
		}
	}
}

func TestRunInitShowStdioDoesNotLoadHTTPToken(t *testing.T) {
	adapter := &fakeInitAdapter{id: "codex", name: "Codex", example: "stdio\n", httpExample: "http\n"}
	var output bytes.Buffer
	if err := runInitWithAdapters([]string{"--mcp-show"}, strings.NewReader("2\n"), &output, []initmcp.Adapter{adapter}, func() (string, error) {
		t.Fatal("token loader called for stdio display")
		return "", nil
	}); err != nil {
		t.Fatalf("runInitWithAdapters(--mcp-show) error = %v", err)
	}
	if !strings.Contains(output.String(), "Local stdio configurations") || strings.Contains(output.String(), "Shared HTTP configurations") {
		t.Errorf("stdio output = %q, want only local stdio heading", output.String())
	}
	if len(adapter.examples) != 1 || adapter.examples[0].Transport != initmcp.TransportStdio {
		t.Errorf("example endpoints = %#v, want only stdio", adapter.examples)
	}
}

func TestRunInitInstallsOnlyDetectedClients(t *testing.T) {
	codex := &fakeInitAdapter{id: "codex", name: "Codex", detected: true, install: initmcp.InstallResult{Path: "codex.toml"}}
	claude := &fakeInitAdapter{id: "claude", name: "Claude Code", detected: true, install: initmcp.InstallResult{Path: "claude.json", AlreadyPresent: true}}
	opencode := &fakeInitAdapter{id: "opencode", name: "OpenCode"}
	var output bytes.Buffer
	if err := runInitWithAdapters([]string{"--mcp"}, strings.NewReader("stdio\n"), &output, []initmcp.Adapter{codex, claude, opencode}, func() (string, error) {
		t.Fatal("token loader called while installing stdio configuration")
		return "", nil
	}); err != nil {
		t.Fatalf("runInitWithAdapters(--mcp) error = %v", err)
	}
	if codex.installs != 1 || claude.installs != 1 || opencode.installs != 0 {
		t.Errorf("install calls = %d/%d/%d, want 1/1/0", codex.installs, claude.installs, opencode.installs)
	}
	if codex.installedEndpoint.Transport != initmcp.TransportStdio || claude.installedEndpoint.Transport != initmcp.TransportStdio {
		t.Errorf("installed endpoints = %#v/%#v, want stdio", codex.installedEndpoint, claude.installedEndpoint)
	}
	if !strings.Contains(output.String(), "Codex: installed") || !strings.Contains(output.String(), "Claude Code: ChannelTerm MCP configuration already exists") {
		t.Errorf("install output = %q, want installed and existing messages", output.String())
	}
}

func TestRunInitInstallDefaultsToAuthenticatedHTTP(t *testing.T) {
	adapter := &fakeInitAdapter{id: "codex", name: "Codex", detected: true, install: initmcp.InstallResult{Path: "codex.toml"}}
	var output bytes.Buffer
	if err := runInitWithAdapters([]string{"--mcp"}, strings.NewReader("\n"), &output, []initmcp.Adapter{adapter}, func() (string, error) {
		return "test-token", nil
	}); err != nil {
		t.Fatalf("runInitWithAdapters(--mcp) error = %v", err)
	}
	if adapter.installedEndpoint.Transport != initmcp.TransportStreamableHTTP || adapter.installedEndpoint.URL != "http://127.0.0.1:37099/mcp" {
		t.Errorf("installed endpoint = %#v, want default HTTP endpoint", adapter.installedEndpoint)
	}
	if got := adapter.installedEndpoint.Headers["Authorization"]; got != "Bearer test-token" {
		t.Errorf("Authorization = %q, want bearer token", got)
	}
}

func TestRunInitRejectsInvalidSelection(t *testing.T) {
	adapters := []initmcp.Adapter{&fakeInitAdapter{id: "codex", name: "Codex"}}
	for _, args := range [][]string{{}, {"--mcp", "--mcp-show"}, {"--mcp-show", "unknown"}, {"--mcp"}} {
		err := runInitWithAdapters(args, strings.NewReader("\n"), &bytes.Buffer{}, adapters, func() (string, error) { return "test-token", nil })
		if err == nil {
			t.Errorf("runInitWithAdapters(%q) succeeded, want error", args)
		}
	}
}

func TestRunInitTransportHelp(t *testing.T) {
	for _, args := range [][]string{{"--mcp-help"}, {"--mcp", "--help"}, {"--mcp", "-h"}} {
		var output bytes.Buffer
		if err := runInitWithAdapters(args, strings.NewReader("invalid\n"), &output, nil, func() (string, error) {
			t.Fatal("token loader called while displaying help")
			return "", nil
		}); err != nil {
			t.Fatalf("runInitWithAdapters(%q) error = %v", args, err)
		}
		for _, want := range []string{
			"HTTP (default)",
			"shared ChannelTerm Host",
			"stdio",
			"local channelterm mcp child process",
			mcpTransportDocsURL,
		} {
			if !strings.Contains(output.String(), want) {
				t.Errorf("help output = %q, want %q", output.String(), want)
			}
		}
		if strings.Contains(output.String(), "Select MCP transport:") {
			t.Errorf("help output = %q, must not open selection prompt", output.String())
		}
	}
}

func TestPromptMCPTransport(t *testing.T) {
	for _, test := range []struct {
		name  string
		input string
		want  initmcp.Transport
	}{
		{name: "enter defaults HTTP", input: "\n", want: initmcp.TransportStreamableHTTP},
		{name: "closed input defaults HTTP", want: initmcp.TransportStreamableHTTP},
		{name: "HTTP number", input: "1\n", want: initmcp.TransportStreamableHTTP},
		{name: "HTTP name", input: "HTTP\n", want: initmcp.TransportStreamableHTTP},
		{name: "stdio number", input: "2\n", want: initmcp.TransportStdio},
		{name: "stdio name", input: "stdio\n", want: initmcp.TransportStdio},
	} {
		t.Run(test.name, func(t *testing.T) {
			got, err := promptMCPTransport(strings.NewReader(test.input), &bytes.Buffer{})
			if err != nil {
				t.Fatalf("promptMCPTransport() error = %v", err)
			}
			if got != test.want {
				t.Errorf("promptMCPTransport() = %q, want %q", got, test.want)
			}
		})
	}
	if _, err := promptMCPTransport(strings.NewReader("invalid\n"), &bytes.Buffer{}); err == nil {
		t.Fatal("promptMCPTransport(invalid) succeeded, want error")
	}
}

func TestRunInitShowReportsTokenLoadFailureBeforeWritingExamples(t *testing.T) {
	var output bytes.Buffer
	err := runInitWithAdapters(
		[]string{"--mcp-show"},
		strings.NewReader("\n"),
		&output,
		[]initmcp.Adapter{&fakeInitAdapter{id: "codex", name: "Codex", example: "stdio\n", httpExample: "http\n"}},
		func() (string, error) { return "", errors.New("credential unavailable") },
	)
	if err == nil || !strings.Contains(err.Error(), "load shared HTTP authentication token") {
		t.Fatalf("runInitWithAdapters() error = %v, want token diagnostic", err)
	}
	if !strings.Contains(output.String(), "Select MCP transport:") || strings.Contains(output.String(), "Shared HTTP configurations") {
		t.Errorf("output = %q, want prompt but no configuration after token load failure", output.String())
	}
}

type fakeInitAdapter struct {
	id                string
	name              string
	detected          bool
	example           string
	httpExample       string
	install           initmcp.InstallResult
	err               error
	installs          int
	examples          []initmcp.Endpoint
	installedEndpoint initmcp.Endpoint
}

func (a *fakeInitAdapter) ID() string { return a.id }

func (a *fakeInitAdapter) Name() string { return a.name }

func (a *fakeInitAdapter) Detect() (bool, error) { return a.detected, a.err }

func (a *fakeInitAdapter) HasChannelTerm() (bool, error) { return a.install.AlreadyPresent, a.err }

func (a *fakeInitAdapter) Example(endpoint initmcp.Endpoint) (string, error) {
	a.examples = append(a.examples, endpoint)
	if endpoint.Transport == initmcp.TransportStreamableHTTP {
		return a.httpExample, a.err
	}
	return a.example, a.err
}

func (a *fakeInitAdapter) Install(endpoint initmcp.Endpoint) (initmcp.InstallResult, error) {
	a.installs++
	a.installedEndpoint = endpoint
	if a.err != nil {
		return initmcp.InstallResult{}, errors.New("install failed")
	}
	return a.install, nil
}
