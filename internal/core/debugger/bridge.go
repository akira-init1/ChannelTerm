package debugger

import (
	"context"
	"errors"
	"io"
	"net"
	"sync"
	"time"
)

// ErrDataLost means the Session overwrote bytes before the relay consumed them.
var ErrDataLost = errors.New("debug stream data lost; debugging cannot continue")

// Stream is the minimal cursor-based Session service used by the RSP relay.
// Read must return an independent bounded copy and report any overwritten bytes.
// Write must use the existing Application lease and Session write serialization.
type Stream interface {
	Read(context.Context) (data []byte, dropped bool, err error)
	Write(context.Context, []byte) error
}

// Relay forwards bytes unchanged in both directions, including binary packets,
// acknowledgements and interrupts. Empty reads are not EOF. Packet framing is
// observed for ACK evidence but never rewritten. It does not negotiate no-ack
// mode or generate acknowledgements. Both directions drain before it returns.
// A failed direction closes the socket and cancels the other Session read.
// Blocked Session writes must be released by Application closing the Session.
func Relay(ctx context.Context, conn net.Conn, stream Stream) error {
	_, err := RelayFinalACK(ctx, conn, stream)
	return err
}

// RelayResult records ACK evidence after both relay directions have drained.
type RelayResult struct {
	// ExitACK confirms a checksum-valid W/X packet was acknowledged.
	ExitACK bool
	// LastACK confirms the most recent complete target packet was acknowledged.
	LastACK bool
}

// RelayFinalACK also reports whether GDB acknowledged a checksum-valid W/X exit
// packet. PTYs cannot convey socket EOF; Application uses this evidence before
// ending a gdbserver that is waiting for stdio EOF after the inferior exited.
// It observes framing only: all original bytes still pass through unchanged.
func RelayFinalACK(ctx context.Context, conn net.Conn, stream Stream) (RelayResult, error) {
	tracker := &exitACKTracker{}
	ctx, cancel := context.WithCancel(ctx)
	defer cancel()
	var wg sync.WaitGroup
	result := make(chan error, 2)
	run := func(fn func() error) { wg.Add(1); go func() { defer wg.Done(); result <- fn() }() }
	go func() { <-ctx.Done(); _ = conn.Close() }()
	run(func() error {
		for {
			data, dropped, e := stream.Read(ctx)
			if dropped {
				return ErrDataLost
			}
			if len(data) > 0 {
				tracker.target(data)
				if _, w := writeAll(conn, data); w != nil {
					return w
				}
			}
			if e != nil {
				return e
			}
			if len(data) == 0 {
				select {
				case <-ctx.Done():
					return ctx.Err()
				case <-time.After(time.Millisecond):
				}
			}
		}
	})
	run(func() error {
		b := make([]byte, 32*1024)
		for {
			n, e := conn.Read(b)
			if n > 0 {
				if w := stream.Write(ctx, b[:n]); w != nil {
					return w
				}
				tracker.client(b[:n])
			}
			if e != nil {
				return e
			}
		}
	})
	e := <-result
	cancel()
	_ = conn.Close()
	wg.Wait()
	if errors.Is(e, io.EOF) {
		return tracker.result(), nil
	}
	return tracker.result(), e
}
func writeAll(w io.Writer, b []byte) (int, error) {
	total := 0
	for len(b) > 0 {
		n, e := w.Write(b)
		total += n
		b = b[n:]
		if e != nil {
			return total, e
		}
		if n == 0 {
			return total, io.ErrShortWrite
		}
	}
	return total, nil
}

// exitACKTracker distinguishes standalone ACKs from binary payload bytes and
// validates checksums across arbitrary fragments without decoding or retaining
// complete packets. ACK mode is forced by this backend before connecting.
type exitACKTracker struct {
	mu          sync.Mutex
	targetFrame rspFrame
	clientFrame rspFrame
	pending     bool
	pendingExit bool
	lastACK     bool
	ack         bool
}
type rspFrame struct {
	phase  int
	sum    byte
	check  byte
	first  byte
	length int
}

func hexNibble(c byte) (byte, bool) {
	if c >= '0' && c <= '9' {
		return c - '0', true
	}
	if c >= 'a' && c <= 'f' {
		return c - 'a' + 10, true
	}
	if c >= 'A' && c <= 'F' {
		return c - 'A' + 10, true
	}
	return 0, false
}
func (f *rspFrame) feed(c byte) (bool, bool) {
	switch f.phase {
	case 0:
		if c == '$' || c == '%' {
			f.phase = 1
			f.sum = 0
			f.first = 0
			f.length = 0
		}
		return false, false
	case 1:
		if c == '#' {
			f.phase = 2
			return false, false
		}
		if f.length == 0 {
			f.first = c
		}
		f.length++
		f.sum += c
	case 2:
		n, ok := hexNibble(c)
		if !ok {
			f.phase = 0
			return false, false
		}
		f.check = n << 4
		f.phase = 3
	case 3:
		n, ok := hexNibble(c)
		f.phase = 0
		return true, ok && f.check|n == f.sum
	}
	return false, false
}
func (t *exitACKTracker) target(data []byte) {
	t.mu.Lock()
	defer t.mu.Unlock()
	for _, b := range data {
		complete, valid := t.targetFrame.feed(b)
		if complete && valid {
			t.pending = true
			t.pendingExit = t.targetFrame.first == 'W' || t.targetFrame.first == 'X'
			t.lastACK = false
			if t.pendingExit {
				t.ack = false
			}
		}
	}
}
func (t *exitACKTracker) client(data []byte) {
	t.mu.Lock()
	defer t.mu.Unlock()
	for _, b := range data {
		if t.clientFrame.phase == 0 && b == '+' && t.pending {
			t.pending = false
			t.lastACK = true
			if t.pendingExit {
				t.ack = true
			}
		}
		t.clientFrame.feed(b)
	}
}
func (t *exitACKTracker) acknowledged() bool { t.mu.Lock(); defer t.mu.Unlock(); return t.ack }

func (t *exitACKTracker) result() RelayResult {
	t.mu.Lock()
	defer t.mu.Unlock()
	return RelayResult{ExitACK: t.ack, LastACK: t.lastACK}
}
