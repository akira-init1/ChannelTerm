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

func TestFileTransferConfirmationExpiresWhileOwnerRenews(t *testing.T) {
	synctest.Test(t, func(t *testing.T) {
		a, terminal := newSerialTestApplication(t)
		lease, err := a.AcquireLease(terminal.ID(), "owner", LeaseTypeFileTransfer)
		if err != nil {
			t.Fatal(err)
		}
		request, err := a.BeginFileTransferCancel(terminal.ID())
		if err != nil {
			t.Fatal(err)
		}
		control := a.leases.fileTransfers[terminal.ID()]
		done := make(chan FileTransferControlAction, 1)
		go func() {
			action, err := a.FileTransferCheckpoint(context.Background(), terminal.ID(), "owner")
			if err != nil {
				t.Error(err)
			}
			done <- action
		}()
		synctest.Wait()
		// The initiating client sends nothing else, including no disconnect.
		for range 3 {
			time.Sleep(10 * time.Second)
			if _, err := a.RenewLease(terminal.ID(), "owner"); err != nil {
				t.Fatal(err)
			}
		}
		if action := <-done; action != FileTransferContinue {
			t.Fatalf("action = %q", action)
		}
		if control.confirmationTimer != nil || control.requestID != "" {
			t.Fatal("expired confirmation retained resources")
		}
		if terminal.State() != session.StateOpen {
			t.Fatal("confirmation timeout closed Session")
		}
		current, ok := a.leases.status(terminal.ID())
		if !ok || current.TransferID != lease.TransferID {
			t.Fatal("confirmation timeout replaced or released lease")
		}
		if _, err := a.ResolveFileTransferCancel(context.Background(), terminal.ID(), request.RequestID, true); !errors.Is(err, ErrFileTransferCancelRequest) {
			t.Fatalf("late answer: %v", err)
		}
		if _, err := a.WriteSession(context.Background(), terminal.ID(), session.WriteRequest{Actor: session.ActorUser, Data: []byte("blocked")}); !errors.Is(err, ErrSessionBusy) {
			t.Fatalf("ordinary write: %v", err)
		}
		if _, err := a.WriteSessionWithLease(context.Background(), terminal.ID(), "owner", session.WriteRequest{Actor: session.ActorSystem, Data: []byte("next block")}); err != nil {
			t.Fatal(err)
		}
		if _, err := a.CloseSession(terminal.ID()); err != nil {
			t.Fatal(err)
		}
	})
}

func TestFileTransferConfirmationDecisionStopsTimer(t *testing.T) {
	for _, cancel := range []bool{false, true} {
		t.Run(map[bool]string{false: "decline", true: "accept"}[cancel], func(t *testing.T) {
			synctest.Test(t, func(t *testing.T) {
				c, request, control := newConfirmationTest(t)
				result := make(chan FileTransferCancelResolution, 1)
				go func() {
					r, err := c.resolveFileTransferCancel(context.Background(), "s", request.RequestID, cancel)
					if err != nil {
						t.Error(err)
					}
					result <- r
				}()
				synctest.Wait()
				if control.confirmationTimer != nil {
					t.Fatal("decision retained timer")
				}
				time.Sleep(FileTransferCancelConfirmationTimeout)
				c.expireFileTransferConfirmation("s", control, request.RequestID)
				action, err := c.fileTransferCheckpoint(context.Background(), "s", "owner")
				want := FileTransferContinue
				if cancel {
					want = FileTransferCancel
				}
				if err != nil || action != want {
					t.Fatalf("checkpoint = %q, %v", action, err)
				}
				if _, _, _, err := c.release("s", "owner"); err != nil {
					t.Fatal(err)
				}
				resolution := <-result
				if cancel && resolution.State != "cancelled" || !cancel && resolution.State != "resumed" {
					t.Fatalf("resolution = %#v", resolution)
				}
				if len(c.fileTransfers) != 0 {
					t.Fatal("finished transfer retained control")
				}
			})
		})
	}
}

// newConfirmationTest uses a long lease to isolate the confirmation deadline.
// Call from a synctest bubble so no wall-clock wait is needed.
func newConfirmationTest(t *testing.T) (*leaseCoordinator, FileTransferCancelRequest, *fileTransferControl) {
	t.Helper()
	c := newLeaseCoordinatorWithTTL(time.Hour)
	if _, err := c.acquire("s", "owner", LeaseTypeFileTransfer); err != nil {
		t.Fatal(err)
	}
	r, err := c.beginFileTransferCancel("s")
	if err != nil {
		t.Fatal(err)
	}
	return c, r, c.fileTransfers["s"]
}

