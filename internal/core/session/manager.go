package session

import (
	"context"
	"errors"
	"strconv"
	"strings"
	"sync"
)

// ErrDuplicateSession is returned when registering an ID that Manager owns.
var ErrDuplicateSession = errors.New("session ID is already registered")

// Manager owns the set of active sessions.
//
// Manager protects registration and lookup with a read-write lock. It does not
// hold that lock while closing sessions, because Close can block on Transport
// resource cleanup.
type Manager struct {
	mu             sync.RWMutex
	sessions       map[string]registeredSession
	nextReferences map[string]uint64
	openings       map[string]*endpointOpening
}

// SessionMetadata describes fixed, display-oriented information for a Session.
//
// Manager records Metadata when the Session is registered. Reference is a
// short, transport-prefixed identifier assigned by Manager and accepted as an
// alternative to the opaque Session ID. It remains valid only while Manager
// owns the Session and is never reused during the Manager lifetime.
type SessionMetadata struct {
	Transport string
	Endpoint  string
	Label     string
	Reference string
}

// SessionInfo is a point-in-time view of one Manager-owned Session.
//
// Metadata is fixed at registration. State is read when ListInfo creates the
// snapshot, so it can change immediately afterwards as the Session lifecycle
// proceeds.
type SessionInfo struct {
	ID       string
	Metadata SessionMetadata
	State    SessionState
}

// registeredSession keeps Manager ownership and fixed display metadata in the
// same map entry. Keeping metadata beside, rather than inside, a Transport
// preserves the protocol-neutral Session boundary.
type registeredSession struct {
	session  Session
	metadata SessionMetadata
}

// endpointOpening coordinates one in-progress physical connection. Waiters do
// not create a second Transport for the same endpoint; they receive the
// resulting Session or the original opening error after the owner finishes.
type endpointOpening struct {
	done chan struct{}
	info SessionInfo
	err  error
}

// NewManager creates an empty Manager ready to register Sessions.
func NewManager() *Manager {
	return &Manager{
		sessions:       make(map[string]registeredSession),
		nextReferences: make(map[string]uint64),
		openings:       make(map[string]*endpointOpening),
	}
}

// Register adds s under its ID.
//
// s remains owned by Manager until Remove or Close returns. Register returns
// ErrDuplicateSession when another Session already has the same ID.
//
// Register rejects duplicate IDs so callers cannot silently replace a live
// Session and lose responsibility for its resources.
func (m *Manager) Register(s Session) error {
	return m.RegisterWithMetadata(s, SessionMetadata{})
}

// RegisterWithMetadata adds s and its fixed display metadata under its ID.
//
// Metadata remains owned by Manager until Remove or Close returns. Manager
// assigns its Reference from Metadata.Transport, so callers must not rely on a
// caller-supplied Reference value. Duplicate labels and endpoints are
// permitted, while duplicate Session IDs return ErrDuplicateSession.
func (m *Manager) RegisterWithMetadata(s Session, metadata SessionMetadata) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	if _, exists := m.sessions[s.ID()]; exists {
		return ErrDuplicateSession
	}
	metadata.Reference = m.nextReferenceLocked(metadata.Transport)
	m.sessions[s.ID()] = registeredSession{session: s, metadata: metadata}
	return nil
}

