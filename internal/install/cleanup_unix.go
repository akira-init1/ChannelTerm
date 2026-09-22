//go:build linux || darwin

package install

import "errors"

// RunDeferredCleanup is unavailable on Unix because a running executable can
// be unlinked directly.
func RunDeferredCleanup(uint32, string, string, string) error {
	return errors.New("deferred uninstall cleanup is supported only on Windows")
}

func parseDeferredCleanupArgs([]string) (uint32, string, string, string, error) {
	return 0, "", "", "", errors.New("deferred uninstall cleanup is supported only on Windows")
}
