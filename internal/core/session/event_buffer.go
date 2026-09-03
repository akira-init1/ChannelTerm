package session

import (
	"context"
	"errors"
	"io"
	"slices"
	"sync"
	"time"
)

var (
	// ErrInvalidEventBufferCapacity is returned when an event buffer has no capacity.
	ErrInvalidEventBufferCapacity = errors.New("event buffer capacity must be positive")
	// ErrInvalidEventReadLimit is returned when an event read does not request events.
	ErrInvalidEventReadLimit = errors.New("event read limit must be positive")
)

// EventType identifies a lifecycle or operation transition on a Session.
//
// The defined values are intentionally transport-neutral. Additional values may
// be added without changing the event stream contract.
type EventType string

const (
	// EventSessionCreated records successful Manager registration of a Session.
	EventSessionCreated EventType = "SESSION_CREATED"
	// EventSessionAttached records one client attachment to a Session.
	EventSessionAttached EventType = "SESSION_ATTACHED"
	// EventSessionDetached records one client detachment from a Session.
	EventSessionDetached EventType = "SESSION_DETACHED"
	// EventLeaseAcquired records an exclusive writer lease becoming active.
	EventLeaseAcquired EventType = "LEASE_ACQUIRED"
	// EventLeaseReleased records an exclusive writer lease ending.
	EventLeaseReleased EventType = "LEASE_RELEASED"
	// EventFileTransferStarted records the beginning of a file transfer.
	EventFileTransferStarted EventType = "FILE_TRANSFER_STARTED"
	// EventFileTransferProgress records confirmed file-transfer progress.
	EventFileTransferProgress EventType = "FILE_TRANSFER_PROGRESS"
	// EventFileTransferCompleted records a verified successful file transfer.
	EventFileTransferCompleted EventType = "FILE_TRANSFER_COMPLETED"
	// EventFileTransferFailed records a failed or cancelled file transfer.
	EventFileTransferFailed EventType = "FILE_TRANSFER_FAILED"
)

// EventCursor identifies the next Session event a consumer expects.
//
// Cursors are monotonically increasing for one Session and are independent
// from output and activity cursors. Consumers keep their own cursor, so one
// slow observer never delays or consumes events for another observer.
type EventCursor uint64

// Event is structured Session state that is deliberately separate from raw
// terminal bytes and write activity.
//
// ID is assigned by the owning Session and is monotonic within that Session.
// SessionID is also assigned by the owning Session. Metadata contains only
// JSON-compatible presentation data; lease owner capabilities and terminal
// payload bytes must never be published there.
type Event struct {
	ID        uint64         `json:"id"`
	Timestamp time.Time      `json:"timestamp"`
	SessionID string         `json:"session_id"`
	Type      EventType      `json:"type"`
	Actor     string         `json:"actor"`
	Metadata  map[string]any `json:"metadata,omitempty"`
}

// EventChunk is a bounded event snapshot for one observer.
type EventChunk struct {
	// Events contains no more events than the caller requested.
	Events []Event
	// Next is the cursor the caller supplies to its next ReadEvents call.
	Next EventCursor
	// Dropped reports that the requested cursor was older than retained events.
	Dropped bool
}

// eventBuffer retains recent structured Session events in a fixed-size ring.
// Notification replacement wakes all current waiters without ever sending to a
// subscriber channel, so publication cannot block Session I/O or writers.
type eventBuffer struct {
	mu sync.Mutex

	events []Event
	start  int
	size   int
	base   EventCursor

	notify chan struct{}
	closed bool
	err    error
}

// newEventBuffer allocates fixed event slots for the lifetime of a Session.
func newEventBuffer(capacity int) (*eventBuffer, error) {
	if capacity <= 0 {
		return nil, ErrInvalidEventBufferCapacity
	}
	return &eventBuffer{events: make([]Event, capacity), notify: make(chan struct{})}, nil
}

