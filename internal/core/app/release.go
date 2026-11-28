package app

import (
	"context"

	"github.com/akira-init1/ChannelTerm/internal/core/session"
	serialtransport "github.com/akira-init1/ChannelTerm/internal/core/transport/serial"
)

// ReleaseTarget identifies a specific registration and its observed retention reason.
type ReleaseTarget struct {
	ID        string `json:"id"`
	Reference string `json:"reference"`
	Endpoint  string `json:"endpoint"`
	Transport string `json:"transport"`
	Reason    string `json:"reason"`
}

func checkReleaseAccess(ctx context.Context) error {
	if ClientIdentityFromContext(ctx).Kind != "user" || CheckClientAccess(ctx, ClientManage) != nil {
		return ErrClientPermissionDenied
	}
	return ctx.Err()
}

// ReleaseTargets snapshots open registrations without opening devices. An empty
// selector lists all; serial aliases are compared against the registration-time key.
func (a *Application) ReleaseTargets(ctx context.Context, selector string) ([]ReleaseTarget, error) {
	if err := checkReleaseAccess(ctx); err != nil {
		return nil, err
	}
	_, debug, err := a.idleObservation(ctx)
	if err != nil {
		return nil, err
	}
	key := serialtransport.EndpointKey(selector)
	a.leases.mu.Lock()
	defer a.leases.mu.Unlock()
	a.instances.mu.Lock()
	defer a.instances.mu.Unlock()
	now := a.instances.clock()
	targets := []ReleaseTarget{}
	for _, item := range a.ListSessions() {
		m := item.Metadata
		if item.State != session.StateOpen {
			continue
		}
		if selector != "" && selector != item.ID && selector != m.Reference {
			if m.Transport != "serial" || key == "" {
				continue
			}
			if a.connectionFor(item.ID).Endpoint != key {
				continue
			}
		}
		reason := a.idleReasonLocked(item.ID, m.Reference, debug, now)
		if reason == "idle" && a.idle.policy.SessionTimeout == 0 {
			reason = "keep"
		}
		targets = append(targets, ReleaseTarget{ID: item.ID, Reference: m.Reference, Endpoint: m.Endpoint, Transport: m.Transport, Reason: reason})
	}
	return targets, nil
}

// ReleaseRegistration revokes admission on the confirmed registration before
// closing its stream to wake blocked I/O. Cleanup never holds the admission lock over I/O.
func (a *Application) ReleaseRegistration(ctx context.Context, id, reference string) error {
	if err := checkReleaseAccess(ctx); err != nil {
		return err
	}
	done, err := a.beginIdleWork()
	if err != nil {
		return err
	}
	defer done()
	a.leases.mu.Lock()
	t, ok := a.manager.RemoveRegistration(id, reference)
	var cancel context.CancelCauseFunc
	if r := a.leases.controls.entries[id]; ok && r != nil && r.reference == reference {
		cancel = r.cancel
		if r.timer != nil {
			r.timer.Stop()
		}
		if r.wait != nil {
			r.wait.finish("recovery_failed")
		}
		delete(a.leases.controls.entries, id)
	}
	a.leases.mu.Unlock()
	if cancel != nil {
		cancel(ErrControlInterrupted)
	}
	if !ok {
		return ErrSessionNotFound
	}
	// Serial jobs retain the non-reused reference. Independent SSH jobs may
	// share a hostname with this shell but own different connections.
	a.transfers.mu.Lock()
	for _, run := range a.transfers.runs {
		if run.request.Transport == "serial" && run.request.SessionID == reference {
			run.cancel()
		}
	}
	a.transfers.mu.Unlock()
	err = t.Close()
	a.leases.removeRegistration(id, reference)
	return err
}
