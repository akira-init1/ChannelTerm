package app

import (
	"context"
	"errors"
	"slices"
	"strings"
	"sync"
	"unicode"

	"github.com/akira-init1/ChannelTerm/internal/core/config"
	serialtransport "github.com/akira-init1/ChannelTerm/internal/core/transport/serial"
)

// ErrManualOnly denies external automation access to a protected connection.
var ErrManualOnly = errors.New("this connection is restricted to manual use")

type connectionAccess struct {
	mu      sync.Mutex
	update  sync.Mutex
	path    string
	blocked map[config.ManualConnection]bool
	calls   map[*protectedCall]config.ManualConnection
}
type protectedCall struct{ cancel context.CancelCauseFunc }

func connectionKey(transport, endpoint string) config.ManualConnection {
	if transport == "serial" {
		endpoint = serialtransport.EndpointKey(endpoint)
	}
	if transport == "ssh" {
		if user, address, ok := strings.Cut(endpoint, "@"); ok {
			endpoint = user + "@" + strings.ToLower(address)
		}
	}
	return config.ManualConnection{Transport: transport, Endpoint: endpoint}
}
func (a *Application) connectionFor(id string) config.ManualConnection {
	transport, key := a.manager.ConnectionEndpoint(id)
	if transport == "serial" {
		return config.ManualConnection{Transport: transport, Endpoint: serialtransport.PortNameKey(key)}
	}
	return connectionKey(transport, key)
}
func manualClient(ctx context.Context) bool {
	return ClientIdentityFromContext(ctx).Kind == "user" || !controlExternal(ctx) && ClientIdentityFromContext(ctx).ID == ""
}
func (a *Application) checkConnectionAccess(ctx context.Context, key config.ManualConnection) error {
	if manualClient(ctx) {
		return nil
	}
	a.access.mu.Lock()
	defer a.access.mu.Unlock()
	if a.access.blocked[key] {
		return ErrManualOnly
	}
	return nil
}

// SessionVisible applies the Host policy before returning inventory metadata.
func (a *Application) SessionVisible(ctx context.Context, id string) bool {
	return a.checkConnectionAccess(ctx, a.connectionFor(id)) == nil
}

// GuardSessionCall registers a cancellable external call. Its completion check
// discards buffered results even if the underlying reader raced cancellation.
func (a *Application) GuardSessionCall(ctx context.Context, id string) (context.Context, func() error, error) {
	if id == "" || manualClient(ctx) {
		return ctx, func() error { return nil }, nil
	}
	return a.guardConnection(ctx, a.connectionFor(id))
}

func (a *Application) guardConnection(ctx context.Context, key config.ManualConnection) (context.Context, func() error, error) {
	if manualClient(ctx) {
		return ctx, func() error { return nil }, nil
	}
	a.access.mu.Lock()
	defer a.access.mu.Unlock()
	if a.access.blocked[key] {
		return nil, nil, ErrManualOnly
	}
	child, cancel := context.WithCancelCause(ctx)
	call := &protectedCall{cancel}
	if a.access.calls == nil {
		a.access.calls = make(map[*protectedCall]config.ManualConnection)
	}
	a.access.calls[call] = key
	return child, func() error {
		a.access.mu.Lock()
		defer a.access.mu.Unlock()
		delete(a.access.calls, call)
		cause := context.Cause(child)
		cancel(nil)
		if errors.Is(cause, ErrManualOnly) || a.access.blocked[key] {
			return ErrManualOnly
		}
		return nil
	}, nil
}

