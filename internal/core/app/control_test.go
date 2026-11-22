package app

import (
	"context"
	"errors"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/akira-init1/ChannelTerm/internal/core/session"
)

func controlTestClient(t *testing.T, a *Application, id, kind string) context.Context {
	t.Helper()
	ctx := WithClientAccess(WithClientIdentity(context.Background(), session.ClientIdentity{ID: id, Kind: kind, Name: id}), ClientAccess{Write: true, Manage: true})
	_, proof, err := a.RegisterClientInstance(ctx, InstanceRegistration{Name: id, Entry: "test"})
	if err != nil {
		t.Fatal(err)
	}
	ctx, err = a.BindClientInstance(WithInstanceProof(ctx, proof), "mcp")
	if err != nil {
		t.Fatal(err)
	}
	return ExternalControl(ctx)
}

func controlProof(v ControlStatus) ControlProof {
	return ControlProof{Version: v.Version, OperationID: v.OperationID}
}

func TestControlRequiresVersionAndExplicitAgentScope(t *testing.T) {
	a, _, _ := instanceFixture(t)
	user := controlTestClient(t, a, "CL-human", "user")
	agent := controlTestClient(t, a, "CL-agent", "agent")
	v, err := a.SessionControl(user, "board")
	if err != nil {
		t.Fatal(err)
	}
	write := func(ctx context.Context) error {
		_, err := a.WriteSession(ctx, "board", session.WriteRequest{Actor: "user", Data: []byte("x")})
		return err
	}
	if !errors.Is(write(user), ErrControlRequired) {
		t.Fatal("old writer admitted")
	}
	if !errors.Is(write(WithControlProof(agent, controlProof(v))), ErrControlRequired) {
		t.Fatal("agent bypassed explicit scope")
	}
	active, err := a.BeginControl(agent, "board", controlProof(v))
	if err != nil {
		t.Fatal(err)
	}
	if err = write(WithControlProof(agent, controlProof(active))); err != nil {
		t.Fatal(err)
	}
	if !errors.Is(write(WithControlProof(user, controlProof(active))), ErrControlBusy) {
		t.Fatal("human bypassed active AI")
	}
	_, err = a.EndControl(agent, "board", controlProof(active))
	if err != nil {
		t.Fatal(err)
	}
	if !errors.Is(write(WithControlProof(agent, controlProof(active))), ErrControlStale) {
		t.Fatal("old operation replayed")
	}
}

func TestAgentOperationHandsControlBackBeforeNextWrite(t *testing.T) {
	for _, leased := range []bool{false, true} {
		a, _, _ := instanceFixture(t)
		agent := controlTestClient(t, a, "CL-agent", "agent")
		human := controlTestClient(t, a, "CL-human", "user")
		idle, _ := a.SessionControl(agent, "board")
		active, err := a.BeginOperationControl(agent, "board", controlProof(idle))
		if err != nil {
			t.Fatal(err)
		}
		writer := WithControlProof(agent, controlProof(active))
		payload := session.WriteRequest{Actor: "agent", Data: []byte("x")}
		if leased {
			lease, err := a.AcquireLeaseForClient(writer, "board", "test-agent", LeaseTypeTerminal)
			if err != nil {
				t.Fatal(err)
			}
			if _, err := a.WriteSessionWithLease(writer, "board", lease.Owner, payload); err != nil {
				t.Fatal(err)
			}
			if err := a.ReleaseLease("board", lease.Owner); err != nil {
				t.Fatal(err)
			}
		} else if _, err := a.WriteSession(writer, "board", payload); err != nil {
			t.Fatal(err)
		}
		released, err := a.SessionControl(human, "board")
		if err != nil || released.State != "idle" || released.Version == active.Version {
			t.Fatalf("after completion: %+v %v", released, err)
		}
		if _, err := a.WriteSession(writer, "board", payload); !errors.Is(err, ErrControlStale) {
			t.Fatalf("stale agent: %v", err)
		}
		if _, err := a.WriteSession(WithControlProof(human, controlProof(released)), "board", session.WriteRequest{Actor: "user", Data: []byte("y")}); err != nil {
			t.Fatal(err)
		}
	}
}

