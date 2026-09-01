// Package config owns ChannelTerm's persisted connection configuration,
// connection policy, and user preferences.
//
// Configuration is intentionally separate from transports and Sessions. The
// application layer translates a resolved connection profile into the concrete
// transport configuration it needs; user preferences never take part in that
// translation.
package config

import (
	"github.com/akira-init1/ChannelTerm/internal/core/connectionpolicy"
)

// File is the complete ChannelTerm configuration file.
//
// Serial stores connection profiles, Connection stores discovery policy, and
// Preferences stores adapter-local user preferences. All three share one
// config.toml lifecycle, while their responsibilities remain independent.
//
// Preferences is omitted from TOML until it contains persisted fields, so
// adding this boundary does not rewrite existing configuration into a new
// schema section.
type File struct {
	Serial      Serial      `toml:"serial"`
	Connection  Connection  `toml:"connection"`
	Preferences Preferences `toml:"preferences,omitempty"`
}

// Connection contains global behavior for clients reacting to device discovery.
//
// DefaultPolicy does not identify a device and never authorizes or prevents an
// explicit user-requested connection. It controls only the default reaction to
// a newly discovered endpoint.
type Connection struct {
	DefaultPolicy string `toml:"default_policy,omitempty"`
}

// ConnectionPolicy resolves the configured default discovery policy.
//
// An omitted default_policy resolves to ask. Any non-empty unsupported value
// returns a clear validation error so callers never silently fall back to a
// different discovery behavior.
func (file File) ConnectionPolicy() (connectionpolicy.Policy, error) {
	return connectionpolicy.FromConfig(file.Connection.DefaultPolicy)
}
