package app

import (
	"context"
	"errors"
	"strings"
	"testing"
	"time"
)

func TestDebugLeaseExpiryFailsClosedWithoutActiveWrite(t *testing.T) {
	c := newLeaseCoordinatorWithTTL(10 * time.Millisecond)
	expired := make(chan bool, 1)
	c.onExpired = func(_ SessionLease, abort bool) { expired <- abort }
	if _, e := c.acquire("s", "debug-owner", LeaseTypeDebug); e != nil {
		t.Fatal(e)
	}
	if abort := <-expired; !abort {
		t.Fatal("debug expiry did not require Session close")
	}
	wrote := false
	if _, e := c.write("s", "", "s", func() (int, error) { wrote = true; return 1, nil }); !errors.Is(e, ErrSessionBusy) || wrote {
		t.Fatalf("write reopened after expiry: %v", e)
	}
	if _, e := c.acquire("s", "new", LeaseTypeTerminal); !errors.Is(e, ErrSessionBusy) {
		t.Fatalf("lease reopened after expiry: %v", e)
	}
}
func TestDebugLeaseRenewAndConflict(t *testing.T) {
	c := newLeaseCoordinatorWithTTL(time.Minute)
	t.Cleanup(func() { c.remove("s") })
	l, e := c.acquire("s", "owner", LeaseTypeDebug)
	if e != nil {
		t.Fatal(e)
	}
	if _, e = c.acquire("s", "other", LeaseTypeDebug); !errors.Is(e, ErrSessionBusy) {
		t.Fatal(e)
	}
	// Model a partially consumed lease without relying on wall-clock resolution.
	c.mu.Lock()
	l.ExpiresAt = l.ExpiresAt.Add(-30 * time.Second)
	c.leases["s"] = l
	c.scheduleExpiryLocked(l)
	c.mu.Unlock()
	n, e := c.renew("s", "owner")
	if e != nil || !n.ExpiresAt.After(l.ExpiresAt) {
		t.Fatalf("renew: %v %v", n, e)
	}
	if _, e = c.renew("s", "other"); !errors.Is(e, ErrLeaseNotOwned) {
		t.Fatal(e)
	}
	c.remove("s")
}
func TestDebugCommandsRejectRawAndMultipleInferiors(t *testing.T) {
	for _, q := range [][2]string{{"console", "shell rm"}, {"add-inferior", ""}, {"thread", "1 --all"}, {"evaluate", "x\n-exec-continue"}, {"continue", "--all"}, {"break_insert", ""}} {
		if _, e := debugMICommand(q[0], q[1]); e == nil {
			t.Fatalf("accepted %v", q)
		}
	}
	cmd, e := debugMICommand("evaluate", `x\";continue`)
	if e != nil || strings.Contains(cmd, "\n") {
		t.Fatalf("quote: %q %v", cmd, e)
	}
}
func TestDebugOwnerAndLostEventCursor(t *testing.T) {
	a := &Application{}
	r := &debugRun{request: DebugStartRequest{Owner: strings.Repeat("a", 32)}, id: "debug", terminal: newFakeConnectedSession("s"), state: "stopped", changed: make(chan struct{})}
	a.debug.runs = map[string]*debugRun{"debug": r}
	if _, e := a.debug.get("debug", "wrong"); e == nil {
		t.Fatal("bad owner accepted")
	}
	r.next = 1000
	r.events = []DebugEvent{{Sequence: 999}}
	if _, e := a.ReadDebugEvents(context.Background(), "debug", r.request.Owner, 0); e == nil {
		t.Fatal("lost events silently skipped")
	}
	if _, e := a.ReadDebugEvents(context.Background(), "debug", r.request.Owner, 1001); e == nil {
		t.Fatal("future cursor accepted")
	}
}
