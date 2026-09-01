//go:build !windows

package terminalinput

import (
	"io"

	"golang.org/x/term"
)

// MakeRaw disables local line editing, echo, and signal processing for an
// interactive input terminal and returns a function that restores its exact
// prior state.
//
// The returned restore function must run exactly once on every exit path after
// MakeRaw succeeds. Non-terminal readers are unchanged and receive a no-op
// restore function.
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
