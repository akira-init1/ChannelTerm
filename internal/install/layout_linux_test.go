//go:build linux

package install

import (
	"path/filepath"
	"testing"
)

func TestDefaultLinuxLayoutUsesXDGStateAndUserBin(t *testing.T) {
	home := t.TempDir()
	configurationHome := filepath.Join(home, "configuration")
	stateHome := filepath.Join(home, "state")
	t.Setenv("HOME", home)
	t.Setenv("XDG_CONFIG_HOME", configurationHome)
	t.Setenv("XDG_STATE_HOME", stateHome)
	resolved, err := defaultLayout()
	if err != nil {
		t.Fatal(err)
	}
	if resolved.binaryPath != filepath.Join(home, ".local", "bin", "channelterm") || resolved.aliasPath != filepath.Join(home, ".local", "bin", "cterm") {
		t.Errorf("command paths = %q/%q", resolved.binaryPath, resolved.aliasPath)
	}
	if resolved.manifestPath != filepath.Join(stateHome, "channelterm", "install.json") {
		t.Errorf("manifest path = %q", resolved.manifestPath)
	}
	if resolved.configPath != filepath.Join(configurationHome, "channelterm", "config.toml") {
		t.Errorf("configuration path = %q", resolved.configPath)
	}
}

func TestDefaultLinuxLayoutIgnoresRelativeXDGStateHome(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)
	t.Setenv("XDG_STATE_HOME", "relative-state")
	resolved, err := defaultLayout()
	if err != nil {
		t.Fatal(err)
	}
	want := filepath.Join(home, ".local", "state", "channelterm", "install.json")
	if resolved.manifestPath != want {
		t.Errorf("manifest path = %q, want %q", resolved.manifestPath, want)
	}
}
