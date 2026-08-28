//go:build !darwin || ios

package serial

import "runtime"

// currentPlatform returns the Go target operating system for platforms that
// do not need a desktop macOS distinction.
func currentPlatform() string {
	return runtime.GOOS
}
