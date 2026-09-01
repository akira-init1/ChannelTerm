// Package terminalinput configures the local console for byte-oriented
// interactive terminal input.
package terminalinput

// fileDescriptorReader is the minimum capability needed to put a local console
// into raw mode without changing pipes, buffers, or other non-terminal readers.
type fileDescriptorReader interface {
	Fd() uintptr
}