// append records event without waiting for event consumers.
func (b *eventBuffer) append(event Event) {
	b.mu.Lock()
	defer b.mu.Unlock()
	if b.closed || len(b.events) == 0 {
		return
	}
	event.ID = uint64(b.base) + uint64(b.size)
	event.Timestamp = event.Timestamp.UTC()
	event.Metadata = cloneEventMetadata(event.Metadata)
	if b.size == len(b.events) {
		b.events[b.start] = event
		b.start = (b.start + 1) % len(b.events)
		b.base++
	} else {
		b.events[(b.start+b.size)%len(b.events)] = event
		b.size++
	}
	b.signalLocked()
}

// readEvents waits for events at next without retaining the buffer lock.
func (b *eventBuffer) readEvents(ctx context.Context, next EventCursor, limit int) (EventChunk, error) {
	if limit <= 0 {
		return EventChunk{}, ErrInvalidEventReadLimit
	}
	for {
		chunk, notify, closed, err := b.readChunk(next, limit)
		if len(chunk.Events) > 0 || chunk.Dropped {
			return chunk, nil
		}
		if closed {
			if err != nil {
				return EventChunk{}, err
			}
			return EventChunk{}, io.EOF
		}
		select {
		case <-ctx.Done():
			return EventChunk{}, ctx.Err()
		case <-notify:
		}
	}
}

// readRecent returns the newest retained events and advances Next to the tail.
func (b *eventBuffer) readRecent(limit int) (EventChunk, error) {
	if limit <= 0 {
		return EventChunk{}, ErrInvalidEventReadLimit
	}
	b.mu.Lock()
	defer b.mu.Unlock()
	if len(b.events) == 0 {
		return EventChunk{}, io.EOF
	}
	count := min(limit, b.size)
	start := b.base + EventCursor(b.size-count)
	return b.copyLocked(start, count, false), nil
}

// readChunk returns a snapshot or the current notification generation.
func (b *eventBuffer) readChunk(next EventCursor, limit int) (EventChunk, <-chan struct{}, bool, error) {
	b.mu.Lock()
	defer b.mu.Unlock()
	if len(b.events) == 0 {
		return EventChunk{}, b.notify, true, b.err
	}
	dropped := next < b.base
	if dropped {
		next = b.base
	}
	end := b.base + EventCursor(b.size)
	if next < end {
		return b.copyLocked(next, min(limit, int(end-next)), dropped), nil, false, nil
	}
	return EventChunk{}, b.notify, b.closed, b.err
}

// copyLocked returns caller-owned event and metadata copies.
func (b *eventBuffer) copyLocked(start EventCursor, count int, dropped bool) EventChunk {
	events := make([]Event, count)
	for index := range events {
		source := b.events[(b.start+int(start-b.base)+index)%len(b.events)]
		source.Metadata = cloneEventMetadata(source.Metadata)
		events[index] = source
	}
	return EventChunk{Events: events, Next: start + EventCursor(count), Dropped: dropped}
}

// close wakes readers while retaining events that have not yet been read.
func (b *eventBuffer) close(err error) {
	b.mu.Lock()
	defer b.mu.Unlock()
	if b.closed {
		return
	}
	b.closed = true
	b.err = err
	b.signalLocked()
}

// release drops retained event storage after Session shutdown.
func (b *eventBuffer) release() {
	b.mu.Lock()
	defer b.mu.Unlock()
	b.events = nil
	b.start = 0
	b.size = 0
	b.signalLocked()
}

// signalLocked wakes every current waiter by closing one notification generation.
func (b *eventBuffer) signalLocked() {
	close(b.notify)
	b.notify = make(chan struct{})
}

// cloneEventMetadata keeps the Session-owned event history isolated from
// publisher and observer mutation without imposing a JSON marshal on hot paths.
func cloneEventMetadata(metadata map[string]any) map[string]any {
	if len(metadata) == 0 {
		return nil
	}
	copy := make(map[string]any, len(metadata))
	for key, value := range metadata {
		copy[key] = cloneEventValue(value)
	}
	return copy
}

func cloneEventValue(value any) any {
	switch value := value.(type) {
	case map[string]any:
		return cloneEventMetadata(value)
	case []any:
		result := make([]any, len(value))
		for index := range value {
			result[index] = cloneEventValue(value[index])
		}
		return result
	case []string:
		return slices.Clone(value)
	default:
		return value
	}
}
