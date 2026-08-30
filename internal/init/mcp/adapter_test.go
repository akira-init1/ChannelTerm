package mcp

import (
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestAdaptersRenderClientSpecificExamplesFromOneEndpointModel(t *testing.T) {
	adapters := testAdapters(t)
	for _, adapter := range adapters {
		t.Run(adapter.ID(), func(t *testing.T) {
			stdio, err := adapter.Example(DefaultEndpoint())
			if err != nil {
				t.Fatalf("Example(stdio) error = %v", err)
			}
			if !strings.Contains(stdio, "channelterm") || !strings.Contains(stdio, "mcp") {
				t.Errorf("stdio example = %q, want ChannelTerm command", stdio)
			}
			http, err := adapter.Example(Endpoint{Transport: TransportStreamableHTTP, URL: "http://127.0.0.1:37099/mcp"})
			if err != nil {
				t.Fatalf("Example(Streamable HTTP) error = %v", err)
			}
			if !strings.Contains(http, "http://127.0.0.1:37099/mcp") {
				t.Errorf("Streamable HTTP example = %q, want endpoint URL", http)
			}
		})
	}
}

func TestAdaptersInstallPreservesExistingConfigurationAndIsIdempotent(t *testing.T) {
	for _, adapter := range testAdapters(t) {
		t.Run(adapter.ID(), func(t *testing.T) {
			path := adapterPath(t, adapter)
			writeAdapterFixture(t, adapter, path)

			first, err := adapter.Install(DefaultEndpoint())
			if err != nil {
				t.Fatalf("Install() error = %v", err)
			}
			if first.AlreadyPresent {
				t.Fatal("first Install() reported existing ChannelTerm configuration")
			}
			assertAdapterConfiguration(t, adapter, path)

			second, err := adapter.Install(DefaultEndpoint())
			if err != nil {
				t.Fatalf("second Install() error = %v", err)
			}
			if !second.AlreadyPresent {
				t.Fatal("second Install() did not report an existing ChannelTerm configuration")
			}
			exists, err := adapter.HasChannelTerm()
			if err != nil {
				t.Fatalf("HasChannelTerm() error = %v", err)
			}
			if !exists {
				t.Fatal("HasChannelTerm() = false after Install()")
			}
		})
	}
}

func TestAdaptersLeaveInvalidConfigurationUnchanged(t *testing.T) {
	for _, adapter := range testAdapters(t) {
		t.Run(adapter.ID(), func(t *testing.T) {
			path := adapterPath(t, adapter)
			invalid := []byte("{ invalid JSON")
			if adapter.ID() == "codex" {
				invalid = []byte("[mcp_servers\n")
			}
			if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
				t.Fatalf("MkdirAll() error = %v", err)
			}
			if err := os.WriteFile(path, invalid, 0o600); err != nil {
				t.Fatalf("WriteFile() error = %v", err)
			}

			if _, err := adapter.Install(DefaultEndpoint()); err == nil {
				t.Fatal("Install() succeeded for invalid configuration")
			}
			after, err := os.ReadFile(path)
			if err != nil {
				t.Fatalf("ReadFile() error = %v", err)
			}
			if string(after) != string(invalid) {
				t.Errorf("configuration after invalid install = %q, want unchanged %q", after, invalid)
			}
		})
	}
}

func TestAdaptersDetectExistingConfigurationOrInstalledClient(t *testing.T) {
	base := t.TempDir()
	adapters, err := NewAdapters(Options{
		Home:      filepath.Join(base, "home"),
		ConfigDir: filepath.Join(base, "config"),
		WorkDir:   filepath.Join(base, "work"),
		LookPath: func(name string) (string, error) {
			if name == "claude" {
				return name, nil
			}
			return "", errors.New("not installed")
		},
	})
	if err != nil {
		t.Fatalf("NewAdapters() error = %v", err)
	}
	for _, adapter := range adapters {
		available, err := adapter.Detect()
		if err != nil {
			t.Fatalf("%s Detect() error = %v", adapter.ID(), err)
		}
		if adapter.ID() == "claude" && !available {
			t.Error("Claude Code was not detected from executable lookup")
		}
		if adapter.ID() != "claude" && available {
			t.Errorf("%s Detect() = true without configuration or executable", adapter.ID())
		}
	}
}

func TestZooCodeDetectsInstalledExtensionStorage(t *testing.T) {
	adapters := testAdapters(t)
	var zoo *zooCodeAdapter
	for _, adapter := range adapters {
		if typed, ok := adapter.(*zooCodeAdapter); ok {
			zoo = typed
			break
		}
	}
	if zoo == nil {
		t.Fatal("Zoo Code adapter is missing")
	}
	if err := os.MkdirAll(filepath.Dir(filepath.Dir(zoo.globalPath)), 0o700); err != nil {
		t.Fatalf("MkdirAll() error = %v", err)
	}
	detected, err := zoo.Detect()
	if err != nil {
		t.Fatalf("Detect() error = %v", err)
	}
	if !detected {
		t.Fatal("Detect() = false with Zoo Code extension storage")
	}
}

