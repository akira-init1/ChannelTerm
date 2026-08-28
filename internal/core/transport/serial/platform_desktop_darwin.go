//go:build darwin && !ios

package serial

// currentPlatform distinguishes desktop macOS from iOS before selecting
// desktop serial diagnostics. iOS has no current Serial Transport support.
func currentPlatform() string {
	return platformMacOS
}
