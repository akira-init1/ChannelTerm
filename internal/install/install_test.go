package install

import (
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

type memoryPathEditor struct {
	record       PathRecord
	ensureCalls  int
	removeCalls  int
	ensureChange bool
	removeChange bool
	ensureErr    error
}

func (editor *memoryPathEditor) Ensure(entry string, previous *PathRecord) (PathRecord, bool, error) {
	editor.ensureCalls++
	if editor.ensureErr != nil {
		return PathRecord{}, false, editor.ensureErr
	}
	if previous != nil && previous.Managed {
		editor.record = *previous
		return *previous, false, nil
	}
	editor.record = PathRecord{Managed: true, Entry: entry, Scope: "test"}
	return editor.record, editor.ensureChange, nil
}

func (editor *memoryPathEditor) Remove(record PathRecord) (bool, error) {
	editor.removeCalls++
	if record != editor.record {
		return false, errors.New("unexpected PATH record")
	}
	return editor.removeChange, nil
}

func TestManagerInstallUpdateAndUninstall(t *testing.T) {
	manager, editor := newTestManager(t, "1.2.3", "first executable")
	editor.ensureChange = true
	result, err := manager.Install(InstallOptions{})
	if err != nil {
		t.Fatalf("Install() error = %v", err)
	}
	if result.Action != "installed" || !result.PathChanged {
		t.Fatalf("Install() result = %#v, want installed with PATH change", result)
	}
	if got, err := os.ReadFile(manager.layout.binaryPath); err != nil || string(got) != "first executable" {
		t.Fatalf("installed executable = %q, %v", got, err)
	}
	if _, err := os.Stat(manager.layout.aliasPath); err != nil {
		t.Fatalf("short command missing: %v", err)
	}
	if _, err := os.Stat(manager.layout.configPath); err != nil {
		t.Fatalf("configuration missing: %v", err)
	}
	manifest, exists, err := loadManifest(manager.layout.manifestPath)
	if err != nil || !exists || manifest.Version != "1.2.3" || !manifest.Path.Managed {
		t.Fatalf("manifest = %#v, %v, %v", manifest, exists, err)
	}

	if err := os.WriteFile(manager.sourcePath, []byte("second executable"), 0o755); err != nil {
		t.Fatal(err)
	}
	manager.build = BuildInfo{Version: "1.3.0", Commit: "def", BuiltAt: "later"}
	result, err = manager.Install(InstallOptions{})
	if err != nil {
		t.Fatalf("update Install() error = %v", err)
	}
	if result.Action != "updated" {
		t.Errorf("update action = %q, want updated", result.Action)
	}
	if editor.ensureCalls != 2 {
		t.Errorf("PATH ensure calls = %d, want 2", editor.ensureCalls)
	}

	editor.removeChange = true
	removed, err := manager.Uninstall(UninstallOptions{})
	if err != nil {
		t.Fatalf("Uninstall() error = %v", err)
	}
	if !removed.PathChanged || removed.Deferred {
		t.Errorf("Uninstall() result = %#v", removed)
	}
	for _, path := range []string{manager.layout.binaryPath, manager.layout.aliasPath, manager.layout.manifestPath} {
		if _, err := os.Lstat(path); !errors.Is(err, os.ErrNotExist) {
			t.Errorf("uninstalled path %q still exists: %v", path, err)
		}
	}
	if _, err := os.Stat(manager.layout.configPath); err != nil {
		t.Errorf("ordinary uninstall removed configuration: %v", err)
	}
}

func TestManagerRefusesUnownedAndModifiedTargets(t *testing.T) {
	manager, _ := newTestManager(t, "1.0.0", "source")
	if err := os.MkdirAll(filepath.Dir(manager.layout.binaryPath), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(manager.layout.binaryPath, []byte("some other program"), 0o755); err != nil {
		t.Fatal(err)
	}
	if _, err := manager.Install(InstallOptions{}); err == nil || !strings.Contains(err.Error(), "without a ChannelTerm ownership record") {
		t.Fatalf("Install() error = %v, want unowned target refusal", err)
	}
	if _, err := manager.Install(InstallOptions{Adopt: true}); err == nil || !strings.Contains(err.Error(), "checksum differs") {
		t.Fatalf("Install(adopt) error = %v, want checksum refusal", err)
	}

	if err := os.WriteFile(manager.layout.binaryPath, []byte("source"), 0o755); err != nil {
		t.Fatal(err)
	}
	if _, err := manager.Install(InstallOptions{Adopt: true}); err != nil {
		t.Fatalf("Install(adopt) error = %v", err)
	}
	if err := os.WriteFile(manager.layout.binaryPath, []byte("tampered"), 0o755); err != nil {
		t.Fatal(err)
	}
	if _, err := manager.Uninstall(UninstallOptions{}); err == nil || !strings.Contains(err.Error(), "modified outside ChannelTerm") {
		t.Fatalf("Uninstall() error = %v, want modified-file refusal", err)
	}
}

func TestManagerRejectsDowngradeAndInvalidConfigurationBeforeChangingBinary(t *testing.T) {
	manager, _ := newTestManager(t, "2.0.0", "version two")
	if _, err := manager.Install(InstallOptions{}); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(manager.sourcePath, []byte("version one"), 0o755); err != nil {
		t.Fatal(err)
	}
	manager.build.Version = "1.0.0"
	if _, err := manager.Install(InstallOptions{}); err == nil || !strings.Contains(err.Error(), "newer than source") {
		t.Fatalf("downgrade error = %v", err)
	}
	if _, err := manager.Install(InstallOptions{AllowDowngrade: true}); err != nil {
		t.Fatalf("allowed downgrade error = %v", err)
	}

	other, _ := newTestManager(t, "1.0.0", "clean source")
	if err := os.MkdirAll(filepath.Dir(other.layout.configPath), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(other.layout.configPath, []byte("not = [valid"), 0o600); err != nil {
		t.Fatal(err)
	}
	if _, err := other.Install(InstallOptions{}); err == nil || !strings.Contains(err.Error(), "validate existing configuration") {
		t.Fatalf("invalid-configuration error = %v", err)
	}
	if _, err := os.Stat(other.layout.binaryPath); !errors.Is(err, os.ErrNotExist) {
		t.Fatalf("binary changed despite invalid configuration: %v", err)
	}
}

func TestManagerPurgeRemovesOnlyKnownUserData(t *testing.T) {
	manager, _ := newTestManager(t, "1.0.0", "source")
	if _, err := manager.Install(InstallOptions{}); err != nil {
		t.Fatal(err)
	}
	for _, path := range []string{manager.layout.statePath, manager.layout.tokenPath} {
		if err := os.WriteFile(path, []byte("state"), 0o600); err != nil {
			t.Fatal(err)
		}
	}
	unknown := filepath.Join(filepath.Dir(manager.layout.configPath), "keep-me.txt")
	if err := os.WriteFile(unknown, []byte("user file"), 0o600); err != nil {
		t.Fatal(err)
	}
	result, err := manager.Uninstall(UninstallOptions{Purge: true})
	if err != nil {
		t.Fatal(err)
	}
	if len(result.Purged) != 3 {
		t.Errorf("purged files = %v, want config, state, and token", result.Purged)
	}
	if _, err := os.Stat(unknown); err != nil {
		t.Errorf("purge removed unknown user file: %v", err)
	}
}

func TestManagerRollsBackProgramFilesWhenPathUpdateFails(t *testing.T) {
	manager, editor := newTestManager(t, "1.0.0", "source")
	editor.ensureErr = errors.New("PATH write failed")
	if _, err := manager.Install(InstallOptions{}); err == nil || !strings.Contains(err.Error(), "PATH write failed") {
		t.Fatalf("Install() error = %v", err)
	}
	for _, path := range []string{manager.layout.binaryPath, manager.layout.aliasPath, manager.layout.manifestPath} {
		if _, err := os.Lstat(path); !errors.Is(err, os.ErrNotExist) {
			t.Errorf("rolled-back path %q exists: %v", path, err)
		}
	}
}

func TestCompareVersions(t *testing.T) {
	for _, test := range []struct {
		source    string
		installed string
		want      int
	}{
		{source: "1.2.4", installed: "1.2.3", want: 1},
		{source: "v1.2.3", installed: "1.2.3", want: 0},
		{source: "1.1.9", installed: "1.2.0", want: -1},
		{source: "devel", installed: "1.2.0", want: 0},
		{source: "1.2.0-rc.1", installed: "1.1.0", want: 0},
	} {
		if got := compareVersions(test.source, test.installed); got != test.want {
			t.Errorf("compareVersions(%q, %q) = %d, want %d", test.source, test.installed, got, test.want)
		}
	}
}

func newTestManager(t *testing.T, version, sourceContent string) (*Manager, *memoryPathEditor) {
	t.Helper()
	root := t.TempDir()
	source := filepath.Join(root, "source", "channelterm")
	if err := os.MkdirAll(filepath.Dir(source), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(source, []byte(sourceContent), 0o755); err != nil {
		t.Fatal(err)
	}
	editor := &memoryPathEditor{}
	configurationDirectory := filepath.Join(root, "config")
	manager := &Manager{
		build: BuildInfo{Version: version, Commit: "abc", BuiltAt: "now"},
		layout: layout{
			binaryPath:   filepath.Join(root, "bin", executableName("channelterm")),
			aliasPath:    filepath.Join(root, "bin", executableName("cterm")),
			manifestPath: filepath.Join(root, "state", "install.json"),
			pathEntry:    filepath.Join(root, "bin"),
			configPath:   filepath.Join(configurationDirectory, "config.toml"),
			statePath:    filepath.Join(configurationDirectory, "state.json"),
			tokenPath:    filepath.Join(configurationDirectory, "http-auth-token"),
		},
		paths:      editor,
		sourcePath: source,
		now:        func() time.Time { return time.Date(2026, 9, 22, 0, 0, 0, 0, time.UTC) },
	}
	return manager, editor
}

func executableName(name string) string {
	if os.PathSeparator == '\\' {
		return name + ".exe"
	}
	return name
}
