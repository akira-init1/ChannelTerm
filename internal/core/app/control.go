package app

import (
	"context"
	"crypto/rand"
	"errors"
	"sync"
	"time"

	"github.com/akira-init1/ChannelTerm/internal/core/session"
)

// ControlTTL bounds an explicit multi-call operation without renewing its leases.
const ControlTTL = 30 * time.Second

var (
	// ErrControlRequired asks old writers to adopt instance and control metadata.
	ErrControlRequired = errors.New("controlled writes require a registered identity, live instance and current control version; migrate the client")
	// ErrControlStale rejects an old generation without retrying its input.
	ErrControlStale = errors.New("control version or operation is stale; input discarded, do not retry automatically")
	// ErrControlBusy reports another holder or unconfirmed recovery.
	ErrControlBusy = errors.New("Session control is occupied or recovery is unconfirmed; input discarded")
	// ErrAgentPaused persists until an authorized human explicitly permits new work.
	ErrAgentPaused = errors.New("AI writes paused by human takeover; wait for explicit permission, do not retry")
	// ErrControlInterrupted is the cancellation cause for a captured operation.
	ErrControlInterrupted = errors.New("operation interrupted by human takeover; do not retry")
)

// ControlProof is public fencing metadata, not an authorization capability.
type ControlProof struct {
	Version     string `json:"version"`
	OperationID string `json:"operation_id,omitempty"`
}

// ControlStatus is a bounded current observation; reading it never renews control.
type ControlStatus struct {
	Writable    bool                   `json:"writable"`
	SessionID   string                 `json:"session_id"`
	Version     string                 `json:"version"`
	OperationID string                 `json:"operation_id,omitempty"`
	State       string                 `json:"state"`
	Client      session.ClientIdentity `json:"client,omitzero"`
	AIPaused    bool                   `json:"ai_paused"`
	ExpiresAt   time.Time              `json:"expires_at,omitzero"`
}

// ControlResult retains the outcome for the exact pre-takeover version/operation.
// Restored means ChannelTerm's recovery completed, never that all remote jobs stopped.
type ControlResult struct {
	SessionID    string                 `json:"session_id"`
	Version      string                 `json:"version"`
	OperationID  string                 `json:"operation_id,omitempty"`
	Status       string                 `json:"status"`
	Reason       string                 `json:"reason"`
	Recovery     string                 `json:"recovery"`
	RequestedBy  session.ClientIdentity `json:"requested_by,omitzero"`
	RetryAllowed bool                   `json:"retry_allowed"`
}

type controlProofKey struct{}
type controlExternalKey struct{}
type controlRecoveryKey struct{}
type controlKindKey struct{}
type controlWorkerKey struct{}

// WithControlProof carries untrusted protocol metadata for Application validation.
func WithControlProof(ctx context.Context, p ControlProof) context.Context {
	return context.WithValue(ctx, controlProofKey{}, p)
}

// ExternalControl marks adapter entry, including unregistered stdio. Trusted Core
// cleanup cannot be impersonated by omitting identity or forging actor fields.
func ExternalControl(ctx context.Context) context.Context {
	return context.WithValue(ctx, controlExternalKey{}, true)
}

type sessionControl struct {
	view        ControlStatus
	humanOwner  session.ClientIdentity
	reference   string
	automatic   bool
	touched     bool
	recovered   bool
	timer       *time.Timer
	cancel      context.CancelCauseFunc
	cancelOwner string
}

type retainedControlResult struct {
	result ControlResult
	at     time.Time
}

// controlStore uses the lease coordinator's mutex and write gate. It is not a
// second writer lock: control admission and lease admission share those locks.
type controlStore struct {
	entries map[string]*sessionControl
	results map[string]retainedControlResult
}

func controlExternal(ctx context.Context) bool {
	v, _ := ctx.Value(controlExternalKey{}).(bool)
	return v
}

func controlIdentity(ctx context.Context) bool {
	c := ClientIdentityFromContext(ctx)
	return c.ID != "" && c.InstanceID != "" && (c.Kind == "user" || c.Kind == "agent")
}

