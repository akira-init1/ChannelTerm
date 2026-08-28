//go:build (!windows && !linux && !darwin) || android || ios

package serial

// enrichPorts keeps unsupported and mobile targets buildable without claiming
// USB metadata that their Serial Transport implementation does not provide.
func enrichPorts(ports []Port) []Port {
	return ports
}
