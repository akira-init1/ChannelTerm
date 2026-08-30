// Package mcp installs ChannelTerm MCP endpoint configurations for supported
// client applications without coupling those client formats to the MCP host.
package mcp

import (
	"encoding/json"
	"errors"
	"fmt"
	"net/url"
	"os"
	"os/exec"
	"path/filepath"
	"strings"

	"github.com/pelletier/go-toml/v2"
)

// serverName is intentionally fixed: the same name lets an Adapter recognize
// a prior install without replacing a user-customized ChannelTerm endpoint.
const serverName = "channelterm"

// Transport identifies how an MCP client connects to the ChannelTerm server.
type Transport string

const (
	// TransportStdio starts ChannelTerm as a child process and exchanges MCP
	// messages over its standard input and output streams.
	TransportStdio Transport = "stdio"
	// TransportStreamableHTTP connects to a ChannelTerm Streamable HTTP endpoint.
	TransportStreamableHTTP Transport = "streamable-http"
)

// Endpoint is the client-neutral description of a ChannelTerm MCP server.
// Exactly the fields for its selected Transport must be populated.
type Endpoint struct {
	Transport Transport
	Command   string
	Args      []string
	URL       string
}

// DefaultEndpoint returns the local stdio configuration installed by init.
func DefaultEndpoint() Endpoint {
	return Endpoint{Transport: TransportStdio, Command: "channelterm", Args: []string{"mcp"}}
}

// Validate reports whether Endpoint has the information required by its
// selected transport before a client-specific Adapter renders it.
func (e Endpoint) Validate() error {
	switch e.Transport {
	case TransportStdio:
		if strings.TrimSpace(e.Command) == "" {
			return errors.New("MCP stdio command is required")
		}
	case TransportStreamableHTTP:
		parsed, err := url.ParseRequestURI(e.URL)
		if err != nil || parsed.Scheme == "" || parsed.Host == "" {
			return fmt.Errorf("MCP Streamable HTTP URL is invalid: %q", e.URL)
		}
	default:
		return fmt.Errorf("unsupported MCP endpoint transport %q", e.Transport)
	}
	return nil
}

// InstallResult describes the completed configuration operation.
type InstallResult struct {
	Path           string
	AlreadyPresent bool
}

// Adapter owns discovery, rendering, and safe installation for one MCP client.
type Adapter interface {
	ID() string
	Name() string
	Detect() (bool, error)
	HasChannelTerm() (bool, error)
	Example(Endpoint) (string, error)
	Install(Endpoint) (InstallResult, error)
}

// Options supplies locations and executable lookup to NewAdapters. Empty
// fields use the current user's standard locations.
type Options struct {
	Home      string
	ConfigDir string
	WorkDir   string
	LookPath  func(string) (string, error)
}

// NewAdapters constructs every first-version supported MCP client adapter.
func NewAdapters(options Options) ([]Adapter, error) {
	resolved, err := resolveOptions(options)
	if err != nil {
		return nil, err
	}
	return []Adapter{
		newCodexAdapter(resolved),
		newClaudeAdapter(resolved),
		newOpenCodeAdapter(resolved),
		newZooCodeAdapter(resolved),
	}, nil
}

type options struct {
	home      string
	configDir string
	workDir   string
	lookPath  func(string) (string, error)
}

// resolveOptions isolates process-specific locations at construction time so
// each Adapter can be tested without reading a real user's configuration.
func resolveOptions(provided Options) (options, error) {
	resolved := options{home: provided.Home, configDir: provided.ConfigDir, workDir: provided.WorkDir, lookPath: provided.LookPath}
	var err error
	if resolved.home == "" {
		if resolved.home, err = os.UserHomeDir(); err != nil {
			return options{}, fmt.Errorf("resolve home directory: %w", err)
		}
	}
	if resolved.configDir == "" {
		if resolved.configDir, err = os.UserConfigDir(); err != nil {
			return options{}, fmt.Errorf("resolve user configuration directory: %w", err)
		}
	}
	if resolved.workDir == "" {
		if resolved.workDir, err = os.Getwd(); err != nil {
			return options{}, fmt.Errorf("resolve working directory: %w", err)
		}
	}
	if !filepath.IsAbs(resolved.workDir) {
		if resolved.workDir, err = filepath.Abs(resolved.workDir); err != nil {
			return options{}, fmt.Errorf("resolve absolute working directory: %w", err)
		}
	}
	if resolved.lookPath == nil {
		resolved.lookPath = exec.LookPath
	}
	return resolved, nil
}

// fileExists distinguishes an absent optional configuration from permission
// failures, which must not be treated as evidence that installation is safe.
func fileExists(path string) (bool, error) {
	_, err := os.Stat(path)
	if err == nil {
		return true, nil
	}
	if errors.Is(err, os.ErrNotExist) {
		return false, nil
	}
	return false, err
}

