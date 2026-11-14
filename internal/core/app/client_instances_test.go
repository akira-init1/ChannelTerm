package app

import (
	"context"
	"encoding/json"
	"errors"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/akira-init1/ChannelTerm/internal/core/session"
)

func instanceFixture(t *testing.T) (*Application, context.Context, *time.Time) {
	t.Helper()
	m := session.NewManager()
	t.Cleanup(func() { _ = m.Close() })
	for _, id := range []string{"board", "other"} {
		s := newFakeConnectedSession(id)
		if err := s.Connect(context.Background()); err != nil {
			t.Fatal(err)
		}
		if err := m.Register(s); err != nil {
			t.Fatal(err)
		}
	}
	a, err := New(Dependencies{Manager: m})
	if err != nil {
		t.Fatal(err)
	}
	now := time.Date(2026, 11, 15, 0, 0, 0, 0, time.UTC)
	a.instances.now = func() time.Time { return now }
	ctx := WithClientIdentity(context.Background(), session.ClientIdentity{ID: "CL-test", Name: "Test agent", Kind: "agent"})
	return a, ctx, &now
}

func registerInstance(t *testing.T, a *Application, ctx context.Context) (ClientInstance, InstanceProof) {
	t.Helper()
	v, p, e := a.RegisterClientInstance(ctx, InstanceRegistration{Name: "Test instance", Entry: "agent"})
	if e != nil {
		t.Fatal(e)
	}
	return v, p
}

func TestClientInstancePresenceAndExpiryDoNotOwnSessions(t *testing.T) {
	a, ctx, now := instanceFixture(t)
	first, p := registerInstance(t, a, ctx)
	second, q := registerInstance(t, a, ctx)
	if first.ID == second.ID || first.Client.ID != second.Client.ID {
		t.Fatal("instance and registration identity conflated")
	}
	use, err := a.SetClientUse(ctx, p, "view-1", "board", "monitor")
	if err != nil {
		t.Fatal(err)
	}
	retry, err := a.SetClientUse(ctx, p, "view-1", "board", "monitor")
	if err != nil || retry.ID != use.ID {
		t.Fatal("usage retry duplicated")
	}
	if _, err = a.SetClientUse(ctx, p, "view-1", "other", "monitor"); !errors.Is(err, ErrClientInstanceInput) {
		t.Fatal("key rebound")
	}
	*now = now.Add(40 * time.Second)
	if _, err = a.RenewClientInstance(ctx, q); err != nil {
		t.Fatal(err)
	}
	*now = now.Add(21 * time.Second)
	if _, err = a.RenewClientInstance(ctx, p); !errors.Is(err, ErrClientInstance) {
		t.Fatal("expired instance revived")
	}
	views, _ := a.ListClientInstances(ctx)
	for _, v := range views {
		if v.ID == first.ID && (v.State != "expired" || len(v.Uses) != 0) {
			t.Fatal("expired use still online")
		}
	}
	if err = a.EndClientInstance(ctx, q); err != nil {
		t.Fatal(err)
	}
	if err = a.EndClientInstance(ctx, q); err != nil {
		t.Fatal("duplicate end", err)
	}
	terminal, err := a.session("board")
	if err != nil || terminal.State() != session.StateOpen {
		t.Fatal("presence closed device")
	}
	*now = now.Add(6 * time.Minute)
	views, _ = a.ListClientInstances(ctx)
	if len(views) != 0 {
		t.Fatal("retention unbounded")
	}
}

