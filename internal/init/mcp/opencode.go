package mcp

import (
	"fmt"
	"path/filepath"
)

type openCodeAdapter struct {
	options options
	path    string
}

// newOpenCodeAdapter selects OpenCode's global JSON configuration. A detected
// executable can therefore be configured even before the file exists.
func newOpenCodeAdapter(options options) *openCodeAdapter {
	return &openCodeAdapter{options: options, path: filepath.Join(options.configDir, "opencode", "opencode.json")}
}

func (*openCodeAdapter) ID() string { return "opencode" }

func (*openCodeAdapter) Name() string { return "OpenCode" }

func (a *openCodeAdapter) Detect() (bool, error) {
	exists, err := fileExists(a.path)
	return exists || executableExists(a.options.lookPath, "opencode"), err
}

func (a *openCodeAdapter) HasChannelTerm() (bool, error) {
	root, err := readJSON(a.path)
	if err != nil {
		return false, err
	}
	servers, err := openCodeServers(root)
	if err != nil {
		return false, err
	}
	_, exists := servers[serverName]
	return exists, nil
}

func (*openCodeAdapter) Example(endpoint Endpoint) (string, error) {
	entry, err := openCodeEntry(endpoint)
	if err != nil {
		return "", err
	}
	return jsonExample(map[string]any{"mcp": map[string]any{"servers": map[string]any{serverName: entry}}})
}

func (a *openCodeAdapter) Install(endpoint Endpoint) (InstallResult, error) {
	root, err := readJSON(a.path)
	if err != nil {
		return InstallResult{Path: a.path}, err
	}
	servers, err := openCodeServers(root)
	if err != nil {
		return InstallResult{Path: a.path}, err
	}
	// Existing ChannelTerm configuration wins over the default to avoid
	// overwriting a user-selected endpoint, timeout, or disabled state.
	if _, exists := servers[serverName]; exists {
		return InstallResult{Path: a.path, AlreadyPresent: true}, nil
	}
	entry, err := openCodeEntry(endpoint)
	if err != nil {
		return InstallResult{Path: a.path}, err
	}
	servers[serverName] = entry
	if err := writeJSON(a.path, root); err != nil {
		return InstallResult{Path: a.path}, err
	}
	return InstallResult{Path: a.path}, nil
}

// openCodeServers validates OpenCode's nested MCP container before mutating
// it, preserving an incompatible existing value for the user to repair.
func openCodeServers(root map[string]any) (map[string]any, error) {
	mcp, err := jsonObject(root, "mcp")
	if err != nil {
		return nil, err
	}
	return jsonObject(mcp, "servers")
}

// openCodeEntry converts Endpoint's split command and arguments into
// OpenCode's single command array for local servers.
func openCodeEntry(endpoint Endpoint) (map[string]any, error) {
	if err := endpoint.Validate(); err != nil {
		return nil, err
	}
	switch endpoint.Transport {
	case TransportStdio:
		return map[string]any{"type": "local", "command": append([]string{endpoint.Command}, endpoint.Args...)}, nil
	case TransportStreamableHTTP:
		return map[string]any{"type": "remote", "url": endpoint.URL}, nil
	default:
		return nil, fmt.Errorf("unsupported OpenCode transport %q", endpoint.Transport)
	}
}
