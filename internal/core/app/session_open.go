package app

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"errors"
	"fmt"

	"github.com/akira-init1/ChannelTerm/internal/core/session"
)

const maxSessionIDAttempts = 4

// ErrSessionIDExhausted is returned only when generated Session IDs collide repeatedly.
var ErrSessionIDExhausted = errors.New("could not allocate a unique session ID")

// ConnectedSession is an unregistered Session candidate that can establish
// its Transport. A Transport-specific open service retains ownership until a
// successful candidate is transferred to the shared Manager.
type ConnectedSession interface {
	session.Session
	// Connect establishes the candidate's Transport and starts its Session I/O.
	//
	// ctx controls cancellation and deadlines for protocol-specific connection
	// work. The opening service retains ownership after success until Manager
	// registration, and remains responsible for closing the candidate on error.
	Connect(context.Context) error
}

type sessionIDGenerator func() (string, error)

// allocateSessionID returns an opaque ID that is not currently owned by
// manager. Registration remains the authoritative duplicate check because a
// concurrent caller can register after this point-in-time lookup.
func allocateSessionID(ctx context.Context, manager *session.Manager, generate sessionIDGenerator) (string, error) {
	for range maxSessionIDAttempts {
		if err := ctx.Err(); err != nil {
			return "", err
		}
		id, err := generate()
		if err != nil {
			return "", fmt.Errorf("generate session ID: %w", err)
		}
		if _, exists := manager.Get(id); !exists {
			return id, nil
		}
	}
	return "", ErrSessionIDExhausted
}

// newSessionID produces an opaque random identifier for Manager lookup.
func newSessionID() (string, error) {
	var bytes [16]byte
	if _, err := rand.Read(bytes[:]); err != nil {
		return "", err
	}
	return hex.EncodeToString(bytes[:]), nil
}

// closeSessionCandidate preserves an opening failure while releasing an
// unregistered candidate. description identifies the Transport-specific
// resource in the joined cleanup error.
func closeSessionCandidate(primary error, candidate ConnectedSession, description string) error {
	if err := candidate.Close(); err != nil {
		return errors.Join(primary, fmt.Errorf("close incomplete %s: %w", description, err))
	}
	return primary
}
