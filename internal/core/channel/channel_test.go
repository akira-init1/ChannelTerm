package channel

import (
	"errors"
	"io"
	"sync"
	"testing"
)

func TestStreamReadWriteCloseLifecycle(t *testing.T) {
	connection := newFakeStream([]byte("ready"))
	stream, err := NewStream(connection)
	if err != nil {
		t.Fatalf("NewStream() error = %v", err)
	}
	if got := stream.State(); got != StateOpen {
		t.Fatalf("State() = %s, want %s", got, StateOpen)
	}

	buffer := make([]byte, 16)
	n, err := stream.Read(buffer)
	if err != nil {
		t.Fatalf("Read() error = %v", err)
	}
	if got := string(buffer[:n]); got != "ready" {
		t.Errorf("Read() = %q, want ready", got)
	}
	if n, err := stream.Write([]byte("status\n")); err != nil || n != len("status\n") {
		t.Fatalf("Write() = (%d, %v), want (%d, nil)", n, err, len("status\n"))
	}
	if got := string(connection.writtenData()); got != "status\n" {
		t.Errorf("underlying writes = %q, want status\\n", got)
	}

	for range 2 {
		if err := stream.Close(); err != nil {
			t.Fatalf("Close() error = %v", err)
		}
	}
	if got := stream.State(); got != StateClosed {
		t.Errorf("State() = %s, want %s", got, StateClosed)
	}
	if got := connection.closeCount(); got != 1 {
		t.Errorf("underlying Close() calls = %d, want 1", got)
	}
	if _, err := stream.Read(buffer); !errors.Is(err, ErrNotOpen) {
		t.Errorf("Read() after Close error = %v, want ErrNotOpen", err)
	}
	if _, err := stream.Write([]byte("again")); !errors.Is(err, ErrNotOpen) {
		t.Errorf("Write() after Close error = %v, want ErrNotOpen", err)
	}
}

func TestStreamFailureUpdatesLifecycleAndStillCloses(t *testing.T) {
	connection := newFakeStream(nil)
	connection.readErr = io.ErrUnexpectedEOF
	stream, err := NewStream(connection)
	if err != nil {
		t.Fatalf("NewStream() error = %v", err)
	}

	if _, err := stream.Read(make([]byte, 1)); !errors.Is(err, io.ErrUnexpectedEOF) {
		t.Fatalf("Read() error = %v, want io.ErrUnexpectedEOF", err)
	}
	if got := stream.State(); got != StateFailed {
		t.Errorf("State() = %s, want %s", got, StateFailed)
	}
	if err := stream.Close(); err != nil {
		t.Fatalf("Close() error = %v", err)
	}
	if got := connection.closeCount(); got != 1 {
		t.Errorf("underlying Close() calls = %d, want 1", got)
	}
}

func TestStreamWriteErrorDoesNotAssumeChannelFailure(t *testing.T) {
	connection := newFakeStream(nil)
	connection.writeErr = io.ErrShortWrite
	stream, err := NewStream(connection)
	if err != nil {
		t.Fatalf("NewStream() error = %v", err)
	}
	defer stream.Close()

	if _, err := stream.Write([]byte("status")); !errors.Is(err, io.ErrShortWrite) {
		t.Fatalf("Write() error = %v, want io.ErrShortWrite", err)
	}
	if got := stream.State(); got != StateOpen {
		t.Errorf("State() = %s, want %s after an unclassified write error", got, StateOpen)
	}
}

func TestNewStreamRejectsNil(t *testing.T) {
	var typedNil *fakeStream
	for _, value := range []io.ReadWriteCloser{nil, typedNil} {
		if _, err := NewStream(value); !errors.Is(err, ErrNilStream) {
			t.Errorf("NewStream(%v) error = %v, want ErrNilStream", value, err)
		}
	}
}

type fakeStream struct {
	mu sync.Mutex

	readData []byte
	readErr  error
	writeErr error
	written  []byte
	closes   int
}

func newFakeStream(readData []byte) *fakeStream {
	return &fakeStream{readData: append([]byte(nil), readData...)}
}

func (s *fakeStream) Read(p []byte) (int, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if len(s.readData) == 0 {
		return 0, s.readErr
	}
	n := copy(p, s.readData)
	s.readData = s.readData[n:]
	return n, nil
}

func (s *fakeStream) Write(p []byte) (int, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.written = append(s.written, p...)
	return len(p), s.writeErr
}

func (s *fakeStream) Close() error {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.closes++
	return nil
}

func (s *fakeStream) writtenData() []byte {
	s.mu.Lock()
	defer s.mu.Unlock()
	return append([]byte(nil), s.written...)
}

func (s *fakeStream) closeCount() int {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.closes
}
