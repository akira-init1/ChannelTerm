package app

import (
	"context"
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
	// ErrFileTransferCancelPending is returned when another attachment already
	// owns the confirmation prompt for the active file transfer.
	ErrFileTransferCancelPending = errors.New("file transfer cancellation confirmation is already pending")
	// ErrFileTransferCancelRequest is returned for a stale or unknown request ID.
	ErrFileTransferCancelRequest = errors.New("file transfer cancellation request is invalid")
	// ErrFileTransferIDMismatch is returned when file-transfer status does not
	// identify the transfer owned by the active file-transfer lease.
	ErrFileTransferIDMismatch = errors.New("file transfer ID does not match the active lease")
)

// defaultLeaseTTL bounds abandoned writer ownership while allowing bundled
// clients enough time for transient request latency between heartbeats.
const defaultLeaseTTL = 30 * time.Second

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
	// TransferID identifies one file-transfer lease across its structured events.
	// It is empty for leases whose Type is not LeaseTypeFileTransfer.
	TransferID string
	CreatedAt  time.Time
	// ExpiresAt is the deadline by which the owner must renew the lease.
	ExpiresAt time.Time
	State     string
}

// FileTransferCancelRequest describes a Host-owned cancellation confirmation.
// RequestID prevents a delayed answer from affecting a later transfer.
type FileTransferCancelRequest struct {
	Active    bool
	RequestID string
	State     string
}

// FileTransferCancelResolution describes the result of answering a pending
// confirmation. A confirmed cancellation returns only after lease release.
type FileTransferCancelResolution struct {
	State       string
	Transferred int64
	Total       int64
	Percent     float64
}

// FileTransferControlAction tells the lease owner whether its next safe
// protocol boundary may continue or must stop as a user cancellation.
type FileTransferControlAction string

const (
	// FileTransferContinue allows the next file-transfer block to start.
	FileTransferContinue FileTransferControlAction = "continue"
	// FileTransferCancel stops the transfer after its current safe boundary.
	FileTransferCancel FileTransferControlAction = "cancel"
)

type fileTransferControl struct {
	requestID   string
	state       string
	changed     chan struct{}
	released    chan struct{}
	transferred int64
	total       int64
	percent     float64
	resolution  FileTransferCancelResolution
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
	mu                sync.Mutex
	leases            map[string]SessionLease
	timers            map[string]*time.Timer
	gates             map[string]*sync.Mutex
	fileTransfers     map[string]*fileTransferControl
	activeWrites      map[string]int
	recovering        map[string]bool
	nextCancelRequest uint64
	nextTransferID    uint64
	ttl               time.Duration
	onExpired         func(SessionLease, bool)
}

func newLeaseCoordinator() *leaseCoordinator {
	return newLeaseCoordinatorWithTTL(defaultLeaseTTL)
}

func newLeaseCoordinatorWithTTL(ttl time.Duration) *leaseCoordinator {
	return &leaseCoordinator{
		leases:        make(map[string]SessionLease),
		timers:        make(map[string]*time.Timer),
		gates:         make(map[string]*sync.Mutex),
		fileTransfers: make(map[string]*fileTransferControl),
		activeWrites:  make(map[string]int),
		recovering:    make(map[string]bool),
		ttl:           ttl,
	}
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
	if c.recovering[sessionID] {
		c.mu.Unlock()
		return SessionLease{}, fmt.Errorf("%w: Session %s is closing after an expired in-flight write", ErrSessionBusy, sessionID)
	}
	if active, exists := c.leases[sessionID]; exists {
		if time.Now().UTC().Before(active.ExpiresAt) {
			c.mu.Unlock()
			return SessionLease{}, &SessionBusyError{SessionID: sessionID, Lease: active}
		}
		expired, abortSession := c.expireLocked(sessionID)
		c.mu.Unlock()
		c.notifyExpired(expired, abortSession)
		c.mu.Lock()
	}
	now := time.Now().UTC()
	lease := SessionLease{SessionID: sessionID, Owner: owner, Type: typ, CreatedAt: now, ExpiresAt: now.Add(c.ttl), State: "active"}
	if typ == LeaseTypeFileTransfer {
		c.nextTransferID++
		lease.TransferID = fmt.Sprintf("FT-%d", c.nextTransferID)
	}
	c.leases[sessionID] = lease
	c.scheduleExpiryLocked(lease)
	if typ == LeaseTypeFileTransfer {
		c.fileTransfers[sessionID] = &fileTransferControl{state: "running", changed: make(chan struct{}), released: make(chan struct{})}
	}
	c.mu.Unlock()
	return lease, nil
}

