//go:build linux || darwin

package install

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"
)

// replaceFile atomically replaces a same-filesystem destination on Unix.
func replaceFile(source, destination string) error {
	return os.Rename(source, destination)
}

// installAlias uses a relative target so moving or mounting the user's home
// directory at a different absolute path does not break the command pair.
func installAlias(binaryPath, aliasPath string) (string, bool, error) {
	if err := os.MkdirAll(filepath.Dir(aliasPath), 0o755); err != nil {
		return "", false, fmt.Errorf("create short-command directory: %w", err)
	}
	temporary := aliasPath + ".channelterm-new"
	_ = os.Remove(temporary)
	target, err := filepath.Rel(filepath.Dir(aliasPath), binaryPath)
	if err != nil {
		return "", false, fmt.Errorf("resolve relative short-command target: %w", err)
	}
	if err := os.Symlink(target, temporary); err != nil {
		return "", false, fmt.Errorf("create short command %q: %w", aliasPath, err)
	}
	defer os.Remove(temporary)
	if err := os.Rename(temporary, aliasPath); err != nil {
		return "", false, fmt.Errorf("install short command %q: %w", aliasPath, err)
	}
	if err := syncParentDirectory(filepath.Dir(aliasPath)); err != nil {
		return "symlink", true, fmt.Errorf("sync short-command directory: %w", err)
	}
	return "symlink", true, nil
}

// verifyAlias proves that the recorded alias is still the expected symbolic
// link before uninstall is allowed to remove it.
func verifyAlias(binaryPath, aliasPath, kind string) error {
	if kind != "symlink" {
		return fmt.Errorf("unsupported Unix short-command kind %q", kind)
	}
	info, err := os.Lstat(aliasPath)
	if err != nil {
		return fmt.Errorf("inspect short command %q: %w", aliasPath, err)
	}
	if info.Mode()&os.ModeSymlink == 0 {
		return fmt.Errorf("short command %q is no longer a symbolic link", aliasPath)
	}
	resolved, err := filepath.EvalSymlinks(aliasPath)
	if err != nil {
		return fmt.Errorf("resolve short command %q: %w", aliasPath, err)
	}
	if !sameFilePath(resolved, binaryPath) {
		return fmt.Errorf("short command %q no longer resolves to %q", aliasPath, binaryPath)
	}
	return nil
}

// removeInstalledFiles removes the alias before its target and reports that no
// deferred cleanup is required on Unix.
func removeInstalledFiles(binaryPath, aliasPath string) (bool, error) {
	// Unix permits unlinking the currently running executable, so no deferred
	// helper is necessary.
	if err := os.Remove(aliasPath); err != nil && !errors.Is(err, os.ErrNotExist) {
		return false, fmt.Errorf("remove short command %q: %w", aliasPath, err)
	}
	if err := os.Remove(binaryPath); err != nil && !errors.Is(err, os.ErrNotExist) {
		return false, fmt.Errorf("remove installed executable %q: %w", binaryPath, err)
	}
	return false, nil
}