func TestControlTakeoverPinsHumanAndRequiresExplicitAllow(t *testing.T) {
	a, _, _ := instanceFixture(t)
	user := controlTestClient(t, a, "CL-human", "user")
	other := controlTestClient(t, a, "CL-human", "user")
	agent := controlTestClient(t, a, "CL-agent", "agent")
	v, _ := a.SessionControl(user, "board")
	active, err := a.BeginControl(agent, "board", controlProof(v))
	if err != nil {
		t.Fatal(err)
	}
	if _, err = a.TakeControl(agent, "board", controlProof(active)); !errors.Is(err, ErrClientPermissionDenied) {
		t.Fatal("agent became human", err)
	}
	result, err := a.TakeControl(user, "board", controlProof(active))
	if err != nil {
		t.Fatal(err)
	}
	for deadline := time.Now().Add(time.Second); result.Status == "stopping" && time.Now().Before(deadline); {
		time.Sleep(time.Millisecond)
		result, err = a.ControlOutcome(user, "board", controlProof(active))
	}
	if err != nil || result.Status != "taken_over" || result.Recovery != "restored" || result.RetryAllowed {
		t.Fatalf("%+v %v", result, err)
	}
	human, _ := a.SessionControl(user, "board")
	if _, err = a.BeginControl(agent, "board", controlProof(human)); !errors.Is(err, ErrAgentPaused) {
		t.Fatal(err)
	}
	if _, err = a.WriteSession(WithControlProof(other, controlProof(human)), "board", session.WriteRequest{Actor: "user", Data: []byte("x")}); !errors.Is(err, ErrControlBusy) {
		t.Fatal("another window silently took over", err)
	}
	allowed, err := a.AllowAI(user, "board", controlProof(human))
	if err != nil || allowed.AIPaused {
		t.Fatal(err)
	}
	newScope, err := a.BeginControl(agent, "board", controlProof(allowed))
	if err != nil {
		t.Fatal(err)
	}
	duplicate, err := a.TakeControl(user, "board", controlProof(active))
	if err != nil || duplicate.Status != "taken_over" {
		t.Fatal(err)
	}
	current, _ := a.SessionControl(user, "board")
	if current.OperationID != newScope.OperationID {
		t.Fatal("late duplicate stopped successor")
	}
}

func TestControlUnknownRawRecoveryClosesOnlyCapturedSession(t *testing.T) {
	a, _, _ := instanceFixture(t)
	user := controlTestClient(t, a, "CL-human", "user")
	agent := controlTestClient(t, a, "CL-agent", "agent")
	v, _ := a.SessionControl(agent, "board")
	active, err := a.BeginControl(agent, "board", controlProof(v))
	if err != nil {
		t.Fatal(err)
	}
	if _, err = a.WriteSession(WithControlProof(agent, controlProof(active)), "board", session.WriteRequest{Actor: "agent", Data: []byte("run-background\n")}); err != nil {
		t.Fatal(err)
	}
	result, err := a.TakeControl(user, "board", controlProof(active))
	if err != nil {
		t.Fatal(err)
	}
	for deadline := time.Now().Add(time.Second); result.Status == "stopping" && time.Now().Before(deadline); {
		time.Sleep(time.Millisecond)
		result, err = a.ControlOutcome(user, "board", controlProof(active))
	}
	if err != nil || result.Recovery != "session_closed" || result.Status != "failed" {
		t.Fatalf("false recovery: %+v %v", result, err)
	}
	if _, err = a.SessionControl(user, "board"); !errors.Is(err, ErrSessionNotFound) {
		t.Fatal("unknown shell was re-enabled", err)
	}
}

func waitControlOutcome(t *testing.T, a *Application, ctx context.Context, p ControlProof) ControlResult {
	t.Helper()
	for deadline := time.Now().Add(2 * time.Second); time.Now().Before(deadline); {
		r, err := a.ControlOutcome(ctx, "board", p)
		if err == nil && r.Status != "stopping" {
			return r
		}
		time.Sleep(time.Millisecond)
	}
	t.Fatal("control recovery did not finish")
	return ControlResult{}
}

