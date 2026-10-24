package command

import (
	"bytes"
	"context"
	"errors"
	"strings"
	"testing"
	"testing/synctest"
	"time"

	"github.com/akira-init1/ChannelTerm/internal/core/app"
	"github.com/akira-init1/ChannelTerm/internal/core/session"
)

// TestFileTransferWriteCancellationPreservesRemoteCount covers cancellation
// after the remote side has accepted bytes but before its MCP response arrives.
func TestFileTransferWriteCancellationPreservesRemoteCount(t *testing.T) {
	for _, stalled := range []bool{false, true} {
		name := "acknowledged"
		if stalled {
			name = "unresponsive"
		}
		t.Run(name, func(t *testing.T) {
			synctest.Test(t, func(t *testing.T) {
				ctx, cancel := context.WithCancel(context.Background())
				defer cancel()
				attached := &cancelledWriteResponseSession{cancel: cancel, stalled: stalled}
				terminal := internalFileTransferSession{attachSession: attached}
				start := time.Now()
				count, err := terminal.WriteContext(ctx, session.WriteRequest{Actor: session.ActorUser, Data: []byte("payload")})
				if stalled {
					if !errors.Is(err, context.DeadlineExceeded) || attached.aborts != 1 || time.Since(start) != fileTransferWriteCancelGrace {
						t.Fatalf("stalled write: count=%d error=%v aborts=%d elapsed=%s", count, err, attached.aborts, time.Since(start))
					}
				} else if err != nil || count != 7 || attached.aborts != 0 {
					t.Fatalf("acknowledged write: count=%d error=%v aborts=%d", count, err, attached.aborts)
				}
			})
		})
	}
}

type cancelledWriteResponseSession struct {
	fakeAttachSession
	cancel  context.CancelFunc
	stalled bool
	aborts  int
}

func (s *cancelledWriteResponseSession) WriteContext(ctx context.Context, request session.WriteRequest) (int, error) {
	s.cancel()
	if s.stalled {
		<-ctx.Done()
		return 0, ctx.Err()
	}
	time.Sleep(time.Second)
	if err := ctx.Err(); err != nil {
		return 0, err
	}
	return len(request.Data), nil
}

func (s *cancelledWriteResponseSession) AbortFileTransferRecovery(context.Context) error {
	s.aborts++
	return nil
}

// TestFileTransferTimeoutSurvivesAttachmentCancellation covers recovery closing
// the Session, which also cancels the attachment before the worker reports back.
func TestFileTransferTimeoutSurvivesAttachmentCancellation(t *testing.T) {
	err := errors.Join(context.DeadlineExceeded, context.Canceled)
	metadata := fileTransferFailureMetadata(nil, err)
	if metadata["reason"] == fileTransferUserCancelled || !strings.Contains(metadata["error"].(string), "deadline exceeded") {
		t.Fatalf("timeout metadata = %#v", metadata)
	}
	var output bytes.Buffer
	cancellation := newFileTransferCancellation()
	cancellation.Request()
	dispatcher := &attachInputDispatcher{
		transferCancellation: cancellation,
		writeLocal:           func(data []byte) error { _, err := output.Write(data); return err },
	}
	dispatcher.finishTransfer(err)
	if got := output.String(); !strings.Contains(got, "File transfer failed") || !strings.Contains(got, "deadline exceeded") || strings.Contains(got, "cancelled by user") {
		t.Fatalf("timeout presentation = %q", got)
	}
	output.Reset()
	progress := newFileTransferProgress(context.Background(), &output, nil, nil)
	_ = finishFileTransferPresentation(&output, progress, err)
	if got := output.String(); !strings.Contains(got, "Transfer failed") || strings.Contains(got, "Transfer cancelled") {
		t.Fatalf("standalone timeout presentation = %q", got)
	}
}

// TestShortcutFileTransferRenewsDuringCancelledRecovery keeps renewal alive
// while a cancelled worker restores the TTY under its independent cleanup context.
func TestShortcutFileTransferRenewsDuringCancelledRecovery(t *testing.T) {
	synctest.Test(t, func(t *testing.T) {
		attached := &cleanupHeartbeatSession{}
		ctx, cancel := context.WithCancel(context.Background())
		defer cancel()
		err := withFileTransferLease(ctx, nonClosingAttachSession{attachSession: attached}, "SER-1", func(operationCtx context.Context) error {
			synctest.Wait()
			cancel()
			<-operationCtx.Done()
			// Virtual time covers cleanup longer than the Host TTL without slowing
			// down the suite or accessing a real terminal.
			time.Sleep(35 * time.Second)
			return operationCtx.Err()
		})
		if !errors.Is(err, context.Canceled) {
			t.Fatalf("transfer result = %v, want cancellation", err)
		}
		if attached.renews < 4 || attached.releases != 1 {
			t.Fatalf("cleanup renewal/release = %d/%d, want at least 4/1", attached.renews, attached.releases)
		}
	})
}

