package app

import (
	"context"
	"errors"
	"strings"
	"sync"
	"testing"
	"testing/synctest"
	"time"

	"github.com/akira-init1/ChannelTerm/internal/core/session"
)

// Exercise actual file-transfer and shell cleanup through a leased Application
// view. The peer is the existing in-memory protocol simulator, not a device.
func TestFileTransferRecoveryDeterminesConnectionReuse(t *testing.T) {
	for _, scenario := range []string{"completed", "payload-failed-recovered", "peer-failed-recovered", "cancelled-recovered", "bootstrap-write-uncertain", "exit-output-dropped", "exit-protocol-error", "recovery-ack-invalid", "close-rpc-failed"} {
		t.Run(scenario, func(t *testing.T) {
			a, terminal := newSerialTestApplication(t)
			peer := newFileTransferTestSession(nil)
			adapter := &leasedRecoverySession{app: a, terminal: terminal, peer: peer, scenario: scenario}
			if _, err := a.AcquireLease(terminal.ID(), "transfer", LeaseTypeFileTransfer); err != nil {
				t.Fatal(err)
			}
			// Record each admitted write through Application and run the peer
			// simulator from the same callback. A partial payload failure is
			// injected at the client boundary with its confirmed count.
			terminal.onWrite = func(request session.WriteRequest) {
				adapter.count, adapter.writeErr = peer.Write(request)
			}
			if scenario == "payload-failed-recovered" || scenario == "peer-failed-recovered" || scenario == "recovery-ack-invalid" {
				peer.failPayloadOnce = true
			}
			var progress FileTransferProgress
			if scenario == "cancelled-recovered" {
				progress = func(sent, _ int64) error {
					if sent > 0 {
						return context.Canceled
					}
					return nil
				}
			}
			_, err := SendFile(context.Background(), adapter, strings.NewReader("payload"), 7, "/tmp/file", progress)
			uncertain := scenario == "bootstrap-write-uncertain" || scenario == "exit-output-dropped" || scenario == "exit-protocol-error" || scenario == "recovery-ack-invalid" || scenario == "close-rpc-failed"
			if errors.Is(err, ErrFileTransferRecoveryUnconfirmed) != uncertain {
				t.Fatalf("recovery classification: %v", err)
			}
			if scenario == "completed" && err != nil {
				t.Fatal(err)
			}
			if scenario != "completed" && err == nil {
				t.Fatal("injected error was lost")
			}
			if scenario == "cancelled-recovered" && !errors.Is(err, context.Canceled) {
				t.Fatalf("lost cancellation: %v", err)
			}
			if uncertain {
				if adapter.aborts != 1 {
					t.Fatalf("abort calls = %d, want 1", adapter.aborts)
				}
				if scenario == "close-rpc-failed" {
					// The CLI must retain this lease on a failed close request;
					// its defer behavior is separately covered in command tests.
					if _, writeErr := a.WriteSession(context.Background(), terminal.ID(), session.WriteRequest{Actor: session.ActorUser, Data: []byte("blocked")}); !errors.Is(writeErr, ErrSessionBusy) {
						t.Fatalf("failed close lost exclusion: %v", writeErr)
					}
					_ = a.ReleaseLease(terminal.ID(), "transfer") // test cleanup only
					return
				}
				// Even an unconditional legacy defer cannot reopen the removed
				// registration after the recovery abort has completed.
				_ = a.ReleaseLease(terminal.ID(), "transfer")
				if terminal.State() != session.StateClosed {
					t.Fatal("unconfirmed recovery retained Session")
				}
				if _, writeErr := a.WriteSession(context.Background(), terminal.ID(), session.WriteRequest{Actor: session.ActorUser, Data: []byte("blocked")}); !errors.Is(writeErr, ErrSessionNotFound) {
					t.Fatalf("write after recovery failure: %v", writeErr)
				}
			} else {
				if adapter.aborts != 0 || terminal.State() != session.StateOpen || peer.shellActive {
					t.Fatal("confirmed recovery did not preserve usable local connection")
				}
				if err := a.ReleaseLease(terminal.ID(), "transfer"); err != nil {
					t.Fatal(err)
				}
				terminal.onWrite = nil
				if _, err := a.WriteSession(context.Background(), terminal.ID(), session.WriteRequest{Actor: session.ActorUser, Data: []byte("next\n")}); err != nil {
					t.Fatal(err)
				}
			}
		})
	}
}

type leasedRecoverySession struct {
	app      *Application
	terminal *fakeConnectedSession
	peer     *fileTransferTestSession
	scenario string
	closing  bool
	count    int
	writeErr error
	aborts   int
}

