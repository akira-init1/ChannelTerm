package app

import (
	"context"
	"errors"
	"fmt"
	"io"
	"os"
	"sort"
	"strings"

	"github.com/akira-init1/ChannelTerm/internal/core/config"
	"github.com/akira-init1/ChannelTerm/internal/core/connectionpolicy"
	"github.com/akira-init1/ChannelTerm/internal/core/device"
	"github.com/akira-init1/ChannelTerm/internal/core/session"
	serialtransport "github.com/akira-init1/ChannelTerm/internal/core/transport/serial"
)

var (
	// ErrNilApplicationManager is returned when Application cannot own serial
	// use cases because no Session Manager was supplied.
	ErrNilApplicationManager = errors.New("application session manager must not be nil")
	// ErrNilDeviceRegistry is returned when a device use case is requested
	// without the optional discovery dependency configured.
	ErrNilDeviceRegistry = errors.New("application device registry must not be nil")
	// ErrSessionNotFound is returned when a Session use case receives no active
	// Session ID or short reference owned by the Application's Manager.
	ErrSessionNotFound = errors.New("application session not found")
)

// Dependencies supplies the Core capabilities assembled into an Application.
//
// Manager is required because Application always provides Session and Serial
// use cases. Devices is optional for callers that only open local sessions;
// device-list and connection-decision use cases return ErrNilDeviceRegistry
// until a Registry is supplied. Policy defaults to connectionpolicy.Default.
// Serial and ListSerialPorts are test seams; nil values select production
// configuration, Serial Transport construction, and port enumeration.
type Dependencies struct {
	Manager         *session.Manager
	Devices         *device.Registry
	Policy          connectionpolicy.Policy
	Serial          SerialDependencies
	ListSerialPorts func() ([]serialtransport.Port, error)
}

// Application is the UI-independent entry point for ChannelTerm use cases.
//
// It delegates each concern to the existing SerialService, Session Manager,
// Device Registry, and Connection Policy packages. Application intentionally
// owns no terminal presentation, protocol schema, stdin/stdout, or process
// lifetime; the Composition Root owns the supplied Manager and Registry.
type Application struct {
	serial          *SerialService
	leases          *leaseCoordinator
	devices         *device.Registry
	policy          connectionpolicy.Policy
	listSerialPorts func() ([]serialtransport.Port, error)
}

// New creates an Application from Core runtime dependencies.
//
// New does not start or close the supplied Device Registry and does not close
// Manager. The caller that assembled those long-lived resources retains their
// lifecycle ownership, allowing one Application to serve multiple adapters.
func New(dependencies Dependencies) (*Application, error) {
	if dependencies.Manager == nil {
		return nil, ErrNilApplicationManager
	}
	serial, err := NewSerialServiceWithDependencies(dependencies.Manager, dependencies.Serial)
	if err != nil {
		return nil, err
	}
	policy := dependencies.Policy
	if policy == "" {
		policy = connectionpolicy.Default
	}
	if _, err := connectionpolicy.Parse(string(policy)); err != nil {
		return nil, err
	}
	listPorts := dependencies.ListSerialPorts
	if listPorts == nil {
		listPorts = serialtransport.ListPorts
	}
	return &Application{
		serial:          serial,
		leases:          newLeaseCoordinator(),
		devices:         dependencies.Devices,
		policy:          policy,
		listSerialPorts: listPorts,
	}, nil
}

// OpenSerial resolves a profile and explicit overrides, opens or reuses a
// serial Session, and returns structured Manager-owned Session metadata.
//
// ctx cancels configuration access and connection setup. A wake byte is sent
// only when the resolved profile enables it, preserving the existing explicit
// opt-in behavior.
func (a *Application) OpenSerial(ctx context.Context, request OpenSerialRequest) (OpenSerialResult, error) {
	if a == nil || a.serial == nil {
		return OpenSerialResult{}, ErrNilApplicationManager
	}
	return a.serial.OpenSerial(ctx, request)
}