func testAdapters(t *testing.T) []Adapter {
	t.Helper()
	base := t.TempDir()
	adapters, err := NewAdapters(Options{
		Home:      filepath.Join(base, "home"),
		ConfigDir: filepath.Join(base, "config"),
		WorkDir:   filepath.Join(base, "work"),
		LookPath:  func(string) (string, error) { return "", errors.New("not installed") },
	})
	if err != nil {
		t.Fatalf("NewAdapters() error = %v", err)
	}
	return adapters
}

func adapterPath(t *testing.T, adapter Adapter) string {
	t.Helper()
	switch typed := adapter.(type) {
	case *codexAdapter:
		return typed.path
	case *claudeAdapter:
		return typed.path
	case *openCodeAdapter:
		return typed.path
	case *zooCodeAdapter:
		return typed.projectPath
	default:
		t.Fatalf("unexpected adapter type %T", adapter)
		return ""
	}
}

func writeAdapterFixture(t *testing.T, adapter Adapter, path string) {
	t.Helper()
	if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
		t.Fatalf("MkdirAll() error = %v", err)
	}
	var fixture string
	switch adapter.ID() {
	case "codex":
		fixture = "model = \"test\"\n[unknown]\nkeep = true\n[mcp_servers.existing]\ncommand = \"other\"\ncustom = \"preserve\"\n"
	case "claude":
		claude, ok := adapter.(*claudeAdapter)
		if !ok {
			t.Fatalf("adapter %T is not Claude Code", adapter)
		}
		fixture = `{"unknown":{"keep":true},"projects":{"` + strings.ReplaceAll(claude.options.workDir, `\`, `\\`) + `":{"mcpServers":{"existing":{"command":"other","custom":true}}}}}`
	case "opencode":
		fixture = `{"unknown":{"keep":true},"mcp":{"timeout":{"startup":1000},"servers":{"existing":{"type":"local","command":["other"],"custom":true}}}}`
	case "zoo":
		fixture = `{"unknown":{"keep":true},"mcpServers":{"existing":{"command":"other","custom":true}}}`
	default:
		t.Fatalf("unexpected adapter %q", adapter.ID())
	}
	if err := os.WriteFile(path, []byte(fixture), 0o600); err != nil {
		t.Fatalf("WriteFile() error = %v", err)
	}
}

func assertAdapterConfiguration(t *testing.T, adapter Adapter, path string) {
	t.Helper()
	if adapter.ID() == "codex" {
		root, err := readTOML(path)
		if err != nil {
			t.Fatalf("readTOML() error = %v", err)
		}
		if unknown, ok := root["unknown"].(map[string]any); !ok || unknown["keep"] != true {
			t.Errorf("unknown TOML configuration = %#v, want preserved table", root["unknown"])
		}
		servers, err := tomlObject(root, "mcp_servers")
		if err != nil {
			t.Fatalf("tomlObject() error = %v", err)
		}
		assertServers(t, servers, false)
		return
	}
	root, err := readJSON(path)
	if err != nil {
		t.Fatalf("readJSON() error = %v", err)
	}
	if unknown, ok := root["unknown"].(map[string]any); !ok || unknown["keep"] != true {
		t.Errorf("unknown JSON configuration = %#v, want preserved object", root["unknown"])
	}
	var servers map[string]any
	switch adapter.ID() {
	case "claude":
		projects, err := jsonObject(root, "projects")
		if err != nil {
			t.Fatal(err)
		}
		for _, value := range projects {
			project, ok := value.(map[string]any)
			if ok {
				servers, _ = jsonObject(project, "mcpServers")
				break
			}
		}
	case "opencode":
		servers, err = openCodeServers(root)
	case "zoo":
		servers, err = zooCodeServers(root)
	}
	if err != nil {
		t.Fatal(err)
	}
	assertServers(t, servers, adapter.ID() == "opencode")
}

func assertServers(t *testing.T, servers map[string]any, openCode bool) {
	t.Helper()
	existing, ok := servers["existing"].(map[string]any)
	if !ok || existing["custom"] != true && existing["custom"] != "preserve" {
		t.Errorf("existing server = %#v, want preserved custom fields", servers["existing"])
	}
	channelTerm, ok := servers[serverName].(map[string]any)
	if !ok {
		t.Fatalf("ChannelTerm server = %#v, want configuration object", servers[serverName])
	}
	if openCode {
		command, ok := channelTerm["command"].([]any)
		if !ok || len(command) != 2 || command[0] != "channelterm" || command[1] != "mcp" {
			t.Errorf("OpenCode command = %#v, want [channelterm mcp]", channelTerm["command"])
		}
		return
	}
	if channelTerm["command"] != "channelterm" {
		t.Errorf("ChannelTerm command = %#v, want channelterm", channelTerm["command"])
	}
}