// RequireControlIdentity rejects unknown external writers, including old stdio.
// It does not authorize a target or replace the per-write generation check.
func RequireControlIdentity(ctx context.Context) error {
	if !controlExternal(ctx) {
		return nil
	}
	if !controlIdentity(ctx) {
		return ErrControlRequired
	}
	return nil
}

func sameController(a, b session.ClientIdentity) bool {
	// Parent links only come from validated, single-use, target-bound delegation.
	return a.ID != "" && a.ID == b.ID && a.InstanceID != "" && (a.InstanceID == b.InstanceID || a.ParentInstanceID != "" && a.ParentInstanceID == b.InstanceID)
}

// controlLocked runs under leases.mu. Registration references fence reused IDs.
func (a *Application) controlLocked(t session.Session, reference string) (*sessionControl, error) {
	c := &a.leases.controls
	if c.entries == nil {
		c.entries = make(map[string]*sessionControl)
		c.results = make(map[string]retainedControlResult)
	}
	if existing := c.entries[t.ID()]; existing != nil && existing.reference == reference {
		return existing, nil
	}
	// Only dead registrations may be removed; never evict a live human pause.
	for id, old := range c.entries {
		if ref, ok := a.manager.Reference(id); !ok || ref != old.reference {
			if old.timer != nil {
				old.timer.Stop()
			}
			delete(c.entries, id)
		}
	}
	if len(c.entries) >= 256 {
		return nil, errors.New("Session control capacity reached")
	}
	r := &sessionControl{reference: reference, view: ControlStatus{SessionID: t.ID(), Version: "CV-" + rand.Text(), State: "idle"}}
	c.entries[t.ID()] = r
	return r, nil
}

func (a *Application) controlTarget(id string) (session.Session, string, error) {
	t, err := a.session(id)
	if err != nil {
		return nil, "", err
	}
	ref, ok := a.manager.Reference(t.ID())
	if !ok || t.State() != session.StateOpen {
		return nil, "", ErrSessionNotFound
	}
	return t, ref, nil
}

// SessionControl reads current control without creating an operation or heartbeat.
func (a *Application) SessionControl(ctx context.Context, id string) (ControlStatus, error) {
	if err := CheckClientAccess(ctx, ClientRead); err != nil {
		return ControlStatus{}, err
	}
	t, ref, err := a.controlTarget(id)
	if err != nil {
		return ControlStatus{}, err
	}
	a.leases.mu.Lock()
	defer a.leases.mu.Unlock()
	r, err := a.controlLocked(t, ref)
	if err != nil {
		return ControlStatus{}, err
	}
	view := r.view
	client := ClientIdentityFromContext(ctx)
	view.Writable = a.validateControlIdentity(ctx, t.ID()) == nil && CheckClientAccess(ctx, ClientWrite) == nil && client.Kind == "user" && (view.State == "idle" || view.State == "human" && sameController(client, view.Client))
	return view, nil
}

// CheckSessionControl checks a captured generation before non-byte Session effects.
// Actual stream writes additionally check under the shared lease write gate.
func (a *Application) CheckSessionControl(ctx context.Context, id string) error {
	if !controlExternal(ctx) {
		return nil
	}
	t, ref, err := a.controlTarget(id)
	if err != nil {
		return err
	}
	a.leases.mu.Lock()
	defer a.leases.mu.Unlock()
	return a.checkControlLocked(ctx, t, ref, "")
}

// CheckControlGeneration fences explicit management/cancellation without giving
// the requester the active writer's identity or raw-write capability.
func (a *Application) CheckControlGeneration(ctx context.Context, id string) error {
	if !controlExternal(ctx) {
		return nil
	}
	if err := RequireControlIdentity(ctx); err != nil {
		return err
	}
	t, ref, err := a.controlTarget(id)
	if err != nil {
		return err
	}
	a.leases.mu.Lock()
	defer a.leases.mu.Unlock()
	r, err := a.controlLocked(t, ref)
	if err != nil {
		return err
	}
	p, ok := ctx.Value(controlProofKey{}).(ControlProof)
	if !ok || p.Version == "" {
		return ErrControlRequired
	}
	if p.Version != r.view.Version || p.OperationID != r.view.OperationID {
		return ErrControlStale
	}
	return nil
}

