//go:build windows

package serial

import (
	"strings"
	"unsafe"

	"golang.org/x/sys/windows"
	"golang.org/x/sys/windows/registry"
)

const (
	digcfPresent = 0x00000002

	spdrpDeviceDesc    = 0
	spdrpHardwareID    = 1
	spdrpMFG           = 11
	spdrpFriendlyName  = 12
	spdrpLocationPaths = 35

	dicsFlagGlobal = 1
	diregDev       = 1
	keyRead        = 0x20019
)

var (
	setupAPI                             = windows.NewLazySystemDLL("setupapi.dll")
	procSetupDiGetClassDevs              = setupAPI.NewProc("SetupDiGetClassDevsW")
	procSetupDiDestroyDeviceInfoList     = setupAPI.NewProc("SetupDiDestroyDeviceInfoList")
	procSetupDiEnumDeviceInfo            = setupAPI.NewProc("SetupDiEnumDeviceInfo")
	procSetupDiGetDeviceRegistryProperty = setupAPI.NewProc("SetupDiGetDeviceRegistryPropertyW")
	procSetupDiOpenDevRegKey             = setupAPI.NewProc("SetupDiOpenDevRegKey")
	procSetupDiGetDeviceInstanceID       = setupAPI.NewProc("SetupDiGetDeviceInstanceIdW")
)

var portsClassGUID = windows.GUID{
	Data1: 0x4d36e978,
	Data2: 0xe325,
	Data3: 0x11ce,
	Data4: [8]byte{0xbf, 0xc1, 0x08, 0x00, 0x2b, 0xe1, 0x03, 0x18},
}

// spDevInfoData matches SP_DEVINFO_DATA. cbSize must contain this target's
// exact layout size before SetupAPI receives it.
type spDevInfoData struct {
	cbSize    uint32
	classGUID windows.GUID
	devInst   uint32
	reserved  uintptr
}

// enrichPorts joins enumerated COM names with present Ports-class Plug and Play
// nodes. SetupAPI is queried once per scan rather than once per port, avoiding
// an all-device traversal on the Registry's periodic scan. Any API failure
// simply leaves a port's metadata empty.
func enrichPorts(ports []Port) []Port {
	metadata := windowsSerialMetadata()
	for index := range ports {
		if discovered, ok := metadata[strings.ToUpper(ports[index].Name)]; ok {
			ports[index].DeviceMetadata = discovered
		}
	}
	return ports
}

// windowsSerialMetadata obtains COM port associations and USB properties from
// native SetupAPI device nodes without PowerShell, WMI, WMIC, or PnPUtil.
func windowsSerialMetadata() map[string]DeviceMetadata {
	set, _, err := procSetupDiGetClassDevs.Call(uintptr(unsafe.Pointer(&portsClassGUID)), 0, 0, digcfPresent)
	if set == uintptr(windows.InvalidHandle) || err != nil && set == 0 {
		return map[string]DeviceMetadata{}
	}
	defer procSetupDiDestroyDeviceInfoList.Call(set)

	metadata := make(map[string]DeviceMetadata)
	for index := uint32(0); ; index++ {
		info := spDevInfoData{cbSize: uint32(unsafe.Sizeof(spDevInfoData{}))}
		ok, _, enumErr := procSetupDiEnumDeviceInfo.Call(set, uintptr(index), uintptr(unsafe.Pointer(&info)))
		if ok == 0 {
			if enumErr == windows.ERROR_NO_MORE_ITEMS {
				break
			}
			continue
		}
		portName := windowsPortName(set, &info)
		if portName == "" {
			continue
		}
		candidate := DeviceMetadata{
			Manufacturer: windowsDeviceProperty(set, &info, spdrpMFG),
			Product:      windowsDeviceProperty(set, &info, spdrpDeviceDesc),
			USBPath:      windowsDeviceProperty(set, &info, spdrpLocationPaths),
		}
		if candidate.Product == "" {
			candidate.Product = windowsDeviceProperty(set, &info, spdrpFriendlyName)
		}
		hardwareID := windowsDeviceProperty(set, &info, spdrpHardwareID)
		candidate.VID, candidate.PID = windowsVIDPID(hardwareID)
		candidate.USBSerial = windowsUSBSerial(windowsDeviceInstanceID(set, &info))
		metadata[strings.ToUpper(portName)] = candidate
	}
	return metadata
}

