// Package channel defines the protocol-neutral lifecycle of an established
// bidirectional byte stream.
package channel

import (
	"errors"
	"io"
	"reflect"
	"sync"
)

var (
	// ErrNilStream is returned when a Stream is created without an underlying
	// bidirectional byte stream.
	ErrNilStream = errors.New("channel stream must not be nil")
	// ErrNotOpen is returned when I/O is attempted after a Channel has stopped
	// accepting reads or writes.
	ErrNotOpen = errors.New("channel is not open")
	// ErrResizeUnsupported is returned when a Channel has no terminal-size
	// capability.
	ErrResizeUnsupported = errors.New("channel does not support terminal resizing")
)

// State describes the lifecycle stage of an established Channel.
type State uint8

const (
	// StateOpen indicates that the Channel accepts reads and writes.
	StateOpen State = iota
	// StateClosing indicates that Close is releasing the underlying stream.
	StateClosing
	// StateClosed indicates that Close has finished.
	StateClosed
	// StateFailed indicates that an underlying read or write failed.
	StateFailed
)

// String returns the stable lowercase form of a State for diagnostics.
func (s State) String() string {
	switch s {
	case StateOpen:
		return "open"
	case StateClosing:
		return "closing"
	case StateClosed:
		return "closed"
	case StateFailed:
		return "failed"
	default:
		return "unknown"
	}
}

// Channel is an established, protocol-neutral bidirectional byte stream.
//
// Implementations own the live stream resource until Close returns. Read and
// Write follow the standard library contracts and must not retain caller-owned
// buffers. Close must be safe to call repeatedly. State is a point-in-time
// lifecycle snapshot and must not be used as a concurrency guard.
type Channel interface {
	Read(p []byte) (int, error)
	Write(p []byte) (int, error)
	Close() error
	State() State
}

// Resizer is an optional Channel capability for terminal-like streams.
//
// Generic Session behavior does not require this capability. Terminal adapters
// may request it without making file, debug, or other byte streams implement a
// meaningless terminal operation.
type Resizer interface {
	Resize(cols, rows uint16) error
}

// Stream gives an io.ReadWriteCloser Channel lifecycle and idempotent cleanup.
//
// Stream is useful when a Transport opens a standard Go byte stream that does
// not need additional Channel capabilities.
type Stream struct {
	stream io.ReadWriteCloser

	mu           sync.Mutex
	state        State
	closeStarted bool
	closeDone    chan struct{}
	closeErr     error
}

// NewStream takes ownership of an already-open stream and returns an open
// Channel. The stream remains owned by Stream until Close completes.
func NewStream(stream io.ReadWriteCloser) (*Stream, error) {
	if isNil(stream) {
		return nil, ErrNilStream
	}
	return &Stream{
		stream:    stream,
		state:     StateOpen,
		closeDone: make(chan struct{}),
	}, nil
}

// Read copies bytes from the underlying stream while the Channel is open.
func (s *Stream) Read(p []byte) (int, error) {
	if !s.isOpen() {
		return 0, ErrNotOpen
	}
	n, err := s.stream.Read(p)
	if err != nil && !errors.Is(err, io.EOF) {
		s.markFailed()
	}
	return n, err
}

// Write copies bytes to the underlying stream while the Channel is open.
func (s *Stream) Write(p []byte) (int, error) {
	if !s.isOpen() {
		return 0, ErrNotOpen
	}
	return s.stream.Write(p)
}

// State returns a synchronized snapshot of the Channel lifecycle.
func (s *Stream) State() State {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.state
}

// Close releases the underlying stream exactly once and returns the original
// cleanup result to sequential or concurrent callers.
func (s *Stream) Close() error {
	s.mu.Lock()
	if s.closeStarted {
		done := s.closeDone
		s.mu.Unlock()
		<-done
		s.mu.Lock()
		err := s.closeErr
		s.mu.Unlock()
		return err
	}
	s.closeStarted = true
	s.state = StateClosing
	s.mu.Unlock()

	err := s.stream.Close()

	s.mu.Lock()
	s.closeErr = err
	s.state = StateClosed
	close(s.closeDone)
	s.mu.Unlock()
	return err
}

func (s *Stream) isOpen() bool {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.state == StateOpen
}

// markFailed prevents new I/O after an underlying failure while preserving a
// concurrent Close transition, which remains responsible for resource cleanup.
func (s *Stream) markFailed() {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.state == StateOpen {
		s.state = StateFailed
	}
}

// isNil detects an interface containing a typed nil pointer, which would
// otherwise panic only after ownership had transferred to Stream.
func isNil(value any) bool {
	if value == nil {
		return true
	}
	reflected := reflect.ValueOf(value)
	switch reflected.Kind() {
	case reflect.Chan, reflect.Func, reflect.Interface, reflect.Map, reflect.Pointer, reflect.Slice:
		return reflected.IsNil()
	default:
		return false
	}
}
