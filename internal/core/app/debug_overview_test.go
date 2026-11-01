package app

import (
	"context"
	"encoding/json"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/akira-init1/ChannelTerm/internal/core/debugger"
	"github.com/akira-init1/ChannelTerm/internal/core/session"
)

func TestDebugObservationPortsIdentityFreshnessAndMI(t *testing.T) {
	m := session.NewManager()
	defer m.Close()
	a, _ := New(Dependencies{Manager: m})
	ctx := WithClientIdentity(context.Background(), session.ClientIdentity{ID: "CL-test", Name: "Test executor", Kind: "agent"})
	q := DebugRegistration{Owner: strings.Repeat("fake-owner", 4), RequestID: "one", Name: "TCP target", Mode: "tcp", Executor: "mi", Ports: []DebugPort{{Role: "target", Configured: "[::1]:0"}}}
	r, err := a.RegisterDebugObservation(ctx, q)
	if err != nil {
		t.Fatal(err)
	}
	if r.SessionID != "" || r.Connection != "unknown" || r.Source != "manual" {
		t.Fatal(r)
	}
	dup, err := a.RegisterDebugObservation(ctx, q)
	if err != nil || dup.ID != r.ID {
		t.Fatal(dup, err)
	}
	q.Name = "conflict"
	if _, err := a.RegisterDebugObservation(ctx, q); err == nil {
		t.Fatal("conflicting retry accepted")
	}
	report := DebugObservationReport{ID: r.ID, Owner: q.Owner, Sequence: 1, Connection: "connected", Ports: []DebugPort{{Role: "target", Address: "[::1]:43123"}}}
	got, err := a.ReportDebugObservation(ctx, report)
	if err != nil || got.TargetState != "unknown" || got.Ports[0].Port != 43123 || got.Ports[0].Configured != "[::1]:0" {
		t.Fatal(got, err)
	}
	for i, mi := range []string{`*running,thread-id="all"`, `*stopped,reason="breakpoint-hit"`} {
		report.Sequence = uint64(i + 2)
		report.MI = mi
		got, err = a.ReportDebugObservation(ctx, report)
		if err != nil {
			t.Fatal(err)
		}
	}
	if got.TargetState != "stopped" || got.Client.ID != "CL-test" || got.Source != "client_report" {
		t.Fatal(got)
	}
	other := WithClientIdentity(ctx, session.ClientIdentity{ID: "CL-other"})
	if _, err := a.ReportDebugObservation(other, report); err == nil {
		t.Fatal("foreign client reported")
	}
	duplicate, err := a.ReportDebugObservation(ctx, report)
	if err != nil || !duplicate.UpdatedAt.Equal(got.UpdatedAt) {
		t.Fatal("retry refreshed", err)
	}
	a.observations.runs[r.ID].item.UpdatedAt = time.Now().Add(-31 * time.Second)
	s, err := a.DebugOverview(ctx)
	if err != nil || !s.Operations[0].Stale || s.Operations[0].TargetState != "unknown" || s.Operations[0].LastTargetState != "stopped" || s.Operations[0].TunnelState != "not_applicable" {
		t.Fatal(s, err)
	}
	encoded, _ := json.Marshal(s)
	if strings.Contains(string(encoded), q.Owner) || strings.Contains(string(encoded), "breakpoint-hit") {
		t.Fatal("secret/raw MI leaked")
	}
	report.Sequence++
	report.Connection = "disconnected"
	report.MI = ""
	got, err = a.ReportDebugObservation(ctx, report)
	if err != nil || got.TargetState != "unknown" || got.LastTargetState != "stopped" {
		t.Fatal(got, err)
	}
	if _, err := a.RegisterDebugObservation(WithClientAccess(ctx, ClientAccess{}), q); err == nil {
		t.Fatal("read-only registration")
	}
	cancelled, cancel := context.WithCancel(ctx)
	cancel()
	if _, err := a.DebugOverview(cancelled); err == nil {
		t.Fatal("cancel ignored")
	}
}