// windowsPortName retrieves the PortName stored on a device's global hardware
// profile key. Absence is normal for non-serial Plug and Play devices.
func windowsPortName(set uintptr, info *spDevInfoData) string {
	key, _, _ := procSetupDiOpenDevRegKey.Call(set, uintptr(unsafe.Pointer(info)), dicsFlagGlobal, 0, diregDev, keyRead)
	if key == uintptr(windows.InvalidHandle) || key == 0 {
		return ""
	}
	registryKey := registry.Key(key)
	defer registryKey.Close()
	value, _, err := registryKey.GetStringValue("PortName")
	if err != nil {
		return ""
	}
	return strings.TrimSpace(value)
}

// windowsDeviceProperty reads the first string in a SetupAPI registry
// property. Hardware IDs and location paths can be multi-string values; their
// first value is the PnP primary value suitable for best-effort metadata.
func windowsDeviceProperty(set uintptr, info *spDevInfoData, property uint32) string {
	var required uint32
	procSetupDiGetDeviceRegistryProperty.Call(set, uintptr(unsafe.Pointer(info)), uintptr(property), 0, 0, 0, uintptr(unsafe.Pointer(&required)))
	if required == 0 {
		return ""
	}
	buffer := make([]uint16, (required+1)/2)
	ok, _, _ := procSetupDiGetDeviceRegistryProperty.Call(set, uintptr(unsafe.Pointer(info)), uintptr(property), 0, uintptr(unsafe.Pointer(&buffer[0])), uintptr(required), uintptr(unsafe.Pointer(&required)))
	if ok == 0 {
		return ""
	}
	return strings.TrimSpace(windows.UTF16ToString(buffer))
}

// windowsDeviceInstanceID returns the Plug and Play instance path used only to
// extract a USB descriptor serial number when Windows supplies one.
func windowsDeviceInstanceID(set uintptr, info *spDevInfoData) string {
	var required uint32
	procSetupDiGetDeviceInstanceID.Call(set, uintptr(unsafe.Pointer(info)), 0, 0, uintptr(unsafe.Pointer(&required)))
	if required == 0 {
		return ""
	}
	buffer := make([]uint16, required)
	ok, _, _ := procSetupDiGetDeviceInstanceID.Call(set, uintptr(unsafe.Pointer(info)), uintptr(unsafe.Pointer(&buffer[0])), uintptr(len(buffer)), uintptr(unsafe.Pointer(&required)))
	if ok == 0 {
		return ""
	}
	return windows.UTF16ToString(buffer)
}

// windowsVIDPID finds the standard USB tokens without treating a descriptive
// product name as identity when the driver did not report those tokens.
func windowsVIDPID(hardwareID string) (string, string) {
	upper := strings.ToUpper(hardwareID)
	return normalizeUSBID(windowsToken(upper, "VID_")), normalizeUSBID(windowsToken(upper, "PID_"))
}

// windowsToken returns exactly four hexadecimal characters following token.
func windowsToken(value, token string) string {
	start := strings.Index(value, token)
	if start < 0 || len(value) < start+len(token)+4 {
		return ""
	}
	return value[start+len(token) : start+len(token)+4]
}

// windowsUSBSerial accepts only the USB instance-id form where the final
// component is the USB device serial number. Other bus formats remain empty
// instead of inferring an identifier from a driver-specific instance string.
func windowsUSBSerial(instanceID string) string {
	parts := strings.Split(instanceID, "\\")
	if len(parts) != 3 || !strings.EqualFold(parts[0], "USB") {
		return ""
	}
	serial := strings.TrimSpace(parts[2])
	if serial == "" || strings.Contains(serial, "&") {
		return ""
	}
	return serial
}