func TestFileTransferConfirmationDeadlineDoesNotDependOnTimerScheduling(t *testing.T) {
	for _, entry := range []string{"begin", "resolve", "checkpoint"} {
		t.Run(entry, func(t *testing.T) {
			synctest.Test(t, func(t *testing.T) {
				c, old, control := newConfirmationTest(t)
				control.confirmationTimer.Stop() // simulate a delayed callback
				time.Sleep(FileTransferCancelConfirmationTimeout)
				switch entry {
				case "begin":
					next, err := c.beginFileTransferCancel("s")
					if err != nil || next.RequestID == old.RequestID {
						t.Fatalf("begin = %#v, %v", next, err)
					}
					c.expireFileTransferConfirmation("s", control, old.RequestID)
					if control.state != "confirming" || control.requestID != next.RequestID {
						t.Fatal("old timer changed next request")
					}
				case "resolve":
					if _, err := c.resolveFileTransferCancel(context.Background(), "s", old.RequestID, true); !errors.Is(err, ErrFileTransferCancelRequest) {
						t.Fatalf("late answer = %v", err)
					}
				case "checkpoint":
					if action, err := c.fileTransferCheckpoint(context.Background(), "s", "owner"); action != FileTransferContinue || err != nil {
						t.Fatalf("checkpoint = %q, %v", action, err)
					}
				}
				if _, err := c.resolveFileTransferCancel(context.Background(), "s", old.RequestID, true); !errors.Is(err, ErrFileTransferCancelRequest) {
					t.Fatalf("old answer = %v", err)
				}
				c.remove("s")
			})
		})
	}
}

func TestFileTransferConfirmationOldRequestCannotAffectReplacementTransfer(t *testing.T) {
	synctest.Test(t, func(t *testing.T) {
		c, old, control := newConfirmationTest(t)
		done := make(chan error, 1)
		go func() { _, err := c.fileTransferCheckpoint(context.Background(), "s", "owner"); done <- err }()
		synctest.Wait()
		if _, _, _, err := c.release("s", "owner"); err != nil {
			t.Fatal(err)
		}
		// Reuse the same owner as well as ID: the waiting call must remain
		// bound to its original TransferID, not join the new operation.
		if _, err := c.acquire("s", "owner", LeaseTypeFileTransfer); err != nil {
			t.Fatal(err)
		}
		time.Sleep(time.Second)
		next, err := c.beginFileTransferCancel("s")
		if err != nil {
			t.Fatal(err)
		}
		if err := <-done; !errors.Is(err, ErrLeaseNotOwned) {
			t.Fatalf("old checkpoint = %v", err)
		}
		time.Sleep(FileTransferCancelConfirmationTimeout - time.Second)
		c.expireFileTransferConfirmation("s", control, old.RequestID)
		if _, err := c.resolveFileTransferCancel(context.Background(), "s", old.RequestID, true); !errors.Is(err, ErrFileTransferCancelRequest) {
			t.Fatalf("old answer = %v", err)
		}
		if c.fileTransfers["s"].requestID != next.RequestID {
			t.Fatal("replacement request changed")
		}
		c.remove("s")
	})
}

func TestFileTransferConfirmationConcurrentDecisionAndExpiry(t *testing.T) {
	for _, offset := range []time.Duration{-time.Nanosecond, 0} {
		synctest.Test(t, func(t *testing.T) {
			c, request, control := newConfirmationTest(t)
			control.confirmationTimer.Stop()
			time.Sleep(FileTransferCancelConfirmationTimeout + offset)
			// A cancelled wait context lets resolve return after applying the
			// decision, without needing a release in the racing goroutines.
			ctx, cancel := context.WithCancel(context.Background())
			cancel()
			start := make(chan struct{})
			var wg sync.WaitGroup
			for range 16 {
				wg.Go(func() { <-start; c.expireFileTransferConfirmation("s", control, request.RequestID) })
				wg.Go(func() { <-start; _, _ = c.resolveFileTransferCancel(ctx, "s", request.RequestID, true) })
			}
			close(start)
			wg.Wait()
			want := "running"
			if offset < 0 {
				want = "cancelled"
			}
			if control.state != want || control.confirmationTimer != nil {
				t.Fatalf("state = %q, timer = %v", control.state, control.confirmationTimer)
			}
			c.remove("s")
		})
	}
}

