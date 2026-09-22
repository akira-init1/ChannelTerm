//go:build windows

package install

import (
	"errors"
	"os"
	"path/filepath"
	"testing"
)

func TestRemoveInstalledFilesRemovesEmptyWindowsProgramDirectories(t *testing.T) {
	parent := t.TempDir()
	programDirectory := filepath.Join(parent, "ChannelTerm")
	binDirectory := filepath.Join(programDirectory, "bin")
	binaryPath := filepath.Join(binDirectory, "channelterm.exe")
	aliasPath := filepath.Join(binDirectory, "cterm.exe")
	writeWindowsCommandPair(t, binaryPath, aliasPath)

	deferred, err := removeInstalledFiles(binaryPath, aliasPath)
	if err != nil {
		t.Fatalf("removeInstalledFiles() error = %v", err)
	}
	if deferred {
		t.Fatal("removeInstalledFiles() unexpectedly deferred temporary files")
	}
	for _, path := range []string{binaryPath, aliasPath, binDirectory, programDirectory} {
		if _, err := os.Lstat(path); !errors.Is(err, os.ErrNotExist) {
			t.Errorf("removed path %q still exists: %v", path, err)
		}
	}
	if _, err := os.Stat(parent); err != nil {
		t.Errorf("parent directory was removed: %v", err)
	}
}

func TestRemoveInstalledFilesPreservesUnknownWindowsProgramContents(t *testing.T) {
	parent := t.TempDir()
	programDirectory := filepath.Join(parent, "ChannelTerm")
	binDirectory := filepath.Join(programDirectory, "bin")
	binaryPath := filepath.Join(binDirectory, "channelterm.exe")
	aliasPath := filepath.Join(binDirectory, "cterm.exe")
	writeWindowsCommandPair(t, binaryPath, aliasPath)
	unknownPath := filepath.Join(programDirectory, "keep-me.txt")
	if err := os.WriteFile(unknownPath, []byte("user content"), 0o600); err != nil {
		t.Fatal(err)
	}

	if _, err := removeInstalledFiles(binaryPath, aliasPath); err != nil {
		t.Fatalf("removeInstalledFiles() error = %v", err)
	}
	if _, err := os.Stat(binDirectory); !errors.Is(err, os.ErrNotExist) {
		t.Errorf("empty bin directory still exists: %v", err)
	}
	if data, err := os.ReadFile(unknownPath); err != nil || string(data) != "user content" {
		t.Errorf("unknown content = %q, %v", data, err)
	}
}

func writeWindowsCommandPair(t *testing.T, binaryPath, aliasPath string) {
	t.Helper()
	if err := os.MkdirAll(filepath.Dir(binaryPath), 0o755); err != nil {
		t.Fatal(err)
	}
	for _, path := range []string{binaryPath, aliasPath} {
		if err := os.WriteFile(path, []byte("test executable"), 0o755); err != nil {
			t.Fatal(err)
		}
	}
}
