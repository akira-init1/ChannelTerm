package mcp

import (
	"bytes"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net"
	"net/url"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"unicode/utf8"

	"github.com/pelletier/go-toml/v2"
)

const repairConfigLimit = 1024 * 1024

// RepairTarget selects one explicit configuration scope. Unlike legacy Install,
// it never chooses a project based on the process's current working directory.
type RepairTarget struct {
	Client  string `json:"client"`
	Scope   string `json:"scope"`
	Project string `json:"project,omitempty"`
	// OpenCodeV1 is selected only after checking the installed major version.
	OpenCodeV1 bool `json:"opencode_v1,omitempty"`
}

// RepairLocations supplies trusted process-level overrides used by client CLIs.
// These are resolved by local composition, never taken from renderer requests.
type RepairLocations struct {
	Options
	CodexDirectory    string
	OpenCodeDirectory string
	ClaudeFile        string
}

// RepairPlan is a read-only snapshot of one target file. Before and After contain
// only the selected entry with all credential and arbitrary string values redacted.
// Path is for a trusted native confirmation, never an untrusted renderer response.
// Serializing a plan cannot expose its private document or credentials.
type RepairPlan struct {
	Path        string `json:"path"`
	Fingerprint string `json:"fingerprint"`
	Existing    bool   `json:"existing"`
	Before      string `json:"before"`
	After       string `json:"after"`
	root        map[string]any
	servers     map[string]any
	entry       map[string]any
	original    []byte
	exists      bool
	toml        bool
}

// PreviewRepair reads and validates a bounded ordinary file without creating any
// directories or registrations. It reuses the existing adapters' entry encoders.
// Applying a plan reserializes its document, preserving unrelated settings by
// value; formatting and TOML comments are not retained and must be disclosed.
func PreviewRepair(locations RepairLocations, target RepairTarget, endpoint Endpoint) (*RepairPlan, error) {
	resolved, err := resolveOptions(locations.Options)
	if err != nil {
		return nil, err
	}
	if target.Scope != "user" && target.Scope != "project" {
		return nil, errors.New("unsupported configuration scope")
	}
	if target.Scope == "project" && (!filepath.IsAbs(target.Project) || target.Project == "") {
		return nil, errors.New("project scope requires an explicit absolute directory")
	}
	if target.Scope == "user" && target.Project != "" {
		return nil, errors.New("user scope does not accept a project directory")
	}
	plan := &RepairPlan{}
	var encode func(Endpoint) (map[string]any, error)
	var keys []string
	switch target.Client {
	case "codex":
		if target.Scope != "user" {
			return nil, errors.New("Codex repair supports user scope only")
		}
		plan.Path, plan.toml = filepath.Join(resolved.home, ".codex", "config.toml"), true
		if locations.CodexDirectory != "" {
			plan.Path = filepath.Join(locations.CodexDirectory, "config.toml")
		}
		encode, keys = codexEntry, []string{"mcp_servers"}
	case "claude":
		plan.Path = filepath.Join(resolved.home, ".claude.json")
		if locations.ClaudeFile != "" {
			plan.Path = locations.ClaudeFile
		}
		if target.Scope == "project" {
			plan.Path = filepath.Join(target.Project, ".mcp.json")
		}
		encode, keys = claudeEntry, []string{"mcpServers"}
	case "opencode":
		plan.Path = filepath.Join(resolved.configDir, "opencode", "opencode.json")
		if locations.OpenCodeDirectory != "" {
			plan.Path = filepath.Join(locations.OpenCodeDirectory, "opencode.json")
		}
		if target.Scope == "project" {
			plan.Path = filepath.Join(target.Project, "opencode.json")
		}
		// JSONC takes precedence in supported clients. Never silently edit a shadowed file.
		if exists, err := fileExists(plan.Path + "c"); err != nil || exists {
			return nil, errors.New("an OpenCode JSONC configuration needs manual review")
		}
		encode, keys = openCodeEntry, []string{"mcp", "servers"}
		if target.OpenCodeV1 {
			keys = []string{"mcp"}
		}
	case "zoo":
		plan.Path = filepath.Join(resolved.configDir, "Code", "User", "globalStorage", "zoo-code.zoo-code", "settings", "mcp_settings.json")
		if target.Scope == "project" {
			plan.Path = filepath.Join(target.Project, ".roo", "mcp.json")
		}
		encode, keys = zooCodeEntry, []string{"mcpServers"}
	default:
		return nil, errors.New("unsupported MCP client")
	}
	if !filepath.IsAbs(plan.Path) {
		return nil, errors.New("client configuration location must be absolute")
	}
	if target.OpenCodeV1 && target.Client != "opencode" {
		return nil, errors.New("OpenCode version applies only to OpenCode")
	}
	plan.original, plan.exists, err = readRepairFile(plan.Path)
	if err != nil {
		return nil, err
	}
	plan.Fingerprint = repairFingerprint(plan.original, plan.exists)
	plan.root = map[string]any{}
	if plan.exists {
		if plan.toml {
			err = toml.Unmarshal(plan.original, &plan.root)
		} else {
			if err := uniqueJSON(plan.original); err != nil {
				return nil, err
			}
			decoder := json.NewDecoder(bytes.NewReader(plan.original))
			decoder.UseNumber()
			err = decoder.Decode(&plan.root)
			if err == nil && (!json.Valid(plan.original) || plan.root == nil) {
				err = errors.New("configuration must be a JSON object")
			}
		}
		if err != nil {
			return nil, errors.New("configuration is damaged or unsupported; original file preserved")
		}
	}
	plan.servers = plan.root
	for _, key := range keys {
		plan.servers, err = jsonObject(plan.servers, key)
		if err != nil {
			return nil, errors.New("configuration container has an incompatible type")
		}
	}
	old, exists := plan.servers[serverName]
	plan.Existing = exists
	if exists {
		if _, ok := old.(map[string]any); !ok {
			return nil, errors.New("existing ChannelTerm entry must be an object")
		}
	}
	plan.entry, err = encode(endpoint)
	if err != nil {
		return nil, err
	}
	plan.Before, plan.After = redactedEntry(old), redactedEntry(plan.entry)
	return plan, nil
}