// ListSessions returns a point-in-time snapshot of Application-managed
// Sessions without exposing their Transport implementations.
func (a *Application) ListSessions() []session.SessionInfo {
	if a == nil || a.serial == nil {
		return nil
	}
	return a.serial.ListSessions()
}

// ReadSession returns retained terminal output or waits for output after the
// supplied cursor. A nil cursor reads recent output without waiting.
//
// ctx controls only cursor waits. The returned OutputChunk is raw terminal
// data and is never converted to CLI or protocol presentation text.
func (a *Application) ReadSession(ctx context.Context, identifier string, cursor *session.OutputCursor, maxBytes int) (session.OutputChunk, error) {
	terminal, err := a.session(identifier)
	if err != nil {
		return session.OutputChunk{}, err
	}
	if cursor == nil {
		return terminal.ReadRecent(maxBytes)
	}
	return terminal.ReadOutput(ctx, *cursor, maxBytes)
}

// ReadSessionActivity returns retained operation activity or waits after the
// supplied cursor. A nil cursor reads the most recent retained events.
//
// ctx controls only cursor waits. Activity remains separate from terminal
// output so adapters cannot accidentally interpret local metadata as device
// bytes.
func (a *Application) ReadSessionActivity(ctx context.Context, identifier string, cursor *session.ActivityCursor, maxEvents int) (session.ActivityChunk, error) {
	terminal, err := a.session(identifier)
	if err != nil {
		return session.ActivityChunk{}, err
	}
	if cursor == nil {
		return terminal.ReadRecentActivity(maxEvents)
	}
	return terminal.ReadActivity(ctx, *cursor, maxEvents)
}

// WriteSession writes one complete payload to a managed Session. It rejects
// writes while another operation owns an exclusive lease.
//
// ctx is checked before the operation and between short-write retries. The
// Session retains responsibility for serializing concurrent writers and for
// recording the supplied Actor; Application never changes request.Data.
func (a *Application) WriteSession(ctx context.Context, identifier string, request session.WriteRequest) (int, error) {
	return a.writeSession(ctx, identifier, "", request)
}

// WriteSessionWithLease writes one complete payload using owner as the
// capability for an active lease on identifier. owner must match the active
// lease exactly; ordinary writes must continue to use WriteSession.
func (a *Application) WriteSessionWithLease(ctx context.Context, identifier, owner string, request session.WriteRequest) (int, error) {
	return a.writeSession(ctx, identifier, owner, request)
}

func (a *Application) writeSession(ctx context.Context, identifier, owner string, request session.WriteRequest) (int, error) {
	if err := ctx.Err(); err != nil {
		return 0, err
	}
	terminal, err := a.session(identifier)
	if err != nil {
		return 0, err
	}
	if !request.Actor.Valid() {
		return 0, session.ErrInvalidActor
	}
	return a.leases.write(terminal.ID(), owner, strings.TrimSpace(identifier), func() (int, error) {
		remaining := request.Data
		written := 0
		for len(remaining) > 0 {
			if err := ctx.Err(); err != nil {
				return written, err
			}
			n, err := terminal.Write(session.WriteRequest{Actor: request.Actor, Data: remaining})
			written += n
			if err != nil {
				return written, err
			}
			if n <= 0 {
				return written, io.ErrShortWrite
			}
			remaining = remaining[n:]
		}
		return written, nil
	})
}

// AcquireLease creates one exclusive application-level lease for an active
// Session. It does not affect readers, raw output, or Session lifecycle.
func (a *Application) AcquireLease(identifier, owner string, typ LeaseType) (SessionLease, error) {
	terminal, err := a.session(identifier)
	if err != nil {
		return SessionLease{}, err
	}
	return a.leases.acquire(terminal.ID(), owner, typ)
}

// ReleaseLease releases identifier's lease only when owner matches its owner
// capability. Releasing an already absent lease is successful and idempotent.
func (a *Application) ReleaseLease(identifier, owner string) error {
	terminal, err := a.session(identifier)
	if err != nil {
		return err
	}
	return a.leases.release(terminal.ID(), owner)
}

