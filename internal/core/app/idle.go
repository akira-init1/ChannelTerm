package app

import (
	"context"
	"errors"
	"sync"
	"time"

	"github.com/akira-init1/ChannelTerm/internal/core/session"
)

var (
	// ErrHostDraining fences new work after an explicit idle-exit decision.
	ErrHostDraining = errors.New("Host is shutting down after confirmed idleness; start or select a Host explicitly")
	// ErrIdleRegistration requires old readers to migrate on a reclaiming Host.
	ErrIdleRegistration = errors.New("this Host reclaims idle Sessions; register a client instance and explicit Session use before monitoring")
)

// DesktopIdleExitTimeout allows the local GUI handshake to complete before idle exit.
const DesktopIdleExitTimeout = 3 * time.Second

// IdlePolicy supplies the launch defaults. Zero retains resources. Exit
// requires Session reclamation and is implemented by the composition root only.
type IdlePolicy struct {
	// ReleaseOnDetach closes idle registrations on confirmed human departure.
	// Explicit retention settings continue to use SessionTimeout alone.
	ReleaseOnDetach bool
	SessionTimeout  time.Duration
	ExitTimeout     time.Duration
	Origin          string
}

// Validate checks time bounds and rejects exit without Session reclamation.
func (p IdlePolicy) Validate() error {
	for _, d := range []time.Duration{p.SessionTimeout, p.ExitTimeout} {
		if d != 0 && (d < time.Second || d > 24*time.Hour) {
			return errors.New("idle timeout must be zero or between 1s and 24h")
		}
	}
	if p.ReleaseOnDetach && p.SessionTimeout == 0 {
		return errors.New("release on detach requires Session reclamation")
	}
	if p.ExitTimeout != 0 && p.SessionTimeout == 0 {
		return errors.New("idle exit requires Session reclamation")
	}
	if p.Origin != "" && p.Origin != "manual" && p.Origin != "desktop" {
		return errors.New("invalid Host launch origin")
	}
	return nil
}

type idleState struct {
	policy             IdlePolicy
	backgroundOverride *bool                // guarded by instances.mu, desktop Host only
	since              map[string]time.Time // never-reused Manager registration references
	detached           map[string]string    // registration reference -> ID awaiting a final idle check
	closing            int                  // removed registrations whose transport cleanup is in progress
	exitSince          time.Time
	work               int
	epoch              uint64
	draining           bool
}

// IdleSession describes a bounded retention decision without terminal contents.
type IdleSession struct {
	SessionID string    `json:"session_id"`
	Reference string    `json:"reference"`
	Reason    string    `json:"reason"`
	IdleSince time.Time `json:"idle_since,omitzero"`
}

// HostIdleStatus is observational. It never renews usage or changes policy.
type HostIdleStatus struct {
	BackgroundSwitch bool          `json:"background_switch,omitempty"`
	Mode             string        `json:"mode"`
	Origin           string        `json:"origin"`
	SessionTimeoutMS int64         `json:"session_timeout_ms"`
	ExitTimeoutMS    int64         `json:"exit_timeout_ms"`
	Draining         bool          `json:"draining"`
	Sessions         []IdleSession `json:"sessions"`
	Truncated        bool          `json:"truncated"`
}

// touchIdleLocked invalidates a pre-admission snapshot, including work that starts
// and ends entirely between sweeps. Callers hold instances.mu, never doing I/O.
func (a *Application) touchIdleLocked(id string) {
	a.idle.epoch++
	a.idle.exitSince = time.Time{}
	if id == "" {
		a.idle.since = nil
		return
	}
	if ref, ok := a.manager.Reference(id); ok {
		delete(a.idle.since, ref)
	}
}

// beginIdleWork protects connection setup and trusted targetless work. It shares
// admission with idle exit without holding a lock during network/device I/O.
func (a *Application) beginIdleWork() (func(), error) {
	a.instances.mu.Lock()
	if a.idle.draining {
		a.instances.mu.Unlock()
		return nil, ErrHostDraining
	}
	a.idle.work++
	a.touchIdleLocked("")
	a.instances.mu.Unlock()
	var once sync.Once
	return func() {
		once.Do(func() { a.instances.mu.Lock(); a.idle.work--; a.touchIdleLocked(""); a.instances.mu.Unlock() })
	}, nil
}

// idleObservation snapshots debug ownership before taking lease/presence locks.
// Admission epochs invalidate a snapshot if concurrent calls could create work.
func (a *Application) idleObservation(ctx context.Context) (uint64, map[string]bool, error) {
	a.instances.mu.Lock()
	epoch := a.idle.epoch
	a.instances.mu.Unlock()
	debug, err := a.DebugOverview(ctx)
	busy := make(map[string]bool)
	for _, operation := range debug.Operations {
		// External observations have no Host-owned worker. A vanished reporter
		// cannot retain a Session or an empty listener indefinitely.
		if operation.Source != "host" && debug.ObservedAt.Sub(operation.UpdatedAt) > debugObservationFreshness {
			continue
		}
		if operation.Connection != "disconnected" && operation.Connection != "closed" && operation.Connection != "failed" {
			busy[operation.SessionID] = true
		}
	}
	return epoch, busy, err
}