// SetManualOnly persists endpoint policy and fences all matching calls and active
// agent operations, including independent SSH shells. Disabling starts no work.
func (a *Application) SetManualOnly(ctx context.Context, id string, p ControlProof, enabled bool) (ControlStatus, error) {
	if err := requireHumanControl(ctx); err != nil {
		return ControlStatus{}, err
	}
	a.access.update.Lock()
	defer a.access.update.Unlock()
	t, _, err := a.controlTarget(id)
	if err != nil {
		return ControlStatus{}, err
	}
	if err = a.validateControlIdentity(ctx, t.ID()); err != nil {
		return ControlStatus{}, err
	}
	status, err := a.SessionControl(ctx, id)
	if err != nil {
		return ControlStatus{}, err
	}
	if p.Version != status.Version || p.OperationID != status.OperationID {
		return ControlStatus{}, ErrControlStale
	}
	key := a.connectionFor(t.ID())
	if key.Endpoint == "" {
		return ControlStatus{}, ErrSessionNotFound
	}
	saveErr := a.setConnectionPolicy(ctx, key, enabled)

	if enabled {
		for _, info := range a.ListSessions() {
			if a.connectionFor(info.ID) != key {
				continue
			}
			a.leases.mu.Lock()
			if r := a.leases.controls.entries[info.ID]; r != nil && r.wait != nil {
				r.wait.finish("superseded")
				r.wait = nil
			}
			a.leases.mu.Unlock()
			view, e := a.SessionControl(ctx, info.ID)
			if e != nil || view.State != "active" || view.Client.Kind != "agent" {
				continue
			}
			if _, e = a.TakeControl(ctx, info.ID, ControlProof{Version: view.Version, OperationID: view.OperationID}); e != nil && !errors.Is(e, ErrControlStale) {
				return ControlStatus{}, e
			}
		}
	}
	if saveErr != nil {
		return ControlStatus{}, saveErr
	}
	return a.SessionControl(ctx, id)
}

// ManualConnections lists protected endpoint keys for the authenticated operator.
func (a *Application) ManualConnections(ctx context.Context) ([]config.ManualConnection, error) {
	if err := requireHumanControl(ctx); err != nil {
		return nil, err
	}
	if err := a.validateControlIdentity(ctx, ""); err != nil {
		return nil, err
	}
	a.access.mu.Lock()
	defer a.access.mu.Unlock()
	keys := make([]config.ManualConnection, 0, len(a.access.blocked))
	for key := range a.access.blocked {
		keys = append(keys, key)
	}
	return keys, nil
}

// SetSavedConnectionPolicy applies a saved endpoint setting while disconnected.
// A live target requires a captured Session generation through SetManualOnly.
func (a *Application) SetSavedConnectionPolicy(ctx context.Context, transport, endpoint string, enabled bool) error {
	if err := requireHumanControl(ctx); err != nil {
		return err
	}
	if err := a.validateControlIdentity(ctx, ""); err != nil {
		return err
	}
	if (transport != "serial" && transport != "ssh") || len(endpoint) < 1 || len(endpoint) > 1024 || strings.ContainsFunc(endpoint, unicode.IsControl) {
		return errors.New("invalid saved connection")
	}
	a.access.update.Lock()
	defer a.access.update.Unlock()
	key := connectionKey(transport, endpoint)
	for _, info := range a.ListSessions() {
		if a.connectionFor(info.ID) == key {
			return ErrControlStale
		}
	}
	return a.setConnectionPolicy(ctx, key, enabled)
}

// checkTransferPause rejects an unbound SSH transfer targeting a manually
// controlled shell's login. A separate SSH file channel has no Session proof.
func (a *Application) checkTransferPause(ctx context.Context, key config.ManualConnection) error {
	if key.Transport != "ssh" || manualClient(ctx) {
		return nil
	}
	a.leases.mu.Lock()
	defer a.leases.mu.Unlock()
	for _, r := range a.leases.controls.entries {
		if r.view.AIPaused && a.connectionFor(r.reference) == key {
			return ErrAgentPaused
		}
	}
	return nil
}

func (a *Application) setConnectionPolicy(ctx context.Context, key config.ManualConnection, enabled bool) error {
	// Revoke reads and admission before filesystem waits. A failed durable save
	// leaves the live protection enabled and reports the persistence failure.
	if enabled {
		a.applyConnectionPolicy(key, true)
	}
	if a.access.path != "" {
		err := config.UpdateContext(ctx, a.access.path, func(file *config.File) error {
			file.ManualOnly = slices.DeleteFunc(file.ManualOnly, func(entry config.ManualConnection) bool { return connectionKey(entry.Transport, entry.Endpoint) == key })
			if enabled {
				if len(file.ManualOnly) >= 256 {
					return errors.New("manual connection limit reached")
				}
				file.ManualOnly = append(file.ManualOnly, key)
			}
			return nil
		})
		if err != nil {
			return err
		}
	}
	if !enabled {
		a.applyConnectionPolicy(key, false)
	}
	return nil
}
func (a *Application) applyConnectionPolicy(key config.ManualConnection, enabled bool) {
	a.access.mu.Lock()
	if enabled {
		a.access.blocked[key] = true
		for call, target := range a.access.calls {
			if target == key {
				call.cancel(ErrManualOnly)
			}
		}
	} else {
		delete(a.access.blocked, key)
	}
	a.access.mu.Unlock()
}