// LeaseStatus reports identifier's current exclusive lease, if any.
func (a *Application) LeaseStatus(identifier string) (SessionLease, bool, error) {
	terminal, err := a.session(identifier)
	if err != nil {
		return SessionLease{}, false, err
	}
	lease, ok := a.leases.status(terminal.ID())
	return lease, ok, nil
}

// CloseSession removes and closes a managed Session identified by its opaque
// ID or short reference. It returns the pre-close SessionInfo so adapters can
// report stable metadata without retaining the Session object.
func (a *Application) CloseSession(identifier string) (session.SessionInfo, error) {
	identifier = strings.TrimSpace(identifier)
	if identifier == "" {
		return session.SessionInfo{}, ErrSessionNotFound
	}
	for _, info := range a.ListSessions() {
		if info.ID != identifier && info.Metadata.Reference != identifier {
			continue
		}
		closed, closeErr := a.serial.CloseSession(identifier)
		if !closed {
			return session.SessionInfo{}, ErrSessionNotFound
		}
		a.leases.remove(info.ID)
		if closeErr != nil {
			return session.SessionInfo{}, closeErr
		}
		return info, nil
	}
	return session.SessionInfo{}, ErrSessionNotFound
}

// ListSerialPorts returns operating-system-reported ports without opening a
// Transport, creating a Session, or changing device discovery state.
func (a *Application) ListSerialPorts(ctx context.Context) ([]serialtransport.Port, error) {
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	if a == nil || a.listSerialPorts == nil {
		return nil, errors.New("application serial port lister must not be nil")
	}
	ports, err := a.listSerialPorts()
	if err != nil {
		return nil, fmt.Errorf("list serial ports: %w", err)
	}
	if ports == nil {
		return []serialtransport.Port{}, nil
	}
	return ports, nil
}

// ListDevices returns the current Device Registry snapshot without opening an
// endpoint or creating a Session.
func (a *Application) ListDevices(ctx context.Context) ([]device.Device, error) {
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	if a == nil || a.devices == nil {
		return nil, ErrNilDeviceRegistry
	}
	return a.devices.List(), nil
}

// ReadDeviceEvents returns retained device transitions or waits after cursor.
// A nil cursor reads recent retained events without waiting.
func (a *Application) ReadDeviceEvents(ctx context.Context, cursor *device.Cursor, maxEvents int) (device.EventChunk, error) {
	if a == nil || a.devices == nil {
		return device.EventChunk{}, ErrNilDeviceRegistry
	}
	if cursor == nil {
		return a.devices.ReadRecent(maxEvents)
	}
	return a.devices.Read(ctx, *cursor, maxEvents)
}

// ConnectionDecision returns the policy action for one discovered endpoint.
// It never opens a Transport, creates a Session, or prompts a user.
func (a *Application) ConnectionDecision(ctx context.Context, transport, endpoint string) (ConnectionDecision, error) {
	if err := ctx.Err(); err != nil {
		return ConnectionDecision{}, err
	}
	if a == nil || a.devices == nil {
		return ConnectionDecision{}, ErrNilDeviceRegistry
	}
	transport = strings.TrimSpace(transport)
	endpoint = strings.TrimSpace(endpoint)
	if transport == "" {
		return ConnectionDecision{}, errors.New("connection decision transport is required")
	}
	if endpoint == "" {
		return ConnectionDecision{}, errors.New("connection decision endpoint is required")
	}
	present := false
	for _, discovered := range a.devices.List() {
		if discovered.Transport == transport && discovered.Endpoint == endpoint && discovered.State == device.StatePresent {
			present = true
			break
		}
	}
	var connectedID string
	for _, info := range a.ListSessions() {
		if info.Metadata.Transport != transport || info.Metadata.Endpoint != endpoint || !activeSessionState(info.State) {
			continue
		}
		if connectedID == "" || info.ID < connectedID {
			connectedID = info.ID
		}
	}
	decision := ConnectionDecision{
		Transport: transport,
		Endpoint:  endpoint,
		Present:   present,
		Connected: connectedID != "",
		Policy:    a.policy,
		Action:    connectionpolicy.Decide(a.policy, present, connectedID != ""),
		SessionID: connectedID,
	}
	if connectedID != "" {
		for _, info := range a.ListSessions() {
			if info.ID == connectedID {
				decision.SessionReference = info.Metadata.Reference
				break
			}
		}
	}
	return decision, nil
}