// idleReasonLocked uses full authoritative records, never truncated display data.
// Lock order is leases.mu -> instances.mu -> Manager. No owner I/O is performed.
func (a *Application) idleReasonLocked(id, ref string, debug map[string]bool, now time.Time) string {
	if a.idle.work != 0 {
		return "setup"
	}
	if debug[id] {
		return "debug"
	}
	if a.leases.activeWrites[id] != 0 {
		return "operation"
	}
	if _, ok := a.leases.leases[id]; ok {
		return "lease"
	}
	if _, ok := a.leases.recovering[id]; ok {
		return "recovery"
	}
	if c := a.leases.controls.entries[id]; c != nil && c.view.State != "human" {
		if c.view.AIPaused {
			return "human_pause"
		}
		if c.view.State != "idle" {
			return "control"
		}
	}
	for _, entry := range a.instances.entries {
		live := entry.view.State != "ended" && now.Before(entry.view.ExpiresAt)
		for _, use := range entry.uses {
			if use.reference == ref && (live || use.Active) {
				return use.Kind
			}
		}
	}
	return "idle"
}

// SetDesktopBackground changes only this desktop listener's idle exit behavior.
// Device reclamation, active work and the admission fence remain authoritative.
func (a *Application) SetDesktopBackground(ctx context.Context, enabled bool) error {
	if err := CheckClientAccess(ctx, ClientManage); err != nil || ClientIdentityFromContext(ctx).Kind != "user" {
		return ErrClientPermissionDenied
	}
	a.instances.mu.Lock()
	defer a.instances.mu.Unlock()
	if a.idle.draining {
		return ErrHostDraining
	}
	if a.idle.policy.Origin != "desktop" || a.idle.policy.SessionTimeout == 0 {
		return errors.New("background switch requires a reclaiming desktop Host")
	}
	a.idle.backgroundOverride = &enabled
	a.idle.exitSince = time.Time{}
	return nil
}

// exitTimeoutLocked resolves the live desktop override under instances.mu.
func (s *idleState) exitTimeoutLocked() time.Duration {
	if s.backgroundOverride != nil {
		if *s.backgroundOverride {
			return 0
		}
		if s.policy.ExitTimeout == 0 {
			return DesktopIdleExitTimeout
		}
	}
	return s.policy.ExitTimeout
}

// HostIdlePolicy observes effective policy and retention reasons; it grants no
// management authority and does not start countdowns merely by being read.
func (a *Application) HostIdlePolicy(ctx context.Context) (HostIdleStatus, error) {
	_, debug, err := a.idleObservation(ctx)
	if err != nil {
		return HostIdleStatus{}, err
	}
	a.leases.mu.Lock()
	defer a.leases.mu.Unlock()
	a.instances.mu.Lock()
	defer a.instances.mu.Unlock()
	p := a.idle.policy
	p.ExitTimeout = a.idle.exitTimeoutLocked()
	out := HostIdleStatus{Mode: "keep", Origin: p.Origin, SessionTimeoutMS: p.SessionTimeout.Milliseconds(), ExitTimeoutMS: p.ExitTimeout.Milliseconds(), Draining: a.idle.draining, Sessions: []IdleSession{}}
	out.BackgroundSwitch = p.Origin == "desktop" && p.SessionTimeout != 0
	if out.Origin == "" {
		out.Origin = "manual"
	}
	if p.SessionTimeout != 0 {
		out.Mode = "release"
	}
	if p.ExitTimeout != 0 {
		out.Mode = "exit"
	}
	for _, info := range a.manager.ListInfo() {
		if len(out.Sessions) == 256 {
			out.Truncated = true
			break
		}
		reason := "keep"
		if p.SessionTimeout != 0 {
			reason = a.idleReasonLocked(info.ID, info.Metadata.Reference, debug, a.instances.clock())
		}
		out.Sessions = append(out.Sessions, IdleSession{SessionID: info.ID, Reference: info.Metadata.Reference, Reason: reason, IdleSince: a.idle.since[info.Metadata.Reference]})
	}
	return out, nil
}

// noteDepartureLocked remembers only a live human monitor's original registration.
// A delayed end/unuse from an expired instance cannot reclaim a new connection.
func (a *Application) noteDepartureLocked(ctx context.Context, entry *instanceRecord, use InstanceUse) {
	if !a.idle.policy.ReleaseOnDetach || use.Kind != "monitor" || use.reference == "" ||
		entry.view.Client.Kind != "user" || CheckClientAccess(ctx, ClientManage) != nil ||
		entry.view.State == "ended" || !a.instances.clock().Before(entry.view.ExpiresAt) {
		return
	}
	if a.idle.detached == nil {
		a.idle.detached = make(map[string]string)
	}
	a.idle.detached[use.reference] = use.SessionID
}

