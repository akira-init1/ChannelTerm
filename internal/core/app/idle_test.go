package app

import (
	"context"
	"errors"
	"sync"
	"testing"
	"time"

	"github.com/akira-init1/ChannelTerm/internal/core/session"
)

func idleFixture(t *testing.T) (*Application, context.Context, *time.Time) {
	a, ctx, now := instanceFixture(t)
	a.idle.policy = IdlePolicy{SessionTimeout: 30 * time.Second}
	return a, ctx, now
}

func sweepIdle(t *testing.T, a *Application) bool {
	t.Helper()
	exit, err := a.SweepIdle(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	return exit
}

func TestIdleGraceUsageAndListObservation(t *testing.T) {
	a, ctx, now := idleFixture(t)
	_, proof := registerInstance(t, a, ctx)
	use, err := a.SetClientUse(ctx, proof, "background-tab", "board", "monitor")
	if err != nil {
		t.Fatal(err)
	}
	sweepIdle(t, a)
	*now = now.Add(30 * time.Second)
	for i := 0; i < 3; i++ {
		a.ListSessions()
		_, _ = a.ListClientInstances(ctx)
		_, _ = a.HostIdlePolicy(ctx)
	}
	sweepIdle(t, a)
	if _, ok := a.manager.Get("other"); ok {
		t.Fatal("list polling retained unused Session")
	}
	if _, ok := a.manager.Get("board"); !ok {
		t.Fatal("background monitor was reclaimed")
	}
	if err := a.EndClientUse(ctx, proof, use.ID); err != nil {
		t.Fatal(err)
	}
	sweepIdle(t, a)
	*now = now.Add(29 * time.Second)
	sweepIdle(t, a)
	if _, ok := a.manager.Get("board"); !ok {
		t.Fatal("grace shortened")
	}
	*now = now.Add(time.Second)
	sweepIdle(t, a)
	if len(a.ListSessions()) != 0 {
		t.Fatal("unused Session survived grace")
	}
}

func TestIdleAcceptedWorkSurvivesPresenceExpiry(t *testing.T) {
	a, ctx, now := idleFixture(t)
	_, p := registerInstance(t, a, ctx)
	ctx, err := a.BindClientInstance(WithInstanceProof(ctx, p), "mcp")
	if err != nil {
		t.Fatal(err)
	}
	_, done, err := a.BeginClientCall(ctx, "board", "transfer", true)
	if err != nil {
		t.Fatal(err)
	}
	sweepIdle(t, a)
	*now = now.Add(2 * time.Minute)
	sweepIdle(t, a)
	if _, ok := a.manager.Get("board"); !ok {
		t.Fatal("expired presence cancelled accepted work")
	}
	done()
	done()
	sweepIdle(t, a)
	*now = now.Add(30 * time.Second)
	sweepIdle(t, a)
	if _, ok := a.manager.Get("board"); ok {
		t.Fatal("completed work retained Session")
	}
}

func TestIdleRetainsAuthorityRecoveryAndHumanPause(t *testing.T) {
	for _, reason := range []string{"lease", "recovery", "human_pause", "control", "operation"} {
		t.Run(reason, func(t *testing.T) {
			a, _, now := idleFixture(t)
			switch reason {
			case "lease":
				a.leases.leases["board"] = SessionLease{SessionID: "board"}
			case "recovery":
				a.leases.recovering["board"] = SessionLease{SessionID: "board"}
			case "operation":
				a.leases.activeWrites["board"] = 1
			default:
				a.leases.controls.entries = map[string]*sessionControl{"board": {view: ControlStatus{State: "active", AIPaused: reason == "human_pause"}}}
			}
			sweepIdle(t, a)
			*now = now.Add(5 * time.Minute)
			sweepIdle(t, a)
			if _, ok := a.manager.Get("board"); !ok {
				t.Fatal("active authority reclaimed")
			}
			status, _ := a.HostIdlePolicy(context.Background())
			if len(status.Sessions) != 1 || status.Sessions[0].Reason != reason {
				t.Fatalf("wrong reason: %+v", status)
			}
		})
	}
}

func TestIdleUseAdmissionRacesRemoval(t *testing.T) {
	for i := 0; i < 60; i++ {
		a, ctx, now := idleFixture(t)
		_, p := registerInstance(t, a, ctx)
		sweepIdle(t, a)
		*now = now.Add(30 * time.Second)
		var wg sync.WaitGroup
		wg.Add(2)
		var useErr, sweepErr error
		go func() { defer wg.Done(); _, useErr = a.SetClientUse(ctx, p, "race", "board", "observe") }()
		go func() { defer wg.Done(); _, sweepErr = a.SweepIdle(ctx) }()
		wg.Wait()
		if sweepErr != nil {
			t.Fatal(sweepErr)
		}
		if _, ok := a.manager.Get("board"); useErr == nil && !ok {
			t.Fatal("successfully admitted view was reclaimed")
		}
		if useErr != nil && !errors.Is(useErr, ErrSessionNotFound) {
			t.Fatal(useErr)
		}
	}
}

func TestIdleOldRegistrationCannotCloseReopenedID(t *testing.T) {
	a, _, now := idleFixture(t)
	sweepIdle(t, a)
	old, _ := a.manager.Get("board")
	ref, _ := a.manager.Reference("board")
	_, _ = a.CloseSession("board")
	newSession := newFakeConnectedSession("board")
	_ = newSession.Connect(context.Background())
	if err := a.manager.Register(newSession); err != nil {
		t.Fatal(err)
	}
	*now = now.Add(time.Minute)
	sweepIdle(t, a)
	if current, ok := a.manager.Get("board"); !ok || current != newSession {
		t.Fatal("old countdown closed successor")
	}
	a.leases.mu.Lock()
	_, err := a.controlLocked(old, ref)
	a.leases.mu.Unlock()
	if !errors.Is(err, ErrSessionNotFound) {
		t.Fatal("late authority attached to old registration", err)
	}
}

func TestIdleExitFencesAdmissionAndWaitsForIndependentWork(t *testing.T) {
	a, ctx, now := idleFixture(t)
	a.idle.policy.ExitTimeout = time.Second
	_, p := registerInstance(t, a, ctx)
	bound, err := a.BindClientInstance(WithInstanceProof(ctx, p), "mcp")
	if err != nil {
		t.Fatal(err)
	}
	_, done, err := a.BeginClientCall(bound, "", "independent-ssh-transfer", true)
	if err != nil {
		t.Fatal(err)
	}
	sweepIdle(t, a)
	*now = now.Add(2 * time.Minute)
	sweepIdle(t, a)
	if len(a.ListSessions()) != 0 {
		t.Fatal("independent work must not retain unrelated Sessions")
	}
	if sweepIdle(t, a) {
		t.Fatal("Host exited with accepted independent work")
	}
	done()
	sweepIdle(t, a)
	*now = now.Add(time.Second)
	if !sweepIdle(t, a) {
		t.Fatal("fully idle Host did not request exit")
	}
	if _, _, err := a.RegisterClientInstance(ctx, InstanceRegistration{Name: "late", Entry: "cli"}); !errors.Is(err, ErrHostDraining) {
		t.Fatal(err)
	}
	if _, err := a.beginIdleWork(); !errors.Is(err, ErrHostDraining) {
		t.Fatal(err)
	}
}

func TestIdleDefaultsAndLegacyReaderMigration(t *testing.T) {
	a, ctx, now := instanceFixture(t)
	sweepIdle(t, a)
	*now = now.Add(time.Hour)
	sweepIdle(t, a)
	if len(a.ListSessions()) != 2 {
		t.Fatal("default lifecycle changed")
	}
	a.idle.policy.SessionTimeout = time.Second
	_, _, err := a.BeginClientCall(ExternalControl(ctx), "board", "read", false)
	if !errors.Is(err, ErrIdleRegistration) {
		t.Fatal(err)
	}
	if _, done, err := a.BeginClientCall(ExternalControl(ctx), "", "list", false); err != nil {
		t.Fatal(err)
	} else {
		done()
	}
	for _, p := range []IdlePolicy{{SessionTimeout: -time.Second}, {SessionTimeout: time.Millisecond}, {ExitTimeout: time.Second}, {SessionTimeout: 25 * time.Hour}} {
		if _, err := New(Dependencies{Manager: session.NewManager(), Idle: p}); err == nil {
			t.Fatal("invalid policy accepted")
		}
	}
}

func TestIdleLeaseAdmissionRacesRemoval(t *testing.T) {
	for i := 0; i < 40; i++ {
		a, _, now := idleFixture(t)
		sweepIdle(t, a)
		*now = now.Add(30 * time.Second)
		var wg sync.WaitGroup
		wg.Add(2)
		var leaseErr, sweepErr error
		go func() { defer wg.Done(); _, leaseErr = a.AcquireLease("board", "idle-test", LeaseTypeTerminal) }()
		go func() { defer wg.Done(); _, sweepErr = a.SweepIdle(context.Background()) }()
		wg.Wait()
		if sweepErr != nil {
			t.Fatal(sweepErr)
		}
		if _, ok := a.manager.Get("board"); leaseErr == nil && !ok {
			t.Fatal("admitted lease lost its Session")
		}
		if leaseErr == nil {
			_ = a.ReleaseLease("board", "idle-test")
		} else if !errors.Is(leaseErr, ErrSessionNotFound) {
			t.Fatal(leaseErr)
		}
	}
}

type idleBlockingClose struct {
	session.Session
	started chan struct{}
	release chan struct{}
	once    sync.Once
}

func (s *idleBlockingClose) Close() error {
	s.once.Do(func() { close(s.started) })
	<-s.release
	return s.Session.Close()
}

func TestIdleCloseRunsOutsideAdmissionAndPreventsConcurrentExit(t *testing.T) {
	a, ctx, now := idleFixture(t)
	a.idle.policy.ExitTimeout = time.Second
	_, _ = a.CloseSession("board")
	_, _ = a.CloseSession("other")
	terminal := newFakeConnectedSession("blocked")
	_ = terminal.Connect(ctx)
	blocked := &idleBlockingClose{Session: terminal, started: make(chan struct{}), release: make(chan struct{})}
	var releaseOnce sync.Once
	release := func() { releaseOnce.Do(func() { close(blocked.release) }) }
	defer release()
	if err := a.manager.Register(blocked); err != nil {
		t.Fatal(err)
	}
	sweepIdle(t, a)
	*now = now.Add(30 * time.Second)
	done := make(chan error, 1)
	go func() { _, err := a.SweepIdle(ctx); done <- err }()
	select {
	case <-blocked.started:
	case <-time.After(time.Second):
		release()
		t.Fatal("Close not reached")
	}
	// No lock remains held by Close: status and fresh instance registration work.
	if _, err := a.HostIdlePolicy(ctx); err != nil {
		t.Fatal(err)
	}
	if sweepIdle(t, a) {
		t.Fatal("exit raced pending Close")
	}
	_, proof := registerInstance(t, a, ctx)
	_ = a.EndClientInstance(ctx, proof)
	release()
	if err := <-done; err != nil {
		t.Fatal(err)
	}
}

func TestIdleDebugRegistrationRetainsSessionWithoutLease(t *testing.T) {
	a, ctx, now := idleFixture(t)
	_, err := a.RegisterDebugObservation(ctx, DebugRegistration{Owner: "01234567890123456789012345678901", RequestID: "idle-debug", Name: "External debugger", Mode: "tcp", Executor: "manual", SessionID: "board", Ports: []DebugPort{{Role: "target", Configured: "127.0.0.1:2345"}}})
	if err != nil {
		t.Fatal(err)
	}
	sweepIdle(t, a)
	*now = now.Add(10 * time.Minute)
	sweepIdle(t, a)
	if _, ok := a.manager.Get("board"); !ok {
		t.Fatal("unconfirmed external debugger reclaimed")
	}
}

func TestIdleDisconnectedDebugReportRemainsCompletedAfterDisplayExpires(t *testing.T) {
	a, _, now := idleFixture(t)
	a.observations.runs = map[string]*debugObservationRun{"completed": {item: DebugObservation{ID: "completed", SessionID: "board", Source: "client_report", Connection: "disconnected", UpdatedAt: time.Now().Add(-time.Hour)}}}
	snapshot, err := a.DebugOverview(context.Background())
	if err != nil || !snapshot.Operations[0].Stale || snapshot.Operations[0].Connection != "unknown" {
		t.Fatalf("expected stale display: %+v %v", snapshot, err)
	}
	sweepIdle(t, a)
	*now = now.Add(30 * time.Second)
	sweepIdle(t, a)
	if _, ok := a.manager.Get("board"); ok {
		t.Fatal("completed observation pinned Session after display became stale")
	}
}