// CheckOperationControl resolves existing opaque debug/task IDs without exposing
// private status. The original tool must still validate its owner capability.
func (a *Application) CheckOperationControl(ctx context.Context, id string) error {
	var target string
	a.debug.mu.Lock()
	if r := a.debug.runs[id]; r != nil {
		target = r.terminal.ID()
	}
	a.debug.mu.Unlock()
	a.bridges.mu.Lock()
	if r := a.bridges.runs[id]; r != nil {
		target = r.terminal.ID()
	}
	a.bridges.mu.Unlock()
	if target != "" {
		return a.CheckSessionControl(ctx, target)
	}
	return nil
}

// checkControlLocked validates a normal write/lease admission while leases.mu is held.
// Internal recovery is allowed only for the captured lease, never a replacement.
func (a *Application) checkControlLocked(ctx context.Context, t session.Session, ref, owner string) error {
	r, err := a.controlLocked(t, ref)
	if err != nil {
		return err
	}
	lease, leased := a.leases.leases[t.ID()]
	if recovery, _ := ctx.Value(controlRecoveryKey{}).(bool); recovery && leased && owner != "" && lease.Owner == owner {
		return nil
	}
	if !controlExternal(ctx) {
		// Composition/test callers remain trusted, but a managed worker's captured
		// lease is still fenced after takeover. Recovery requires the private marker.
		if leased && owner != "" && lease.Owner == owner && lease.controlVersion != "" {
			if lease.controlVersion != r.view.Version || r.view.State != "active" || !time.Now().Before(r.view.ExpiresAt) {
				return ErrControlStale
			}
		}
		return nil
	}
	if err := CheckClientAccess(ctx, ClientWrite); err != nil {
		return err
	}
	if accepted, _ := ctx.Value(controlWorkerKey{}).(bool); !accepted {
		if err := a.validateControlIdentity(ctx, t.ID()); err != nil {
			return err
		}
	}
	p, ok := ctx.Value(controlProofKey{}).(ControlProof)
	if !ok || p.Version == "" {
		return ErrControlRequired
	}
	if p.Version != r.view.Version {
		return ErrControlStale
	}
	client := ClientIdentityFromContext(ctx)
	if r.view.AIPaused && client.Kind != "user" {
		return ErrAgentPaused
	}
	if r.view.State == "stopping" || r.view.State == "failed" {
		return ErrControlBusy
	}
	if r.view.State == "active" {
		if !sameController(client, r.view.Client) {
			return ErrControlBusy
		}
		if !time.Now().Before(r.view.ExpiresAt) {
			return ErrControlStale
		}
		if p.OperationID != r.view.OperationID && !(leased && owner != "" && lease.Owner == owner && r.automatic) {
			return ErrControlStale
		}
	} else if r.view.State == "human" {
		if !sameController(client, r.view.Client) {
			return ErrControlBusy
		}
		if p.OperationID != "" {
			return ErrControlStale
		}
	} else if p.OperationID != "" {
		return ErrControlStale
	}
	return nil
}

// validateControlIdentity rechecks presence and delegation at effect admission.
// Callers may hold leases.mu; presence code never takes it while holding instances.mu.
func (a *Application) validateControlIdentity(ctx context.Context, target string) error {
	if !controlIdentity(ctx) {
		return ErrControlRequired
	}
	proof, ok := ctx.Value(instanceProofKey{}).(InstanceProof)
	if !ok {
		return ErrControlRequired
	}
	a.instances.mu.Lock()
	defer a.instances.mu.Unlock()
	e, err := a.instances.authenticate(ctx, proof, false)
	if err != nil {
		return err
	}
	if e.view.ID != ClientIdentityFromContext(ctx).InstanceID || e.target != "" && e.target != target {
		return ErrClientInstance
	}
	return nil
}

// BeginControl reserves a multi-call workflow. Reads never acquire this scope.
func (a *Application) BeginControl(ctx context.Context, id string, expected ControlProof) (ControlStatus, error) {
	return a.beginControl(ctx, id, expected, false)
}