func TestFileTransferConfirmationConcurrentBeginHasOneWinner(t *testing.T) {
	synctest.Test(t, func(t *testing.T) {
		c := newLeaseCoordinatorWithTTL(time.Hour)
		if _, err := c.acquire("s", "owner", LeaseTypeFileTransfer); err != nil {
			t.Fatal(err)
		}
		results := make(chan error, 16)
		for range 16 {
			go func() { _, err := c.beginFileTransferCancel("s"); results <- err }()
		}
		winners := 0
		for range 16 {
			err := <-results
			if err == nil {
				winners++
			} else if !errors.Is(err, ErrFileTransferCancelPending) {
				t.Fatal(err)
			}
		}
		if winners != 1 {
			t.Fatalf("winners = %d", winners)
		}
		c.remove("s")
	})
}

func TestFileTransferConfirmationTerminalCleanup(t *testing.T) {
	for _, end := range []string{"complete", "close", "lease-expiry", "recovery-unconfirmed"} {
		t.Run(end, func(t *testing.T) {
			synctest.Test(t, func(t *testing.T) {
				a, terminal := newSerialTestApplication(t)
				a.leases.ttl = time.Hour
				if end == "lease-expiry" {
					a.leases.ttl = time.Second
				}
				if _, err := a.AcquireLease(terminal.ID(), "owner", LeaseTypeFileTransfer); err != nil {
					t.Fatal(err)
				}
				request, err := a.BeginFileTransferCancel(terminal.ID())
				if err != nil {
					t.Fatal(err)
				}
				control := a.leases.fileTransfers[terminal.ID()]
				done := make(chan error, 1)
				go func() { _, err := a.FileTransferCheckpoint(context.Background(), terminal.ID(), "owner"); done <- err }()
				synctest.Wait()
				switch end {
				case "complete":
					if err := a.ReleaseLease(terminal.ID(), "owner"); err != nil {
						t.Fatal(err)
					}
				case "close":
					if _, err := a.CloseSession(terminal.ID()); err != nil {
						t.Fatal(err)
					}
				case "lease-expiry":
					time.Sleep(time.Second)
				case "recovery-unconfirmed":
					adapter := &leasedRecoverySession{app: a, terminal: terminal}
					err := (&fileProtocol{terminal: adapter}).recoveryError(ErrFileTransferProtocol)
					if !errors.Is(err, ErrFileTransferRecoveryUnconfirmed) {
						t.Fatal(err)
					}
				}
				if err := <-done; !errors.Is(err, ErrLeaseNotOwned) {
					t.Fatalf("terminal checkpoint = %v", err)
				}
				synctest.Wait()
				if control.confirmationTimer != nil {
					t.Fatal("terminal state retained timer")
				}
				time.Sleep(FileTransferCancelConfirmationTimeout)
				a.leases.expireFileTransferConfirmation(terminal.ID(), control, request.RequestID)
				if _, err := a.RenewLease(terminal.ID(), "owner"); err == nil {
					t.Fatal("timeout restored lease")
				}
				if end == "complete" {
					r, err := a.ResolveFileTransferCancel(context.Background(), terminal.ID(), request.RequestID, true)
					if err != nil || r.State != "completed" {
						t.Fatalf("completed answer = %#v, %v", r, err)
					}
				} else if terminal.State() != session.StateClosed {
					t.Fatal("timeout reopened Session")
				}
				_, _ = a.CloseSession(terminal.ID())
				_, _ = a.CloseSession(terminal.ID())
			})
		})
	}
}

func TestFileTransferConfirmationCompletedPromptCanCloseWithoutDoubleCleanup(t *testing.T) {
	synctest.Test(t, func(t *testing.T) {
		c, _, control := newConfirmationTest(t)
		if _, _, _, err := c.release("s", "owner"); err != nil {
			t.Fatal(err)
		}
		c.remove("s")
		c.remove("s")
		if control.confirmationTimer != nil || len(c.fileTransfers) != 0 {
			t.Fatal("close retained confirmation")
		}
	})
}

