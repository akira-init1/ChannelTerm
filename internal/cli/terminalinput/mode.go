// Package terminalinput configures byte-oriented local console input and
// observes terminal dimensions without owning remote terminal behavior.
package terminalinput

// fileDescriptorReader is the minimum capability needed to put a local console
// into raw mode without changing pipes, buffers, or other non-terminal readers.
type fileDescriptorReader interface {
	Fd() uintptr
}