func (a *Application) beginControl(ctx context.Context, id string, expected ControlProof, automatic bool) (ControlStatus, error) {
	ctx = WithControlProof(ExternalControl(ctx), expected)
	t, ref, err := a.controlTarget(id)
	if err != nil {
		return ControlStatus{}, err
	}
	gate := a.leases.gate(t.ID())
	if !gate.TryLock() {
		return ControlStatus{}, ErrControlBusy
	}
	defer gate.Unlock()
	a.leases.mu.Lock()
	defer a.leases.mu.Unlock()
	if err = a.checkControlLocked(ctx, t, ref, ""); err != nil {
		return ControlStatus{}, err
	}
	r := a.leases.controls.entries[t.ID()]
	if r.view.State == "active" {
		if automatic && r.automatic {
			return ControlStatus{}, ErrControlBusy
		}
		return r.view, nil
	}
	if _, ok := a.leases.leases[t.ID()]; ok {
		return ControlStatus{}, ErrControlBusy
	}
	a.startControlLocked(r, ClientIdentityFromContext(ctx), automatic)
	return r.view, nil
}

func (a *Application) startControlLocked(r *sessionControl, client session.ClientIdentity, automatic bool) {
	r.view.State = "active"
	r.view.Client = client
	r.view.OperationID = "OC-" + rand.Text()
	r.automatic = automatic
	r.touched = false
	r.recovered = false
	a.renewControlLocked(r)
}

func (a *Application) renewControlLocked(r *sessionControl) {
	r.view.ExpiresAt = time.Now().Add(ControlTTL)
	if r.timer != nil {
		r.timer.Stop()
	}
	version, operation := r.view.Version, r.view.OperationID
	r.timer = time.AfterFunc(ControlTTL, func() { a.expireControl(r, version, operation) })
}

// RenewControl extends only a live scope owned by the same authenticated instance.
func (a *Application) RenewControl(ctx context.Context, id string, p ControlProof) (ControlStatus, error) {
	t, ref, err := a.controlTarget(id)
	if err != nil {
		return ControlStatus{}, err
	}
	a.leases.mu.Lock()
	defer a.leases.mu.Unlock()
	if err = a.checkControlLocked(WithControlProof(ExternalControl(ctx), p), t, ref, ""); err != nil {
		return ControlStatus{}, err
	}
	r := a.leases.controls.entries[t.ID()]
	if r.view.State != "active" || p.OperationID != r.view.OperationID {
		return ControlStatus{}, ErrControlStale
	}
	a.renewControlLocked(r)
	return r.view, nil
}

// EndControl ends only an idle protocol scope, never an active lease or pending write.
func (a *Application) EndControl(ctx context.Context, id string, p ControlProof) (ControlStatus, error) {
	t, ref, err := a.controlTarget(id)
	if err != nil {
		return ControlStatus{}, err
	}
	gate := a.leases.gate(t.ID())
	if !gate.TryLock() {
		return ControlStatus{}, ErrControlBusy
	}
	defer gate.Unlock()
	a.leases.mu.Lock()
	defer a.leases.mu.Unlock()
	if err = a.checkControlLocked(WithControlProof(ExternalControl(ctx), p), t, ref, ""); err != nil {
		return ControlStatus{}, err
	}
	r := a.leases.controls.entries[t.ID()]
	if r.view.State != "active" || p.OperationID != r.view.OperationID {
		return ControlStatus{}, ErrControlStale
	}
	if _, ok := a.leases.leases[t.ID()]; ok {
		return ControlStatus{}, ErrControlBusy
	}
	finishControlLocked(r)
	return r.view, nil
}

func finishControlLocked(r *sessionControl) {
	if r.timer != nil {
		r.timer.Stop()
		r.timer = nil
	}
	r.view.Version = "CV-" + rand.Text()
	r.view.OperationID = ""
	r.view.ExpiresAt = time.Time{}
	r.cancel = nil
	if r.view.AIPaused {
		r.view.State = "human"
		if r.humanOwner.ID != "" {
			r.view.Client = r.humanOwner
		}
	} else {
		r.view.State = "idle"
		r.view.Client = session.ClientIdentity{}
	}
}

func requireHumanControl(ctx context.Context) error {
	if err := CheckClientAccess(ctx, ClientManage); err != nil {
		return err
	}
	if !controlIdentity(ctx) || ClientIdentityFromContext(ctx).Kind != "user" {
		return ErrClientPermissionDenied
	}
	return nil
}

