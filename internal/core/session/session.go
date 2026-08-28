// Package session manages protocol-neutral terminal sessions.
package session

import (
	"context"
	"errors"
	"fmt"
	"io"
	"reflect"
	"sync"
	"time"

	"github.com/akira-init1/ChannelTerm/internal/core/transport"
)

const (
	// DefaultReceiveBufferSize is the retained terminal output capacity for a
	// newly created Session. The buffer overwrites its oldest bytes when full.
	DefaultReceiveBufferSize = 16 * 1024 * 1024
	// DefaultAIReadLimit is the initial recent-output limit for AI consumers.
	// Callers may request a larger bounded chunk when their task requires it.
	DefaultAIReadLimit = 64 * 1024
	// DefaultActivityBufferCapacity is the fixed number of recent operations a
	// Session retains for independent CLI and Agent activity consumers.
	DefaultActivityBufferCapacity = 1024
	// readerBufferSize amortizes Transport.Read calls without retaining another
	// copy of terminal history outside the bounded receive buffer.
	readerBufferSize = 32 * 1024
	// readerIdleDelay prevents a broken Transport returning (0, nil) from
	// spinning the dedicated reader goroutine at full CPU usage.
	readerIdleDelay = time.Millisecond
)

var (
	// ErrNotOpen is returned when terminal I/O is requested before a session opens
	// or after it has closed.
	ErrNotOpen = errors.New("session is not open")
	// ErrInvalidID is returned when creating a session without an ID.
	ErrInvalidID = errors.New("session ID must not be empty")
	// ErrNilTransport is returned when creating a session without a transport.
	ErrNilTransport = errors.New("session transport must not be nil")
	// ErrInvalidActor is returned when a write request has no recognized source.
	ErrInvalidActor = errors.New("session write actor is invalid")
)

// Actor identifies the ChannelTerm component that initiated a terminal write.
//
// Actor is internal metadata only. Core never forwards it to Transport, so a
// connected device receives exactly the bytes supplied in WriteRequest.Data.
type Actor string

const (
	// ActorUser identifies input initiated by a human using a ChannelTerm client.
	ActorUser Actor = "user"
	// ActorAgent identifies input initiated through an Agent-facing integration.
	ActorAgent Actor = "agent"
	// ActorSystem identifies input initiated by ChannelTerm itself.
	ActorSystem Actor = "system"
)

// Valid reports whether actor is one of ChannelTerm's defined write sources.
func (actor Actor) Valid() bool {
	switch actor {
	case ActorUser, ActorAgent, ActorSystem:
		return true
	default:
		return false
	}
}

// WriteRequest contains one terminal payload together with its operation source.
//
// Data remains owned by the caller and is not retained after Write returns. Actor
// describes the operation source recorded by Session activity; it does not alter
// the bytes passed to the underlying Transport.
type WriteRequest struct {
	Actor Actor
	Data  []byte
}

// SessionState describes the lifecycle stage of a Session.
type SessionState uint8

const (
	// StateNew indicates that the Session has a Transport but has not connected.
	StateNew SessionState = iota
	// StateConnecting indicates that Connect is currently establishing Transport.
	StateConnecting
	// StateOpen indicates that terminal I/O can be forwarded to Transport.
	StateOpen
	// StateClosing indicates that Close has started and I/O is no longer allowed.
	StateClosing
	// StateClosed indicates that Close has finished and cannot run again.
	StateClosed
	// StateFailed indicates that Connect or the Transport reader has failed.
	StateFailed
)

