package app

import (
	"context"
	"errors"
	"sync"
	"testing"
	"testing/synctest"
	"time"

	"github.com/akira-init1/ChannelTerm/internal/core/session"
)

// Every phase waits for peer input with no Channel write in flight. Pause the
// expiry callback to exercise the exclusion window before asynchronous Close.
func TestFileTransferExpiryExcludesWritersWhileWaitingForPeer(t *testing.T) {
	for _, phase := range []string{"READY", "ACK", "receive-data", "client-disconnected"} {
		t.Run(phase, func(t *testing.T) {
			synctest.Test(t, func(t *testing.T) {
				a, terminal := newSerialTestApplication(t)
				a.leases.ttl = time.Second
				closing, proceed := make(chan struct{}), make(chan struct{})
				a.leases.onExpired = func(lease SessionLease, abort bool) {
					if !abort {
						t.Error("file-transfer expiry did not require closure")
					}
					close(closing)
					<-proceed
					a.publishExpiredLease(lease, abort)
				}
				lease, err := a.AcquireLease(terminal.ID(), "transfer", LeaseTypeFileTransfer)
				if err != nil {
					t.Fatal(err)
				}
				request := session.WriteRequest{Actor: session.ActorSystem, Data: []byte("announced block\n")}
				if _, err := a.WriteSessionWithLease(context.Background(), terminal.ID(), lease.Owner, request); err != nil {
					t.Fatal(err)
				}
				readDone := make(chan error, 1)
				if phase != "client-disconnected" {
					go func() {
						p := &fileProtocol{terminal: expiryProtocolSession{terminal}, token: "test"}
						var err error
						if phase == "receive-data" {
							_, err = p.readExact(context.Background(), make([]byte, 7))
						} else {
							_, err = p.expect(context.Background(), phase, "7")
						}
						readDone <- err
					}()
				}
				synctest.Wait()
				a.leases.mu.Lock()
				active := a.leases.activeWrites[terminal.ID()]
				a.leases.mu.Unlock()
				if active != 0 {
					t.Fatalf("waiting phase has %d active writes", active)
				}
				time.Sleep(time.Second)
				<-closing
				var writers sync.WaitGroup
				for range 16 {
					writers.Go(func() {
						if _, err := a.WriteSession(context.Background(), terminal.ID(), request); !errors.Is(err, ErrSessionBusy) {
							t.Errorf("ordinary write during expiry: %v", err)
						}
						if _, err := a.AcquireLease(terminal.ID(), "next", LeaseTypeTerminal); !errors.Is(err, ErrSessionBusy) {
							t.Errorf("new operation during expiry: %v", err)
						}
						if _, err := a.WriteSessionWithLease(context.Background(), terminal.ID(), lease.Owner, request); !errors.Is(err, ErrLeaseNotOwned) {
							t.Errorf("stale transfer write: %v", err)
						}
					})
				}
				writers.Wait()
				for range 2 {
					if err := a.ReleaseLease(terminal.ID(), lease.Owner); err != nil {
						t.Fatal(err)
					}
				}
				if _, err := a.RenewLease(terminal.ID(), lease.Owner); !errors.Is(err, ErrLeaseNotOwned) {
					t.Fatalf("late renew: %v", err)
				}
				if got := string(terminal.writtenData()); got != string(request.Data) {
					t.Fatalf("expiry admitted bytes: %q", got)
				}
				close(proceed)
				synctest.Wait()
				if terminal.State() != session.StateClosed {
					t.Fatal("expired Session was not closed")
				}
				if _, err := a.WriteSession(context.Background(), terminal.ID(), request); !errors.Is(err, ErrSessionNotFound) {
					t.Fatalf("write after closure: %v", err)
				}
				if phase != "client-disconnected" && !errors.Is(<-readDone, session.ErrNotOpen) {
					t.Fatal("Close did not end the pending protocol read")
				}
				_ = terminal.Close()
			})
		})
	}
}

type expiryProtocolSession struct{ *fakeConnectedSession }

