package mcp

import (
	"fmt"
	"path/filepath"
)

type zooCodeAdapter struct {
	options     options
	projectPath string
	globalPath  string
}

// newZooCodeAdapter prefers an existing project configuration and otherwise
// targets Zoo Code's standard VS Code global-storage configuration location.
func newZooCodeAdapter(options options) *zooCodeAdapter {
	return &zooCodeAdapter{
		options:     options,
		projectPath: filepath.Join(options.workDir, ".roo", "mcp.json"),
		globalPath:  filepath.Join(options.configDir, "Code", "User", "globalStorage", "zoo-code.zoo-code", "settings", "mcp_settings.json"),
	}
}

func (*zooCodeAdapter) ID() string { return "zoo" }

func (*zooCodeAdapter) Name() string { return "Zoo Code" }

func (a *zooCodeAdapter) Detect() (bool, error) {
	// The extension's storage directory is detection evidence even when it has
	// not created mcp_settings.json yet, allowing first-time configuration.
	for _, path := range []string{a.projectPath, a.globalPath, filepath.Dir(a.globalPath), filepath.Dir(filepath.Dir(a.globalPath))} {
		exists, err := fileExists(path)
		if err != nil {
			return false, err
		}
		if exists {
			return true, nil
		}
	}
	return false, nil
}

func (a *zooCodeAdapter) HasChannelTerm() (bool, error) {
	paths, err := a.configuredPaths()
	if err != nil {
		return false, err
	}
	for _, path := range paths {
		root, err := readJSON(path)
		if err != nil {
			return false, err
		}
		servers, err := zooCodeServers(root)
		if err != nil {
			return false, err
		}
		if _, exists := servers[serverName]; exists {
			return true, nil
		}
	}
	return false, nil
}

func (*zooCodeAdapter) Example(endpoint Endpoint) (string, error) {
	entry, err := zooCodeEntry(endpoint)
	if err != nil {
		return "", err
	}
	return jsonExample(map[string]any{"mcpServers": map[string]any{serverName: entry}})
}

func (a *zooCodeAdapter) Install(endpoint Endpoint) (InstallResult, error) {
	path, err := a.targetPath()
	if err != nil {
		return InstallResult{}, err
	}
	root, err := readJSON(path)
	if err != nil {
		return InstallResult{Path: path}, err
	}
	servers, err := zooCodeServers(root)
	if err != nil {
		return InstallResult{Path: path}, err
	}
	// Do not override a project-scoped ChannelTerm definition, which has higher
	// Zoo Code precedence than a global definition of the same name.
	if _, exists := servers[serverName]; exists {
		return InstallResult{Path: path, AlreadyPresent: true}, nil
	}
	entry, err := zooCodeEntry(endpoint)
	if err != nil {
		return InstallResult{Path: path}, err
	}
	servers[serverName] = entry
	if err := writeJSON(path, root); err != nil {
		return InstallResult{Path: path}, err
	}
	return InstallResult{Path: path}, nil
}

// configuredPaths returns only files that currently exist so HasChannelTerm
// can check project and global definitions without creating either one.
func (a *zooCodeAdapter) configuredPaths() ([]string, error) {
	paths := make([]string, 0, 2)
	for _, path := range []string{a.projectPath, a.globalPath} {
		exists, err := fileExists(path)
		if err != nil {
			return nil, err
		}
		if exists {
			paths = append(paths, path)
		}
	}
	return paths, nil
}

// targetPath keeps a detected project configuration project-scoped; only when
// it is absent does installation create or update the global configuration.
func (a *zooCodeAdapter) targetPath() (string, error) {
	projectExists, err := fileExists(a.projectPath)
	if err != nil {
		return "", err
	}
	if projectExists {
		return a.projectPath, nil
	}
	return a.globalPath, nil
}

func zooCodeServers(root map[string]any) (map[string]any, error) {
	return jsonObject(root, "mcpServers")
}

// zooCodeEntry emits Zoo Code's explicit streamable-http type while stdio can
// use its documented command-and-args form without a type field.
func zooCodeEntry(endpoint Endpoint) (map[string]any, error) {
	if err := endpoint.Validate(); err != nil {
		return nil, err
	}
	switch endpoint.Transport {
	case TransportStdio:
		return map[string]any{"command": endpoint.Command, "args": endpoint.Args}, nil
	case TransportStreamableHTTP:
		return map[string]any{"type": "streamable-http", "url": endpoint.URL}, nil
	default:
		return nil, fmt.Errorf("unsupported Zoo Code transport %q", endpoint.Transport)
	}
}
