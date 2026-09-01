// Package serial implements the Transport contract for physical serial ports.
package serial

import (
	"context"
	"errors"
	"fmt"
	"io/fs"
	"strings"

	"github.com/akira-init1/ChannelTerm/internal/core/channel"
	goserial "go.bug.st/serial"
)

var (
	// ErrInvalidPortName is returned when a serial configuration has no port name.
	ErrInvalidPortName = errors.New("serial port name must not be empty")
	// ErrInvalidBaudRate is returned when a serial configuration has no baud rate.
	ErrInvalidBaudRate = errors.New("serial baud rate must be positive")
	// ErrInvalidDataBits is returned when a serial configuration uses unsupported data bits.
	ErrInvalidDataBits = errors.New("serial data bits must be one of 5, 6, 7, or 8")
	// ErrInvalidParity is returned when a serial configuration uses unsupported parity.
	ErrInvalidParity = errors.New("serial parity is invalid")
	// ErrInvalidStopBits is returned when a serial configuration uses unsupported stop bits.
	ErrInvalidStopBits = errors.New("serial stop bits must be 1, 1.5, or 2")
	// ErrInvalidFlowControl is returned when a serial configuration uses an unknown flow control mode.
	ErrInvalidFlowControl = errors.New("serial flow control must be none, software, or hardware")
	// ErrFlowControlUnsupported is returned when the selected flow control mode
	// cannot be configured by the cross-platform serial backend.
	ErrFlowControlUnsupported = errors.New("serial flow control is unsupported by the serial backend")
)

// Parity configures serial parity checking.
type Parity string

const (
	// ParityNone disables parity checking.
	ParityNone Parity = "none"
	// ParityOdd uses odd parity checking.
	ParityOdd Parity = "odd"
	// ParityEven uses even parity checking.
	ParityEven Parity = "even"
	// ParityMark uses mark parity checking when the platform supports it.
	ParityMark Parity = "mark"
	// ParitySpace uses space parity checking when the platform supports it.
	ParitySpace Parity = "space"
)

// StopBits configures the number of stop bits after each serial character.
type StopBits string

const (
	// StopBitsOne selects one stop bit.
	StopBitsOne StopBits = "1"
	// StopBitsOnePointFive selects one and a half stop bits.
	StopBitsOnePointFive StopBits = "1.5"
	// StopBitsTwo selects two stop bits.
	StopBitsTwo StopBits = "2"
)

// FlowControl configures serial input and output flow control.
type FlowControl string

const (
	// FlowControlNone disables software and hardware flow control.
	FlowControlNone FlowControl = "none"
	// FlowControlSoftware requests XON/XOFF flow control.
	FlowControlSoftware FlowControl = "software"
	// FlowControlHardware requests RTS/CTS flow control.
	FlowControlHardware FlowControl = "hardware"
)

// Config contains the physical settings used to open a serial port.
//
// Port is the platform port name, such as COM3 on Windows, /dev/ttyUSB0 on
// Linux, or /dev/cu.usbserial-123 on macOS. BaudRate must be positive. Zero
// DataBits, empty Parity, empty StopBits, and empty FlowControl select the
// conventional 8-N-1 configuration with no flow control. Software and hardware
// FlowControl are validated but currently return ErrFlowControlUnsupported;
// go.bug.st/serial v1.6.2 does not expose a portable way to configure them.
type Config struct {
	Port        string
	BaudRate    int
	DataBits    int
	Parity      Parity
	StopBits    StopBits
	FlowControl FlowControl
}

// DeviceMetadata describes USB and hardware information associated with a
// serial endpoint.
//
// Every field is best effort. In particular, USB serial numbers are optional,
// and non-USB serial controllers normally leave every field empty. VID and PID
// use four lowercase hexadecimal digits when the operating system supplies a
// valid value.
type DeviceMetadata struct {
	VID          string `json:"vid"`
	PID          string `json:"pid"`
	USBSerial    string `json:"usb_serial"`
	Manufacturer string `json:"manufacturer"`
	Product      string `json:"product"`
	USBPath      string `json:"usb_path"`
}

