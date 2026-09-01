package config

// Preferences contains user-owned presentation preferences.
//
// It deliberately has no fields yet. Future terminal-display, UI, TUI, and
// GUI preferences belong here, are loaded and saved through File, and must not
// affect connection profile resolution or Transport construction. Its empty
// value is omitted from TOML, preserving existing config.toml output until a
// concrete preference is introduced.
type Preferences struct{}

// DefaultPreferences returns the default user preferences.
//
// The current model has no configurable values. Keeping a constructor makes
// defaults explicit at the configuration boundary: Load starts from this value
// before decoding TOML, so later defaulted fields can remain absent from older
// configuration files.
func DefaultPreferences() Preferences {
	return Preferences{}
}
