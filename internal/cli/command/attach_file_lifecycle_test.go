package command

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/akira-init1/ChannelTerm/internal/core/session"
)

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
			if attached.acquires != 1 || attached.renews != 1 || attached.releases != 1 {
				t.Errorf("lease lifecycle = %d/%d/%d, want acquire/renew/release 1/1/1", attached.acquires, attached.renews, attached.releases)
			}
			if err := wrapped.Close(); err != nil {
				t.Fatal(err)
			}
			if attached.closeCount() != 0 {
				t.Fatal("shortcut closed the shared attachment")
			}
			if _, err := attached.Write(session.WriteRequest{Actor: session.ActorUser, Data: []byte("echo ready\r")}); err != nil {
				t.Fatalf("input after transfer: %v", err)
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