// reclaimDetached checks only the original registration. A transient read can
// finish just after detach; its completion repeats this check without a timer.
func (a *Application) reclaimDetached(ctx context.Context, ref string) error {
	// Continuous admissions can invalidate a debug snapshot. Leave the marker
	// for call completion or the next sweep rather than spinning indefinitely.
	for attempt := 0; attempt < 2; attempt++ {
		epoch, debug, err := a.idleObservation(ctx)
		if err != nil {
			return err
		}
		a.leases.mu.Lock()
		a.instances.mu.Lock()
		id, pending := a.idle.detached[ref]
		retry := pending && epoch != a.idle.epoch
		var terminal session.Session
		if pending && !retry && a.idleReasonLocked(id, ref, debug, a.instances.clock()) == "idle" {
			terminal, _ = a.manager.RemoveRegistration(id, ref)
			delete(a.idle.detached, ref)
			delete(a.idle.since, ref)
			if terminal != nil {
				a.idle.closing++
			}
		}
		a.instances.mu.Unlock()
		a.leases.mu.Unlock()
		if terminal != nil {
			return a.closeIdle(terminal, ref)
		}
		if !retry {
			return nil
		}
	}
	return nil
}

// closeIdle owns a removed registration. Never hold admission locks over device I/O.
func (a *Application) closeIdle(terminal session.Session, ref string) error {
	terminal.PublishEvent(session.Event{Type: session.EventOperationState, Actor: "system", Metadata: map[string]any{"operation": "idle_release", "state": "closing", "reason": "no_registered_use"}})
	err := terminal.Close()
	a.leases.removeRegistration(terminal.ID(), ref)
	a.instances.mu.Lock()
	a.idle.closing--
	a.idle.epoch++
	a.idle.exitSince = time.Time{}
	a.instances.mu.Unlock()
	return err
}

// SweepIdle releases only registrations still idle at atomic removal. Close runs
// outside locks. The returned exit decision is irreversible admission fencing;
// the composition root must stop accepting requests and gracefully stop its Host.
func (a *Application) SweepIdle(ctx context.Context) (exit bool, err error) {
	if a.idle.policy.SessionTimeout == 0 {
		return false, nil
	}
	epoch, debug, err := a.idleObservation(ctx)
	if err != nil {
		return false, err
	}
	a.leases.mu.Lock()
	a.instances.mu.Lock()
	if epoch != a.idle.epoch || ctx.Err() != nil {
		a.instances.mu.Unlock()
		a.leases.mu.Unlock()
		return false, ctx.Err()
	}
	now := a.instances.clock()
	if a.idle.since == nil {
		a.idle.since = make(map[string]time.Time)
	}
	live := make(map[string]bool)
	type removedSession struct {
		terminal  session.Session
		reference string
	}
	var removed []removedSession
	for _, info := range a.manager.ListInfo() {
		ref := info.Metadata.Reference
		live[ref] = true
		if a.idleReasonLocked(info.ID, ref, debug, now) != "idle" {
			delete(a.idle.since, ref)
			continue
		}
		since, ok := a.idle.since[ref]
		if _, departed := a.idle.detached[ref]; !departed {
			if !ok {
				a.idle.since[ref] = now
				continue
			}
			if now.Sub(since) < a.idle.policy.SessionTimeout {
				continue
			}
		}
		delete(a.idle.detached, ref)
		if terminal, ok := a.manager.Remove(ref); ok {
			removed = append(removed, removedSession{terminal, ref})
		}
		delete(a.idle.since, ref)
	}
	for ref := range a.idle.since {
		if !live[ref] {
			delete(a.idle.since, ref)
		}
	}
	for ref := range a.idle.detached {
		if !live[ref] {
			delete(a.idle.detached, ref)
		}
	}
	busy := a.idle.work != 0 || a.idle.closing != 0 || len(removed) != 0 || len(a.manager.ListInfo()) != 0 || len(debug) != 0
	busy = busy || len(a.leases.leases) != 0 || len(a.leases.recovering) != 0
	for _, active := range a.leases.activeWrites {
		busy = busy || active != 0
	}
	for _, entry := range a.instances.entries {
		if entry.view.Client.Kind != "agent" && entry.view.State != "ended" && now.Before(entry.view.ExpiresAt) {
			busy = true
		}
		for _, use := range entry.uses {
			if use.Active {
				busy = true
			}
		}
	}
	exitTimeout := a.idle.exitTimeoutLocked()
	if busy || exitTimeout == 0 {
		a.idle.exitSince = time.Time{}
	} else if a.idle.exitSince.IsZero() {
		a.idle.exitSince = now
	} else if now.Sub(a.idle.exitSince) >= exitTimeout {
		a.idle.draining = true
		exit = true
	}
	// Another concurrent sweep must not authorize Host exit while Close is still
	// releasing transport resources outside the coordinator locks.
	a.idle.closing += len(removed)
	a.instances.mu.Unlock()
	a.leases.mu.Unlock()
	for _, item := range removed {
		err = errors.Join(err, a.closeIdle(item.terminal, item.reference))
	}
	return exit, err
}