func TestControlManagedCommandTakeoverRestoresBeforeHumanWrites(t *testing.T) {
	a, _, _ := instanceFixture(t)
	user := controlTestClient(t, a, "CL-human", "user")
	agent := controlTestClient(t, a, "CL-agent", "agent")
	terminal, _ := a.session("board")
	fake := terminal.(*fakeConnectedSession)
	started := make(chan struct{})
	var token string
	fake.onWrite = func(q session.WriteRequest) {
		if len(q.Data) == 1 && q.Data[0] == 3 {
			fake.emitOutput([]byte("\x1d\x1c" + token + ":130\x1c\x1droot# "))
			return
		}
		fake.mu.Lock()
		for _, e := range fake.events {
			if e.Type == session.EventTerminalCommandStarted {
				token = strings.TrimPrefix(e.Metadata["command_id"].(string), "CMD-")
			}
		}
		fake.mu.Unlock()
		fake.emitOutput([]byte("\x1e\x1f" + token + "\x1f\x1e"))
		close(started)
	}
	initial, _ := a.SessionControl(agent, "board")
	done := make(chan error, 1)
	go func() {
		_, err := a.ExecuteTerminalCommand(WithControlProof(agent, controlProof(initial)), "board", "sleep 60")
		done <- err
	}()
	select {
	case <-started:
	case <-time.After(time.Second):
		t.Fatal("command not started")
	}
	active, _ := a.SessionControl(user, "board")
	if _, err := a.TakeControl(user, "board", controlProof(active)); err != nil {
		t.Fatal(err)
	}
	if err := <-done; !errors.Is(err, ErrControlInterrupted) {
		t.Fatal("lost interruption cause", err)
	}
	result := waitControlOutcome(t, a, user, controlProof(active))
	if result.Status != "taken_over" || result.Recovery != "restored" {
		t.Fatalf("%+v", result)
	}
	final, _ := a.SessionControl(user, "board")
	if !final.Writable || !final.AIPaused {
		t.Fatal("human not pinned", final)
	}
	if !strings.HasSuffix(string(fake.writtenData()), string([]byte{3})) {
		t.Fatal("missing bounded recovery interrupt")
	}
}

func TestControlExpiryClosesEvenUntouchedScopeAndCannotRenew(t *testing.T) {
	a, _, _ := instanceFixture(t)
	agent := controlTestClient(t, a, "CL-agent", "agent")
	v, _ := a.SessionControl(agent, "board")
	active, err := a.BeginControl(agent, "board", controlProof(v))
	if err != nil {
		t.Fatal(err)
	}
	a.leases.mu.Lock()
	r := a.leases.controls.entries["board"]
	r.view.ExpiresAt = time.Now().Add(-time.Second)
	a.leases.mu.Unlock()
	a.expireControl(r, active.Version, active.OperationID)
	result := waitControlOutcome(t, a, agent, controlProof(active))
	if result.Reason != "control_expired" || result.Recovery != "session_closed" {
		t.Fatalf("%+v", result)
	}
	if _, err = a.RenewControl(agent, "board", controlProof(active)); !errors.Is(err, ErrSessionNotFound) {
		t.Fatal(err)
	}
}

func TestControlRejectsEndedPresenceAndConcurrentTakeover(t *testing.T) {
	a, _, _ := instanceFixture(t)
	user := controlTestClient(t, a, "CL-user", "user")
	expired := controlTestClient(t, a, "CL-user", "user")
	p := expired.Value(instanceProofKey{}).(InstanceProof)
	if err := a.EndClientInstance(expired, p); err != nil {
		t.Fatal(err)
	}
	v, _ := a.SessionControl(user, "board")
	if _, err := a.WriteSession(WithControlProof(expired, controlProof(v)), "board", session.WriteRequest{Actor: "user", Data: []byte("stale")}); !errors.Is(err, ErrClientInstance) {
		t.Fatal(err)
	}
	var wg sync.WaitGroup
	results := make(chan ControlResult, 16)
	for range 16 {
		wg.Go(func() {
			r, err := a.TakeControl(user, "board", controlProof(v))
			if err != nil {
				t.Error(err)
			}
			results <- r
		})
	}
	wg.Wait()
	close(results)
	for r := range results {
		if r.Version != v.Version {
			t.Fatal("duplicate targeted another generation")
		}
	}
	final := waitControlOutcome(t, a, user, controlProof(v))
	if final.Status != "taken_over" {
		t.Fatal(final)
	}
}

func TestControlReleaseFailureDoesNotLeaveAutomaticScope(t *testing.T) {
	a, _, _ := instanceFixture(t)
	user := controlTestClient(t, a, "CL-user", "user")
	v, _ := a.SessionControl(user, "board")
	if _, err := a.WriteSession(WithControlProof(user, controlProof(v)), "board", session.WriteRequest{Actor: "user", Data: []byte("unfinished")}); err != nil {
		t.Fatal(err)
	}
	if _, err := a.AcquireLeaseForClient(WithControlProof(user, controlProof(v)), "board", "test-owner", LeaseTypeFileTransfer); !errors.Is(err, ErrTerminalCommandInputPending) {
		t.Fatal(err)
	}
	next, _ := a.SessionControl(user, "board")
	if next.State != "idle" || next.Version == v.Version {
		t.Fatal("failed admission retained automatic scope", next)
	}
}