type cleanupHeartbeatSession struct {
	leaseTrackingAttachSession
	renews int
}

func (s *cleanupHeartbeatSession) RenewFileTransferLease(context.Context) error {
	s.renews++
	return nil
}

func TestShortcutFileTransferLeaseHeartbeat(t *testing.T) {
	renewFailure := errors.New("renewal rejected")
	for _, tt := range []struct {
		name string
		err  error
	}{
		{name: "success"},
		{name: "renewal failure", err: renewFailure},
	} {
		t.Run(tt.name, func(t *testing.T) {
			attached := &shortcutLifecycleSession{renewed: make(chan struct{}), renewErr: tt.err}
			wrapped := nonClosingAttachSession{attachSession: attached}
			ctx, cancel := context.WithTimeout(context.Background(), time.Second)
			defer cancel()
			err := withFileTransferLease(ctx, wrapped, "SER-1", func(operationCtx context.Context) error {
				if tt.err != nil {
					<-operationCtx.Done()
					return operationCtx.Err()
				}
				select {
				case <-attached.renewed:
					return nil
				case <-operationCtx.Done():
					return operationCtx.Err()
				}
			})
			if !errors.Is(err, tt.err) {
				t.Fatalf("transfer error = %v, want %v", err, tt.err)
			}
			if tt.err != nil && !errors.Is(err, context.Canceled) {
				t.Errorf("transfer error = %v, want operation cancellation", err)
			}
			wantReleases := 1
			if tt.err != nil {
				wantReleases = 0
				if !errors.Is(err, app.ErrFileTransferRecoveryUnconfirmed) || attached.abortCtx == nil {
					t.Fatalf("renewal failure did not request closure: %v", err)
				}
			}
			if attached.acquires != 1 || attached.renews != 1 || attached.releases != wantReleases {
				t.Errorf("lease lifecycle = %d/%d/%d, want acquire/renew/release 1/1/%d", attached.acquires, attached.renews, attached.releases, wantReleases)
			}
			if err := wrapped.Close(); err != nil {
				t.Fatal(err)
			}
			if attached.closeCount() != 0 {
				t.Fatal("shortcut closed the shared attachment")
			}
			if tt.err == nil {
				if _, err := attached.Write(session.WriteRequest{Actor: session.ActorUser, Data: []byte("echo ready\r")}); err != nil {
					t.Fatalf("input after successful transfer: %v", err)
				}
			}
		})
	}
}

func TestShortcutFileTransferRecoveryAbort(t *testing.T) {
	abortFailure := errors.New("recovery close failed")
	for _, tt := range []struct {
		name string
		err  error
	}{
		{name: "success"},
		{name: "close failure", err: abortFailure},
	} {
		t.Run(tt.name, func(t *testing.T) {
			attached := &shortcutLifecycleSession{abortErr: tt.err}
			terminal := internalFileTransferSession{attachSession: nonClosingAttachSession{attachSession: attached}}
			ctx, cancel := context.WithTimeout(context.Background(), time.Second)
			defer cancel()
			if err := terminal.AbortFileTransferRecovery(ctx); !errors.Is(err, tt.err) {
				t.Fatalf("recovery abort = %v, want %v", err, tt.err)
			}
			if attached.abortCtx != ctx {
				t.Fatal("recovery abort did not retain its bounded context")
			}
			if attached.closeCount() != 0 {
				t.Fatal("recovery abort used attachment Close instead of the explicit recovery operation")
			}
		})
	}
}

// shortcutLifecycleSession records optional Host capabilities through the same
// wrapper used by the interactive send and receive workers. Heartbeat completion
// synchronizes counter inspection after withFileTransferLease returns.
type shortcutLifecycleSession struct {
	leaseTrackingAttachSession
	renewed  chan struct{}
	renews   int
	renewErr error
	abortCtx context.Context
	abortErr error
}

func (s *shortcutLifecycleSession) RenewFileTransferLease(ctx context.Context) error {
	s.renews++
	if ctx.Err() != nil {
		return ctx.Err()
	}
	close(s.renewed)
	return s.renewErr
}

func (s *shortcutLifecycleSession) AbortFileTransferRecovery(ctx context.Context) error {
	s.abortCtx = ctx
	return s.abortErr
}