func TestClientInstanceCapabilityAndDelegationBoundaries(t *testing.T) {
	a, ctx, now := instanceFixture(t)
	parent, p := registerInstance(t, a, ctx)
	foreign := WithClientIdentity(ctx, session.ClientIdentity{ID: "CL-other", Kind: "user"})
	for _, bad := range []InstanceProof{{ID: p.ID}, {ID: p.ID, Secret: "wrong"}} {
		if _, err := a.RenewClientInstance(ctx, bad); !errors.Is(err, ErrClientInstance) {
			t.Fatal("public ID authorized")
		}
	}
	if _, err := a.RenewClientInstance(foreign, p); !errors.Is(err, ErrClientInstance) {
		t.Fatal("cross credential proof accepted")
	}
	use, err := a.SetClientUse(ctx, p, "parent-job", "board", "operation")
	if err != nil {
		t.Fatal(err)
	}
	ticket, err := a.DelegateClientInstance(ctx, p, use.ID)
	if err != nil {
		t.Fatal(err)
	}
	if _, _, err = a.RegisterClientInstance(foreign, InstanceRegistration{Name: "child", Entry: "cli", Delegation: ticket}); !errors.Is(err, ErrClientInstance) {
		t.Fatal("delegation crossed credential")
	}
	child, cp, err := a.RegisterClientInstance(ctx, InstanceRegistration{Name: "child", Entry: "cli", Delegation: ticket})
	if err != nil {
		t.Fatal(err)
	}
	if child.ParentInstanceID != parent.ID || child.ParentOperationID != use.ID {
		t.Fatal("lost parent")
	}
	if _, _, err = a.RegisterClientInstance(ctx, InstanceRegistration{Name: "replay", Entry: "cli", Delegation: ticket}); !errors.Is(err, ErrClientInstance) {
		t.Fatal("delegation replay")
	}
	bound, err := a.BindClientInstance(WithInstanceProof(ctx, cp), "mcp")
	if err != nil {
		t.Fatal(err)
	}
	identity := ClientIdentityFromContext(bound)
	if identity.Kind != "agent" || identity.DeclaredEntry != "cli" || identity.Entry != "mcp" || identity.OperationID != use.ID {
		t.Fatal("executor replaced authenticated initiator")
	}
	if _, _, err = a.BeginClientCall(bound, "other", "terminal_write", true); !errors.Is(err, ErrClientInstance) {
		t.Fatal("delegation target escaped")
	}
	if err = a.EndClientUse(ctx, p, use.ID); err != nil {
		t.Fatal(err)
	}
	public, _ := a.ListClientInstances(ctx)
	raw, _ := json.Marshal(public)
	for _, secret := range []string{p.Secret, cp.Secret, ticket} {
		if strings.Contains(string(raw), secret) {
			t.Fatal("capability in public snapshot")
		}
	}
	use, _ = a.SetClientUse(ctx, p, "next", "board", "operation")
	ticket, _ = a.DelegateClientInstance(ctx, p, use.ID)
	*now = now.Add(31 * time.Second)
	if _, _, err = a.RegisterClientInstance(ctx, InstanceRegistration{Name: "late", Entry: "cli", Delegation: ticket}); !errors.Is(err, ErrClientInstance) {
		t.Fatal("expired delegation accepted")
	}
}

func TestClientInstanceOperationProvenanceSurvivesEnd(t *testing.T) {
	a, ctx, now := instanceFixture(t)
	_, p := registerInstance(t, a, ctx)
	bound, err := a.BindClientInstance(WithInstanceProof(ctx, p), "mcp")
	if err != nil {
		t.Fatal(err)
	}
	bound, done, err := a.BeginClientCall(bound, "board", "terminal_exec", true)
	if err != nil {
		t.Fatal(err)
	}
	ref, err := a.sessionForClient(bound, "board")
	if err != nil {
		t.Fatal(err)
	}
	if err = a.EndClientInstance(ctx, p); err != nil {
		t.Fatal(err)
	}
	*now = now.Add(10 * time.Minute)
	views, _ := a.ListClientInstances(ctx)
	if len(views) != 1 || len(views[0].Uses) != 1 || !views[0].Uses[0].Active {
		t.Fatal("in-flight operation lost")
	}
	ref.PublishEvent(session.Event{Type: session.EventOperationState, Actor: "system"})
	chunk, err := a.ReadSessionEvents(ctx, "board", nil, 20)
	if err != nil {
		t.Fatal(err)
	}
	last := chunk.Events[len(chunk.Events)-1]
	if last.Client.InstanceID != p.ID || last.Client.OperationID == "" {
		t.Fatal("async provenance lost")
	}
	done()
	done()
	views, _ = a.ListClientInstances(ctx)
	if len(views) != 0 {
		t.Fatal("active record leaked")
	}
	if _, err = a.BindClientInstance(ctx, "mcp"); err != nil {
		t.Fatal("legacy client rejected")
	}
}