// executableExists intentionally treats a lookup failure as non-detection;
// adapters still surface filesystem failures from their configuration paths.
func executableExists(lookPath func(string) (string, error), name string) bool {
	_, err := lookPath(name)
	return err == nil
}

// readJSON decodes dynamically rather than into a client schema so unknown
// fields survive an install round trip. Invalid roots are rejected before any
// directory or temporary file is created.
func readJSON(path string) (map[string]any, error) {
	data, err := os.ReadFile(path)
	if errors.Is(err, os.ErrNotExist) {
		return map[string]any{}, nil
	}
	if err != nil {
		return nil, err
	}
	var root any
	if err := json.Unmarshal(data, &root); err != nil {
		return nil, fmt.Errorf("parse JSON configuration: %w", err)
	}
	object, ok := root.(map[string]any)
	if !ok {
		return nil, errors.New("JSON configuration root must be an object")
	}
	return object, nil
}

// jsonObject creates only missing object nodes. A scalar or array at a known
// object location is a client configuration error and must remain untouched.
func jsonObject(parent map[string]any, key string) (map[string]any, error) {
	if value, ok := parent[key]; ok {
		object, ok := value.(map[string]any)
		if !ok {
			return nil, fmt.Errorf("JSON field %q must be an object", key)
		}
		return object, nil
	}
	object := map[string]any{}
	parent[key] = object
	return object, nil
}

// writeJSON appends a normal final newline before delegating durability and
// replacement guarantees to atomicWrite.
func writeJSON(path string, root map[string]any) error {
	data, err := json.MarshalIndent(root, "", "  ")
	if err != nil {
		return fmt.Errorf("encode JSON configuration: %w", err)
	}
	return atomicWrite(path, append(data, '\n'))
}

// readTOML uses a dynamic map for the same preservation reason as readJSON.
func readTOML(path string) (map[string]any, error) {
	data, err := os.ReadFile(path)
	if errors.Is(err, os.ErrNotExist) {
		return map[string]any{}, nil
	}
	if err != nil {
		return nil, err
	}
	root := map[string]any{}
	if err := toml.Unmarshal(data, &root); err != nil {
		return nil, fmt.Errorf("parse TOML configuration: %w", err)
	}
	return root, nil
}

// tomlObject follows the JSON object policy: only absent tables are created;
// an incompatible existing value prevents a destructive rewrite.
func tomlObject(parent map[string]any, key string) (map[string]any, error) {
	if value, ok := parent[key]; ok {
		object, ok := value.(map[string]any)
		if !ok {
			return nil, fmt.Errorf("TOML field %q must be a table", key)
		}
		return object, nil
	}
	object := map[string]any{}
	parent[key] = object
	return object, nil
}

// writeTOML serializes the complete preserved document before atomicWrite
// replaces the original file.
func writeTOML(path string, root map[string]any) error {
	data, err := toml.Marshal(root)
	if err != nil {
		return fmt.Errorf("encode TOML configuration: %w", err)
	}
	return atomicWrite(path, data)
}

// atomicWrite keeps a parseable original configuration in place until a
// complete, synchronized replacement is ready in the same directory. The
// deferred cleanup removes only a failed temporary file, never the original.
func atomicWrite(path string, data []byte) (err error) {
	directory := filepath.Dir(path)
	if err := os.MkdirAll(directory, 0o700); err != nil {
		return fmt.Errorf("create configuration directory: %w", err)
	}
	mode := os.FileMode(0o600)
	if info, statErr := os.Stat(path); statErr == nil {
		mode = info.Mode().Perm()
	} else if !errors.Is(statErr, os.ErrNotExist) {
		return statErr
	}
	temporary, err := os.CreateTemp(directory, ".channelterm-*.tmp")
	if err != nil {
		return fmt.Errorf("create temporary configuration: %w", err)
	}
	temporaryPath := temporary.Name()
	committed := false
	defer func() {
		if !committed {
			_ = temporary.Close()
			_ = os.Remove(temporaryPath)
		}
	}()
	if err := temporary.Chmod(mode); err != nil {
		return fmt.Errorf("set temporary configuration permissions: %w", err)
	}
	if _, err := temporary.Write(data); err != nil {
		return fmt.Errorf("write temporary configuration: %w", err)
	}
	if err := temporary.Sync(); err != nil {
		return fmt.Errorf("sync temporary configuration: %w", err)
	}
	if err := temporary.Close(); err != nil {
		return fmt.Errorf("close temporary configuration: %w", err)
	}
	if err := os.Rename(temporaryPath, path); err != nil {
		return fmt.Errorf("replace configuration atomically: %w", err)
	}
	committed = true
	return nil
}
