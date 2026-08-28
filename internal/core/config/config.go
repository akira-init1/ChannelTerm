// Package config loads and saves ChannelTerm's user configuration.
//
// Configuration is intentionally separate from transports and sessions. It
// contains connection preferences, while the CLI translates a resolved profile
// into the concrete transport configuration it needs.
package config

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"github.com/akira-init1/ChannelTerm/internal/core/connectionpolicy"
	"github.com/pelletier/go-toml/v2"
)

const (
	applicationDirectory = "channelterm"
	configurationFile    = "config.toml"
	stateFile            = "state.json"
	defaultBaudRate      = 115200
	defaultDataBits      = 8
	defaultParity        = "none"
	defaultStopBits      = "1"
	defaultFlowControl   = "none"
)

var (
	// ErrProfileNotFound is returned when a named serial profile does not exist.
	ErrProfileNotFound = errors.New("serial profile not found")
	// ErrSerialPortRequired is returned when the final serial settings have no port.
	ErrSerialPortRequired = errors.New("serial port is required")
)

// File is the complete ChannelTerm configuration file.
//
// Additional protocol sections can be added alongside Serial without changing
// the location or lifecycle of the shared configuration file.
type File struct {
	Serial     Serial     `toml:"serial"`
	Connection Connection `toml:"connection"`
}

// Connection contains global behavior for clients reacting to device discovery.
//
// DefaultPolicy does not identify a device and never authorizes or prevents an
// explicit user-requested connection. It controls only the default reaction to
// a newly discovered endpoint.
type Connection struct {
	DefaultPolicy string `toml:"default_policy,omitempty"`
}

// Serial contains named serial connection profiles and an optional default.
type Serial struct {
	Default  string                   `toml:"default,omitempty"`
	Profiles map[string]SerialProfile `toml:"profiles,omitempty"`
}

// SerialProfile contains serial connection settings stored in a named profile.
//
// Empty numeric and string fields inherit the built-in conventional settings
// when the profile is resolved. Wake is false unless explicitly enabled.
type SerialProfile struct {
	Port        string `toml:"port,omitempty"`
	BaudRate    int    `toml:"baud,omitempty"`
	DataBits    int    `toml:"data_bits,omitempty"`
	Parity      string `toml:"parity,omitempty"`
	StopBits    string `toml:"stop_bits,omitempty"`
	FlowControl string `toml:"flow_control,omitempty"`
	Wake        bool   `toml:"wake"`
}

// SerialOverrides represents serial values explicitly supplied by the CLI.
// Nil fields leave the resolved profile unchanged, which preserves the
// distinction between an omitted CLI flag and a flag using its default value.
type SerialOverrides struct {
	Port        *string
	BaudRate    *int
	DataBits    *int
	Parity      *string
	StopBits    *string
	FlowControl *string
	Wake        *bool
}

// DefaultPath returns the platform's normal location for config.toml.
//
// The path is based on os.UserConfigDir, which uses the appropriate Windows or
// Unix user configuration directory. It does not create files or directories.
func DefaultPath() (string, error) {
	directory, err := os.UserConfigDir()
	if err != nil {
		return "", fmt.Errorf("resolve user configuration directory: %w", err)
	}
	return filepath.Join(directory, applicationDirectory, configurationFile), nil
}

// DefaultStatePath returns the platform's normal location for state.json.
//
// State is kept beside the user configuration but has a separate lifecycle:
// it is maintained by ChannelTerm to retain discovered device identities and
// must never be treated as user connection configuration.
func DefaultStatePath() (string, error) {
	directory, err := os.UserConfigDir()
	if err != nil {
		return "", fmt.Errorf("resolve user configuration directory: %w", err)
	}
	return filepath.Join(directory, applicationDirectory, stateFile), nil
}

// Load reads and decodes a TOML configuration file at path.
func Load(path string) (File, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return File{}, fmt.Errorf("read configuration %q: %w", path, err)
	}
	var file File
	if err := toml.Unmarshal(data, &file); err != nil {
		return File{}, fmt.Errorf("decode configuration %q: %w", path, err)
	}
	if _, err := file.ConnectionPolicy(); err != nil {
		return File{}, err
	}
	return file, nil
}

