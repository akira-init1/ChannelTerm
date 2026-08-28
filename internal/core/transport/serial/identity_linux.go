//go:build linux && !android

package serial

import (
	"os"
	"path/filepath"
	"strings"
)

// enrichPorts reads sysfs only after normal serial enumeration succeeds. A
// missing or inaccessible sysfs node describes incomplete metadata, not a
// missing serial endpoint, so every error intentionally leaves that Port
// present with zero-value metadata.
func enrichPorts(ports []Port) []Port {
	for index := range ports {
		ports[index].DeviceMetadata = linuxDeviceMetadata(ports[index].Name)
	}
	return ports
}

// linuxDeviceMetadata walks from a tty class device to its USB-device parent.
// USB idVendor and idProduct live on that parent, while ttyUSB and ttyACM
// nodes can be nested below interface-specific directories.
func linuxDeviceMetadata(endpoint string) DeviceMetadata {
	name := filepath.Base(endpoint)
	if name == "." || name == string(filepath.Separator) || name == "" {
		return DeviceMetadata{}
	}
	path, err := filepath.EvalSymlinks(filepath.Join("/sys/class/tty", name, "device"))
	if err != nil {
		return DeviceMetadata{}
	}
	for {
		vid, vidErr := readSysfsValue(filepath.Join(path, "idVendor"))
		pid, pidErr := readSysfsValue(filepath.Join(path, "idProduct"))
		if vidErr == nil && pidErr == nil {
			return DeviceMetadata{
				VID:          normalizeUSBID(vid),
				PID:          normalizeUSBID(pid),
				USBSerial:    readOptionalSysfsValue(filepath.Join(path, "serial")),
				Manufacturer: readOptionalSysfsValue(filepath.Join(path, "manufacturer")),
				Product:      readOptionalSysfsValue(filepath.Join(path, "product")),
				USBPath:      readOptionalSysfsValue(filepath.Join(path, "devpath")),
			}
		}
		parent := filepath.Dir(path)
		if parent == path || parent == "/" {
			return DeviceMetadata{}
		}
		path = parent
	}
}

// readSysfsValue trims the newline sysfs uses for scalar attributes while
// preserving an empty value as meaningful absent metadata.
func readSysfsValue(path string) (string, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return "", err
	}
	return strings.TrimSpace(string(data)), nil
}

// readOptionalSysfsValue suppresses per-field metadata failures so one missing
// descriptor never affects the parent USB identity or endpoint presence.
func readOptionalSysfsValue(path string) string {
	value, err := readSysfsValue(path)
	if err != nil {
		return ""
	}
	return value
}
