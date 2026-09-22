//go:build windows

package install

import (
	"path/filepath"
	"testing"
)

func TestDefaultWindowsLayoutUsesPerUserProgramsAndLocalManifest(t *testing.T) {
	root := t.TempDir()
	localAppData := filepath.Join(root, "Local")
	roamingAppData := filepath.Join(root, "Roaming")
	t.Setenv("LOCALAPPDATA", localAppData)
	t.Setenv("APPDATA", roamingAppData)
	resolved, err := defaultLayout()
	if err != nil {
		t.Fatal(err)
	}
	bin := filepath.Join(localAppData, "Programs", "ChannelTerm", "bin")
	if resolved.binaryPath != filepath.Join(bin, "channelterm.exe") || resolved.aliasPath != filepath.Join(bin, "cterm.exe") {
		t.Errorf("command paths = %q/%q", resolved.binaryPath, resolved.aliasPath)
	}
	if resolved.manifestPath != filepath.Join(localAppData, "ChannelTerm", "install.json") {
		t.Errorf("manifest path = %q", resolved.manifestPath)
	}
	if resolved.configPath != filepath.Join(roamingAppData, "channelterm", "config.toml") {
		t.Errorf("configuration path = %q", resolved.configPath)
	}
}

func TestWindowsPathComparisonExpandsEnvironmentReferences(t *testing.T) {
	localAppData := filepath.Join(t.TempDir(), "Local")
	t.Setenv("LOCALAPPDATA", localAppData)
	referenced := `%LOCALAPPDATA%\Programs\ChannelTerm\bin`
	expanded := filepath.Join(localAppData, "Programs", "ChannelTerm", "bin")
	if !windowsPathEqual(referenced, expanded) {
		t.Fatalf("windowsPathEqual(%q, %q) = false", referenced, expanded)
	}
}