func (c *leaseCoordinator) renew(sessionID, owner string) (SessionLease, error) {
	owner = strings.TrimSpace(owner)
	gate := c.gate(sessionID)
	gate.Lock()
	defer gate.Unlock()
	c.mu.Lock()
	lease, exists := c.leases[sessionID]
	if !exists || lease.Owner != owner {
		c.mu.Unlock()
		return SessionLease{}, ErrLeaseNotOwned
	}
	if !time.Now().UTC().Before(lease.ExpiresAt) {
		expired, abortSession := c.expireLocked(sessionID)
		c.mu.Unlock()
		c.notifyExpired(expired, abortSession)
		return SessionLease{}, ErrLeaseNotOwned
	}
	lease.ExpiresAt = time.Now().UTC().Add(c.ttl)
	c.leases[sessionID] = lease
	c.scheduleExpiryLocked(lease)
	c.mu.Unlock()
	return lease, nil
}

func (c *leaseCoordinator) release(sessionID, owner string) (SessionLease, bool, *FileTransferCancelResolution, error) {
	gate := c.gate(sessionID)
	gate.Lock()
	defer gate.Unlock()
	c.mu.Lock()
	defer c.mu.Unlock()
	active, exists := c.leases[sessionID]
	if !exists {
		return SessionLease{}, false, nil, nil
	}
	if active.Owner != owner {
		return SessionLease{}, false, nil, ErrLeaseNotOwned
	}
	c.stopExpiryLocked(sessionID)
	delete(c.leases, sessionID)
	var cancelled *FileTransferCancelResolution
	if control := c.fileTransfers[sessionID]; control != nil {
		switch control.state {
		case "cancelled":
			control.resolution = FileTransferCancelResolution{
				State:       "cancelled",
				Transferred: control.transferred,
				Total:       control.total,
				Percent:     control.percent,
			}
			result := control.resolution
			cancelled = &result
		case "confirming":
			// The transfer can finish its final verification before reaching
			// another checkpoint. Preserve this request until the attachment
			// consumes its prompt answer, but do not turn success into cancel.
			control.state = "completed"
			control.resolution = FileTransferCancelResolution{State: "completed", Transferred: control.transferred, Total: control.total, Percent: control.percent}
		}
		close(control.released)
		close(control.changed)
		if control.state != "completed" {
			delete(c.fileTransfers, sessionID)
		}
	}
	return active, true, cancelled, nil
}

// scheduleExpiryLocked replaces the Session timer while preserving the owner
// capability in the callback. A stale callback cannot remove a replacement
// lease acquired after the original owner expired or released it.
func (c *leaseCoordinator) scheduleExpiryLocked(lease SessionLease) {
	c.stopExpiryLocked(lease.SessionID)
	delay := time.Until(lease.ExpiresAt)
	if delay < 0 {
		delay = 0
	}
	c.timers[lease.SessionID] = time.AfterFunc(delay, func() {
		c.expire(lease.SessionID, lease.Owner, lease.ExpiresAt)
	})
}

func (c *leaseCoordinator) expire(sessionID, owner string, expiresAt time.Time) {
	c.mu.Lock()
	lease, exists := c.leases[sessionID]
	if !exists || lease.Owner != owner || !lease.ExpiresAt.Equal(expiresAt) || time.Now().UTC().Before(lease.ExpiresAt) {
		c.mu.Unlock()
		return
	}
	expired, abortSession := c.expireLocked(sessionID)
	c.mu.Unlock()
	c.notifyExpired(expired, abortSession)
}