// ExistingCredential returns a bearer value only to the trusted local caller.
// It does not resolve environment expressions or return an unrelated client secret.
func (p *RepairPlan) ExistingCredential() string {
	entry, _ := p.servers[serverName].(map[string]any)
	for _, field := range []string{"http_headers", "headers"} {
		headers, _ := entry[field].(map[string]any)
		value, _ := headers["Authorization"].(string)
		if strings.HasPrefix(value, "Bearer ") {
			return strings.TrimPrefix(value, "Bearer ")
		}
		if headers, ok := entry[field].(map[string]string); ok {
			return strings.TrimPrefix(headers["Authorization"], "Bearer ")
		}
	}
	return ""
}

// ExistingURL returns only the selected entry's declared URL for trusted probes.
func (p *RepairPlan) ExistingURL() string {
	entry, _ := p.servers[serverName].(map[string]any)
	value, _ := entry["url"].(string)
	return value
}

// ReplacementFingerprint identifies the exact serialized replacement for crash
// recovery without putting its bytes or credential into ordinary output.
func (p *RepairPlan) ReplacementFingerprint() (string, error) {
	data, err := p.replacement()
	if err != nil {
		return "", err
	}
	return repairFingerprint(data, true), nil
}

// Apply explicitly replaces only the selected entry after a second byte-for-byte
// file check. Callers serialize their repair transactions; unrelated editors are
// detected before publication, but this is not a filesystem compare-and-swap API.
// A failed atomic replacement leaves the original file in place.
func (p *RepairPlan) Apply(expected string) error {
	if expected != p.Fingerprint {
		return errors.New("configuration changed; request a fresh preview")
	}
	data, err := p.replacement()
	if err != nil {
		return err
	}
	current, exists, err := readRepairFile(p.Path)
	if err != nil {
		return err
	}
	if repairFingerprint(current, exists) != p.Fingerprint {
		return errors.New("configuration changed; request a fresh preview")
	}
	return writeConfigAtomic(p.Path, data, true)
}

func (p *RepairPlan) replacement() ([]byte, error) {
	old, existed := p.servers[serverName]
	p.servers[serverName] = p.entry
	defer func() {
		if existed {
			p.servers[serverName] = old
		} else {
			delete(p.servers, serverName)
		}
	}()
	if p.toml {
		return toml.Marshal(p.root)
	}
	data, err := json.MarshalIndent(p.root, "", "  ")
	return append(data, '\n'), err
}

// RepairFingerprint reads only a bounded ordinary configuration file, including
// a distinct marker for absence. Recovery callers never need its secret contents.
func RepairFingerprint(path string) (string, error) {
	data, exists, err := readRepairFile(path)
	if err != nil {
		return "", err
	}
	return repairFingerprint(data, exists), nil
}

func repairFingerprint(data []byte, exists bool) string {
	hash := sha256.New()
	fmt.Fprintf(hash, "%t\x00", exists)
	hash.Write(data)
	return hex.EncodeToString(hash.Sum(nil))
}