// GetOrCreate returns the active Session for metadata's transport and endpoint
// or creates exactly one new Session through create.
//
// create runs without Manager's lock because opening a Transport can block. Its
// caller retains ownership of a failed candidate, while Manager takes ownership
// only after successful registration. Concurrent callers for the same endpoint
// wait for the first attempt and receive the same successful Session or error.
// Failed and closed Sessions do not prevent a later retry.
func (m *Manager) GetOrCreate(ctx context.Context, metadata SessionMetadata, create func() (Session, error)) (SessionInfo, bool, error) {
	if err := ctx.Err(); err != nil {
		return SessionInfo{}, false, err
	}
	if create == nil {
		return SessionInfo{}, false, errors.New("session create function must not be nil")
	}
	key := sessionEndpointKey(metadata.Transport, metadata.Endpoint)
	for {
		m.mu.Lock()
		if info, ok := m.activeSessionForEndpointLocked(metadata.Transport, metadata.Endpoint); ok {
			m.mu.Unlock()
			return info, false, nil
		}
		if opening, ok := m.openings[key]; ok {
			m.mu.Unlock()
			select {
			case <-ctx.Done():
				return SessionInfo{}, false, ctx.Err()
			case <-opening.done:
				if opening.err != nil {
					return SessionInfo{}, false, opening.err
				}
				return opening.info, false, nil
			}
		}
		opening := &endpointOpening{done: make(chan struct{})}
		m.openings[key] = opening
		m.mu.Unlock()

		candidate, err := create()
		if err == nil && candidate == nil {
			err = errors.New("session create function returned nil session")
		}
		var info SessionInfo
		if err == nil {
			m.mu.Lock()
			if _, exists := m.sessions[candidate.ID()]; exists {
				err = ErrDuplicateSession
			} else {
				metadata.Reference = m.nextReferenceLocked(metadata.Transport)
				m.sessions[candidate.ID()] = registeredSession{session: candidate, metadata: metadata}
				info = SessionInfo{ID: candidate.ID(), Metadata: metadata, State: candidate.State()}
			}
			m.mu.Unlock()
		}
		if err != nil && candidate != nil {
			if closeErr := candidate.Close(); closeErr != nil {
				err = errors.Join(err, closeErr)
			}
		}

		m.mu.Lock()
		opening.info = info
		opening.err = err
		delete(m.openings, key)
		close(opening.done)
		m.mu.Unlock()
		if err != nil {
			return SessionInfo{}, false, err
		}
		return info, true, nil
	}
}

// Get returns the Session registered under identifier and whether it exists.
//
// identifier can be the opaque ID supplied when the Session was created or the
// Manager-assigned short Reference. The returned Session remains owned by
// Manager and must not be closed by a lookup caller.
func (m *Manager) Get(identifier string) (Session, bool) {
	m.mu.RLock()
	defer m.mu.RUnlock()
	registered, ok := m.sessionLocked(identifier)
	return registered.session, ok
}

// Reference returns the short reference assigned to identifier.
//
// identifier can be either a Session ID or a short Reference. The returned
// reference is process-local and is not a persistent device identity.
func (m *Manager) Reference(identifier string) (string, bool) {
	m.mu.RLock()
	defer m.mu.RUnlock()
	registered, ok := m.sessionLocked(identifier)
	if !ok {
		return "", false
	}
	return registered.metadata.Reference, true
}

// List returns a snapshot of every currently registered Session.
//
// The returned slice is independent from Manager's internal map, but each
// Session remains owned by Manager. Callers must not close a listed Session;
// use Remove when ownership needs to transfer for explicit cleanup.
func (m *Manager) List() []Session {
	m.mu.RLock()
	defer m.mu.RUnlock()

	sessions := make([]Session, 0, len(m.sessions))
	for _, registered := range m.sessions {
		sessions = append(sessions, registered.session)
	}
	return sessions
}

// ListInfo returns a snapshot of every currently registered Session and its
// fixed metadata.
//
// The returned slice and metadata values are independent from Manager's map.
// Session State is read after the registration snapshot is copied so Manager
// never holds its lock while calling into a Session implementation.
func (m *Manager) ListInfo() []SessionInfo {
	m.mu.RLock()
	registered := make([]registeredSession, 0, len(m.sessions))
	for _, entry := range m.sessions {
		registered = append(registered, entry)
	}
	m.mu.RUnlock()

	infos := make([]SessionInfo, 0, len(registered))
	for _, entry := range registered {
		infos = append(infos, SessionInfo{
			ID:       entry.session.ID(),
			Metadata: entry.metadata,
			State:    entry.session.State(),
		})
	}
	return infos
}