// expireLocked removes one known-expired lease and wakes every waiter exactly
// once. The caller must hold c.mu. Timer-driven expiry deliberately does not
// wait for the Session gate: an in-flight Channel write may be permanently
// blocked, in which case Application closes the Session to release it.
func (c *leaseCoordinator) expireLocked(sessionID string) (SessionLease, bool) {
	lease := c.leases[sessionID]
	delete(c.leases, sessionID)
	delete(c.timers, sessionID)
	abortSession := c.activeWrites[sessionID] > 0
	if abortSession {
		// A Channel write has no generic context or deadline capability. Mark the
		// Session as recovering until Application closes it, which is the only
		// portable way to release a blocked write without allowing a replacement
		// lease to race the stale operation.
		c.recovering[sessionID] = true
	}
	if control := c.fileTransfers[sessionID]; control != nil {
		control.state = "expired"
		control.resolution = FileTransferCancelResolution{State: "expired", Transferred: control.transferred, Total: control.total, Percent: control.percent}
		close(control.released)
		close(control.changed)
		delete(c.fileTransfers, sessionID)
	}
	return lease, abortSession
}

// notifyExpired publishes adapter-visible state only after coordinator locks
// are released because event consumers may immediately call back into leases.
func (c *leaseCoordinator) notifyExpired(lease SessionLease, abortSession bool) {
	if c.onExpired != nil {
		c.onExpired(lease, abortSession)
	}
}

func (c *leaseCoordinator) stopExpiryLocked(sessionID string) {
	if timer := c.timers[sessionID]; timer != nil {
		timer.Stop()
		delete(c.timers, sessionID)
	}
}

func (c *leaseCoordinator) beginFileTransferCancel(sessionID string) (FileTransferCancelRequest, error) {
	c.mu.Lock()
	defer c.mu.Unlock()
	lease, exists := c.leases[sessionID]
	if !exists || lease.Type != LeaseTypeFileTransfer {
		return FileTransferCancelRequest{Active: false, State: "inactive"}, nil
	}
	control := c.fileTransfers[sessionID]
	if control == nil {
		return FileTransferCancelRequest{}, errors.New("active file-transfer lease has no control state")
	}
	if control.state != "running" {
		return FileTransferCancelRequest{}, ErrFileTransferCancelPending
	}
	c.nextCancelRequest++
	control.requestID = fmt.Sprintf("file-transfer-cancel-%d", c.nextCancelRequest)
	control.state = "confirming"
	return FileTransferCancelRequest{Active: true, RequestID: control.requestID, State: control.state}, nil
}

func (c *leaseCoordinator) resolveFileTransferCancel(ctx context.Context, sessionID, requestID string, cancel bool) (FileTransferCancelResolution, error) {
	c.mu.Lock()
	control := c.fileTransfers[sessionID]
	if control != nil && control.state == "completed" && control.requestID == requestID {
		result := control.resolution
		delete(c.fileTransfers, sessionID)
		c.mu.Unlock()
		return result, nil
	}
	if control == nil || control.state != "confirming" || control.requestID != requestID {
		c.mu.Unlock()
		return FileTransferCancelResolution{}, ErrFileTransferCancelRequest
	}
	oldChanged := control.changed
	control.changed = make(chan struct{})
	if !cancel {
		control.state = "running"
		control.requestID = ""
		result := FileTransferCancelResolution{State: "resumed", Transferred: control.transferred, Total: control.total, Percent: control.percent}
		close(oldChanged)
		c.mu.Unlock()
		return result, nil
	}
	control.state = "cancelled"
	released := control.released
	close(oldChanged)
	c.mu.Unlock()

	select {
	case <-ctx.Done():
		return FileTransferCancelResolution{}, ctx.Err()
	case <-released:
		return control.resolution, nil
	}
}

func (c *leaseCoordinator) fileTransferCheckpoint(ctx context.Context, sessionID, owner string) (FileTransferControlAction, error) {
	for {
		c.mu.Lock()
		lease, exists := c.leases[sessionID]
		if !exists || lease.Owner != owner || lease.Type != LeaseTypeFileTransfer {
			c.mu.Unlock()
			return "", ErrLeaseNotOwned
		}
		control := c.fileTransfers[sessionID]
		if control == nil || control.state == "running" {
			c.mu.Unlock()
			return FileTransferContinue, nil
		}
		if control.state == "cancelled" {
			c.mu.Unlock()
			return FileTransferCancel, nil
		}
		changed := control.changed
		c.mu.Unlock()
		select {
		case <-ctx.Done():
			return "", ctx.Err()
		case <-changed:
		}
	}
}