// ConnectionDecision is the structured result of evaluating a discovered
// endpoint against the current Device Registry, Session snapshot, and policy.
type ConnectionDecision struct {
	Transport        string
	Endpoint         string
	Present          bool
	Connected        bool
	Policy           connectionpolicy.Policy
	Action           connectionpolicy.Action
	SessionID        string
	SessionReference string
}

// SerialProfileInfo is one named, fully resolved Serial connection profile.
// Name identifies the configuration entry; Profile contains inherited values
// after resolution and is never rendered for a particular Adapter.
type SerialProfileInfo struct {
	Name    string
	Profile config.SerialProfile
}

// ListSerialProfiles loads an existing configuration file and returns named
// Serial profiles after inheritance resolution. An absent file is treated as
// an empty profile list so read-only discovery never creates configuration.
func (a *Application) ListSerialProfiles(ctx context.Context, configuredPath string) ([]SerialProfileInfo, error) {
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	path := strings.TrimSpace(configuredPath)
	if path == "" {
		var err error
		path, err = config.DefaultPath()
		if err != nil {
			return nil, fmt.Errorf("resolve serial configuration path: %w", err)
		}
	}
	file, err := config.Load(path)
	if errors.Is(err, os.ErrNotExist) {
		return []SerialProfileInfo{}, nil
	}
	if err != nil {
		return nil, fmt.Errorf("load serial configuration %q: %w", path, err)
	}
	names := make([]string, 0, len(file.Serial.Profiles))
	for name := range file.Serial.Profiles {
		names = append(names, name)
	}
	sort.Strings(names)
	profiles := make([]SerialProfileInfo, 0, len(names))
	for _, name := range names {
		profile, err := file.ResolveSerial(name)
		if err != nil {
			return nil, fmt.Errorf("resolve serial profile %q: %w", name, err)
		}
		profiles = append(profiles, SerialProfileInfo{Name: name, Profile: profile})
	}
	return profiles, nil
}

// ResolveSerialTarget resolves a currently present `SER-<port>` reference to
// its operating-system serial port name. It performs discovery only and never
// opens a Transport or creates a Session.
func (a *Application) ResolveSerialTarget(ctx context.Context, reference string) (string, error) {
	reference = strings.TrimSpace(reference)
	if !strings.HasPrefix(strings.ToUpper(reference), "SER-") {
		return "", fmt.Errorf("unsupported direct target reference %q; currently only SER-* targets are supported", reference)
	}
	ports, err := a.ListSerialPorts(ctx)
	if err != nil {
		return "", fmt.Errorf("list serial targets: %w", err)
	}
	for _, port := range ports {
		if strings.EqualFold("SER-"+strings.TrimSpace(port.Name), reference) {
			return port.Name, nil
		}
	}
	return "", fmt.Errorf("serial target reference %q is not present; run channelterm list --transport serial", reference)
}

// session resolves one Manager-owned Session without transferring ownership.
func (a *Application) session(identifier string) (session.Session, error) {
	if a == nil || a.serial == nil {
		return nil, ErrNilApplicationManager
	}
	terminal, ok := a.serial.GetSession(strings.TrimSpace(identifier))
	if !ok {
		return nil, ErrSessionNotFound
	}
	return terminal, nil
}

// activeSessionState identifies lifecycles that still own an endpoint and
// therefore suppress a discovery-driven duplicate connection attempt.
func activeSessionState(state session.SessionState) bool {
	switch state {
	case session.StateNew, session.StateConnecting, session.StateOpen, session.StateClosing:
		return true
	default:
		return false
	}
}