// Port describes one serial device detected by the operating system.
//
// Name is the platform-specific port identifier accepted by Config.Port, such
// as COM8 on Windows or /dev/ttyUSB0 on Linux. DeviceMetadata is collected
// without opening the endpoint and may be entirely empty when the platform or
// controller does not expose USB information.
type Port struct {
	Name string `json:"name"`
	DeviceMetadata
}

// ListPorts returns the serial devices currently reported by the operating
// system without opening any of them.
//
// The returned names are passed through unchanged so callers can use them as
// Config.Port values. ListPorts may return an empty slice when no device is
// detected, and returns the underlying enumeration error when discovery fails.
func ListPorts() ([]Port, error) {
	names, err := goserial.GetPortsList()
	if err != nil {
		return nil, fmt.Errorf("enumerate serial ports: %w", err)
	}
	ports := make([]Port, 0, len(names))
	for _, name := range names {
		ports = append(ports, Port{Name: name})
	}
	return enrichPorts(ports), nil
}

// Transport opens serial-backed Channels.
//
// Transport retains only validated connection configuration. A successful
// Connect transfers the opened port to the returned Channel, while Session owns
// receive-history buffering above that Channel.
type Transport struct {
	config Config

	open func(name string, mode *goserial.Mode) (goserial.Port, error)
}

// New validates config and returns a disconnected serial Transport.
func New(config Config) (*Transport, error) {
	if err := validateConfig(&config); err != nil {
		return nil, err
	}
	return &Transport{config: config, open: goserial.Open}, nil
}

// Connect opens the configured serial port and transfers it to a Channel.
//
// ctx is checked before and immediately after the operating-system open call.
// A cancelled context after the port opens closes the port before Connect
// returns. Physical port opening itself is delegated to the serial library and
// therefore cannot be interrupted once the operating system call has begun.
func (t *Transport) Connect(ctx context.Context) (channel.Channel, error) {
	if err := ctx.Err(); err != nil {
		return nil, err
	}

	port, err := t.open(t.config.Port, toMode(t.config))
	if err != nil {
		return nil, diagnoseOpenError(currentPlatform(), t.config.Port, err)
	}
	if err := ctx.Err(); err != nil {
		closeErr := port.Close()
		if closeErr != nil {
			return nil, errors.Join(err, fmt.Errorf("close serial port after cancellation: %w", closeErr))
		}
		return nil, err
	}
	stream, err := channel.NewStream(port)
	if err != nil {
		return nil, errors.Join(err, port.Close())
	}
	return stream, nil
}

// openFailure is the internal remediation category selected without discarding
// the driver error that users and callers need for detailed diagnostics.
type openFailure int

// platformMacOS identifies desktop macOS after the darwin platform adapter has
// excluded iOS, which does not currently support this Serial Transport.
const platformMacOS = "macos"

const (
	// openFailureUnknown keeps an unrecognized driver error intact without
	// guessing a remediation that could mislead a user.
	openFailureUnknown openFailure = iota
	// openFailureMissingDevice identifies a removed or misspelled endpoint.
	openFailureMissingDevice
	// openFailurePermissionDenied identifies a platform access-policy failure.
	openFailurePermissionDenied
	// openFailureBusy identifies an endpoint already held by another process.
	openFailureBusy
)

// diagnoseOpenError adds an actionable, platform-specific explanation while
// wrapping err so callers can still inspect the exact driver or OS failure.
func diagnoseOpenError(platform, port string, err error) error {
	switch classifyOpenFailure(platform, err) {
	case openFailureMissingDevice:
		return fmt.Errorf("serial device %q does not exist or may have been disconnected: %w", port, err)
	case openFailurePermissionDenied:
		if platform == platformMacOS {
			return fmt.Errorf("permission denied opening serial device %q; check the device permissions in /dev and reconnect the device: %w", port, err)
		}
		return fmt.Errorf("permission denied opening serial device %q; check device permissions and that your user is in the dialout group: %w", port, err)
	case openFailureBusy:
		return fmt.Errorf("serial port %q may be in use by another program: %w", port, err)
	default:
		return fmt.Errorf("open serial port %q: %w", port, err)
	}
}

