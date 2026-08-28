// Package transport defines the protocol-neutral terminal byte-stream contract.
package transport

import "context"

// Transport represents a bidirectional terminal byte stream.
//
// Implementations establish a protocol-specific connection in Connect and own
// its resources until Close returns. Session owns the single continuous Read
// loop and retains received bytes; Transport implementations must not maintain
// terminal-history buffers. Write may block to apply natural input backpressure.
// Implementations must make Close safe to call more than once.
type Transport interface {
	// Connect establishes the underlying connection.
	//
	// ctx controls cancellation and the connection deadline. Connect returns a
	// context or protocol error and must release any partially acquired resources.
	Connect(ctx context.Context) error
	// Read copies terminal output into p and follows the io.Reader contract.
	//
	// p is caller-owned storage that the implementation must not retain after
	// Read returns. Session calls Read from exactly one reader goroutine.
	Read(p []byte) (int, error)
	// Write sends terminal input from p and follows the io.Writer contract.
	//
	// p is caller-owned input that the implementation must not retain after
	// Write returns.
	Write(p []byte) (int, error)
	// Resize changes the remote terminal dimensions.
	//
	// cols is the terminal width in character columns. rows is the terminal
	// height in character rows. Implementations return a protocol error when the
	// connected endpoint does not support terminal resizing.
	Resize(cols, rows uint16) error
	// Close releases all resources owned by the transport.
	Close() error
}
