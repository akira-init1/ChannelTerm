package app

import (
	"context"
	"errors"
	"strings"
	"testing"

	"github.com/akira-init1/ChannelTerm/internal/core/session"
)

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