// LoadOrCreate loads a TOML configuration file, creating the minimal template
// when it does not exist. Existing configuration files are read only.
func LoadOrCreate(path string) (File, error) {
	file, err := Load(path)
	if err == nil {
		return file, nil
	}
	if !errors.Is(err, os.ErrNotExist) {
		return File{}, err
	}
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		return File{}, fmt.Errorf("create configuration directory for %q: %w", path, err)
	}
	if err := os.WriteFile(path, []byte(defaultTemplate), 0o600); err != nil {
		return File{}, fmt.Errorf("create configuration %q: %w", path, err)
	}
	return Load(path)
}

// Save writes file as TOML to path, creating its parent directory if needed.
func Save(path string, file File) error {
	data, err := toml.Marshal(file)
	if err != nil {
		return fmt.Errorf("encode configuration %q: %w", path, err)
	}
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		return fmt.Errorf("create configuration directory for %q: %w", path, err)
	}
	if err := os.WriteFile(path, data, 0o600); err != nil {
		return fmt.Errorf("write configuration %q: %w", path, err)
	}
	return nil
}

// ResolveSerial selects a named profile, or the configured default when name is
// empty, and applies built-in serial defaults to omitted profile fields.
func (file File) ResolveSerial(name string) (SerialProfile, error) {
	profile := builtinSerialProfile()
	name = strings.TrimSpace(name)
	if name == "" {
		name = strings.TrimSpace(file.Serial.Default)
	}
	if name == "" {
		return profile, nil
	}
	stored, ok := file.Serial.Profiles[name]
	if !ok {
		return SerialProfile{}, fmt.Errorf("%w: %q", ErrProfileNotFound, name)
	}
	return mergeProfile(profile, stored), nil
}

// ConnectionPolicy resolves the configured default discovery policy.
//
// An omitted default_policy resolves to ask. Any non-empty unsupported value
// returns a clear validation error so callers never silently fall back to a
// different discovery behavior.
func (file File) ConnectionPolicy() (connectionpolicy.Policy, error) {
	return connectionpolicy.FromConfig(file.Connection.DefaultPolicy)
}

// ApplySerialOverrides applies explicitly provided CLI settings to profile.
func ApplySerialOverrides(profile SerialProfile, overrides SerialOverrides) SerialProfile {
	if overrides.Port != nil {
		profile.Port = *overrides.Port
	}
	if overrides.BaudRate != nil {
		profile.BaudRate = *overrides.BaudRate
	}
	if overrides.DataBits != nil {
		profile.DataBits = *overrides.DataBits
	}
	if overrides.Parity != nil {
		profile.Parity = *overrides.Parity
	}
	if overrides.StopBits != nil {
		profile.StopBits = *overrides.StopBits
	}
	if overrides.FlowControl != nil {
		profile.FlowControl = *overrides.FlowControl
	}
	if overrides.Wake != nil {
		profile.Wake = *overrides.Wake
	}
	return profile
}

// RequireSerialPort returns an error when profile does not name a serial port.
func RequireSerialPort(profile SerialProfile) error {
	if strings.TrimSpace(profile.Port) == "" {
		return ErrSerialPortRequired
	}
	return nil
}

// builtinSerialProfile establishes values that remain valid when a stored
// profile omits optional serial settings. The port intentionally has no default
// because choosing a physical device is always an explicit caller decision.
func builtinSerialProfile() SerialProfile {
	return SerialProfile{
		BaudRate:    defaultBaudRate,
		DataBits:    defaultDataBits,
		Parity:      defaultParity,
		StopBits:    defaultStopBits,
		FlowControl: defaultFlowControl,
	}
}

// mergeProfile overlays only explicitly stored scalar values so profiles can
// inherit conventional serial settings. Wake is always copied because false is
// both its safe default and the only meaningful disabled value.
func mergeProfile(base, profile SerialProfile) SerialProfile {
	if profile.Port != "" {
		base.Port = profile.Port
	}
	if profile.BaudRate != 0 {
		base.BaudRate = profile.BaudRate
	}
	if profile.DataBits != 0 {
		base.DataBits = profile.DataBits
	}
	if profile.Parity != "" {
		base.Parity = profile.Parity
	}
	if profile.StopBits != "" {
		base.StopBits = profile.StopBits
	}
	if profile.FlowControl != "" {
		base.FlowControl = profile.FlowControl
	}
	base.Wake = profile.Wake
	return base
}

const defaultTemplate = "# ChannelTerm configuration.\n# Add named serial profiles before connecting without --port.\n\n[serial]\n# default = \"profile-name\"\n\n[connection]\n# default_policy = \"ask\"\n"
