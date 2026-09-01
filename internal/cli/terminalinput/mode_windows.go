//go:build windows

package terminalinput

import (
	"io"

	"golang.org/x/sys/windows"
)

// MakeRaw configures a Windows console input handle for byte-oriented terminal
// input. It returns a reader that normalizes console Esc key records to byte
// 0x1b and a function that restores the exact prior console mode.
//
// Raw input disables local echo, line editing, and processed input so Ctrl+C
// reaches the remote terminal as 0x03. It also enables virtual-terminal input,
// causing navigation keys to be read as their standard VT byte sequences. The
// returned restore function must run exactly once on every exit path after
// MakeRaw succeeds. Non-console readers are unchanged and receive a no-op
// restore function.
func MakeRaw(input io.Reader) (io.Reader, func() error, error) {
	terminal, ok := input.(fileDescriptorReader)
	if !ok {
		return input, func() error { return nil }, nil
	}

	handle := windows.Handle(terminal.Fd())
	var originalMode uint32
	if err := windows.GetConsoleMode(handle, &originalMode); err != nil {
		return input, func() error { return nil }, nil
	}
	if err := windows.SetConsoleMode(handle, windowsRawInputMode(originalMode)); err != nil {
		return nil, nil, err
	}
	return newWindowsConsoleReader(handle), func() error {
		return windows.SetConsoleMode(handle, originalMode)
	}, nil
}