func (c *leaseCoordinator) recordFileTransferProgress(sessionID string, metadata map[string]any) {
	c.mu.Lock()
	defer c.mu.Unlock()
	control := c.fileTransfers[sessionID]
	if control == nil {
		return
	}
	control.transferred = fileTransferMetadataInt64(metadata, "sent", "received", "transferred")
	control.total = fileTransferMetadataInt64(metadata, "total")
	control.percent = fileTransferMetadataFloat64(metadata, "percent")
}

// validateFileTransferReporter verifies the capability and transfer identity
// together while the lease state is locked. Event publication remains outside
// this coordinator because Session owns the structured event buffer.
func (c *leaseCoordinator) validateFileTransferReporter(sessionID, owner, transferID string) error {
	owner = strings.TrimSpace(owner)
	transferID = strings.TrimSpace(transferID)
	c.mu.Lock()
	defer c.mu.Unlock()
	lease, exists := c.leases[sessionID]
	if !exists || lease.Type != LeaseTypeFileTransfer || lease.Owner != owner {
		return ErrLeaseNotOwned
	}
	if transferID == "" || lease.TransferID != transferID {
		return ErrFileTransferIDMismatch
	}
	return nil
}

func (c *leaseCoordinator) fileTransferCancelConfirmed(sessionID string) bool {
	c.mu.Lock()
	defer c.mu.Unlock()
	control := c.fileTransfers[sessionID]
	return control != nil && control.state == "cancelled"
}

func fileTransferMetadataInt64(metadata map[string]any, keys ...string) int64 {
	for _, key := range keys {
		switch value := metadata[key].(type) {
		case int:
			return int64(value)
		case int64:
			return value
		case float64:
			return int64(value)
		}
	}
	return 0
}

func fileTransferMetadataFloat64(metadata map[string]any, key string) float64 {
	switch value := metadata[key].(type) {
	case float64:
		return value
	case int:
		return float64(value)
	case int64:
		return float64(value)
	}
	return 0
}

func (c *leaseCoordinator) status(sessionID string) (SessionLease, bool) {
	gate := c.gate(sessionID)
	gate.Lock()
	defer gate.Unlock()
	c.mu.Lock()
	lease, ok := c.leases[sessionID]
	if ok && !time.Now().UTC().Before(lease.ExpiresAt) {
		expired, abortSession := c.expireLocked(sessionID)
		c.mu.Unlock()
		c.notifyExpired(expired, abortSession)
		return SessionLease{}, false
	}
	c.mu.Unlock()
	return lease, ok
}

func (c *leaseCoordinator) write(sessionID, owner, displayID string, operation func() (int, error)) (int, error) {
	requireLease := strings.TrimSpace(owner) != ""
	gate := c.gate(sessionID)
	gate.Lock()
	defer gate.Unlock()
	c.mu.Lock()
	if c.recovering[sessionID] {
		c.mu.Unlock()
		if requireLease {
			return 0, ErrLeaseNotOwned
		}
		return 0, fmt.Errorf("%w: Session %s is closing after an expired in-flight write", ErrSessionBusy, displayID)
	}
	lease, exists := c.leases[sessionID]
	if exists && !time.Now().UTC().Before(lease.ExpiresAt) {
		expired, abortSession := c.expireLocked(sessionID)
		c.mu.Unlock()
		c.notifyExpired(expired, abortSession)
		if requireLease {
			return 0, ErrLeaseNotOwned
		}
		return operation()
	}
	if requireLease {
		if !exists || lease.Owner != owner {
			c.mu.Unlock()
			return 0, ErrLeaseNotOwned
		}
		c.activeWrites[sessionID]++
		c.mu.Unlock()
		written, err := operation()
		c.mu.Lock()
		if c.activeWrites[sessionID] <= 1 {
			delete(c.activeWrites, sessionID)
		} else {
			c.activeWrites[sessionID]--
		}
		c.mu.Unlock()
		return written, err
	}
	c.mu.Unlock()
	if !exists {
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
	c.stopExpiryLocked(sessionID)
	delete(c.leases, sessionID)
	delete(c.activeWrites, sessionID)
	delete(c.recovering, sessionID)
	if control := c.fileTransfers[sessionID]; control != nil {
		close(control.released)
		close(control.changed)
		delete(c.fileTransfers, sessionID)
	}
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
