//go:build linux || darwin

package install

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestUnixPathEditorAddsAndRemovesOwnedBlock(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)
	t.Setenv("SHELL", "/bin/bash")
	t.Setenv("PATH", "/usr/bin:/bin")
	profile := filepath.Join(home, ".profile")
	if err := os.WriteFile(profile, []byte("export EXISTING=value"), 0o640); err != nil {
		t.Fatal(err)
	}
	entry := filepath.Join(home, ".local", "bin")
	editor := unixPathEditor{}
	record, changed, err := editor.Ensure(entry, nil)
	if err != nil || !changed || !record.Managed || record.CreatedFile {
		t.Fatalf("Ensure() = %#v, %v, %v", record, changed, err)
	}
	data, err := os.ReadFile(profile)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(data), pathBlockStart) || !strings.Contains(string(data), entry) {
		t.Fatalf("profile = %q, want managed PATH block", data)
	}
	removed, err := editor.Remove(record)
	if err != nil || !removed {
		t.Fatalf("Remove() = %v, %v", removed, err)
	}
	data, err = os.ReadFile(profile)
	if err != nil || string(data) != "export EXISTING=value" {
		t.Fatalf("restored profile = %q, %v", data, err)
	}
}

func TestUnixPathEditorLeavesPreexistingPathUnmanaged(t *testing.T) {
	home := t.TempDir()
	entry := filepath.Join(home, ".local", "bin")
	t.Setenv("HOME", home)
	t.Setenv("PATH", "/usr/bin:"+entry)
	record, changed, err := (unixPathEditor{}).Ensure(entry, nil)
	if err != nil || changed || record.Managed {
		t.Fatalf("Ensure(preexisting) = %#v, %v, %v", record, changed, err)
	}
}

func TestUnixPathEditorRejectsManifestDirectedAtArbitraryFile(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)
	record := PathRecord{
		Managed:   true,
		Entry:     filepath.Join(home, ".local", "bin"),
		Scope:     "user",
		ShellFile: filepath.Join(home, "important.txt"),
		Block:     pathBlockStart + "\nmalicious\n" + pathBlockEnd + "\n",
	}
	if _, err := (unixPathEditor{}).Remove(record); err == nil || !strings.Contains(err.Error(), "outside") {
		t.Fatalf("Remove(arbitrary file) error = %v", err)
	}
}