func TestDebugObservationModesAndManualLimits(t *testing.T) {
	m := session.NewManager()
	defer m.Close()
	a, _ := New(Dependencies{Manager: m})
	ctx := context.Background()
	ssh := newFakeConnectedSession("opaque-ssh")
	_ = ssh.Connect(ctx)
	_ = m.RegisterWithMetadata(ssh, session.SessionMetadata{Transport: "ssh"})
	for _, mode := range []string{"tcp", "ssh_tunnel", "ssh_stdio", "ssh_terminal", "ssh_terminal_tcp"} {
		q := DebugRegistration{Owner: strings.Repeat("test", 8), RequestID: mode, Name: mode, Mode: mode, Executor: "mi"}
		if strings.HasPrefix(mode, "ssh_terminal") {
			q.SessionID = ssh.ID()
		}
		r, err := a.RegisterDebugObservation(ctx, q)
		if err != nil {
			t.Fatal(mode, err)
		}
		for _, p := range r.Ports {
			if p.Role == "target" && (mode == "ssh_stdio" || mode == "ssh_terminal") && p.State != "not_applicable" {
				t.Fatal(p)
			}
		}
		if mode == "ssh_tunnel" {
			got, err := a.ReportDebugObservation(ctx, DebugObservationReport{ID: r.ID, Owner: q.Owner, Sequence: 1, Connection: "disconnected", TunnelState: "connected", Ports: []DebugPort{{Role: "ssh", Address: "host.example:2222"}, {Role: "local_forward", Address: "127.0.0.1:43210"}, {Role: "target", Address: "127.0.0.1:2345"}}})
			if err != nil || got.TunnelState != "connected" || got.Connection != "disconnected" || got.Ports[2].Perspective != "remote" {
				t.Fatal(got, err)
			}
		}
	}
	q := DebugRegistration{Owner: strings.Repeat("test", 8), RequestID: "manual", Name: "manual", Mode: "tcp", Executor: "manual"}
	leak := q
	leak.Ports = []DebugPort{{Role: "target", Configured: q.Owner + ":1234"}}
	if _, err := a.RegisterDebugObservation(ctx, leak); err == nil {
		t.Fatal("private owner was exposed in an endpoint")
	}
	r, err := a.RegisterDebugObservation(ctx, q)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := a.ReportDebugObservation(ctx, DebugObservationReport{ID: r.ID, Owner: q.Owner, Sequence: 1, Connection: "connected"}); err == nil {
		t.Fatal("manual observation promoted")
	}
	for _, ports := range [][]DebugPort{{{Role: "target", Address: "127.0.0.1:9999"}}, {{Role: "bad"}}, {{Role: "ssh"}, {Role: "ssh"}}} {
		if _, err := normalizeDebugPorts("ssh_stdio", ports, true); err == nil {
			t.Fatal("invalid ports accepted", ports)
		}
	}
}

func TestDebugObservationConcurrentRetryAndSnapshot(t *testing.T) {
	m := session.NewManager()
	defer m.Close()
	a, _ := New(Dependencies{Manager: m})
	ctx := context.Background()
	q := DebugRegistration{Owner: strings.Repeat("test", 8), RequestID: "concurrent", Name: "Concurrent target", Mode: "tcp", Executor: "mi"}
	var workers sync.WaitGroup
	for range 8 {
		workers.Add(1)
		go func() {
			defer workers.Done()
			r, err := a.RegisterDebugObservation(ctx, q)
			if err != nil {
				t.Error(err)
				return
			}
			report := DebugObservationReport{ID: r.ID, Owner: q.Owner, Sequence: 1, Connection: "connected", MI: `*stopped,reason="breakpoint-hit"`}
			if _, err := a.ReportDebugObservation(ctx, report); err != nil {
				t.Error(err)
			}
			for range 10 {
				if _, err := a.DebugOverview(ctx); err != nil {
					t.Error(err)
				}
			}
		}()
	}
	workers.Wait()
	out, _ := a.DebugOverview(ctx)
	if len(out.Operations) != 1 || out.Operations[0].TargetState != "stopped" {
		t.Fatal(out)
	}
}

func TestDebugOverviewHostSnapshotsDoNotRenewOrRequireOwner(t *testing.T) {
	a, s := bridgeFixture(t)
	ctx := context.Background()
	start, err := a.StartRSPBridge(ctx, bridgeRequest())
	if err != nil {
		t.Fatal(err)
	}
	r := a.bridges.runs[start.Status.BridgeID]
	before := r.snapshot().ExpiresAt
	d := &debugRun{id: "DBG-test", terminal: s, state: "running", updated: time.Now(), expires: before, changed: make(chan struct{})}
	a.debug.runs = map[string]*debugRun{d.id: d}
	for _, state := range []string{"listening", "connected", "disconnected", "recovering"} {
		r.setState(state, nil)
		out, err := a.DebugOverview(ctx)
		if err != nil || len(out.Operations) != 2 {
			t.Fatal(out, err)
		}
		if out.Operations[0].TargetState != "running" || out.Operations[1].Connection != state || out.Operations[1].Ports[0].Port == 0 {
			t.Fatal(out)
		}
	}
	d.record(debugger.Record{Kind: "*", Class: "stopped"})
	out, _ := a.DebugOverview(ctx)
	if out.Operations[0].TargetState != "stopped" {
		t.Fatal(out)
	}
	if !r.snapshot().ExpiresAt.Equal(before) || !d.expires.Equal(before) {
		t.Fatal("observation renewed heartbeat")
	}
	r.setState("connected", nil)
}
