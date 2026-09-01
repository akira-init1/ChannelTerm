package config

import (
	"errors"
	"fmt"
	"strings"
)

const (
	defaultBaudRate    = 115200
	defaultDataBits    = 8
	defaultParity      = "none"
	defaultStopBits    = "1"
	defaultFlowControl = "none"
)

var (
	// ErrProfileNotFound is returned when a named serial profile does not exist.
	ErrProfileNotFound = errors.New("serial profile not found")
	// ErrSerialPortRequired is returned when the final serial settings have no port.
	ErrSerialPortRequired = errors.New("serial port is required")
)

// Serial contains named serial connection profiles and an optional default.
//
// It is connection configuration only. It does not contain presentation
// preferences or own a live serial Transport.
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

// SerialOverrides represents serial values explicitly supplied by an adapter.
// Nil fields leave the resolved profile unchanged, preserving the distinction
// between an omitted CLI flag or MCP property and one set to its default value.
type SerialOverrides struct {
	Port        *string
	BaudRate    *int
	DataBits    *int
	Parity      *string
	StopBits    *string
	FlowControl *string
	Wake        *bool
}

// ResolveSerial selects a named profile, or the configured default when name is
// empty, and applies built-in serial defaults to omitted profile fields.
//
// With neither a requested nor configured name, it returns only the built-in
// serial defaults. It reads only Serial and never consults Preferences.
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

// SaveSerialProfile stores a resolved serial profile under name.
//
// It initializes the profile map when necessary and selects name as the
// default only when no default profile is configured. It preserves name exactly
// so established --save profile-key behavior remains unchanged. It changes only
// the connection-profile portion of File; callers persist the result with Save.
func (file *File) SaveSerialProfile(name string, profile SerialProfile) {
	if file.Serial.Profiles == nil {
		file.Serial.Profiles = make(map[string]SerialProfile)
	}
	file.Serial.Profiles[name] = profile
	if strings.TrimSpace(file.Serial.Default) == "" {
		file.Serial.Default = name
	}
}

// ApplySerialOverrides applies explicitly supplied adapter settings to profile.
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
