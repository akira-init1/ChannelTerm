package session

import (
	"context"
	"errors"
	"io"
	"sync"
	"time"
)

var (
	// ErrInvalidActivityBufferCapacity is returned when an activity buffer has no capacity.
	ErrInvalidActivityBufferCapacity = errors.New("activity buffer capacity must be positive")
	// ErrInvalidActivityReadLimit is returned when an activity read does not request events.
	ErrInvalidActivityReadLimit = errors.New("activity read limit must be positive")
)

// ActivityCursor identifies the next activity event a consumer expects.
//
// Cursors are monotonically increasing for one Session. They are independent
// from OutputCursor because activity metadata and stream output have separate
// retention, overflow, and consumer lifecycles.
type ActivityCursor uint64

// Operation identifies the action represented by a SessionEvent.
type Operation string

const (
	// OperationWrite records bytes successfully passed to a Session Channel.
	OperationWrite Operation = "write"
)

// SessionEvent records a completed stream operation and its internal source.
//
// Timestamp is the local time at which Core began executing the operation. Data
// is a copied snapshot of the bytes actually written; it may be a prefix of the
// original request when a Channel reports a partial write followed by an error.
type SessionEvent struct {
	Timestamp time.Time
	Actor     Actor
	Operation Operation
	Data      []byte
}

// ActivityChunk is a bounded activity-event snapshot for one consumer.
type ActivityChunk struct {
	// Events contains no more events than the caller requested.
	Events []SessionEvent
	// Next is the cursor the caller supplies to its next ReadActivity call.
	Next ActivityCursor
	// Dropped reports that the requested cursor was older than retained activity.
	Dropped bool
}

// activityBuffer retains recent Session events in a fixed-size circular array.
//
// It uses a replacement notification channel rather than sending directly to
// consumers. Consequently, appending activity never waits for a slow reader.
type activityBuffer struct {
	mu sync.Mutex

	events []SessionEvent
	start  int
	size   int
	base   ActivityCursor

	notify chan struct{}
	closed bool
	err    error
}

// newActivityBuffer allocates fixed event slots for the lifetime of a Session.
func newActivityBuffer(capacity int) (*activityBuffer, error) {
	if capacity <= 0 {
		return nil, ErrInvalidActivityBufferCapacity
	}
	return &activityBuffer{events: make([]SessionEvent, capacity), notify: make(chan struct{})}, nil
}

// append records one completed operation. It clones Data while holding the
// buffer lock so callers can safely reuse the original WriteRequest storage.
func (b *activityBuffer) append(event SessionEvent) {
	b.mu.Lock()
	defer b.mu.Unlock()
	if b.closed || len(b.events) == 0 {
		return
	}

	event.Data = append([]byte(nil), event.Data...)
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

// readActivity waits for events at next without retaining the buffer lock.
func (b *activityBuffer) readActivity(ctx context.Context, next ActivityCursor, limit int) (ActivityChunk, error) {
	if limit <= 0 {
		return ActivityChunk{}, ErrInvalidActivityReadLimit
	}
	for {
		chunk, notify, closed, err := b.readChunk(next, limit)
		if len(chunk.Events) > 0 || chunk.Dropped {
			return chunk, nil
		}
		if closed {
			if err != nil {
				return ActivityChunk{}, err
			}
			return ActivityChunk{}, io.EOF
		}
		select {
		case <-ctx.Done():
			return ActivityChunk{}, ctx.Err()
		case <-notify:
		}
	}
}

// readRecent returns the newest retained events and always advances Next to the
// current tail, including when no events have been produced yet.
func (b *activityBuffer) readRecent(limit int) (ActivityChunk, error) {
	if limit <= 0 {
		return ActivityChunk{}, ErrInvalidActivityReadLimit
	}
	b.mu.Lock()
	defer b.mu.Unlock()
	if len(b.events) == 0 {
		return ActivityChunk{}, io.EOF
	}
	count := min(limit, b.size)
	start := b.base + ActivityCursor(b.size-count)
	return b.copyLocked(start, count, false), nil
}

// readChunk returns a self-consistent snapshot or the notification channel for
// the next state transition. Its caller waits after releasing b.mu.
func (b *activityBuffer) readChunk(next ActivityCursor, limit int) (ActivityChunk, <-chan struct{}, bool, error) {
	b.mu.Lock()
	defer b.mu.Unlock()
	if len(b.events) == 0 {
		return ActivityChunk{}, b.notify, true, b.err
	}
	dropped := next < b.base
	if dropped {
		next = b.base
	}
	end := b.base + ActivityCursor(b.size)
	if next < end {
		return b.copyLocked(next, min(limit, int(end-next)), dropped), nil, false, nil
	}
	return ActivityChunk{}, b.notify, b.closed, b.err
}

// copyLocked produces caller-owned event and payload copies from the circular
// store. The caller holds b.mu, keeping both slot order and payload ownership stable.
func (b *activityBuffer) copyLocked(start ActivityCursor, count int, dropped bool) ActivityChunk {
	events := make([]SessionEvent, count)
	for index := range events {
		source := b.events[(b.start+int(start-b.base)+index)%len(b.events)]
		source.Data = append([]byte(nil), source.Data...)
		events[index] = source
	}
	return ActivityChunk{Events: events, Next: start + ActivityCursor(count), Dropped: dropped}
}

// close wakes all readers with the final stream condition while retaining
// already-recorded events for readers whose cursor has not reached the tail.
func (b *activityBuffer) close(err error) {
	b.mu.Lock()
	defer b.mu.Unlock()
	if b.closed {
		return
	}
	b.closed = true
	b.err = err
	b.signalLocked()
}

// release drops retained event and payload storage after Session shutdown.
func (b *activityBuffer) release() {
	b.mu.Lock()
	defer b.mu.Unlock()
	b.events = nil
	b.start = 0
	b.size = 0
	b.signalLocked()
}

// signalLocked wakes every current waiter by closing one notification generation.
func (b *activityBuffer) signalLocked() {
	close(b.notify)
	b.notify = make(chan struct{})
}