func (s expiryProtocolSession) ReadRecent(_ context.Context, n int) (session.OutputChunk, error) {
	return s.fakeConnectedSession.ReadRecent(n)
}

func TestFileTransferExpiryIsEnforcedBeforeDelayedTimer(t *testing.T) {
	for _, entry := range []string{"write", "acquire", "renew", "release", "status", "timer"} {
		t.Run(entry, func(t *testing.T) {
			c := newLeaseCoordinator()
			lease, err := c.acquire("s", "owner", LeaseTypeFileTransfer)
			if err != nil {
				t.Fatal(err)
			}
			c.mu.Lock()
			c.stopExpiryLocked("s")
			lease.ExpiresAt = time.Now().Add(-time.Second)
			c.leases["s"] = lease
			c.mu.Unlock()
			calls := 0
			c.onExpired = func(_ SessionLease, abort bool) {
				calls++
				if !abort {
					t.Error("expiry did not require close")
				}
			}
			write := func() (int, error) { t.Error("expired write reached Session"); return 1, nil }
			switch entry {
			case "write":
				_, err = c.write("s", "", "s", write)
			case "acquire":
				_, err = c.acquire("s", "next", LeaseTypeTerminal)
			case "renew":
				_, err = c.renew("s", "owner")
			case "release":
				_, _, _, err = c.release("s", "owner")
			case "status":
				_, active := c.status("s")
				if active {
					t.Fatal("expired status still active")
				}
			case "timer":
				c.expire("s", lease.Owner, lease.ExpiresAt)
			}
			if entry != "status" && entry != "timer" && err == nil {
				t.Fatal("expired request succeeded")
			}
			if _, err := c.write("s", "", "s", write); !errors.Is(err, ErrSessionBusy) {
				t.Fatalf("expiry lost exclusion: %v", err)
			}
			c.expire("s", lease.Owner, lease.ExpiresAt)
			if calls != 1 {
				t.Fatalf("expiry callbacks = %d", calls)
			}
		})
	}
}

func TestDelayedFileTransferExpiryDoesNotCloseReplacementRegistration(t *testing.T) {
	synctest.Test(t, func(t *testing.T) {
		a, original := newSerialTestApplication(t)
		a.leases.ttl = time.Second
		expired := make(chan SessionLease, 1)
		a.leases.onExpired = func(lease SessionLease, _ bool) { expired <- lease }
		if _, err := a.AcquireLease(original.ID(), "old", LeaseTypeFileTransfer); err != nil {
			t.Fatal(err)
		}
		time.Sleep(time.Second)
		old := <-expired
		if _, err := a.CloseSession(original.ID()); err != nil {
			t.Fatal(err)
		}
		replacement := newFakeConnectedSession(original.ID())
		_ = replacement.Connect(context.Background())
		if err := a.manager.RegisterWithMetadata(replacement, session.SessionMetadata{Transport: "serial", Endpoint: "fake"}); err != nil {
			t.Fatal(err)
		}
		a.leases.ttl = time.Hour
		current, err := a.AcquireLease(replacement.ID(), "new", LeaseTypeFileTransfer)
		if err != nil {
			t.Fatal(err)
		}
		for range 2 {
			a.publishExpiredLease(old, true)
			a.leases.removeRegistration(old.SessionID, old.sessionReference)
		}
		synctest.Wait()
		if replacement.State() != session.StateOpen {
			t.Fatal("delayed expiry closed replacement")
		}
		if _, err := a.WriteSession(context.Background(), replacement.ID(), session.WriteRequest{Actor: session.ActorUser, Data: []byte("blocked")}); !errors.Is(err, ErrSessionBusy) {
			t.Fatalf("replacement lost protection: %v", err)
		}
		if _, err := a.RenewLease(replacement.ID(), current.Owner); err != nil {
			t.Fatal(err)
		}
		if err := a.ReleaseLease(replacement.ID(), old.Owner); !errors.Is(err, ErrLeaseNotOwned) {
			t.Fatalf("old release: %v", err)
		}
		if err := a.ReleaseLease(replacement.ID(), current.Owner); err != nil {
			t.Fatal(err)
		}
	})
}

