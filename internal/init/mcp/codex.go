package mcp

import (
	"fmt"
	"path/filepath"

	"github.com/pelletier/go-toml/v2"
)

type codexAdapter struct {
	options options
	path    string
}

// newCodexAdapter selects Codex's user-level TOML location. Direct editing is
// used here so init can preserve unrelated configuration in one atomic update.
func newCodexAdapter(options options) *codexAdapter {
	return &codexAdapter{options: options, path: filepath.Join(options.home, ".codex", "config.toml")}
}

func (*codexAdapter) ID() string { return "codex" }

func (*codexAdapter) Name() string { return "Codex" }

func (a *codexAdapter) Detect() (bool, error) {
	exists, err := fileExists(a.path)
	return exists || executableExists(a.options.lookPath, "codex"), err
}

func (a *codexAdapter) HasChannelTerm() (bool, error) {
	root, err := readTOML(a.path)
	if err != nil {
		return false, err
	}
	servers, err := tomlObject(root, "mcp_servers")
	if err != nil {
		return false, err
	}
	_, exists := servers[serverName]
	return exists, nil
}

func (*codexAdapter) Example(endpoint Endpoint) (string, error) {
	entry, err := codexEntry(endpoint)
	if err != nil {
		return "", err
	}
	data, err := writeTOMLExample(map[string]any{"mcp_servers": map[string]any{serverName: entry}})
	if err != nil {
		return "", err
	}
	return string(data), nil
}

func (a *codexAdapter) Install(endpoint Endpoint) (InstallResult, error) {
	root, err := readTOML(a.path)
	if err != nil {
		return InstallResult{Path: a.path}, err
	}
	servers, err := tomlObject(root, "mcp_servers")
	if err != nil {
		return InstallResult{Path: a.path}, err
	}
	// A pre-existing entry may carry a deliberate command or transport choice.
	// Leave it intact so a repeat install is safe and idempotent.
	if _, exists := servers[serverName]; exists {
		return InstallResult{Path: a.path, AlreadyPresent: true}, nil
	}
	entry, err := codexEntry(endpoint)
	if err != nil {
		return InstallResult{Path: a.path}, err
	}
	servers[serverName] = entry
	if err := writeTOML(a.path, root); err != nil {
		return InstallResult{Path: a.path}, err
	}
	return InstallResult{Path: a.path}, nil
}

// codexEntry is the sole conversion from the shared Endpoint to Codex's TOML
// MCP-server fields; both Example and Install call it to prevent drift.
func codexEntry(endpoint Endpoint) (map[string]any, error) {
	if err := endpoint.Validate(); err != nil {
		return nil, err
	}
	switch endpoint.Transport {
	case TransportStdio:
		return map[string]any{"command": endpoint.Command, "args": endpoint.Args}, nil
	case TransportStreamableHTTP:
		return map[string]any{"url": endpoint.URL}, nil
	default:
		return nil, fmt.Errorf("unsupported Codex transport %q", endpoint.Transport)
	}
}

// writeTOMLExample uses the production TOML encoder so displayed syntax cannot
// diverge from the syntax an install writes.
func writeTOMLExample(root map[string]any) ([]byte, error) {
	data, err := toml.Marshal(root)
	if err != nil {
		return nil, fmt.Errorf("encode Codex example: %w", err)
	}
	return data, nil
}