// Remove unregisters and returns the Session under identifier without closing it.
//
// identifier can be either the opaque Session ID or its short Reference. The
// caller becomes responsible for closing a successfully removed Session.
func (m *Manager) Remove(identifier string) (Session, bool) {
	m.mu.Lock()
	defer m.mu.Unlock()
	id, registered, ok := m.sessionIDLocked(identifier)
	if ok {
		delete(m.sessions, id)
	}
	return registered.session, ok
}

// Close closes every registered Session and removes it from Manager.
//
// Close snapshots the current registrations before cleanup so it does not hold
// Manager's lock while a Session blocks during resource release. It returns all
// close failures joined together after every Session has been given a chance to
// close.
func (m *Manager) Close() error {
	m.mu.Lock()
	sessions := m.sessions
	m.sessions = make(map[string]registeredSession)
	m.mu.Unlock()

	var errs []error
	for _, registered := range sessions {
		if err := registered.session.Close(); err != nil {
			errs = append(errs, err)
		}
	}
	return errors.Join(errs...)
}

// nextReferenceLocked allocates a readable reference without reusing a value
// after a Session closes. The lock also protects the registration map, so a
// concurrent registration cannot observe the same reference.
func (m *Manager) nextReferenceLocked(transport string) string {
	key := strings.ToLower(strings.TrimSpace(transport))
	prefix := referencePrefix(key)
	for {
		m.nextReferences[key]++
		reference := prefix + "-" + strconv.FormatUint(m.nextReferences[key], 10)
		if _, _, exists := m.sessionIDLocked(reference); !exists {
			return reference
		}
	}
}

// sessionLocked resolves either durable Session IDs or short references while
// Manager's lock is held. Direct ID lookup wins so an unusual caller-provided
// ID can never be shadowed by a presentation reference.
func (m *Manager) sessionLocked(identifier string) (registeredSession, bool) {
	_, registered, ok := m.sessionIDLocked(identifier)
	return registered, ok
}

// sessionIDLocked returns the canonical map key for a supported identifier.
func (m *Manager) sessionIDLocked(identifier string) (string, registeredSession, bool) {
	if registered, ok := m.sessions[identifier]; ok {
		return identifier, registered, true
	}
	for id, registered := range m.sessions {
		if registered.metadata.Reference == identifier {
			return id, registered, true
		}
	}
	return "", registeredSession{}, false
}

// activeSessionForEndpointLocked returns one Session that still owns an
// endpoint. It intentionally excludes failed lifecycle states so callers can retry
// after an unplugged device or failed protocol connection.
func (m *Manager) activeSessionForEndpointLocked(transport, endpoint string) (SessionInfo, bool) {
	var selected SessionInfo
	for id, registered := range m.sessions {
		if registered.metadata.Transport != transport || registered.metadata.Endpoint != endpoint || !managerOwnsEndpoint(registered.session.State()) {
			continue
		}
		if selected.ID == "" || id < selected.ID {
			selected = SessionInfo{ID: id, Metadata: registered.metadata, State: registered.session.State()}
		}
	}
	return selected, selected.ID != ""
}

// managerOwnsEndpoint identifies lifecycle states whose Transport remains
// reserved. This duplicates no protocol behavior; it prevents a second opener
// from racing an already-open, connecting, or closing physical endpoint.
func managerOwnsEndpoint(state SessionState) bool {
	switch state {
	case StateNew, StateConnecting, StateOpen, StateClosing:
		return true
	default:
		return false
	}
}

// sessionEndpointKey keeps reservations transport-specific without imposing a
// platform-specific endpoint normalization rule on the protocol-neutral core.
func sessionEndpointKey(transport, endpoint string) string {
	return transport + "\x00" + endpoint
}

// referencePrefix keeps short references unambiguous across the transport
// families ChannelTerm supports. Unknown future transports remain readable
// without requiring a Session-core change.
func referencePrefix(transport string) string {
	switch transport {
	case "serial":
		return "SER"
	case "ssh":
		return "SSH"
	case "telnet":
		return "TELNET"
	case "":
		return "SESSION"
	default:
		return strings.ToUpper(strings.Join(strings.Fields(transport), "-"))
	}
}