// AllowAI invalidates every old generation and permits only new explicit work.
func (a *Application) AllowAI(ctx context.Context, id string, p ControlProof) (ControlStatus, error) {
	if err := requireHumanControl(ctx); err != nil {
		return ControlStatus{}, err
	}
	t, ref, err := a.controlTarget(id)
	if err != nil {
		return ControlStatus{}, err
	}
	gate := a.leases.gate(t.ID())
	if !gate.TryLock() {
		return ControlStatus{}, ErrControlBusy
	}
	defer gate.Unlock()
	if err := a.validateControlIdentity(ctx, t.ID()); err != nil {
		return ControlStatus{}, err
	}
	a.leases.mu.Lock()
	defer a.leases.mu.Unlock()
	r, err := a.controlLocked(t, ref)
	if err != nil {
		return ControlStatus{}, err
	}
	if r.view.Version != p.Version || r.view.OperationID != p.OperationID {
		return ControlStatus{}, ErrControlStale
	}
	if r.view.State != "human" && r.view.State != "idle" {
		return ControlStatus{}, ErrControlBusy
	}
	if _, ok := a.leases.leases[t.ID()]; ok {
		return ControlStatus{}, ErrControlBusy
	}
	r.view.AIPaused = false
	finishControlLocked(r)
	return r.view, nil
}

func controlResultKey(id string, p ControlProof) string {
	return id + "\x00" + p.Version + "\x00" + p.OperationID
}

func (a *Application) retainControlResultLocked(result ControlResult) {
	s := &a.leases.controls
	for key, item := range s.results {
		if time.Since(item.at) > 5*time.Minute {
			delete(s.results, key)
		}
	}
	if len(s.results) >= 128 {
		var oldest string
		var stamp time.Time
		for key, item := range s.results {
			if oldest == "" || item.at.Before(stamp) {
				oldest, stamp = key, item.at
			}
		}
		delete(s.results, oldest)
	}
	s.results[controlResultKey(result.SessionID, ControlProof{Version: result.Version, OperationID: result.OperationID})] = retainedControlResult{result: result, at: time.Now()}
}

// ControlOutcome reads retained results even after Session closure. Results expire
// after five minutes or capacity pressure and carry no cancellation capability.
func (a *Application) ControlOutcome(ctx context.Context, id string, p ControlProof) (ControlResult, error) {
	if err := CheckClientAccess(ctx, ClientRead); err != nil {
		return ControlResult{}, err
	}
	a.leases.mu.Lock()
	defer a.leases.mu.Unlock()
	r, ok := a.leases.controls.results[controlResultKey(id, p)]
	if !ok || time.Since(r.at) > 5*time.Minute {
		return ControlResult{}, errors.New("control result unavailable or expired")
	}
	return r.result, nil
}

// TakeControl fences the captured operation before scheduling bounded recovery.
// A delayed or duplicate request can never interrupt a replacement operation.
func (a *Application) TakeControl(ctx context.Context, id string, p ControlProof) (ControlResult, error) {
	if err := requireHumanControl(ctx); err != nil {
		return ControlResult{}, err
	}
	if old, err := a.ControlOutcome(ctx, id, p); err == nil {
		return old, nil
	}
	t, ref, err := a.controlTarget(id)
	if err != nil {
		return ControlResult{}, err
	}
	if err := a.validateControlIdentity(ctx, t.ID()); err != nil {
		return ControlResult{}, err
	}
	a.leases.mu.Lock()
	r, err := a.controlLocked(t, ref)
	if err != nil {
		a.leases.mu.Unlock()
		return ControlResult{}, err
	}
	if old, ok := a.leases.controls.results[controlResultKey(t.ID(), p)]; ok {
		a.leases.mu.Unlock()
		return old.result, nil
	}
	if r.view.Version != p.Version || r.view.OperationID != p.OperationID {
		a.leases.mu.Unlock()
		return ControlResult{}, ErrControlStale
	}
	if r.view.State == "stopping" || r.view.State == "failed" {
		a.leases.mu.Unlock()
		return ControlResult{}, ErrControlBusy
	}
	result := a.stopControlLocked(r, ClientIdentityFromContext(ctx), "user_interrupted")
	lease := a.leases.leases[t.ID()]
	cancel := r.cancel
	a.leases.mu.Unlock()
	if cancel != nil {
		cancel(ErrControlInterrupted)
	}
	go a.recoverControl(r, lease, result)
	return result, nil
}

