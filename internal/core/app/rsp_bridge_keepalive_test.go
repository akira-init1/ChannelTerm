package app

import (
	"context"
	"errors"
	"os"
	"testing"
	"time"

	"github.com/akira-init1/ChannelTerm/internal/core/debugger"
	"github.com/akira-init1/ChannelTerm/internal/core/session"
)

// TestRSPBridgeIdleFiveMinutes measures actual wall time with a fake serial
// Session and real TLS/control sockets. It is not GDB or hardware validation.
func TestRSPBridgeIdleFiveMinutes(t *testing.T) {
	if os.Getenv("CHANNELTERM_TEST_RSP_LONG") != "1" {
		t.Skip("set CHANNELTERM_TEST_RSP_LONG=1 for the 310-second idle test")
	}
	a, s, r, disconnect := liveBridge(t)
	initial := r.snapshot().ExpiresAt
	deadline := time.NewTimer(310 * time.Second)
	defer deadline.Stop()
	ticker := time.NewTicker(5 * time.Second)
	defer ticker.Stop()
	for {
		select {
		case <-ticker.C:
			status := r.snapshot()
			lease, active, err := a.LeaseStatus(s.ID())
			if status.State != "connected" || !status.AutoKeepalive || !status.ExpiresAt.After(initial) || err != nil || !active || !time.Now().Before(lease.ExpiresAt) {
				t.Fatalf("idle lifetime failed: %+v lease active=%t err=%v", status, active, err)
			}
			if len(s.writtenData()) != 0 {
				t.Fatal("idle heartbeat contaminated RSP")
			}
		case <-deadline.C:
			disconnect()
			before := r.snapshot().ExpiresAt
			r.maintain()
			if !r.snapshot().ExpiresAt.Equal(before) {
				t.Fatal("renewed after helper exit")
			}
			t.Log("310 seconds of idle RSP with live TLS control: both leases survived; helper exit stopped auto renewal")
			return
		}
	}
}

func liveBridge(t *testing.T) (*Application, *fakeConnectedSession, *bridgeRun, func()) {
	t.Helper()
	a, s := bridgeFixture(t)
	result, err := a.StartRSPBridge(context.Background(), bridgeRequest())
	if err != nil {
		t.Fatal(err)
	}
	client, err := debugger.DialBridgeKeepAlive(context.Background(), *result.Connection)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { client.Close() })
	waitBridgeState(t, a, result.Status.BridgeID, "connected")
	r := a.bridges.runs[result.Status.BridgeID]
	deadline := time.Now().Add(time.Second)
	for !r.endpoint.ClientAlive() {
		if time.Now().After(deadline) {
			t.Fatal("no live heartbeat")
		}
		time.Sleep(time.Millisecond)
	}
	return a, s, r, func() { client.Close(); waitBridgeState(t, a, r.id, "disconnected") }
}

func TestRSPBridgeAutomaticRenewalRequiresLiveControl(t *testing.T) {
	a, s, r, disconnect := liveBridge(t)
	before := consumeBridgeManagementTTL(r)
	r.maintain()
	if status := r.snapshot(); !status.AutoKeepalive || !status.ExpiresAt.After(before) {
		t.Fatalf("live idle bridge not renewed: %+v", status)
	}
	if len(s.writtenData()) != 0 {
		t.Fatal("heartbeat wrote target bytes")
	}
	disconnect()
	before = consumeBridgeManagementTTL(r)
	r.maintain()
	if status := r.snapshot(); status.AutoKeepalive || !status.ExpiresAt.Equal(before) {
		t.Fatalf("disconnected bridge auto-renewed: %+v", status)
	}
	if _, err := a.WriteSession(context.Background(), s.ID(), session.WriteRequest{Actor: session.ActorUser, Data: []byte("unsafe")}); !errors.Is(err, ErrSessionBusy) {
		t.Fatalf("disconnect opened shell writes: %v", err)
	}
	if status, err := a.RenewRSPBridge(r.id, bridgeTestOwner); err != nil || !status.ExpiresAt.After(before) {
		t.Fatalf("legacy explicit renewal failed: %+v %v", status, err)
	}
}

// consumeBridgeManagementTTL models elapsed lease time without depending on OS
// clock resolution. It leaves enough time for the next operation to renew safely.
func consumeBridgeManagementTTL(r *bridgeRun) time.Time {
	r.op.Lock()
	defer r.op.Unlock()
	r.mu.Lock()
	defer r.mu.Unlock()
	r.status.ExpiresAt = r.status.ExpiresAt.Add(-debugClientTTL / 2)
	return r.status.ExpiresAt
}

func TestRSPBridgeLeaseFailureAndMaximumFailClosed(t *testing.T) {
	for _, mode := range []string{"management", "internal", "maximum", "explicit-renew"} {
		t.Run(mode, func(t *testing.T) {
			a, s, r, _ := liveBridge(t)
			r.op.Lock()
			if mode == "internal" || mode == "explicit-renew" {
				a.leases.mu.Lock()
				lease := a.leases.leases[s.ID()]
				lease.ExpiresAt = time.Now().Add(-time.Second)
				a.leases.leases[s.ID()] = lease
				a.leases.mu.Unlock()
			} else {
				r.mu.Lock()
				if mode == "maximum" {
					r.status.MaxExpiresAt = time.Now().Add(-time.Second)
				} else {
					r.status.ExpiresAt = time.Now().Add(-time.Second)
				}
				r.mu.Unlock()
			}
			r.op.Unlock()
			if mode == "explicit-renew" {
				if _, err := a.RenewRSPBridge(r.id, bridgeTestOwner); err == nil {
					t.Fatal("revived expired internal lease")
				}
			} else {
				r.maintain()
			}
			if status := r.snapshot(); status.State != "failed" || status.AutoKeepalive || status.TargetState == "shell_verified" {
				t.Fatalf("unsafe failure status: %+v", status)
			}
			if s.State() != session.StateClosed {
				t.Fatal("expiry retained open serial Session")
			}
			if _, err := a.WriteSession(context.Background(), s.ID(), session.WriteRequest{Actor: session.ActorUser, Data: []byte("unsafe")}); err == nil {
				t.Fatal("expiry enabled ordinary writes")
			}
		})
	}
}

func TestRSPBridgeManualRenewalCappedByMaximum(t *testing.T) {
	a, _, r, _ := liveBridge(t)
	r.op.Lock()
	r.mu.Lock()
	r.status.MaxExpiresAt = time.Now().Add(20 * time.Second)
	maximum := r.status.MaxExpiresAt
	r.mu.Unlock()
	r.op.Unlock()
	status, err := a.RenewRSPBridge(r.id, bridgeTestOwner)
	if err != nil || !status.ExpiresAt.Equal(maximum) {
		t.Fatalf("uncapped renewal: %+v %v", status, err)
	}
}
