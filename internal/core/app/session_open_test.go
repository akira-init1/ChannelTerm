package app

import (
	"context"
	"errors"
	"strings"
	"testing"

	"github.com/akira-init1/ChannelTerm/internal/core/session"
)

// TestAllocateSessionIDSkipsManagerOwnedCollision verifies that allocation
// retries an ID already registered with the shared Manager.
func TestAllocateSessionIDSkipsManagerOwnedCollision(t *testing.T) {
	manager := session.NewManager()
	if err := manager.Register(newFakeConnectedSession("existing")); err != nil {
		t.Fatalf("Register() error = %v", err)
	}
	ids := []string{"existing", "available"}
	got, err := allocateSessionID(context.Background(), manager, func() (string, error) {
		id := ids[0]
		ids = ids[1:]
		return id, nil
	})
	if err != nil || got != "available" {
		t.Fatalf("allocateSessionID() = %q, %v, want available, nil", got, err)
	}
}

// TestAllocateSessionIDReportsExhaustedCollisions verifies that repeated
// collisions stop at the bounded attempt count with a semantic error.
func TestAllocateSessionIDReportsExhaustedCollisions(t *testing.T) {
	manager := session.NewManager()
	if err := manager.Register(newFakeConnectedSession("existing")); err != nil {
		t.Fatalf("Register() error = %v", err)
	}
	attempts := 0
	_, err := allocateSessionID(context.Background(), manager, func() (string, error) {
		attempts++
		return "existing", nil
	})
	if !errors.Is(err, ErrSessionIDExhausted) || attempts != maxSessionIDAttempts {
		t.Fatalf("allocateSessionID() error = %v after %d attempts, want ErrSessionIDExhausted after %d", err, attempts, maxSessionIDAttempts)
	}
}

// TestAllocateSessionIDPropagatesContextAndGeneratorErrors verifies that a
// cancelled allocation does no generator work and that generator failures
// retain their semantic cause with application context.
func TestAllocateSessionIDPropagatesContextAndGeneratorErrors(t *testing.T) {
	manager := session.NewManager()
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	called := false
	_, err := allocateSessionID(ctx, manager, func() (string, error) {
		called = true
		return "unused", nil
	})
	if !errors.Is(err, context.Canceled) || called {
		t.Fatalf("cancelled allocateSessionID() = %v, generator called=%t; want context.Canceled without generation", err, called)
	}

	generationErr := errors.New("random source unavailable")
	_, err = allocateSessionID(context.Background(), manager, func() (string, error) {
		return "", generationErr
	})
	if !errors.Is(err, generationErr) || !strings.Contains(err.Error(), "generate session ID") {
		t.Fatalf("generator allocateSessionID() error = %v, want wrapped generation failure", err)
	}
}

// TestCloseSessionCandidateJoinsCleanupFailure verifies that callers retain
// both the primary opening failure and a candidate cleanup failure.
func TestCloseSessionCandidateJoinsCleanupFailure(t *testing.T) {
	primary := errors.New("connect failed")
	cleanup := errors.New("close failed")
	candidate := &closeErrorCandidate{fakeConnectedSession: newFakeConnectedSession("candidate"), closeErr: cleanup}

	err := closeSessionCandidate(primary, candidate, "test session")
	if !errors.Is(err, primary) || !errors.Is(err, cleanup) || !strings.Contains(err.Error(), "close incomplete test session") {
		t.Fatalf("closeSessionCandidate() error = %v, want joined connect and cleanup failures", err)
	}
}

type closeErrorCandidate struct {
	*fakeConnectedSession
	closeErr error
}

func (c *closeErrorCandidate) Close() error { return c.closeErr }
