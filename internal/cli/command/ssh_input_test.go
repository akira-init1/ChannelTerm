package command

import (
	"context"
	"errors"
	"io"
	"os"
	"sync"
	"testing"
	"time"

	"github.com/akira-init1/ChannelTerm/internal/core/session"
)

// blockedSSHInput models a write that only closing its owning connection can
// interrupt. This proves local escape does not depend on a write timeout.
type blockedSSHInput struct {
	*fakeAttachSession
	started, closed, returned chan struct{}
	once                      sync.Once
}

type rejectFirstSSHInput struct {
	*fakeAttachSession
	rejected bool
}

// Write simulates a recoverable lease rejection followed by an accepted write.
func (s *rejectFirstSSHInput) Write(r session.WriteRequest) (int, error) {
	if !s.rejected {
		s.rejected = true
		return 0, errors.New("fixture session busy")
	}
	return s.fakeAttachSession.Write(r)
}

func TestSSHOwnerRetainsConnectionAfterWriteRejection(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	reader, writer := io.Pipe()
	defer reader.Close()
	defer writer.Close()
	terminal := &rejectFirstSSHInput{fakeAttachSession: &fakeAttachSession{}}
	var output lockedBuffer
	write := func(data []byte) error { _, err := output.Write(data); return err }
	done := make(chan struct{})
	go func() {
		defer close(done)
		forwardSSHOwnerInput(ctx, reader, terminal, write, write, func() error { return nil }, cancel, nil, func() {})
	}()
	writer.Write([]byte("rejected"))
	waitSSHOutput(t, &output, "fixture session busy")
	writer.Write([]byte("accepted"))
	if got := waitForAttachWrite(t, terminal.fakeAttachSession); string(got) != "accepted" {
		t.Fatalf("replayed or lost input: %q", got)
	}
	writer.Write([]byte{0x1d, 'q'})
	select {
	case <-done:
	case <-time.After(3 * time.Second):
		t.Fatal("owner did not exit")
	}
}

func (s *blockedSSHInput) Write(session.WriteRequest) (int, error) {
	s.once.Do(func() { close(s.started) })
	<-s.closed
	close(s.returned)
	return 0, io.ErrClosedPipe
}

type contextualSSHInput struct{ *blockedSSHInput }

// Write must not be used when the contextual API is available.
func (s *contextualSSHInput) Write(session.WriteRequest) (int, error) {
	return 0, errors.New("contextual write API required")
}

// WriteContext models a local MCP request that only its context can release.
func (s *contextualSSHInput) WriteContext(ctx context.Context, _ session.WriteRequest) (int, error) {
	s.once.Do(func() { close(s.started) })
	<-ctx.Done()
	close(s.returned)
	return 0, ctx.Err()
}

func TestSSHOwnerCancelsBlockedInput(t *testing.T) {
	for _, mode := range []string{"escape", "contextual-escape", "context", "eof", "overflow"} {
		t.Run(mode, func(t *testing.T) {
			ctx, cancel := context.WithCancel(context.Background())
			defer cancel()
			reader, writer := io.Pipe()
			defer reader.Close()
			defer writer.Close()
			terminal := &blockedSSHInput{fakeAttachSession: &fakeAttachSession{}, started: make(chan struct{}), closed: make(chan struct{}), returned: make(chan struct{})}
			var attached attachSession = terminal
			if mode == "contextual-escape" {
				attached = &contextualSSHInput{terminal}
			}
			var output lockedBuffer
			write := func(data []byte) error { _, err := output.Write(data); return err }
			done := make(chan struct{})
			go func() {
				defer close(done)
				forwardSSHOwnerInput(ctx, reader, attached, write, write, func() error { return nil }, cancel, (<-chan os.Signal)(nil), func() { close(terminal.closed) })
			}()
			if _, err := writer.Write([]byte("blocked input")); err != nil {
				t.Fatal(err)
			}
			select {
			case <-terminal.started:
			case <-time.After(3 * time.Second):
				t.Fatal("write did not start")
			}
			switch mode {
			case "escape", "contextual-escape":
				writer.Write([]byte{0x1d})
				writer.Write([]byte{'q'})
			case "context":
				cancel()
			case "eof":
				writer.Close()
			case "overflow":
				go func() {
					for range 258 {
						if _, err := writer.Write([]byte("more")); err != nil {
							return
						}
					}
				}()
			}
			select {
			case <-done:
			case <-time.After(3 * time.Second):
				t.Fatal("owner did not exit while write was blocked")
			}
			select {
			case <-terminal.returned:
			default:
				t.Fatal("write worker was not joined")
			}
		})
	}
}