func (s *leasedRecoverySession) ReadRecent(ctx context.Context, n int) (session.OutputChunk, error) {
	return s.peer.ReadRecent(ctx, n)
}

func (s *leasedRecoverySession) ReadOutput(ctx context.Context, cursor session.OutputCursor, n int) (session.OutputChunk, error) {
	chunk, err := s.peer.ReadOutput(ctx, cursor, n)
	if s.closing {
		switch s.scenario {
		case "exit-output-dropped", "close-rpc-failed":
			chunk.Dropped = true
		case "exit-protocol-error":
			return session.OutputChunk{}, ErrFileTransferProtocol
		}
	}
	if s.scenario == "recovery-ack-invalid" {
		chunk.Data = []byte(strings.ReplaceAll(string(chunk.Data), ":ACK:7", ":ACK:wrong"))
	}
	if s.scenario == "peer-failed-recovered" {
		chunk.Data = []byte(strings.ReplaceAll(string(chunk.Data), ":ACK:7", ":ERROR:write"))
	}
	return chunk, err
}

func (s *leasedRecoverySession) Write(request session.WriteRequest) (int, error) {
	return s.WriteContext(context.Background(), request)
}

func (s *leasedRecoverySession) WriteContext(ctx context.Context, request session.WriteRequest) (int, error) {
	s.closing = strings.Contains(string(request.Data), fileTransferShellCloseCommand())
	request.Actor = session.ActorSystem
	if s.scenario == "bootstrap-write-uncertain" {
		request.Data = request.Data[:12]
		n, err := s.app.WriteSessionWithLease(ctx, s.terminal.ID(), "transfer", request)
		return n, errors.Join(err, errors.New("injected partial bootstrap write"))
	}
	if _, err := s.app.WriteSessionWithLease(ctx, s.terminal.ID(), "transfer", request); err != nil {
		return 0, err
	}
	return s.count, s.writeErr
}

func (s *leasedRecoverySession) AbortFileTransferRecovery(context.Context) error {
	s.aborts++
	if s.scenario == "close-rpc-failed" {
		return errors.New("injected close RPC failure")
	}
	_, err := s.app.CloseSession(s.terminal.ID())
	return err
}

func TestFileTransferRecoveryClosesBlockedHeartbeatWithoutWriteLock(t *testing.T) {
	synctest.Test(t, func(t *testing.T) {
		peer := &blockedRecoveryHeartbeatSession{fileTransferTestSession: newFileTransferTestSession(nil), entered: make(chan struct{}), closed: make(chan struct{})}
		shell := &shellFileTransferSession{terminal: peer, started: true, idle: true, heartbeatOn: true, stop: make(chan struct{}), done: make(chan struct{})}
		go func() { shell.heartbeat(); close(shell.done) }()
		<-peer.entered
		ctx, cancel := context.WithTimeout(context.Background(), time.Second)
		defer cancel()
		err := shell.close(ctx)
		if !errors.Is(err, ErrFileTransferRecoveryUnconfirmed) || !errors.Is(err, context.DeadlineExceeded) {
			t.Fatalf("blocked heartbeat recovery: %v", err)
		}
		<-shell.done
		if peer.aborts != 1 {
			t.Fatalf("abort calls = %d", peer.aborts)
		}
		if err := shell.close(context.Background()); !errors.Is(err, ErrFileTransferRecoveryUnconfirmed) {
			t.Fatalf("repeated cleanup forgot failure: %v", err)
		}
		if _, err := shell.Write(session.WriteRequest{Actor: session.ActorSystem, Data: []byte("late cleanup")}); !errors.Is(err, ErrFileTransferRecoveryUnconfirmed) {
			t.Fatalf("late cleanup write: %v", err)
		}
		if peer.aborts != 1 {
			t.Fatal("repeated cleanup retried closure")
		}
	})
}

type blockedRecoveryHeartbeatSession struct {
	*fileTransferTestSession
	entered chan struct{}
	closed  chan struct{}
	once    sync.Once
	aborts  int
}

// WriteContext models a Channel write that only Close can interrupt.
func (s *blockedRecoveryHeartbeatSession) WriteContext(context.Context, session.WriteRequest) (int, error) {
	close(s.entered)
	<-s.closed
	return 0, session.ErrNotOpen
}

// AbortFileTransferRecovery unblocks the simulated Channel without writeMu.
func (s *blockedRecoveryHeartbeatSession) AbortFileTransferRecovery(context.Context) error {
	s.aborts++
	s.once.Do(func() { close(s.closed) })
	return nil
}
