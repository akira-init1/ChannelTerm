package app

import (
	"errors"
	"fmt"
	"strings"
	"sync"
	"time"
)

var (
	// ErrInvalidLeaseOwner is returned when a lease request has no owner token.
	ErrInvalidLeaseOwner = errors.New("lease owner is required")
	// ErrInvalidLeaseType is returned when a lease request uses an unsupported type.
	ErrInvalidLeaseType = errors.New("lease type is invalid")
	// ErrSessionBusy is returned when a writer does not own a Session's active lease.
	ErrSessionBusy = errors.New("session is busy")
	// ErrLeaseNotOwned is returned when a caller attempts to release a lease held by another owner.
	ErrLeaseNotOwned = errors.New("session lease is not owned by caller")
)

// LeaseType identifies the exclusive operation currently using a Session.
type LeaseType string

const (
	// LeaseTypeTerminal reserves a Session for a terminal-oriented operation.
	LeaseTypeTerminal LeaseType = "terminal"
	// LeaseTypeFileTransfer reserves a Session for a multi-chunk file transfer.
	LeaseTypeFileTransfer LeaseType = "file-transfer"
	// LeaseTypeDebug is reserved for a future debugging operation.
	LeaseTypeDebug LeaseType = "debug"
)

// Valid reports whether typ is a supported lease type.
func (typ LeaseType) Valid() bool {
	switch typ {
	case LeaseTypeTerminal, LeaseTypeFileTransfer, LeaseTypeDebug:
		return true
	default:
		return false
	}
}

// SessionLease is a point-in-time description of an exclusive Session lease.
// Owner is an opaque caller-selected capability and must not be presented as a
// human identity.
type SessionLease struct {
	SessionID string
	Owner     string
	Type      LeaseType
	CreatedAt time.Time
	State     string
}

// SessionBusyError identifies the active lease preventing a write. It unwraps
// ErrSessionBusy so callers can handle all contention errors consistently.
type SessionBusyError struct {
	SessionID string
	Lease     SessionLease
}

// Error returns a concise, user-facing reason without exposing the owner token.
func (e *SessionBusyError) Error() string {
	return fmt.Sprintf("Session %s is locked by %s", e.SessionID, e.Lease.Type)
}

// Unwrap makes SessionBusyError comparable with ErrSessionBusy.
func (*SessionBusyError) Unwrap() error { return ErrSessionBusy }

// leaseCoordinator owns application-level writer coordination. It deliberately
// has no dependency on Session so raw stream buffering and write serialization
// remain Session responsibilities.
type leaseCoordinator struct {
	mu     sync.Mutex
	leases map[string]SessionLease
	gates  map[string]*sync.Mutex
}

func newLeaseCoordinator() *leaseCoordinator {
	return &leaseCoordinator{leases: make(map[string]SessionLease), gates: make(map[string]*sync.Mutex)}
}

func (c *leaseCoordinator) acquire(sessionID, owner string, typ LeaseType) (SessionLease, error) {
	owner = strings.TrimSpace(owner)
	if owner == "" {
		return SessionLease{}, ErrInvalidLeaseOwner
	}
	if !typ.Valid() {
		return SessionLease{}, fmt.Errorf("%w: %q", ErrInvalidLeaseType, typ)
	}
	gate := c.gate(sessionID)
	gate.Lock()
	defer gate.Unlock()
	c.mu.Lock()
	defer c.mu.Unlock()
	if active, exists := c.leases[sessionID]; exists {
		return SessionLease{}, &SessionBusyError{SessionID: sessionID, Lease: active}
	}
	lease := SessionLease{SessionID: sessionID, Owner: owner, Type: typ, CreatedAt: time.Now().UTC(), State: "active"}
	c.leases[sessionID] = lease
	return lease, nil
}

func (c *leaseCoordinator) release(sessionID, owner string) (SessionLease, bool, error) {
	gate := c.gate(sessionID)
	gate.Lock()
	defer gate.Unlock()
	c.mu.Lock()
	defer c.mu.Unlock()
	active, exists := c.leases[sessionID]
	if !exists {
		return SessionLease{}, false, nil
	}
	if active.Owner != owner {
		return SessionLease{}, false, ErrLeaseNotOwned
	}
	delete(c.leases, sessionID)
	return active, true, nil
}

func (c *leaseCoordinator) status(sessionID string) (SessionLease, bool) {
	c.mu.Lock()
	defer c.mu.Unlock()
	lease, ok := c.leases[sessionID]
	return lease, ok
}

func (c *leaseCoordinator) write(sessionID, owner, displayID string, operation func() (int, error)) (int, error) {
	gate := c.gate(sessionID)
	gate.Lock()
	defer gate.Unlock()
	c.mu.Lock()
	lease, exists := c.leases[sessionID]
	c.mu.Unlock()
	if !exists || lease.Owner == owner {
		return operation()
	}
	return 0, &SessionBusyError{SessionID: displayID, Lease: lease}
}

func (c *leaseCoordinator) remove(sessionID string) {
	gate := c.gate(sessionID)
	gate.Lock()
	defer gate.Unlock()
	c.mu.Lock()
	defer c.mu.Unlock()
	delete(c.leases, sessionID)
}

// gate returns a stable per-Session operation gate. Holding it over a complete
// Application write makes acquiring a lease atomic with respect to the final
// pre-lease writer without serializing unrelated Sessions.
func (c *leaseCoordinator) gate(sessionID string) *sync.Mutex {
	c.mu.Lock()
	defer c.mu.Unlock()
	gate := c.gates[sessionID]
	if gate == nil {
		gate = &sync.Mutex{}
		c.gates[sessionID] = gate
	}
	return gate
}