// String returns the stable lowercase form of a SessionState for diagnostics.
func (s SessionState) String() string {
	switch s {
	case StateNew:
		return "new"
	case StateConnecting:
		return "connecting"
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

// Session represents a protocol-neutral terminal connection.
//
// Session owns the continuous Transport reader and keeps only the newest output
// in a fixed-capacity Ring Buffer. UI and AI consumers use cursor-based chunk
// reads; they never access Transport.Read or the buffer implementation directly.
type Session interface {
	// ID returns the stable caller-provided identifier for this Session.
	ID() string
	// State returns a snapshot of the current lifecycle stage.
	State() SessionState
	// ReadOutput waits for output at next and returns at most maxBytes.
	//
	// next is the cursor returned by the preceding OutputChunk. ctx cancels the
	// wait for new output. Dropped is true when next has been overwritten.
	ReadOutput(ctx context.Context, next OutputCursor, maxBytes int) (OutputChunk, error)
	// ReadRecent returns at most maxBytes from the newest retained output.
	//
	// AI consumers should begin with DefaultAIReadLimit instead of the complete
	// receive buffer. maxBytes can grow for a specific task but remains bounded.
	ReadRecent(maxBytes int) (OutputChunk, error)
	// ReadActivity waits for activity events at next and returns at most maxEvents.
	//
	// ctx cancels the wait for future events. Dropped is true when next has been
	// overwritten by the bounded Activity Event Buffer.
	ReadActivity(ctx context.Context, next ActivityCursor, maxEvents int) (ActivityChunk, error)
	// ReadRecentActivity returns at most maxEvents from the newest retained activity.
	//
	// Its Next cursor always represents the current activity tail, so consumers
	// can use it to begin waiting without replaying older events.
	ReadRecentActivity(maxEvents int) (ActivityChunk, error)
	// Write sends request.Data when State is StateOpen.
	//
	// request.Actor must be a recognized Actor. It is internal metadata only and
	// is never encoded into the terminal byte stream.
	Write(request WriteRequest) (int, error)
	// Resize requests a terminal size change when State is StateOpen.
	//
	// cols is the terminal width in character columns and rows is its height in
	// character rows.
	Resize(cols, rows uint16) error
	// Close releases Session resources and is safe to call repeatedly.
	Close() error
}

// Option configures a Core created by New.
type Option func(*config) error

type config struct {
	receiveBufferCapacity  int
	activityBufferCapacity int
}

// WithReceiveBufferCapacity sets the fixed number of output bytes retained by
// a Session.
//
// bytes must be positive. When the buffer becomes full, incoming output
// overwrites the oldest bytes rather than blocking the Transport reader.
func WithReceiveBufferCapacity(bytes int) Option {
	return func(cfg *config) error {
		if bytes <= 0 {
			return ErrInvalidBufferCapacity
		}
		cfg.receiveBufferCapacity = bytes
		return nil
	}
}

// WithActivityBufferCapacity sets the fixed number of events retained by a Session.
//
// events must be positive. The Activity Event Buffer overwrites the oldest event
// when full, so activity consumers never impose backpressure on Session.Write.
func WithActivityBufferCapacity(events int) Option {
	return func(cfg *config) error {
		if events <= 0 {
			return ErrInvalidActivityBufferCapacity
		}
		cfg.activityBufferCapacity = events
		return nil
	}
}

// Core is the default Session implementation.
//
// Core serializes lifecycle transitions with mu. Its dedicated reader goroutine
// appends output to receive, allowing slow UI and AI consumers to wait without
// blocking Transport.Read.
type Core struct {
	id        string
	transport transport.Transport
	receive   *receiveBuffer
	activity  *activityBuffer

	mu         sync.Mutex
	writeMu    sync.Mutex
	state      SessionState
	closeErr   error
	readerDone chan struct{}
	readerStop chan struct{}
}

// New creates a Core in StateNew with a fixed-capacity receive buffer.
//
// id becomes the stable Session identifier and Manager registration key. terminal
// is owned by Core after New succeeds and remains owned until Close completes.
// options may change the default receive-output or activity-event capacity.
func New(id string, terminal transport.Transport, options ...Option) (*Core, error) {
	if id == "" {
		return nil, ErrInvalidID
	}
	if isNilTransport(terminal) {
		return nil, ErrNilTransport
	}

	cfg := config{receiveBufferCapacity: DefaultReceiveBufferSize, activityBufferCapacity: DefaultActivityBufferCapacity}
	for _, option := range options {
		if option == nil {
			continue
		}
		if err := option(&cfg); err != nil {
			return nil, err
		}
	}
	receive, err := newReceiveBuffer(cfg.receiveBufferCapacity)
	if err != nil {
		return nil, err
	}
	activity, err := newActivityBuffer(cfg.activityBufferCapacity)
	if err != nil {
		return nil, err
	}
	return &Core{
		id:         id,
		transport:  terminal,
		receive:    receive,
		activity:   activity,
		state:      StateNew,
		readerStop: make(chan struct{}),
	}, nil
}

// isNilTransport detects both a nil interface and an interface containing a
// typed nil pointer. The latter would otherwise pass a direct interface-nil
// comparison and panic later when Core invokes a Transport method.
func isNilTransport(terminal transport.Transport) bool {
	if terminal == nil {
		return true
	}

	value := reflect.ValueOf(terminal)
	switch value.Kind() {
	case reflect.Chan, reflect.Func, reflect.Interface, reflect.Map, reflect.Pointer, reflect.Slice:
		return value.IsNil()
	default:
		return false
	}
}

// ID returns the stable identifier supplied to New.
func (s *Core) ID() string { return s.id }

// State returns a synchronized snapshot of the current Core lifecycle stage.
func (s *Core) State() SessionState {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.state
}

// Connect establishes the underlying Transport and starts its output reader.
//
// ctx controls the connection attempt. Core checks ctx before calling
// Transport.Connect and passes it to Transport so cancellation and deadlines
// can interrupt protocol-specific connection work. A pre-cancelled ctx does
// not call Transport and moves Core directly to StateFailed.
//
// Connect may run only once, while Core is in StateNew. It records StateFailed
// when ctx is cancelled or Transport returns an error, and otherwise moves to
// StateOpen before starting the dedicated reader goroutine. Connect must not run
// concurrently with Close because the caller owns the connection lifecycle.
func (s *Core) Connect(ctx context.Context) error {
	s.mu.Lock()
	if s.state != StateNew {
		state := s.state
		s.mu.Unlock()
		return fmt.Errorf("cannot connect session in %s state", state)
	}
	if err := ctx.Err(); err != nil {
		s.state = StateFailed
		s.receive.close(err)
		s.activity.close(err)
		s.mu.Unlock()
		return err
	}
	s.state = StateConnecting
	s.mu.Unlock()

	err := s.transport.Connect(ctx)

	s.mu.Lock()
	defer s.mu.Unlock()
	if err != nil {
		s.state = StateFailed
		s.receive.close(err)
		s.activity.close(err)
		return err
	}
	s.state = StateOpen
	s.readerDone = make(chan struct{})
	go s.readLoop(s.readerDone)
	return nil
}

// ReadOutput waits for terminal output at next and returns at most maxBytes.
//
// next is the cursor returned by the preceding OutputChunk. ctx stops the wait
// if no output is available. A slow consumer receives Dropped=true and resumes
// from the oldest retained byte after the Ring Buffer overwrites its requested
// cursor. ReadOutput does not copy more than maxBytes into the returned chunk.
func (s *Core) ReadOutput(ctx context.Context, next OutputCursor, maxBytes int) (OutputChunk, error) {
	if !s.isOpen() {
		return OutputChunk{}, ErrNotOpen
	}
	return s.receive.readOutput(ctx, next, maxBytes)
}

// ReadRecent returns at most maxBytes from the newest retained terminal output.
//
// maxBytes bounds allocation and prevents consumers from accidentally copying
// the complete receive buffer. AI callers should use DefaultAIReadLimit unless
// their requested diagnostic task needs more context.
func (s *Core) ReadRecent(maxBytes int) (OutputChunk, error) {
	if !s.isOpen() {
		return OutputChunk{}, ErrNotOpen
	}
	return s.receive.readRecent(maxBytes)
}

// ReadActivity waits for activity events at next and returns at most maxEvents.
//
// The Activity Event Buffer is separate from terminal output, so reading it
// cannot consume device bytes or affect any OutputCursor consumer.
func (s *Core) ReadActivity(ctx context.Context, next ActivityCursor, maxEvents int) (ActivityChunk, error) {
	if !s.isOpen() {
		return ActivityChunk{}, ErrNotOpen
	}
	return s.activity.readActivity(ctx, next, maxEvents)
}

// ReadRecentActivity returns a bounded snapshot of recent Session operations.
//
// The returned event payloads are copies and therefore remain valid after this
// call returns even while future writes overwrite the bounded event buffer.
func (s *Core) ReadRecentActivity(maxEvents int) (ActivityChunk, error) {
	if !s.isOpen() {
		return ActivityChunk{}, ErrNotOpen
	}
	return s.activity.readRecent(maxEvents)
}

// Write sends all terminal input in request as one contiguous transport write
// sequence when Core is open.
//
// request.Data is caller-owned input and is never retained by Core.
// request.Actor is validated and recorded in the independent Activity Event
// Buffer, but Core deliberately passes only request.Data to Transport. Write
// serializes the complete short-write retry loop so concurrent callers cannot
// interleave their payload bytes. Close does not acquire writeMu: closing the
// Transport instead releases an in-flight write, avoiding a lifecycle lock
// cycle. Write returns ErrNotOpen rather than invoking Transport before Connect
// or after Close.
func (s *Core) Write(request WriteRequest) (int, error) {
	if !request.Actor.Valid() {
		return 0, fmt.Errorf("%w: %q", ErrInvalidActor, request.Actor)
	}
	p := request.Data
	timestamp := time.Now()
	s.writeMu.Lock()
	defer s.writeMu.Unlock()

	if !s.isOpen() {
		return 0, ErrNotOpen
	}

	written := 0
	for len(p) > 0 {
		n, err := s.transport.Write(p)
		if n > len(p) {
			n = len(p)
		}
		written += n
		p = p[n:]
		if err != nil {
			s.recordWriteActivity(timestamp, request, written)
			return written, err
		}
		if n == 0 {
			s.recordWriteActivity(timestamp, request, written)
			return written, io.ErrShortWrite
		}
	}
	s.recordWriteActivity(timestamp, request, written)
	return written, nil
}

// recordWriteActivity records only bytes confirmed written by Transport. It is
// called while writeMu is held, preserving the order of events with respect to
// Session.Write atomicity without waiting for any activity consumer.
func (s *Core) recordWriteActivity(timestamp time.Time, request WriteRequest, written int) {
	if written == 0 {
		return
	}
	s.activity.append(SessionEvent{
		Timestamp: timestamp,
		Actor:     request.Actor,
		Operation: OperationWrite,
		Data:      request.Data[:written],
	})
}

// Resize forwards a terminal dimension change when Core is open.
//
// cols is the terminal width in character columns and rows is the terminal
// height in character rows. Resize returns ErrNotOpen before Connect or after
// Close so an unavailable Transport never receives a stale terminal-size request.
func (s *Core) Resize(cols, rows uint16) error {
	if !s.isOpen() {
		return ErrNotOpen
	}
	return s.transport.Resize(cols, rows)
}

// isOpen reads the lifecycle state without holding Core's lock during a
// potentially blocking Transport operation. The state may change immediately
// after this method returns, so callers must not use it as a concurrency guard.
func (s *Core) isOpen() bool {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.state == StateOpen
}

// readLoop is the only goroutine that calls Transport.Read for a Core. It
// publishes received bytes before handling an accompanying error because an
// io.Reader may validly return both data and an error in the same call.
func (s *Core) readLoop(done chan<- struct{}) {
	defer close(done)

	buffer := make([]byte, readerBufferSize)
	for {
		n, err := s.transport.Read(buffer)
		if n > 0 {
			s.receive.append(buffer[:n])
		}
		if err != nil {
			s.handleReadError(err)
			return
		}
		if n == 0 {
			// io.Reader implementations should block or return an error when no
			// bytes are available. A faulty driver returning (0, nil) must not make
			// this loop consume a CPU core or prevent Close from joining the reader.
			select {
			case <-s.readerStop:
				return
			case <-time.After(readerIdleDelay):
			}
		}
	}
}

// handleReadError preserves a deliberate Close as a normal end of output but
// records unexpected reader failures in the receive buffer for waiting clients.
// The state check happens under mu so Close cannot race into StateFailed.
func (s *Core) handleReadError(err error) {
	s.mu.Lock()
	closing := s.state == StateClosing || s.state == StateClosed
	if !closing {
		s.state = StateFailed
	}
	s.mu.Unlock()

	if closing || errors.Is(err, io.EOF) {
		s.receive.close(io.EOF)
		s.activity.close(io.EOF)
		return
	}
	s.receive.close(err)
	s.activity.close(err)
}

// Close releases the underlying Transport, output reader, and receive buffer.
//
// Close first wakes output consumers, then closes Transport to unblock its
// reader, waits for that goroutine to exit, and finally releases the retained
// buffer memory. Sequential calls after StateClosed return the original close
// result so cleanup errors remain observable.
func (s *Core) Close() error {
	s.mu.Lock()
	if s.state == StateClosed {
		err := s.closeErr
		s.mu.Unlock()
		return err
	}
	if s.state == StateClosing {
		s.mu.Unlock()
		return nil
	}
	s.state = StateClosing
	done := s.readerDone
	stop := s.readerStop
	s.mu.Unlock()

	s.receive.close(io.EOF)
	s.activity.close(io.EOF)
	close(stop)
	err := s.transport.Close()
	if done != nil {
		<-done
	}
	s.receive.release()
	s.activity.release()

	s.mu.Lock()
	s.closeErr = err
	s.state = StateClosed
	s.mu.Unlock()
	return err
}