func TestControlStaleManagementCannotCloseSuccessor(t *testing.T) {
	a, _, _ := instanceFixture(t)
	user := controlTestClient(t, a, "CL-user", "user")
	v, _ := a.SessionControl(user, "board")
	if _, err := a.TakeControl(user, "board", controlProof(v)); err != nil {
		t.Fatal(err)
	}
	_ = waitControlOutcome(t, a, user, controlProof(v))
	if _, err := a.CloseSessionForClient(WithControlProof(user, controlProof(v)), "board"); !errors.Is(err, ErrControlStale) {
		t.Fatal(err)
	}
	current, err := a.SessionControl(user, "board")
	if err != nil || !current.Writable {
		t.Fatal("stale close affected takeover", current, err)
	}
}

func TestControlDelegatedExecutorReturnsHumanOwnershipToParent(t *testing.T) {
	a, _, _ := instanceFixture(t)
	user := controlTestClient(t, a, "CL-user", "user")
	initial, _ := a.SessionControl(user, "board")
	_, err := a.TakeControl(user, "board", controlProof(initial))
	if err != nil {
		t.Fatal(err)
	}
	_ = waitControlOutcome(t, a, user, controlProof(initial))
	parentProof := user.Value(instanceProofKey{}).(InstanceProof)
	use, err := a.SetClientUse(user, parentProof, "file", "board", "operation")
	if err != nil {
		t.Fatal(err)
	}
	ticket, err := a.DelegateClientInstance(user, parentProof, use.ID)
	if err != nil {
		t.Fatal(err)
	}
	_, childProof, err := a.RegisterClientInstance(user, InstanceRegistration{Name: "child", Entry: "cli", Delegation: ticket})
	if err != nil {
		t.Fatal(err)
	}
	child, err := a.BindClientInstance(WithInstanceProof(user, childProof), "mcp")
	if err != nil {
		t.Fatal(err)
	}
	current, _ := a.SessionControl(child, "board")
	if !current.Writable {
		t.Fatal("validated child cannot execute parent intent")
	}
	lease, err := a.AcquireLeaseForClient(WithControlProof(child, controlProof(current)), "board", "child-owner", LeaseTypeTerminal)
	if err != nil {
		t.Fatal(err)
	}
	if _, err = a.WriteSessionWithLease(WithControlProof(child, lease.Control), "board", lease.Owner, session.WriteRequest{Actor: "user", Data: []byte("child\n")}); err != nil {
		t.Fatal(err)
	}
	if err = a.ReleaseLease("board", lease.Owner); err != nil {
		t.Fatal(err)
	}
	final, _ := a.SessionControl(user, "board")
	if !final.Writable || final.Client.InstanceID != ClientIdentityFromContext(user).InstanceID {
		t.Fatal("child stole parent ownership", final)
	}
	other, _ := a.SessionControl(user, "other")
	if _, err = a.WriteSession(WithControlProof(child, controlProof(other)), "other", session.WriteRequest{Actor: "user", Data: []byte("escape")}); !errors.Is(err, ErrClientInstance) {
		t.Fatal("delegate crossed target", err)
	}
}

func TestControlPrivateDebugRecoveryCannotWriteThroughReplacementLease(t *testing.T) {
	a, _, _ := instanceFixture(t)
	agent := controlTestClient(t, a, "CL-agent", "agent")
	v, _ := a.SessionControl(agent, "board")
	lease, err := a.AcquireLeaseForClient(WithControlProof(agent, controlProof(v)), "board", "debug-owner", LeaseTypeDebug)
	if err != nil {
		t.Fatal(err)
	}
	terminal, _ := a.session("board")
	run := &debugRun{app: a, terminal: terminal, owner: lease.Owner}
	a.leases.mu.Lock()
	r := a.leases.controls.entries["board"]
	a.stopControlLocked(r, session.ClientIdentity{}, "control_expired")
	a.leases.mu.Unlock()
	if err = run.write(context.Background(), []byte("stale")); !errors.Is(err, ErrControlStale) {
		t.Fatal(err)
	}
	run.controlRecovery = true
	if err = run.write(context.Background(), []byte("recovery")); err != nil {
		t.Fatal(err)
	}
	if err = a.ReleaseLease("board", lease.Owner); err != nil {
		t.Fatal(err)
	}
	if err = run.write(context.Background(), []byte("late")); !errors.Is(err, ErrLeaseNotOwned) {
		t.Fatal("recovery survived owner release", err)
	}
}
