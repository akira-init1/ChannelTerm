package serial

import (
	"context"
	"errors"
	"io/fs"

	goserial "go.bug.st/serial"
)

// serialOpenError preserves the original diagnostic chain while allowing adapters
// to show bounded remediation without interpreting driver text. Discovery cannot
// establish these conditions: this error describes one explicit open attempt.
type serialOpenError struct {
	cause error
	code  string
}

// Error returns the existing platform-specific diagnostic.
func (e *serialOpenError) Error() string { return e.cause.Error() }

// Unwrap retains OS and driver errors for semantic comparisons.
func (e *serialOpenError) Unwrap() error { return e.cause }

// ErrorCode returns a stable, protocol-neutral open failure classification.
func (e *serialOpenError) ErrorCode() string { return e.code }

// diagnoseOpenError keeps textual guidance separate from reliable machine reasons.
// Windows access-denied errors can mean either occupancy or access policy; the
// serial driver also maps access denied to PortBusy, so neither proves occupancy.
func diagnoseOpenError(platform, port string, err error) error {
	diagnostic := diagnoseOpenErrorMessage(platform, port, err)
	if errors.Is(err, context.Canceled) || errors.Is(err, context.DeadlineExceeded) {
		return diagnostic
	}
	code := "serial_open_failed"
	var driver *goserial.PortError
	switch {
	case errors.Is(err, fs.ErrNotExist):
		code = "serial_not_found"
	case errors.Is(err, fs.ErrPermission):
		if platform == "windows" {
			code = "serial_busy_or_denied"
		} else {
			code = "serial_permission_denied"
		}
	case errors.As(err, &driver):
		switch driver.Code() {
		case goserial.PortNotFound:
			code = "serial_not_found"
		case goserial.PermissionDenied:
			code = "serial_permission_denied"
		case goserial.PortBusy:
			code = "serial_busy"
			if platform == "windows" {
				code = "serial_busy_or_denied"
			}
		}
	}
	return &serialOpenError{cause: diagnostic, code: code}
}
