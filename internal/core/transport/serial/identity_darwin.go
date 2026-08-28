//go:build darwin && !ios

package serial

// enrichPorts deliberately leaves metadata empty on desktop macOS. IOKit
// exposes these values through C APIs; ChannelTerm keeps CGO disabled and does
// not invoke external commands, so endpoint enumeration remains the reliable
// v0.1 behavior on this platform.
func enrichPorts(ports []Port) []Port {
	return ports
}
