//go:build !windows

package terminalinput

import (
	"io"

	"golang.org/x/term"
)

// MakeRaw disables local line editing, echo, and signal processing for an
// interactive input terminal. It returns the reader to use while raw mode is
// active and a function that restores the exact prior state.
//
// The returned restore function must run exactly once on every exit path after
// MakeRaw succeeds. Non-terminal readers are unchanged and receive a no-op
// restore function.
func MakeRaw(input io.Reader) (io.Reader, func() error, error) {
	terminal, ok := input.(fileDescriptorReader)
	if !ok {
		return input, func() error { return nil }, nil
	}
	fd := int(terminal.Fd())
	if !term.IsTerminal(fd) {
		return input, func() error { return nil }, nil
	}
	state, err := term.MakeRaw(fd)
	if err != nil {
		return nil, nil, err
	}
	return input, func() error { return term.Restore(fd, state) }, nil
}
