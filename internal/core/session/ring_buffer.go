package session

import (
	"context"
	"errors"
	"io"
	"sync"
)

var (
	// ErrInvalidBufferCapacity is returned when a receive buffer has no capacity.
	ErrInvalidBufferCapacity = errors.New("receive buffer capacity must be positive")
	// ErrInvalidReadLimit is returned when a chunk read does not request bytes.
	ErrInvalidReadLimit = errors.New("output read limit must be positive")
)

// OutputCursor identifies the next byte a consumer expects from a Session.
//
// Cursors are monotonically increasing for one Session. They are not offsets
// into the current Ring Buffer because old bytes can be overwritten.
type OutputCursor uint64

// OutputChunk is a bounded copy of terminal output retained by a Session.
type OutputChunk struct {
	// Data contains no more bytes than the read limit requested by the caller.
	Data []byte
	// Next is the cursor the caller supplies to its next ReadOutput call.
	Next OutputCursor
	// Dropped reports that the requested cursor was older than retained output.
	Dropped bool
}

// receiveBuffer retains the newest fixed-capacity terminal output.
//
// It replaces notify whenever data arrives or the buffer closes. Readers capture
// that channel while holding mu and wait without the lock, so a slow consumer
// never prevents the Transport reader from appending new output.
type receiveBuffer struct {
	mu sync.Mutex

	data  []byte
	start int
	size  int
	base  OutputCursor

	notify chan struct{}
	closed bool
	err    error
}

// newReceiveBuffer allocates the fixed backing storage once so receive-side
// memory remains bounded for the lifetime of a Session.
func newReceiveBuffer(capacity int) (*receiveBuffer, error) {
	if capacity <= 0 {
		return nil, ErrInvalidBufferCapacity
	}
	return &receiveBuffer{
		data:   make([]byte, capacity),
		notify: make(chan struct{}),
	}, nil
}

// append writes p into the circular storage while advancing base for every
// overwritten byte. It signals after mutations so readers never wait for data
// that is already retained.
func (b *receiveBuffer) append(p []byte) {
	if len(p) == 0 {
		return
	}

	b.mu.Lock()
	defer b.mu.Unlock()
	if b.closed || len(b.data) == 0 {
		return
	}

	capacity := len(b.data)
	end := b.base + OutputCursor(b.size)
	if len(p) >= capacity {
		// Only the newest capacity bytes survive. The absolute cursor still moves
		// by every input byte, including the overwritten prefix.
		copy(b.data, p[len(p)-capacity:])
		b.start = 0
		b.size = capacity
		b.base = end + OutputCursor(len(p)-capacity)
		b.signalLocked()
		return
	}

	writeAt := (b.start + b.size) % capacity
	first := min(len(p), capacity-writeAt)
	copy(b.data[writeAt:writeAt+first], p[:first])
	copy(b.data[:len(p)-first], p[first:])

	overwritten := max(0, b.size+len(p)-capacity)
	b.start = (b.start + overwritten) % capacity
	b.base += OutputCursor(overwritten)
	b.size = min(capacity, b.size+len(p))
	b.signalLocked()
}

// readOutput waits for data at next without holding mu. Replacing notify after
// each state change avoids missed wakeups between inspecting the buffer and
// blocking on the notification channel.
func (b *receiveBuffer) readOutput(ctx context.Context, next OutputCursor, limit int) (OutputChunk, error) {
	if limit <= 0 {
		return OutputChunk{}, ErrInvalidReadLimit
	}

	for {
		chunk, notify, closed, err := b.readChunk(next, limit)
		if len(chunk.Data) > 0 || chunk.Dropped {
			return chunk, nil
		}
		if closed {
			if err != nil {
				return OutputChunk{}, err
			}
			return OutputChunk{}, io.EOF
		}

		select {
		case <-ctx.Done():
			return OutputChunk{}, ctx.Err()
		case <-notify:
		}
	}
}

// readRecent returns a snapshot of the newest retained bytes without waiting
// for future output. It shares copyLocked so returned data never aliases the
// mutable circular buffer.
func (b *receiveBuffer) readRecent(limit int) (OutputChunk, error) {
	if limit <= 0 {
		return OutputChunk{}, ErrInvalidReadLimit
	}

	b.mu.Lock()
	defer b.mu.Unlock()
	if len(b.data) == 0 {
		return OutputChunk{}, io.EOF
	}

	count := min(limit, b.size)
	start := b.base + OutputCursor(b.size-count)
	return b.copyLocked(start, count, false), nil
}

// readChunk examines one consistent buffer state and returns either available
// output or the notification channel that will change that state. It must keep
// the channel and closed flag from the same lock acquisition to avoid races.
func (b *receiveBuffer) readChunk(next OutputCursor, limit int) (OutputChunk, <-chan struct{}, bool, error) {
	b.mu.Lock()
	defer b.mu.Unlock()

	if len(b.data) == 0 {
		return OutputChunk{}, b.notify, true, b.err
	}

	dropped := next < b.base
	if dropped {
		next = b.base
	}
	end := b.base + OutputCursor(b.size)
	if next < end {
		count := min(limit, int(end-next))
		return b.copyLocked(next, count, dropped), nil, false, nil
	}
	return OutputChunk{}, b.notify, b.closed, b.err
}

// copyLocked creates a linear caller-owned snapshot from possibly wrapped
// circular storage. Its caller holds mu so start remains valid during both copy
// operations.
func (b *receiveBuffer) copyLocked(start OutputCursor, count int, dropped bool) OutputChunk {
	data := make([]byte, count)
	offset := int(start - b.base)
	readAt := (b.start + offset) % len(b.data)
	first := min(count, len(b.data)-readAt)
	copy(data, b.data[readAt:readAt+first])
	copy(data[first:], b.data[:count-first])
	return OutputChunk{
		Data:    data,
		Next:    start + OutputCursor(count),
		Dropped: dropped,
	}
}

// close records the terminal end condition once and wakes all pending readers.
// The first error is retained because later cleanup must not hide the original
// transport failure.
func (b *receiveBuffer) close(err error) {
	b.mu.Lock()
	defer b.mu.Unlock()
	if b.closed {
		return
	}
	b.closed = true
	b.err = err
	b.signalLocked()
}

// release drops the backing array after the reader has stopped so a closed
// Session does not retain its full receive capacity in memory.
func (b *receiveBuffer) release() {
	b.mu.Lock()
	defer b.mu.Unlock()
	b.data = nil
	b.start = 0
	b.size = 0
	b.signalLocked()
}

// signalLocked closes the current generation before publishing a new channel.
// Callers already hold mu, which prevents a reader from observing a channel
// that belongs to a different buffer state.
func (b *receiveBuffer) signalLocked() {
	close(b.notify)
	b.notify = make(chan struct{})
}
