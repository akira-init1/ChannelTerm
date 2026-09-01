// Package session manages protocol-neutral shared stream sessions.
package session

import (
	"context"
	"errors"
	"fmt"
	"io"
	"reflect"
	"sync"
	"time"

	"github.com/akira-init1/ChannelTerm/internal/core/channel"
	"github.com/akira-init1/ChannelTerm/internal/core/transport"
)

const (
	// DefaultReceiveBufferSize is the retained stream-output capacity for a
	// newly created Session. The buffer overwrites its oldest bytes when full.
	DefaultReceiveBufferSize = 16 * 1024 * 1024
	// DefaultAIReadLimit is the initial recent-output limit for AI consumers.
	// Callers may request a larger bounded chunk when their task requires it.
	DefaultAIReadLimit = 64 * 1024
	// DefaultActivityBufferCapacity is the fixed number of recent operations a
	// Session retains for independent CLI and Agent activity consumers.
	DefaultActivityBufferCapacity = 1024
	// readerBufferSize amortizes Channel.Read calls without retaining another
	// copy of stream history outside the bounded receive buffer.
	readerBufferSize = 32 * 1024
	// readerIdleDelay prevents a broken Channel returning (0, nil) from
	// spinning the dedicated reader goroutine at full CPU usage.
	readerIdleDelay = time.Millisecond
)

var (
	// ErrNotOpen is returned when stream I/O is requested before a session opens
	// or after it has closed.
	ErrNotOpen = errors.New("session is not open")
	// ErrInvalidID is returned when creating a session without an ID.
	ErrInvalidID = errors.New("session ID must not be empty")
	// ErrNilTransport is returned when creating a session without a transport.
	ErrNilTransport = errors.New("session transport must not be nil")
	// ErrNilChannel is returned when a Transport reports a successful connection
	// without transferring an established Channel.
	ErrNilChannel = errors.New("session transport returned a nil channel")
	// ErrInvalidActor is returned when a write request has no recognized source.
	ErrInvalidActor = errors.New("session write actor is invalid")
)

// Actor identifies the ChannelTerm component that initiated a Channel write.
//
// Actor is internal metadata only. Core never forwards it to Channel, so the
// connected endpoint receives exactly the bytes supplied in WriteRequest.Data.
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

// WriteRequest contains one stream payload together with its operation source.
//
// Data remains owned by the caller and is not retained after Write returns. Actor
// describes the operation source recorded by Session activity; it does not alter
// the bytes passed to the underlying Channel.
type WriteRequest struct {
	Actor Actor
	Data  []byte
}

// SessionState describes the lifecycle stage of a Session.
type SessionState uint8

