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

// ManualConnection identifies one protected physical serial endpoint or SSH login on this Host.
type ManualConnection struct {
	Transport string `toml:"transport" json:"transport"`
	Endpoint  string `toml:"endpoint" json:"endpoint"`
}

// File is the complete ChannelTerm configuration file.
//
// Serial stores connection profiles, Connection stores discovery policy, and
// Preferences stores adapter-local user preferences. Host selects idle policy.
// FileTransfer grants
// managed transfers access to an explicit Host root and SSH profiles. They share one
// config.toml lifecycle, while their responsibilities remain independent.
//
// Empty preferences are omitted from TOML; client choices do not grant Host access.
type File struct {
	ManualOnly   []ManualConnection `toml:"manual_only,omitempty"`
	Host         Host               `toml:"host,omitempty"`
	Serial       Serial             `toml:"serial"`
	Connection   Connection         `toml:"connection"`
	Preferences  Preferences        `toml:"preferences,omitempty"`
	FileTransfer FileTransfer       `toml:"file_transfer,omitempty"`
}

// Host configures only persistent Host idleness. Empty values select the launch
// default; explicit "0s" retains resources. Clients cannot change this policy.
type Host struct {
	IdleSessionTimeout string `toml:"idle_session_timeout,omitempty"`
	IdleExitTimeout    string `toml:"idle_exit_timeout,omitempty"`
}

// FileTransfer grants managed transfers access to one Host directory and named
// SSH destinations. An empty Root disables the use case. Credentials stay in
// Host configuration; MCP arguments cannot choose key or trust database paths.
type FileTransfer struct {
	Root string                     `toml:"root,omitempty"`
	SSH  map[string]FileTransferSSH `toml:"ssh,omitempty"`
}

// FileTransferSSH describes a noninteractive, strictly host-key-checked SSH
// destination. Paths are absolute Host paths; secrets are never tool arguments.
type FileTransferSSH struct {
	User       string `toml:"user"`
	Host       string `toml:"host"`
	Port       int    `toml:"port,omitempty"`
	PrivateKey string `toml:"private_key"`
	KnownHosts string `toml:"known_hosts"`
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