func TestClientInstanceBoundsAndConcurrentCleanup(t *testing.T) {
	a, ctx, _ := instanceFixture(t)
	_, p := registerInstance(t, a, ctx)
	var wg sync.WaitGroup
	for i := 0; i < 20; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			for j := 0; j < 10; j++ {
				bound, err := a.BindClientInstance(WithInstanceProof(ctx, p), "mcp")
				if err != nil {
					t.Error(err)
					return
				}
				_, done, err := a.BeginClientCall(bound, "board", "terminal_write", true)
				if err != nil {
					t.Error(err)
					return
				}
				done()
				if _, err = a.RenewClientInstance(ctx, p); err != nil {
					t.Error(err)
				}
				_, _ = a.ListClientInstances(ctx)
			}
		}()
	}
	wg.Wait()
	for i := 1; i < maxClientInstances; i++ {
		registerInstance(t, a, ctx)
	}
	if _, _, err := a.RegisterClientInstance(ctx, InstanceRegistration{Name: "overflow", Entry: "agent"}); !errors.Is(err, ErrClientInstanceLimit) {
		t.Fatal("unbounded instances")
	}
	for i := 0; i < maxInstanceUses; i++ {
		if _, err := a.SetClientUse(ctx, p, strings.Repeat("x", i+1), "board", "observe"); err != nil {
			t.Fatal(err)
		}
	}
	if _, err := a.SetClientUse(ctx, p, "overflow", "board", "observe"); !errors.Is(err, ErrClientInstanceLimit) {
		t.Fatal("unbounded uses")
	}
	for _, name := range []string{"", "bad\nname", strings.Repeat("x", 129)} {
		if _, _, err := a.RegisterClientInstance(ctx, InstanceRegistration{Name: name, Entry: "agent"}); !errors.Is(err, ErrClientInstanceInput) {
			t.Fatal("bad name accepted")
		}
	}
	if err := a.EndClientInstance(ctx, p); err != nil {
		t.Fatal(err)
	}
	registerInstance(t, a, ctx)
	if _, err := a.RenewClientInstance(ctx, p); !errors.Is(err, ErrClientInstance) {
		t.Fatal("evicted history revived")
	}
}

func TestClientInstanceOfflineLeaseAndHostRestart(t *testing.T) {
	a, ctx, now := instanceFixture(t)
	_, proof := registerInstance(t, a, ctx)
	bound, err := a.BindClientInstance(WithInstanceProof(ctx, proof), "mcp")
	if err != nil {
		t.Fatal(err)
	}
	if _, err = a.AcquireLeaseForClient(bound, "board", "test-owner", LeaseTypeTerminal); err != nil {
		t.Fatal(err)
	}
	if err = a.EndClientInstance(ctx, proof); err != nil {
		t.Fatal(err)
	}
	// Advance presence time only: the existing lease clock/lifetime is independent.
	*now = now.Add(10 * time.Minute)
	views, err := a.ListClientInstances(ctx)
	if err != nil || len(views) != 1 || views[0].State != "ended" || len(views[0].Uses) != 1 || !views[0].Uses[0].Active {
		t.Fatalf("offline lease lost: %+v, %v", views, err)
	}
	if err = a.ReleaseLease("board", "test-owner"); err != nil {
		t.Fatal(err)
	}
	views, _ = a.ListClientInstances(ctx)
	if len(views) != 0 {
		t.Fatal("released lease pinned history")
	}
	restarted, _, _ := instanceFixture(t)
	if _, err = restarted.BindClientInstance(WithInstanceProof(ctx, proof), "mcp"); !errors.Is(err, ErrClientInstance) {
		t.Fatal("prior Host proof accepted")
	}
}
