//go:build windows

package install

import (
	"fmt"
	"path/filepath"
	"strings"
	"unsafe"

	"golang.org/x/sys/windows"
	"golang.org/x/sys/windows/registry"
)

const userEnvironmentRegistryPath = `Environment`

// windowsPathEditor manages only the current user's Path registry value.
type windowsPathEditor struct{}

// newPathEditor constructs the Windows PATH integration used by Manager.
func newPathEditor() pathEditor {
	return windowsPathEditor{}
}

func (windowsPathEditor) Ensure(entry string, previous *PathRecord) (PathRecord, bool, error) {
	if previous != nil && previous.Managed {
		if err := validateWindowsPathRecord(*previous); err != nil {
			return PathRecord{}, false, err
		}
	}
	// HKCU keeps installation administrator-free and avoids changing the PATH
	// of other users on the machine.
	key, _, err := registry.CreateKey(registry.CURRENT_USER, userEnvironmentRegistryPath, registry.QUERY_VALUE|registry.SET_VALUE)
	if err != nil {
		return PathRecord{}, false, err
	}
	defer key.Close()
	value, valueType, err := key.GetStringValue("Path")
	if err != nil && err != registry.ErrNotExist {
		return PathRecord{}, false, err
	}
	if windowsPathContains(value, entry) {
		if previous != nil && previous.Managed {
			return *previous, false, nil
		}
		return PathRecord{Entry: entry, Scope: "user"}, false, nil
	}
	updated := entry
	if value != "" {
		updated = strings.TrimRight(value, ";") + ";" + entry
	}
	if err := setRegistryPath(key, valueType, updated); err != nil {
		return PathRecord{}, false, err
	}
	if err := broadcastEnvironmentChange(); err != nil {
		return PathRecord{}, false, err
	}
	return PathRecord{Managed: true, Entry: entry, Scope: "user"}, true, nil
}

func (windowsPathEditor) Remove(record PathRecord) (bool, error) {
	if !record.Managed {
		return false, nil
	}
	if err := validateWindowsPathRecord(record); err != nil {
		return false, err
	}
	key, err := registry.OpenKey(registry.CURRENT_USER, userEnvironmentRegistryPath, registry.QUERY_VALUE|registry.SET_VALUE)
	if err == registry.ErrNotExist {
		return false, nil
	}
	if err != nil {
		return false, err
	}
	defer key.Close()
	value, valueType, err := key.GetStringValue("Path")
	if err == registry.ErrNotExist {
		return false, nil
	}
	if err != nil {
		return false, err
	}
	parts := strings.Split(value, ";")
	removed := false
	kept := make([]string, 0, len(parts))
	for _, part := range parts {
		// Remove one occurrence only. Any duplicates not created by this
		// installation remain owned by the user or another installer.
		if !removed && windowsPathEqual(part, record.Entry) {
			removed = true
			continue
		}
		if part != "" {
			kept = append(kept, part)
		}
	}
	if !removed {
		return false, nil
	}
	if err := setRegistryPath(key, valueType, strings.Join(kept, ";")); err != nil {
		return false, err
	}
	if err := broadcastEnvironmentChange(); err != nil {
		return false, err
	}
	return true, nil
}

func setRegistryPath(key registry.Key, valueType uint32, value string) error {
	// Preserve REG_EXPAND_SZ so existing entries such as %USERPROFILE% keep
	// their expansion semantics after the value is rewritten.
	if valueType == registry.EXPAND_SZ {
		return key.SetExpandStringValue("Path", value)
	}
	return key.SetStringValue("Path", value)
}

// windowsPathContains searches a semicolon-delimited Windows Path value.
func windowsPathContains(value, entry string) bool {
	for _, part := range strings.Split(value, ";") {
		if windowsPathEqual(part, entry) {
			return true
		}
	}
	return false
}

func windowsPathEqual(left, right string) bool {
	// Compare expanded forms so an existing %LOCALAPPDATA% entry is recognized
	// as equivalent to the absolute installation path.
	left = expandWindowsEnvironment(left)
	right = expandWindowsEnvironment(right)
	left = strings.TrimRight(strings.TrimSpace(left), `\/`)
	right = strings.TrimRight(strings.TrimSpace(right), `\/`)
	return left != "" && strings.EqualFold(filepath.Clean(left), filepath.Clean(right))
}

// expandWindowsEnvironment resolves percent-delimited variables for comparison
// while leaving the original registry spelling untouched.
func expandWindowsEnvironment(value string) string {
	source, err := windows.UTF16PtrFromString(value)
	if err != nil {
		return value
	}
	required, err := windows.ExpandEnvironmentStrings(source, nil, 0)
	if err != nil || required == 0 {
		return value
	}
	buffer := make([]uint16, required)
	if _, err := windows.ExpandEnvironmentStrings(source, &buffer[0], required); err != nil {
		return value
	}
	return windows.UTF16ToString(buffer)
}

// validateWindowsPathRecord rejects Unix-only fields and non-user scope before
// a manifest can influence the registry.
func validateWindowsPathRecord(record PathRecord) error {
	if record.Entry == "" || record.Scope != "user" || record.ShellFile != "" || record.Block != "" {
		return fmt.Errorf("managed Windows PATH record is invalid")
	}
	return nil
}

func broadcastEnvironmentChange() error {
	// This notification lets Explorer and other listeners update the environment
	// inherited by newly launched processes; already-running shells are unchanged.
	message, err := windows.UTF16PtrFromString("Environment")
	if err != nil {
		return err
	}
	procedure := windows.NewLazySystemDLL("user32.dll").NewProc("SendMessageTimeoutW")
	const (
		hwndBroadcast   = 0xffff
		wmSettingChange = 0x001a
		smtoAbortIfHung = 0x0002
	)
	var result uintptr
	returned, _, callErr := procedure.Call(
		hwndBroadcast,
		wmSettingChange,
		0,
		uintptr(unsafe.Pointer(message)),
		smtoAbortIfHung,
		5000,
		uintptr(unsafe.Pointer(&result)),
	)
	if returned == 0 && callErr != windows.ERROR_SUCCESS {
		return fmt.Errorf("broadcast environment change: %w", callErr)
	}
	return nil
}
