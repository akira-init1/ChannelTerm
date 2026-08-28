// Package terminalinput configures the local console for byte-oriented
// interactive terminal input.
package terminalinput

import (
	"io"

	"golang.org/x/term"
)

// fileDescriptorReader is the minimum capability needed to put a local console
// into raw mode without changing pipes, buffers, or other non-terminal readers.
type fileDescriptorReader interface {
	Fd() uintptr
}

// MakeRaw disables local line editing, echo, and signal processing for an
// interactive input terminal and returns a function that restores its exact
// prior state.
//
// On Unix, x/term clears ISIG so Ctrl+C and Ctrl+] reach the caller as 0x03
// and 0x1D. On Windows, it clears ENABLE_PROCESSED_INPUT so Ctrl+C reaches the
// caller instead of becoming a console control event. The returned restore
// function must run exactly once on every exit path after MakeRaw succeeds.
// Non-terminal readers are unchanged and receive a no-op restore function.
func MakeRaw(input io.Reader) (func() error, error) {
	terminal, ok := input.(fileDescriptorReader)
	if !ok {
		return func() error { return nil }, nil
	}
	fd := int(terminal.Fd())
	if !term.IsTerminal(fd) {
		return func() error { return nil }, nil
	}
	state, err := term.MakeRaw(fd)
	if err != nil {
		return nil, err
	}
	return func() error { return term.Restore(fd, state) }, nil
}