// classifyOpenFailure recognizes the portable errors returned by os.Open and
// supplements them with messages produced by serial drivers on each platform.
// The error is never discarded; this classification only selects user guidance.
func classifyOpenFailure(platform string, err error) openFailure {
	message := strings.ToLower(err.Error())
	if errors.Is(err, fs.ErrNotExist) || containsAny(message,
		"no such file or directory", "file not found", "cannot find the file", "the system cannot find") {
		return openFailureMissingDevice
	}

	if (platform == "linux" || platform == platformMacOS) && (errors.Is(err, fs.ErrPermission) || containsAny(message, "permission denied", "operation not permitted")) {
		return openFailurePermissionDenied
	}

	if containsAny(message, "device or resource busy", "resource busy", "sharing violation", "being used by another process") {
		return openFailureBusy
	}
	if platform == "windows" && (errors.Is(err, fs.ErrPermission) || containsAny(message, "access denied", "access is denied")) {
		return openFailureBusy
	}
	return openFailureUnknown
}

func containsAny(message string, values ...string) bool {
	for _, value := range values {
		if strings.Contains(message, value) {
			return true
		}
	}
	return false
}

// validateConfig applies conventional serial defaults before checking the
// finite protocol settings. It modifies only New's private config copy, never
// the caller-owned Config value.
func validateConfig(config *Config) error {
	if config.Port == "" {
		return ErrInvalidPortName
	}
	if config.BaudRate <= 0 {
		return ErrInvalidBaudRate
	}
	if config.DataBits == 0 {
		config.DataBits = 8
	}
	if config.Parity == "" {
		config.Parity = ParityNone
	}
	if config.StopBits == "" {
		config.StopBits = StopBitsOne
	}
	if config.FlowControl == "" {
		config.FlowControl = FlowControlNone
	}
	if config.DataBits < 5 || config.DataBits > 8 {
		return ErrInvalidDataBits
	}
	if _, ok := parityMode(config.Parity); !ok {
		return ErrInvalidParity
	}
	if _, ok := stopBitsMode(config.StopBits); !ok {
		return ErrInvalidStopBits
	}
	if config.FlowControl != FlowControlNone && config.FlowControl != FlowControlSoftware && config.FlowControl != FlowControlHardware {
		return ErrInvalidFlowControl
	}
	if config.FlowControl != FlowControlNone {
		// go.bug.st/serial v1.6.2 exposes neither XON/XOFF nor RTS/CTS in
		// Mode. Its native open paths explicitly disable both, so accepting the
		// setting would falsely report a configured device.
		return fmt.Errorf("%w: %s", ErrFlowControlUnsupported, config.FlowControl)
	}
	return nil
}

// toMode converts a validated Config to the serial library representation.
// Validation guarantees the ignored boolean results are true here.
func toMode(config Config) *goserial.Mode {
	parity, _ := parityMode(config.Parity)
	stopBits, _ := stopBitsMode(config.StopBits)
	return &goserial.Mode{
		BaudRate: config.BaudRate,
		DataBits: config.DataBits,
		Parity:   parity,
		StopBits: stopBits,
	}
}

// parityMode keeps the application's stable string values independent from
// the third-party serial library's enumeration.
func parityMode(parity Parity) (goserial.Parity, bool) {
	switch parity {
	case ParityNone:
		return goserial.NoParity, true
	case ParityOdd:
		return goserial.OddParity, true
	case ParityEven:
		return goserial.EvenParity, true
	case ParityMark:
		return goserial.MarkParity, true
	case ParitySpace:
		return goserial.SpaceParity, true
	default:
		return goserial.NoParity, false
	}
}

// stopBitsMode keeps the application's stable string values independent from
// the third-party serial library's enumeration.
func stopBitsMode(stopBits StopBits) (goserial.StopBits, bool) {
	switch stopBits {
	case StopBitsOne:
		return goserial.OneStopBit, true
	case StopBitsOnePointFive:
		return goserial.OnePointFiveStopBits, true
	case StopBitsTwo:
		return goserial.TwoStopBits, true
	default:
		return goserial.OneStopBit, false
	}
}
