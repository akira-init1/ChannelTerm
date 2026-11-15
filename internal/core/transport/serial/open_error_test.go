package serial

import (
	"context"
	"errors"
	"fmt"
	"io/fs"
	"testing"

	goserial "go.bug.st/serial"
)

// TestSerialOpenReasonsRequireTypedEvidence distinguishes Windows ambiguity and
// preserves errors.Is/As without turning a discovery scan into an open probe.
func TestSerialOpenReasonsRequireTypedEvidence(t *testing.T) {
	for _, tc := range []struct {
		name, platform, code string
		err                  error
	}{
		{"missing", "linux", "serial_not_found", fs.ErrNotExist},
		{"permission", "linux", "serial_permission_denied", fs.ErrPermission},
		{"mac permission", "macos", "serial_permission_denied", fs.ErrPermission},
		{"windows access", "windows", "serial_busy_or_denied", fs.ErrPermission},
		{"driver busy", "linux", "serial_busy", &goserial.PortError{}},
		{"windows driver ambiguity", "windows", "serial_busy_or_denied", &goserial.PortError{}},
		{"untrusted driver text", "linux", "serial_open_failed", errors.New("permission denied")},
		{"unknown", "linux", "serial_open_failed", errors.New("private diagnostic")},
	} {
		t.Run(tc.name, func(t *testing.T) {
			err := diagnoseOpenError(tc.platform, "fixture-port", fmt.Errorf("driver: %w", tc.err))
			var classified interface{ ErrorCode() string }
			if !errors.As(err, &classified) || classified.ErrorCode() != tc.code || !errors.Is(err, tc.err) {
				t.Fatalf("error=%v code=%v", err, classified)
			}
		})
	}
	for _, source := range []error{context.Canceled, context.DeadlineExceeded} {
		err := diagnoseOpenError("linux", "fixture-port", source)
		var classified interface{ ErrorCode() string }
		if errors.As(err, &classified) || !errors.Is(err, source) {
			t.Fatalf("context error was reclassified: %v", err)
		}
	}
}