func TestFileTransferConfirmationTimeoutCannotBypassOverdueLeaseTimer(t *testing.T) {
	synctest.Test(t, func(t *testing.T) {
		a, terminal := newSerialTestApplication(t)
		if _, err := a.AcquireLease(terminal.ID(), "owner", LeaseTypeFileTransfer); err != nil {
			t.Fatal(err)
		}
		if _, err := a.BeginFileTransferCancel(terminal.ID()); err != nil {
			t.Fatal(err)
		}
		a.leases.mu.Lock()
		a.leases.stopExpiryLocked(terminal.ID()) // the confirmation timer fires first
		a.leases.mu.Unlock()
		done := make(chan error, 1)
		go func() { _, err := a.FileTransferCheckpoint(context.Background(), terminal.ID(), "owner"); done <- err }()
		synctest.Wait()
		time.Sleep(FileTransferCancelConfirmationTimeout)
		if err := <-done; !errors.Is(err, ErrLeaseNotOwned) {
			t.Fatalf("overdue lease resumed: %v", err)
		}
		synctest.Wait()
		if terminal.State() != session.StateClosed {
			t.Fatal("overdue lease did not follow task A closure")
		}
		if _, err := a.WriteSession(context.Background(), terminal.ID(), session.WriteRequest{Actor: session.ActorUser, Data: []byte("blocked")}); !errors.Is(err, ErrSessionNotFound) {
			t.Fatalf("timeout reopened writing: %v", err)
		}
	})
}

func TestFileTransferConfirmationDecisionExpiryAndEndRace(t *testing.T) {
	for _, end := range []string{"release", "close"} {
		for _, offset := range []time.Duration{-time.Nanosecond, 0} {
			synctest.Test(t, func(t *testing.T) {
				c, request, control := newConfirmationTest(t)
				control.confirmationTimer.Stop()
				time.Sleep(FileTransferCancelConfirmationTimeout + offset)
				start := make(chan struct{})
				var wg sync.WaitGroup
				wg.Go(func() { <-start; c.expireFileTransferConfirmation("s", control, request.RequestID) })
				wg.Go(func() {
					<-start
					_, _ = c.resolveFileTransferCancel(context.Background(), "s", request.RequestID, true)
				})
				wg.Go(func() {
					<-start
					if end == "release" {
						if _, _, _, err := c.release("s", "owner"); err != nil {
							t.Error(err)
						}
					} else {
						c.remove("s")
					}
				})
				close(start)
				wg.Wait()
				if control.confirmationTimer != nil {
					t.Fatal("racing terminal path retained timer")
				}
				if _, err := c.fileTransferCheckpoint(context.Background(), "s", "owner"); !errors.Is(err, ErrLeaseNotOwned) {
					t.Fatalf("racing end resumed transfer: %v", err)
				}
				c.remove("s")
			})
		}
	}
}

func TestFileTransferConfirmationWakeRechecksSessionLifecycle(t *testing.T) {
	for _, end := range []string{"stream-closed", "removal-before-channel-close"} {
		t.Run(end, func(t *testing.T) {
			synctest.Test(t, func(t *testing.T) {
				a, terminal := newSerialTestApplication(t)
				a.leases.ttl = time.Hour
				if _, err := a.AcquireLease(terminal.ID(), "owner", LeaseTypeFileTransfer); err != nil {
					t.Fatal(err)
				}
				if _, err := a.BeginFileTransferCancel(terminal.ID()); err != nil {
					t.Fatal(err)
				}
				done := make(chan error, 1)
				go func() { _, err := a.FileTransferCheckpoint(context.Background(), terminal.ID(), "owner"); done <- err }()
				synctest.Wait()
				want := session.ErrNotOpen
				if end == "stream-closed" {
					_ = terminal.Close()
				} else {
					// Model CloseSession between Manager removal and Channel.Close.
					_, _ = a.manager.Remove(terminal.ID())
					want = ErrSessionNotFound
				}
				time.Sleep(FileTransferCancelConfirmationTimeout)
				if err := <-done; !errors.Is(err, want) {
					t.Fatalf("closed Session checkpoint = %v, want %v", err, want)
				}
				a.leases.remove(terminal.ID())
				_ = terminal.Close()
			})
		})
	}
}