func (a *Application) stopControlLocked(r *sessionControl, requester session.ClientIdentity, reason string) ControlResult {
	result := ControlResult{SessionID: r.view.SessionID, Version: r.view.Version, OperationID: r.view.OperationID, Status: "stopping", Reason: reason, Recovery: "pending", RequestedBy: requester}
	r.view.State = "stopping"
	r.view.AIPaused = true
	r.view.Version = "CV-" + rand.Text()
	if r.timer != nil {
		r.timer.Stop()
		r.timer = nil
	}
	a.retainControlResultLocked(result)
	return result
}

func (a *Application) expireControl(r *sessionControl, version, operation string) {
	a.leases.mu.Lock()
	if r.view.State != "active" || r.view.Version != version || r.view.OperationID != operation || time.Now().Before(r.view.ExpiresAt) {
		a.leases.mu.Unlock()
		return
	}
	result := a.stopControlLocked(r, session.ClientIdentity{}, "control_expired")
	lease, cancel := a.leases.leases[r.view.SessionID], r.cancel
	a.leases.mu.Unlock()
	if cancel != nil {
		cancel(ErrControlStale)
	}
	go a.recoverControl(r, lease, result)
}

// recoverControl never forwards caller-selected bytes or kills external processes.
// Known managed workers restore through their own private path. Unknown raw or
// external execution is fenced and closed rather than falsely marked safe.
func (a *Application) recoverControl(r *sessionControl, lease SessionLease, result ControlResult) {
	restored := false
	if lease.Owner == "" {
		gate := a.leases.gate(result.SessionID)
		if gate.TryLock() {
			a.leases.mu.Lock()
			restored = !r.touched
			a.leases.mu.Unlock()
			gate.Unlock()
		}
	} else if lease.controlKind == "debug" {
		restored = a.stopControlDebug(lease)
	} else if lease.controlKind == "command" || lease.controlKind == "transfer" {
		deadline := time.Now().Add(10 * time.Second)
		for time.Now().Before(deadline) {
			a.leases.mu.Lock()
			current, exists := a.leases.leases[result.SessionID]
			a.leases.mu.Unlock()
			if !exists || current.Owner != lease.Owner {
				a.leases.mu.Lock()
				restored = r.recovered
				a.leases.mu.Unlock()
				break
			}
			time.Sleep(10 * time.Millisecond)
		}
	}
	t, err := a.session(r.reference)
	if err != nil || t.State() != session.StateOpen {
		restored = false
	}
	if result.RequestedBy.ID == "" {
		restored = false
	}
	if !restored {
		_, _ = a.CloseSession(r.reference)
	}
	a.leases.mu.Lock()
	if restored && result.RequestedBy.ID != "" {
		r.view.State = "human"
		r.view.Client = result.RequestedBy
		r.humanOwner = result.RequestedBy
		result.Status = "taken_over"
		result.Recovery = "restored"
	} else {
		r.view.State = "failed"
		result.Status = "failed"
		result.Recovery = "session_closed"
	}
	r.view.OperationID = ""
	r.view.ExpiresAt = time.Time{}
	r.cancel = nil
	a.retainControlResultLocked(result)
	a.leases.mu.Unlock()
	if t != nil {
		t.PublishEvent(session.Event{Type: session.EventOperationState, Actor: "system", Client: result.RequestedBy, Metadata: map[string]any{"operation": "control", "result": result}})
	}
}

func (a *Application) stopControlDebug(lease SessionLease) bool {
	a.debug.mu.Lock()
	var run *debugRun
	for _, candidate := range a.debug.runs {
		if candidate.owner == lease.Owner && candidate.terminal.ID() == lease.SessionID {
			run = candidate
			break
		}
	}
	a.debug.mu.Unlock()
	if run == nil {
		return false
	}
	status, err := a.StopDebug(context.Background(), run.id, run.request.Owner, false)
	return err == nil && status.State == "closed"
}

