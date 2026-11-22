package app

import (
	"context"
	"errors"
	"time"

	"github.com/akira-init1/ChannelTerm/internal/core/session"
)

// WaitOptions are the human operator's language and maximum interruption duration.
type WaitOptions struct {
	Locale  string
	Seconds int
}
type waitOptionsKey struct{}

// WithWaitOptions bounds human takeover settings before the operation is fenced.
func WithWaitOptions(ctx context.Context, o WaitOptions) (context.Context, error) {
	if o.Seconds < 0 || o.Seconds > 3600 || len(o.Locale) > 35 {
		return nil, errors.New("invalid wait settings")
	}
	return context.WithValue(ctx, waitOptionsKey{}, o), nil
}

// All interruptedTask fields except immutable metadata are guarded by leases.mu.
// Keeping the completed signal handles a human releasing before registration.
type interruptedTask struct {
	original  ControlProof
	owner     session.ClientIdentity
	deadline  time.Time
	options   WaitOptions
	done      chan struct{}
	outcome   string
	pending   bool
	delivered bool
}

func (w *interruptedTask) owns(ctx context.Context) bool {
	client := ClientIdentityFromContext(ctx)
	return client.ID == w.owner.ID && client.InstanceID == w.owner.InstanceID
}

func (w *interruptedTask) finish(outcome string) {
	if w.outcome != "" {
		return
	}
	if !time.Now().Before(w.deadline) {
		outcome = "timed_out"
	}
	w.outcome = outcome
	close(w.done)
}

// WaitNotice tells the original client how to handle fenced input without replay.
type WaitNotice struct {
	ControlProof
	Status         string    `json:"status"`
	WriteForbidden bool      `json:"write_forbidden"`
	Deadline       time.Time `json:"deadline"`
	RetryAllowed   bool      `json:"retry_allowed"`
	Endpoint       string    `json:"endpoint"`
	Locale         string    `json:"-"`
	Seconds        int       `json:"wait_seconds"`
}

func (w *interruptedTask) notice(endpoint string) WaitNotice {
	status := w.outcome
	if status == "" {
		status = "user_takeover"
	}
	if (status == "user_takeover" || status == "allowed") && !time.Now().Before(w.deadline) {
		status = "timed_out"
	}
	return WaitNotice{ControlProof: w.original, Status: status, WriteForbidden: status != "allowed", Deadline: w.deadline, Endpoint: endpoint, Locale: w.options.Locale, Seconds: w.options.Seconds}
}

// InterruptedTaskNotice is scoped to the original credential and instance. The
// original proof identifies one task even after a later control generation.
func (a *Application) InterruptedTaskNotice(ctx context.Context, id string, p ControlProof) (WaitNotice, bool) {
	t, _, err := a.controlTarget(id)
	if err != nil {
		return WaitNotice{}, false
	}
	info := a.diagnosticSessionInfo(t.ID())
	a.leases.mu.Lock()
	defer a.leases.mu.Unlock()
	r := a.leases.controls.entries[t.ID()]
	if r == nil || r.wait == nil || r.wait.delivered || (p.Version != "" && p.Version != r.view.Version && p.Version != r.wait.original.Version) || (p.OperationID != "" && p.OperationID != r.wait.original.OperationID) || !r.wait.owns(ctx) {
		return WaitNotice{}, false
	}
	return r.wait.notice(info.Metadata.Endpoint), true
}

// InterruptedNoticeForRequest resolves the original version from trusted request context.
func (a *Application) InterruptedNoticeForRequest(ctx context.Context, id string) (WaitNotice, bool) {
	p, _ := ctx.Value(controlProofKey{}).(ControlProof)
	return a.InterruptedTaskNotice(ctx, id, p)
}

// WaitForHuman registers one live request for the exact interrupted operation.
// Cancellation, expired presence and timeout never change write permission.
func (a *Application) WaitForHuman(ctx context.Context, id string, p ControlProof) (WaitNotice, error) {
	t, _, err := a.controlTarget(id)
	if err != nil {
		return WaitNotice{}, err
	}
	if err = a.validateControlIdentity(ctx, t.ID()); err != nil {
		return WaitNotice{}, err
	}
	endpoint := a.diagnosticSessionInfo(t.ID()).Metadata.Endpoint
	a.leases.mu.Lock()
	r := a.leases.controls.entries[t.ID()]
	if r == nil || r.wait == nil || r.wait.original != p || !r.wait.owns(ctx) {
		a.leases.mu.Unlock()
		return WaitNotice{}, ErrControlStale
	}
	w := r.wait
	if w.delivered {
		a.leases.mu.Unlock()
		return WaitNotice{}, ErrControlStale
	}
	if w.pending {
		a.leases.mu.Unlock()
		return WaitNotice{}, ErrControlBusy
	}
	if w.outcome != "" {
		w.delivered = true
		notice := w.notice(endpoint)
		a.leases.mu.Unlock()
		return notice, nil
	}
	w.pending = true
	a.leases.mu.Unlock()
	defer func() { a.leases.mu.Lock(); w.pending = false; a.leases.mu.Unlock() }()
	timer := time.NewTimer(time.Until(w.deadline))
	defer timer.Stop()
	tick := time.NewTicker(time.Second)
	defer tick.Stop()
	for {
		select {
		case <-ctx.Done():
			return WaitNotice{}, ctx.Err()
		case <-tick.C:
			if err := a.validateControlIdentity(ctx, t.ID()); err != nil {
				return WaitNotice{}, err
			}
			if _, _, err := a.controlTarget(id); err != nil {
				return WaitNotice{}, err
			}
			continue
		case <-timer.C:
			a.leases.mu.Lock()
			w.finish("timed_out")
			a.leases.mu.Unlock()
		case <-w.done:
		}
		a.leases.mu.Lock()
		w.delivered = true
		notice := w.notice(endpoint)
		a.leases.mu.Unlock()
		return notice, nil
	}
}
