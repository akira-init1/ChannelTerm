//go:build darwin

package install

import (
	"path/filepath"
	"testing"
)

func TestDefaultDarwinLayoutUsesApplicationSupportManifest(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)
	resolved, err := defaultLayout()
	if err != nil {
		t.Fatal(err)
	}
	want := filepath.Join(home, "Library", "Application Support", "channelterm", "install.json")
	if resolved.manifestPath != want {
		t.Errorf("manifest path = %q, want %q", resolved.manifestPath, want)
	}
	if resolved.binaryPath != filepath.Join(home, ".local", "bin", "channelterm") {
		t.Errorf("binary path = %q", resolved.binaryPath)
	}
}
