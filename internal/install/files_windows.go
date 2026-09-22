//go:build windows

package install

import (
	"errors"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"

	"golang.org/x/sys/windows"
)

// replaceFile requests an atomic write-through replacement on Windows.
func replaceFile(source, destination string) error {
	from, err := windows.UTF16PtrFromString(source)
	if err != nil {
		return err
	}
	to, err := windows.UTF16PtrFromString(destination)
	if err != nil {
		return err
	}
	return windows.MoveFileEx(from, to, windows.MOVEFILE_REPLACE_EXISTING|windows.MOVEFILE_WRITE_THROUGH)
}

// installAlias creates a synchronized executable copy because unprivileged
// Windows users cannot reliably create symbolic links.
func installAlias(binaryPath, aliasPath string) (string, bool, error) {
	// Windows locks a running executable. When invoked through cterm.exe, keep
	// an already-correct alias and require another source for an actual update.
	if current, err := os.Executable(); err == nil && pathEqual(current, aliasPath) {
		binaryHash, binaryErr := hashFile(binaryPath)
		aliasHash, aliasErr := hashFile(aliasPath)
		if binaryErr == nil && aliasErr == nil && binaryHash == aliasHash {
			return "copy", false, nil
		}
		return "", false, fmt.Errorf("cannot replace the running short command %q; run channelterm install from a downloaded executable or the full channelterm command", aliasPath)
	}
	if err := copyExecutableAtomic(binaryPath, aliasPath); err != nil {
		return "", false, fmt.Errorf("install short command: %w", err)
	}
	return "copy", true, nil
}

// verifyAlias checks that the short executable is still byte-identical to the
// full command before uninstall removes it.
func verifyAlias(binaryPath, aliasPath, kind string) error {
	if kind != "copy" {
		return fmt.Errorf("unsupported Windows short-command kind %q", kind)
	}
	binaryHash, err := hashFile(binaryPath)
	if err != nil {
		return err
	}
	aliasHash, err := hashFile(aliasPath)
	if err != nil {
		return fmt.Errorf("verify short command %q: %w", aliasPath, err)
	}
	if binaryHash != aliasHash {
		return fmt.Errorf("short command %q was modified outside ChannelTerm", aliasPath)
	}
	return nil
}

// removeInstalledFiles delegates to a temporary copy when either target is the
// currently locked Windows executable.
func removeInstalledFiles(binaryPath, aliasPath string) (bool, error) {
	current, err := os.Executable()
	if err == nil && (pathEqual(current, binaryPath) || pathEqual(current, aliasPath)) {
		// A copy outside the install directory survives the parent long enough to
		// remove both locked command files after this process exits.
		temporaryDirectory, err := os.MkdirTemp("", "channelterm-uninstall-")
		if err != nil {
			return false, fmt.Errorf("create deferred-uninstall directory: %w", err)
		}
		helper := filepath.Join(temporaryDirectory, "channelterm-uninstall.exe")
		if err := copyExecutableAtomic(current, helper); err != nil {
			_ = os.RemoveAll(temporaryDirectory)
			return false, fmt.Errorf("prepare deferred-uninstall helper: %w", err)
		}
		command := exec.Command(helper, "__uninstall-cleanup", "--parent", strconv.Itoa(os.Getpid()), "--binary", binaryPath, "--alias", aliasPath, "--helper", helper)
		if err := command.Start(); err != nil {
			_ = os.RemoveAll(temporaryDirectory)
			return false, fmt.Errorf("start deferred-uninstall helper: %w", err)
		}
		_ = command.Process.Release()
		return true, nil
	}
	if err := os.Remove(aliasPath); err != nil && !errors.Is(err, os.ErrNotExist) {
		return false, fmt.Errorf("remove short command %q: %w", aliasPath, err)
	}
	if err := os.Remove(binaryPath); err != nil && !errors.Is(err, os.ErrNotExist) {
		return false, fmt.Errorf("remove installed executable %q: %w", binaryPath, err)
	}
	return false, nil
}

// RunDeferredCleanup waits for the uninstalling parent process and removes the
// Windows executable that could not be deleted while it was running.
func RunDeferredCleanup(parentID uint32, binaryPath, aliasPath, helperPath string) error {
	// Waiting on the process handle avoids racing Windows executable locks. If
	// the parent is already gone, OpenProcess fails and cleanup can proceed.
	process, err := windows.OpenProcess(windows.SYNCHRONIZE, false, parentID)
	if err == nil {
		_, _ = windows.WaitForSingleObject(process, windows.INFINITE)
		_ = windows.CloseHandle(process)
	}
	if err := os.Remove(binaryPath); err != nil && !errors.Is(err, os.ErrNotExist) {
		return fmt.Errorf("remove installed executable %q: %w", binaryPath, err)
	}
	if err := os.Remove(aliasPath); err != nil && !errors.Is(err, os.ErrNotExist) {
		return fmt.Errorf("remove short command %q: %w", aliasPath, err)
	}
	_ = os.Remove(filepath.Dir(binaryPath))
	_ = os.Remove(filepath.Dir(filepath.Dir(binaryPath)))

	// cmd.exe runs after this helper exits. The helper paths are passed through
	// environment variables so command metacharacters in user directory names
	// remain inside quoted expansions rather than becoming command syntax.
	cleanup := exec.Command("cmd.exe", "/D", "/Q", "/C", "ping 127.0.0.1 -n 2 >NUL & del /F /Q \"%CHANNELTERM_UNINSTALL_HELPER%\" >NUL 2>&1 & rmdir \"%CHANNELTERM_UNINSTALL_DIR%\" >NUL 2>&1")
	cleanup.Env = append(os.Environ(), "CHANNELTERM_UNINSTALL_HELPER="+helperPath, "CHANNELTERM_UNINSTALL_DIR="+filepath.Dir(helperPath))
	if err := cleanup.Start(); err != nil {
		return fmt.Errorf("schedule deferred helper cleanup: %w", err)
	}
	_ = cleanup.Process.Release()
	return nil
}

// parseDeferredCleanupArgs accepts only the rigid argument shape emitted by
// removeInstalledFiles; ParseAndRunDeferredCleanup validates the paths.
func parseDeferredCleanupArgs(args []string) (uint32, string, string, string, error) {
	if len(args) != 8 || args[0] != "--parent" || args[2] != "--binary" || args[4] != "--alias" || args[6] != "--helper" {
		return 0, "", "", "", errors.New("invalid deferred-uninstall arguments")
	}
	parent, err := strconv.ParseUint(args[1], 10, 32)
	if err != nil || parent == 0 {
		return 0, "", "", "", errors.New("invalid deferred-uninstall parent process")
	}
	if strings.TrimSpace(args[3]) == "" || strings.TrimSpace(args[5]) == "" || strings.TrimSpace(args[7]) == "" {
		return 0, "", "", "", errors.New("invalid deferred-uninstall path")
	}
	return uint32(parent), args[3], args[5], args[7], nil
}