// bindControlCancellation pins the worker's cancellation hook until its cleanup
// returns. The mutex never spans cancellation, I/O or worker joins.
func (a *Application) bindControlCancellation(id, owner string, cancel context.CancelCauseFunc) func() {
	a.leases.mu.Lock()
	r := a.leases.controls.entries[id]
	lease := a.leases.leases[id]
	if r == nil || lease.Owner != owner || lease.controlVersion == "" {
		a.leases.mu.Unlock()
		return func() {}
	}
	r.cancel = cancel
	r.cancelOwner = owner
	stale := r.view.State == "stopping" || lease.controlVersion != r.view.Version
	a.leases.mu.Unlock()
	if stale {
		cancel(ErrControlInterrupted)
	}
	var once sync.Once
	return func() {
		once.Do(func() {
			a.leases.mu.Lock()
			if r.cancelOwner == owner {
				r.cancel = nil
				r.cancelOwner = ""
			}
			a.leases.mu.Unlock()
		})
	}
}

func (a *Application) controlRecovered(id, owner string) {
	a.leases.mu.Lock()
	defer a.leases.mu.Unlock()
	if lease, ok := a.leases.leases[id]; ok && lease.Owner == owner {
		if r := a.leases.controls.entries[id]; r != nil {
			r.recovered = true
		}
	}
}

// bindPreparingControl covers managed preparation before the first lease. A
// cancelled preparer must pass lease admission again before touching the device.
func (a *Application) bindPreparingControl(id string, p ControlProof, cancel context.CancelFunc) func() {
	a.leases.mu.Lock()
	r := a.leases.controls.entries[id]
	if r == nil || r.view.Version != p.Version || r.view.OperationID != p.OperationID {
		a.leases.mu.Unlock()
		cancel()
		return func() {}
	}
	token := "preparing-" + rand.Text()
	r.cancelOwner, r.cancel = token, func(error) { cancel() }
	a.leases.mu.Unlock()
	return func() {
		a.leases.mu.Lock()
		defer a.leases.mu.Unlock()
		if r.cancelOwner == token {
			r.cancel = nil
			r.cancelOwner = ""
		}
		if r.automatic && r.view.Version == p.Version && r.view.OperationID == p.OperationID && r.view.State == "active" {
			if _, leased := a.leases.leases[id]; !leased {
				finishControlLocked(r)
			}
		}
	}
}

// CloseSessionForClient fences explicit management removal against takeover.
// Removal is atomic with the version check; potentially blocking Close runs
// after releasing control locks and never targets a reused registration.
func (a *Application) CloseSessionForClient(ctx context.Context, id string) (session.SessionInfo, error) {
	if !controlExternal(ctx) {
		return a.CloseSession(id)
	}
	if err := CheckClientAccess(ctx, ClientManage); err != nil {
		return session.SessionInfo{}, err
	}
	t, ref, err := a.controlTarget(id)
	if err != nil {
		return session.SessionInfo{}, err
	}
	if err = a.validateControlIdentity(ctx, t.ID()); err != nil {
		return session.SessionInfo{}, err
	}
	var info session.SessionInfo
	for _, candidate := range a.ListSessions() {
		if candidate.ID == t.ID() && candidate.Metadata.Reference == ref {
			info = candidate
			break
		}
	}
	a.leases.mu.Lock()
	r, err := a.controlLocked(t, ref)
	if err != nil {
		a.leases.mu.Unlock()
		return session.SessionInfo{}, err
	}
	p, ok := ctx.Value(controlProofKey{}).(ControlProof)
	if !ok || p.Version == "" {
		a.leases.mu.Unlock()
		return session.SessionInfo{}, ErrControlRequired
	}
	if p.Version != r.view.Version || p.OperationID != r.view.OperationID {
		a.leases.mu.Unlock()
		return session.SessionInfo{}, ErrControlStale
	}
	removed, exists := a.manager.Remove(ref)
	a.leases.mu.Unlock()
	if !exists {
		return session.SessionInfo{}, ErrSessionNotFound
	}
	err = removed.Close()
	a.leases.removeRegistration(t.ID(), ref)
	return info, err
}
