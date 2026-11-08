package config

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestTransferPreferencesDefaultsValidationAndPreservation(t *testing.T) {
	path := filepath.Join(t.TempDir(), "config.toml")
	p, err := LoadTransferPreferences(path)
	if err != nil {
		t.Fatal(err)
	}
	if mode, _ := p.Mode(); mode != "ask" {
		t.Fatal(mode)
	}
	if _, err := os.Stat(path); !os.IsNotExist(err) {
		t.Fatal("read created configuration")
	}
	for _, mode := range []string{"ask", "tar", "files"} {
		if err := Update(path, func(file *File) error {
			file.Connection.DefaultPolicy = "deny"
			file.FileTransfer.Root = "/fake/managed-root"
			file.Preferences.FileTransfer.DirectoryMode = mode
			return nil
		}); err != nil {
			t.Fatal(err)
		}
		file, err := Load(path)
		if err != nil {
			t.Fatal(err)
		}
		if got, _ := file.Preferences.FileTransfer.Mode(); got != mode || file.Connection.DefaultPolicy != "deny" || file.FileTransfer.Root != "/fake/managed-root" {
			t.Fatalf("lost config: %+v", file)
		}
	}
	if err := os.WriteFile(path, []byte("[preferences.file_transfer]\ndirectory_mode = 'zip'\n"), 0600); err != nil {
		t.Fatal(err)
	}
	if _, err := LoadTransferPreferences(path); err == nil || !strings.Contains(err.Error(), "directory_mode") {
		t.Fatal(err)
	}
}
