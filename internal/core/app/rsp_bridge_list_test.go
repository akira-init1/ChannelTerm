package app

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/netip"
	"reflect"
	"strings"
	"testing"

	"github.com/akira-init1/ChannelTerm/internal/core/debugger"
	"github.com/akira-init1/ChannelTerm/internal/core/session"
)

func TestRSPBridgeListFiltersAndSortsSnapshots(t *testing.T) {
	a, s := bridgeFixture(t)
	// These status-only records exercise ordering and all lifecycle states
	// without relying on IPv6 availability or binding fixed test ports.
	runs := map[string]*bridgeRun{}
	for i, state := range []string{"listening", "connected", "disconnected", "recovering", "closed", "failed", "restored"} {
		id := fmt.Sprintf("RSP-%d", i)
		runs[id] = &bridgeRun{status: BridgeStatus{BridgeID: id, SessionID: s.ID(), State: state, Address: fmt.Sprintf("[::1]:%d", 48003-i)}}
	}
	runs["RSP-extra"] = &bridgeRun{status: BridgeStatus{BridgeID: "RSP-extra", SessionID: "removed", State: "disconnected", Address: "127.0.0.1:48000"}}
	a.bridges.runs = runs
	defer func() { a.bridges.runs = nil }() // The records own no workers or endpoints.
	items, err := a.ListRSPBridges(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	var ids []string
	for _, item := range items {
		ids = append(ids, item.BridgeID)
		if item.BridgeID == "RSP-extra" {
			if item.SessionID != "removed" || item.SessionRef != "" || item.Target != "" {
				t.Fatalf("removed Session metadata: %+v", item)
			}
		} else if item.SessionRef != "SER-1" || item.Target != "TEST-NO-HARDWARE" || item.Port < 48000 {
			t.Fatalf("incorrect Session/IPv6 metadata: %+v", item)
		}
	}
	if want := []string{"RSP-extra", "RSP-3", "RSP-2", "RSP-1", "RSP-0"}; !reflect.DeepEqual(ids, want) {
		t.Fatalf("bridge order = %v, want %v", ids, want)
	}
}

func TestRSPBridgeListObservesWithoutRenewingOrDisclosingSecrets(t *testing.T) {
	a, s := bridgeFixture(t)
	items, err := a.ListRSPBridges(context.Background())
	if err != nil || items == nil || len(items) != 0 {
		t.Fatalf("empty inventory = %+v, %v", items, err)
	}
	start, err := a.StartRSPBridge(context.Background(), bridgeRequest())
	if err != nil {
		t.Fatal(err)
	}
	r := a.bridges.runs[start.Status.BridgeID]
	lease, _, err := a.LeaseStatus(s.ID())
	if err != nil {
		t.Fatal(err)
	}
	assertListed := func(state string) {
		t.Helper()
		items, err := a.ListRSPBridges(context.Background())
		if err != nil || len(items) != 1 {
			t.Fatalf("inventory = %+v, %v", items, err)
		}
		item := items[0]
		if item.State != state || item.BridgeID != start.Status.BridgeID || item.SessionID != s.ID() || item.Address != start.Connection.Address || item.Port != netip.MustParseAddrPort(start.Connection.Address).Port() {
			t.Fatalf("incorrect snapshot: %+v", item)
		}
		if !item.ExpiresAt.Equal(start.Status.ExpiresAt) {
			t.Fatal("inventory renewed management heartbeat")
		}
		encoded, err := json.Marshal(items)
		if err != nil {
			t.Fatal(err)
		}
		for _, secret := range []string{bridgeTestOwner, r.owner, start.Connection.Token, start.Connection.Fingerprint} {
			if strings.Contains(string(encoded), secret) {
				t.Fatal("inventory disclosed a credential")
			}
		}
	}
	assertListed("listening")
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	if _, err := a.ListRSPBridges(ctx); !errors.Is(err, context.Canceled) {
		t.Fatalf("canceled inventory: %v", err)
	}
	client, err := debugger.DialBridge(context.Background(), *start.Connection)
	if err != nil {
		t.Fatal(err)
	}
	defer client.Close()
	waitBridgeState(t, a, start.Status.BridgeID, "connected")
	assertListed("connected")
	client.Close()
	waitBridgeState(t, a, start.Status.BridgeID, "disconnected")
	assertListed("disconnected")
	after, active, err := a.LeaseStatus(s.ID())
	if err != nil || !active || !after.ExpiresAt.Equal(lease.ExpiresAt) || len(s.writtenData()) != 0 {
		t.Fatalf("observation changed lease or target input: %+v, %v", after, err)
	}
	if _, err := a.CloseRSPBridge(start.Status.BridgeID, bridgeTestOwner); err != nil {
		t.Fatal(err)
	}
	items, err = a.ListRSPBridges(context.Background())
	if err != nil || items == nil || len(items) != 0 {
		t.Fatalf("closed bridge retained in inventory: %+v, %v", items, err)
	}
}

func TestRSPBridgeListConcurrentStartAndClose(t *testing.T) {
	a, _ := bridgeFixture(t)
	ctx, cancel := context.WithCancel(context.Background())
	done := make(chan error, 1)
	go func() {
		for {
			if _, err := a.ListRSPBridges(ctx); err != nil {
				done <- err
				return
			}
		}
	}()
	defer func() {
		cancel()
		if err := <-done; !errors.Is(err, context.Canceled) {
			t.Errorf("concurrent inventory: %v", err)
		}
	}()
	for i := range 8 {
		s := newFakeConnectedSession(fmt.Sprintf("board-%d", i))
		if err := s.Connect(context.Background()); err != nil {
			t.Fatal(err)
		}
		if err := a.manager.RegisterWithMetadata(s, session.SessionMetadata{Transport: "serial", Endpoint: "TEST-NO-HARDWARE"}); err != nil {
			t.Fatal(err)
		}
		q := bridgeRequest()
		q.SessionID, q.RequestID = s.ID(), s.ID()
		start, err := a.StartRSPBridge(context.Background(), q)
		if err != nil {
			t.Fatal(err)
		}
		if _, err := a.CloseRSPBridge(start.Status.BridgeID, bridgeTestOwner); err != nil {
			t.Fatal(err)
		}
	}
}
