package session

import (
	"context"
	"testing"
)

func BenchmarkReceiveBufferAppendContinuous(b *testing.B) {
	buffer, err := newReceiveBuffer(DefaultReceiveBufferSize)
	if err != nil {
		b.Fatalf("newReceiveBuffer() error = %v", err)
	}
	payload := make([]byte, 4*1024)
	b.SetBytes(int64(len(payload)))
	b.ReportAllocs()
	b.ResetTimer()
	for range b.N {
		buffer.append(payload)
	}
}

func BenchmarkReceiveBufferAppendOverwrite(b *testing.B) {
	const capacity = 64 * 1024
	buffer, err := newReceiveBuffer(capacity)
	if err != nil {
		b.Fatalf("newReceiveBuffer() error = %v", err)
	}
	payload := make([]byte, 4*1024)
	buffer.append(make([]byte, capacity))
	b.SetBytes(int64(len(payload)))
	b.ReportAllocs()
	b.ResetTimer()
	for range b.N {
		buffer.append(payload)
	}
}

func BenchmarkReceiveBufferReadRecent(b *testing.B) {
	buffer, err := newReceiveBuffer(DefaultReceiveBufferSize)
	if err != nil {
		b.Fatalf("newReceiveBuffer() error = %v", err)
	}
	buffer.append(make([]byte, DefaultAIReadLimit))
	b.SetBytes(DefaultAIReadLimit)
	b.ReportAllocs()
	b.ResetTimer()
	for range b.N {
		if _, err := buffer.readRecent(DefaultAIReadLimit); err != nil {
			b.Fatalf("readRecent() error = %v", err)
		}
	}
}

func BenchmarkReceiveBufferReadOutput(b *testing.B) {
	buffer, err := newReceiveBuffer(DefaultReceiveBufferSize)
	if err != nil {
		b.Fatalf("newReceiveBuffer() error = %v", err)
	}
	payload := make([]byte, 4*1024)
	buffer.append(payload)
	b.SetBytes(int64(len(payload)))
	b.ReportAllocs()
	b.ResetTimer()
	for range b.N {
		chunk, err := buffer.readOutput(context.Background(), 0, len(payload))
		if err != nil {
			b.Fatalf("readOutput() error = %v", err)
		}
		if chunk.Next != OutputCursor(len(payload)) {
			b.Fatalf("chunk.Next = %d, want %d", chunk.Next, len(payload))
		}
	}
}