func readRepairFile(path string) ([]byte, bool, error) {
	info, err := os.Lstat(path)
	if errors.Is(err, os.ErrNotExist) {
		return nil, false, nil
	}
	if err != nil {
		return nil, false, errors.New("configuration cannot be inspected")
	}
	if !info.Mode().IsRegular() || info.Size() > repairConfigLimit {
		return nil, false, errors.New("configuration must be an ordinary file of at most 1 MiB")
	}
	file, err := os.Open(path)
	if err != nil {
		return nil, false, errors.New("configuration cannot be read")
	}
	defer file.Close()
	opened, err := file.Stat()
	if err != nil || !os.SameFile(info, opened) {
		return nil, false, errors.New("configuration changed while opening")
	}
	data, err := io.ReadAll(io.LimitReader(file, repairConfigLimit+1))
	if err != nil || len(data) > repairConfigLimit || !utf8.Valid(data) {
		return nil, false, errors.New("configuration is unreadable, oversized or not UTF-8")
	}
	return data, true, nil
}

// Only documented safe enum values and credential-free loopback URLs are shown.
// Unknown settings, commands, arguments, headers and nested values remain opaque.
func redactedEntry(value any) string {
	if value == nil {
		return "null"
	}
	entry, ok := value.(map[string]any)
	if !ok {
		return "\"[redacted]\""
	}
	result := map[string]any{}
	known := map[string]bool{"url": true, "type": true, "command": true, "args": true, "headers": true, "http_headers": true, "oauth": true, "enabled": true, "disabled": true, "bearer_token_env_var": true, "env_http_headers": true}
	for key, value := range entry {
		if !known[key] {
			result["[other fields]"] = "[redacted]"
			continue
		}
		result[key] = "[redacted]"
		if key == "type" && (value == "http" || value == "remote" || value == "local" || value == "streamable-http") {
			result[key] = value
		}
		if key == "url" {
			if text, ok := value.(string); ok && SafeRepairEndpoint(text) {
				result[key] = text
			}
		}
		if key == "oauth" || key == "enabled" || key == "disabled" {
			if flag, ok := value.(bool); ok {
				result[key] = flag
			}
		}
	}
	data, _ := json.MarshalIndent(result, "", "  ")
	return string(data)
}

// SafeRepairEndpoint limits the desktop wizard to numeric loopback HTTP without
// URL credentials, query or fragment. It never weakens the legacy init contract.
func SafeRepairEndpoint(value string) bool {
	u, err := url.Parse(value)
	if err != nil || len(value) > 2048 || u.Scheme != "http" || u.User != nil || u.RawQuery != "" || u.ForceQuery || u.Fragment != "" || u.Opaque != "" {
		return false
	}
	if port := u.Port(); port != "" {
		n, err := strconv.Atoi(port)
		if err != nil || n < 1 || n > 65535 {
			return false
		}
	}
	ip := net.ParseIP(u.Hostname())
	return ip != nil && ip.IsLoopback() && u.Host != "" && !strings.ContainsAny(value, "\r\n\x00")
}

// Duplicate keys are ambiguous across clients; a rewrite must not silently select
// one meaning. Limit nesting as well as bytes before decoding the mutable document.
func uniqueJSON(data []byte) error {
	decoder := json.NewDecoder(bytes.NewReader(data))
	decoder.UseNumber()
	var walk func(int) error
	walk = func(depth int) error {
		if depth > 64 {
			return errors.New("configuration nesting limit exceeded")
		}
		token, err := decoder.Token()
		if err != nil {
			return errors.New("invalid JSON configuration")
		}
		delimiter, ok := token.(json.Delim)
		if !ok {
			return nil
		}
		switch delimiter {
		case '{':
			seen := map[string]bool{}
			for decoder.More() {
				key, err := decoder.Token()
				if err != nil {
					return errors.New("invalid JSON key")
				}
				name, ok := key.(string)
				if !ok || seen[name] {
					return errors.New("duplicate or invalid JSON key")
				}
				seen[name] = true
				if err := walk(depth + 1); err != nil {
					return err
				}
			}
		case '[':
			for decoder.More() {
				if err := walk(depth + 1); err != nil {
					return err
				}
			}
		default:
			return errors.New("invalid JSON delimiter")
		}
		_, err = decoder.Token()
		return err
	}
	if err := walk(0); err != nil {
		return err
	}
	if _, err := decoder.Token(); err != io.EOF {
		return errors.New("unexpected trailing JSON")
	}
	return nil
}