func TestFileTransferExpiryKeepsExclusionUntilConcurrentCloserFinishes(t *testing.T) {
	synctest.Test(t, func(t *testing.T) {
		manager := session.NewManager()
		terminal := &delayedFileTransferCloseSession{fakeConnectedSession: newFakeConnectedSession("closing"), entered: make(chan struct{}), proceed: make(chan struct{})}
		_ = terminal.Connect(context.Background())
		if err := manager.Register(terminal); err != nil {
			t.Fatal(err)
		}
		a, err := New(Dependencies{Manager: manager})
		if err != nil {
			t.Fatal(err)
		}
		a.leases.ttl = time.Second
		if _, err := a.AcquireLease(terminal.ID(), "owner", LeaseTypeFileTransfer); err != nil {
			t.Fatal(err)
		}
		done := make(chan error, 1)
		go func() { _, err := a.CloseSession(terminal.ID()); done <- err }()
		<-terminal.entered
		// Explicit closure has removed the registration, but has not reached
		// Channel.Close. Expiry's own remove now fails and must retain exclusion.
		time.Sleep(time.Second)
		synctest.Wait()
		_, err = a.leases.write(terminal.ID(), "", "closing", func() (int, error) {
			t.Error("a previously resolved writer entered the not-yet-closed Session")
			return 1, nil
		})
		if !errors.Is(err, ErrSessionBusy) {
			t.Fatalf("concurrent close lost exclusion: %v", err)
		}
		close(terminal.proceed)
		if err := <-done; err != nil {
			t.Fatal(err)
		}
		if terminal.State() != session.StateClosed {
			t.Fatal("explicit closer did not finish")
		}
		_ = terminal.Close()
	})
}

// Pause before the fake's Close changes state, modeling a closer preempted
// immediately after Manager removal. No physical Channel is accessed.
type delayedFileTransferCloseSession struct {
	*fakeConnectedSession
	entered chan struct{}
	proceed chan struct{}
	once    sync.Once
}

func (s *delayedFileTransferCloseSession) Close() error {
	s.once.Do(func() { close(s.entered); <-s.proceed })
	return s.fakeConnectedSession.Close()
}

func TestFileTransferExpiryInterruptsWriteWithoutRollingBackPrefix(t *testing.T) {
	synctest.Test(t, func(t *testing.T) {
		terminal := &partialExpiryWriteSession{blockingWriteSession: newBlockingWriteSession("partial")}
		_ = terminal.Connect(context.Background())
		manager := session.NewManager()
		if err := manager.Register(terminal); err != nil {
			t.Fatal(err)
		}
		a, err := New(Dependencies{Manager: manager})
		if err != nil {
			t.Fatal(err)
		}
		a.leases.ttl = time.Second
		if _, err := a.AcquireLease(terminal.ID(), "owner", LeaseTypeFileTransfer); err != nil {
			t.Fatal(err)
		}
		done := make(chan struct{})
		go func() {
			defer close(done)
			n, err := a.WriteSessionWithLease(context.Background(), terminal.ID(), "owner", session.WriteRequest{Actor: session.ActorSystem, Data: []byte("payload")})
			if n != 3 || !errors.Is(err, session.ErrNotOpen) {
				t.Errorf("interrupted write = %d, %v", n, err)
			}
		}()
		<-terminal.writeStarted
		time.Sleep(time.Second)
		<-done
		synctest.Wait()
		if string(terminal.writtenData()) != "pay" {
			t.Fatal("confirmed prefix was not preserved")
		}
		if terminal.State() != session.StateClosed {
			t.Fatal("expiry did not close the blocked writer")
		}
	})
}

type partialExpiryWriteSession struct{ *blockingWriteSession }

// Write confirms a prefix, then blocks until the owning Session closes.
func (s *partialExpiryWriteSession) Write(request session.WriteRequest) (int, error) {
	request.Data = request.Data[:3]
	_, _ = s.fakeConnectedSession.Write(request)
	s.startOnce.Do(func() { close(s.writeStarted) })
	<-s.closed
	return 3, session.ErrNotOpen
}