const (
	// StateNew indicates that the Session has a Transport but no Channel yet.
	StateNew SessionState = iota
	// StateConnecting indicates that Connect is establishing a Channel.
	StateConnecting
	// StateOpen indicates that stream I/O can be forwarded to Channel.
	StateOpen
	// StateClosing indicates that Close has started and I/O is no longer allowed.
	StateClosing
	// StateClosed indicates that Close has finished and cannot run again.
	StateClosed
	// StateFailed indicates that Connect or the Channel reader has failed.
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

// Session represents shared access to a protocol-neutral stream Channel.
//
// Session owns the continuous Channel reader and keeps only the newest output
// in a fixed-capacity Ring Buffer. UI and AI consumers use cursor-based chunk
// reads; they never access Channel.Read or the buffer implementation directly.
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
	// is never encoded into the Channel byte stream.
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
// overwrites the oldest bytes rather than blocking the Channel reader.
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
// appends output to receive, allowing slow clients to wait without blocking
// Channel.Read.
type Core struct {
	id        string
	transport transport.Transport
	channel   channel.Channel
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
// id becomes the stable Session identifier and Manager registration key. source
// is used once by Connect to establish the Channel that Core owns until Close.
// options may change the default receive-output or activity-event capacity.
func New(id string, source transport.Transport, options ...Option) (*Core, error) {
	if id == "" {
		return nil, ErrInvalidID
	}
	if isNilTransport(source) {
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
		transport:  source,
		receive:    receive,
		activity:   activity,
		state:      StateNew,
		readerStop: make(chan struct{}),
	}, nil
}

// isNilTransport detects both a nil interface and an interface containing a
// typed nil pointer. The latter would otherwise pass a direct interface-nil
// comparison and panic later when Core invokes a Transport method.
func isNilTransport(source transport.Transport) bool {
	if source == nil {
		return true
	}

	value := reflect.ValueOf(source)
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

// Connect asks the Transport to establish a Channel and starts its output reader.
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

	stream, err := s.transport.Connect(ctx)

	s.mu.Lock()
	defer s.mu.Unlock()
	if err != nil {
		s.state = StateFailed
		s.receive.close(err)
		s.activity.close(err)
		return err
	}
	if isNilChannel(stream) {
		s.state = StateFailed
		s.receive.close(ErrNilChannel)
		s.activity.close(ErrNilChannel)
		return ErrNilChannel
	}
	s.channel = stream
	s.state = StateOpen
	s.readerDone = make(chan struct{})
	go s.readLoop(s.readerDone)
	return nil
}

// isNilChannel detects a typed nil returned through the Channel interface.
func isNilChannel(stream channel.Channel) bool {
	if stream == nil {
		return true
	}
	value := reflect.ValueOf(stream)
	switch value.Kind() {
	case reflect.Chan, reflect.Func, reflect.Interface, reflect.Map, reflect.Pointer, reflect.Slice:
		return value.IsNil()
	default:
		return false
	}
}

// ReadOutput waits for stream output at next and returns at most maxBytes.
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

// ReadRecent returns at most maxBytes from the newest retained stream output.
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
// The Activity Event Buffer is separate from stream output, so reading it
// cannot consume Channel bytes or affect any OutputCursor consumer.
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

// Write sends request data as one contiguous Channel write
// sequence when Core is open.
//
// request.Data is caller-owned input and is never retained by Core.
// request.Actor is validated and recorded in the independent Activity Event
// Buffer, but Core deliberately passes only request.Data to Channel. Write
// serializes the complete short-write retry loop so concurrent callers cannot
// interleave their payload bytes. Close does not acquire writeMu: closing the
// Channel instead releases an in-flight write, avoiding a lifecycle lock
// cycle. Write returns ErrNotOpen rather than invoking Channel before Connect
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
		n, err := s.channel.Write(p)
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

// recordWriteActivity records only bytes confirmed written by Channel. It is
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

// Resize forwards a terminal dimension change when the Channel supports it.
//
// cols is the terminal width in character columns and rows is the terminal
// height in character rows. Resize returns ErrNotOpen before Connect or after
// Close so an unavailable Channel never receives a stale terminal-size request.
func (s *Core) Resize(cols, rows uint16) error {
	if !s.isOpen() {
		return ErrNotOpen
	}
	resizer, ok := s.channel.(channel.Resizer)
	if !ok {
		return channel.ErrResizeUnsupported
	}
	return resizer.Resize(cols, rows)
}

// isOpen reads the lifecycle state without holding Core's lock during a
// potentially blocking Channel operation. The state may change immediately
// after this method returns, so callers must not use it as a concurrency guard.
func (s *Core) isOpen() bool {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.state == StateOpen
}

// readLoop is the only goroutine that calls Channel.Read for a Core. It
// publishes received bytes before handling an accompanying error because an
// io.Reader may validly return both data and an error in the same call.
func (s *Core) readLoop(done chan<- struct{}) {
	defer close(done)

	buffer := make([]byte, readerBufferSize)
	for {
		n, err := s.channel.Read(buffer)
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

// Close releases the underlying Channel, output reader, and receive buffer.
//
// Close first wakes output consumers, then closes Channel to unblock its
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
	var err error
	if s.channel != nil {
		err = s.channel.Close()
	}
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
