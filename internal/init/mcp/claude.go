package mcp

import (
	"encoding/json"
	"fmt"
	"path/filepath"
)

type claudeAdapter struct {
	options options
	path    string
}

// newClaudeAdapter targets Claude Code's user configuration, which stores
// local-scope MCP entries below the absolute working-directory project key.
func newClaudeAdapter(options options) *claudeAdapter {
	return &claudeAdapter{options: options, path: filepath.Join(options.home, ".claude.json")}
}

func (*claudeAdapter) ID() string { return "claude" }

func (*claudeAdapter) Name() string { return "Claude Code" }

func (a *claudeAdapter) Detect() (bool, error) {
	exists, err := fileExists(a.path)
	return exists || executableExists(a.options.lookPath, "claude"), err
}

func (a *claudeAdapter) HasChannelTerm() (bool, error) {
	servers, err := a.servers(readJSON)
	if err != nil {
		return false, err
	}
	_, exists := servers[serverName]
	return exists, nil
}

func (*claudeAdapter) Example(endpoint Endpoint) (string, error) {
	entry, err := claudeEntry(endpoint)
	if err != nil {
		return "", err
	}
	return jsonExample(map[string]any{"mcpServers": map[string]any{serverName: entry}})
}

func (a *claudeAdapter) Install(endpoint Endpoint) (InstallResult, error) {
	root, err := readJSON(a.path)
	if err != nil {
		return InstallResult{Path: a.path}, err
	}
	servers, err := a.serversFromRoot(root)
	if err != nil {
		return InstallResult{Path: a.path}, err
	}
	// Preserve an existing entry rather than silently replacing a user's local
	// scope configuration with ChannelTerm's default.
	if _, exists := servers[serverName]; exists {
		return InstallResult{Path: a.path, AlreadyPresent: true}, nil
	}
	entry, err := claudeEntry(endpoint)
	if err != nil {
		return InstallResult{Path: a.path}, err
	}
	servers[serverName] = entry
	if err := writeJSON(a.path, root); err != nil {
		return InstallResult{Path: a.path}, err
	}
	return InstallResult{Path: a.path}, nil
}

func (a *claudeAdapter) servers(read func(string) (map[string]any, error)) (map[string]any, error) {
	root, err := read(a.path)
	if err != nil {
		return nil, err
	}
	return a.serversFromRoot(root)
}

// serversFromRoot traverses only Claude Code's local-scope project branch and
// leaves every other project and top-level setting in the decoded document.
func (a *claudeAdapter) serversFromRoot(root map[string]any) (map[string]any, error) {
	projects, err := jsonObject(root, "projects")
	if err != nil {
		return nil, err
	}
	project, err := jsonObject(projects, a.options.workDir)
	if err != nil {
		return nil, err
	}
	return jsonObject(project, "mcpServers")
}

// claudeEntry is shared by installation and example rendering so their JSON
// transport representation stays identical.
func claudeEntry(endpoint Endpoint) (map[string]any, error) {
	if err := endpoint.Validate(); err != nil {
		return nil, err
	}
	switch endpoint.Transport {
	case TransportStdio:
		return map[string]any{"command": endpoint.Command, "args": endpoint.Args}, nil
	case TransportStreamableHTTP:
		return map[string]any{"type": "http", "url": endpoint.URL}, nil
	default:
		return nil, fmt.Errorf("unsupported Claude Code transport %q", endpoint.Transport)
	}
}

// jsonExample deliberately reuses JSON's production encoder rather than a
// separate handwritten template.
func jsonExample(root map[string]any) (string, error) {
	data, err := json.MarshalIndent(root, "", "  ")
	if err != nil {
		return "", err
	}
	return string(data) + "\n", nil
}
