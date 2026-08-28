package session

import (
	"context"
	"sync"
	"testing"
	"time"
)

func TestReceiveBufferReadOutput(t *testing.T) {
	buffer, err := newReceiveBuffer(8)
	if err != nil {
		t.Fatalf("newReceiveBuffer() error = %v", err)
	}
	buffer.append([]byte("hello"))
	chunk, err := buffer.readOutput(context.Background(), 0, 3)
	if err != nil {
		t.Fatalf("readOutput() error = %v", err)
	}
	if got := string(chunk.Data); got != "hel" || chunk.Next != 3 || chunk.Dropped {
		t.Errorf("chunk = %+v, want Data=hel Next=3 Dropped=false", chunk)
	}
	chunk, err = buffer.readOutput(context.Background(), chunk.Next, 3)
	if err != nil {
		t.Fatalf("second readOutput() error = %v", err)
	}
	if got := string(chunk.Data); got != "lo" || chunk.Next != 5 {
		t.Errorf("chunk = %+v, want Data=lo Next=5", chunk)
	}
}

func TestReceiveBufferWrapsAround(t *testing.T) {
	buffer, _ := newReceiveBuffer(5)
	buffer.append([]byte("abcd"))
	buffer.append([]byte("ef"))
	chunk, err := buffer.readRecent(5)
	if err != nil {
		t.Fatalf("readRecent() error = %v", err)
	}
	if got := string(chunk.Data); got != "bcdef" {
		t.Errorf("ReadRecent() = %q, want %q", got, "bcdef")
	}
}

func TestReceiveBufferOverwritesOldOutput(t *testing.T) {
	buffer, _ := newReceiveBuffer(4)
	buffer.append([]byte("abcdef"))
	chunk, err := buffer.readOutput(context.Background(), 0, 4)
	if err != nil {
		t.Fatalf("readOutput() error = %v", err)
	}
	if got := string(chunk.Data); got != "cdef" || !chunk.Dropped || chunk.Next != 6 {
		t.Errorf("chunk = %+v, want Data=cdef Next=6 Dropped=true", chunk)
	}
}

func TestReceiveBufferKeepsTailOfLargeWrite(t *testing.T) {
	buffer, _ := newReceiveBuffer(4)
	buffer.append([]byte("0123456789"))
	chunk, err := buffer.readRecent(16)
	if err != nil {
		t.Fatalf("readRecent() error = %v", err)
	}
	if got := string(chunk.Data); got != "6789" || chunk.Next != 10 {
		t.Errorf("chunk = %+v, want Data=6789 Next=10", chunk)
	}
}

func TestReceiveBufferConcurrentReadWrite(t *testing.T) {
	buffer, _ := newReceiveBuffer(1024)
	var writers sync.WaitGroup
	for range 4 {
		writers.Add(1)
		go func() {
			defer writers.Done()
			for range 1000 {
				buffer.append([]byte("output"))
			}
		}()
	}

	var readers sync.WaitGroup
	for range 4 {
		readers.Add(1)
		go func() {
			defer readers.Done()
			for range 1000 {
				if _, err := buffer.readRecent(64); err != nil {
					t.Errorf("readRecent() error = %v", err)
					return
				}
			}
		}()
	}
	writers.Wait()
	readers.Wait()
}

func TestReceiveBufferReadOutputHonorsContext(t *testing.T) {
	buffer, _ := newReceiveBuffer(8)
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Millisecond)
	defer cancel()
	if _, err := buffer.readOutput(ctx, 0, 1); err == nil {
		t.Error("readOutput() error = nil, want context deadline error")
	}
}
