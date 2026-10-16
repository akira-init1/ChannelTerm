package app

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"io"
	"net"
	"regexp"
	"strings"
	"testing"
	"time"

	"github.com/akira-init1/ChannelTerm/internal/core/debugger"
	"github.com/akira-init1/ChannelTerm/internal/core/session"
)

const bridgeTestOwner = "EXAMPLE-BRIDGE-MANAGEMENT-CAPABILITY-ONLY"

func bridgeFixture(t *testing.T) (*Application, *fakeConnectedSession) {
	t.Helper()
	m := session.NewManager()
	s := newFakeConnectedSession("bridge-serial")
	s.Connect(context.Background())
	if err := m.RegisterWithMetadata(s, session.SessionMetadata{Transport: "serial", Endpoint: "TEST-NO-HARDWARE"}); err != nil {
		t.Fatal(err)
	}
	a, err := New(Dependencies{Manager: m})
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		for _, r := range a.bridges.runs {
			_, _ = a.CloseRSPBridge(r.id, bridgeTestOwner)
		}
		m.Close()
	})
	return a, s
}

func bridgeRequest() BridgeStartRequest {
	return BridgeStartRequest{SessionID: "bridge-serial", Owner: bridgeTestOwner, RequestID: "start", AcknowledgeUnverifiedTarget: true}
}

func waitBridgeState(t *testing.T, a *Application, id, want string) BridgeStatus {
	t.Helper()
	deadline := time.Now().Add(3 * time.Second)
	for {
		status, err := a.RSPBridgeStatus(id, bridgeTestOwner)
		if err != nil {
			t.Fatal(err)
		}
		if status.State == want {
			return status
		}
		if time.Now().After(deadline) {
			t.Fatalf("want %s, got %+v", want, status)
		}
		time.Sleep(time.Millisecond)
	}
}

func TestRSPBridgeReadinessLeaseAndBinaryRelay(t *testing.T) {
	a, s := bridgeFixture(t)
	q := bridgeRequest()
	q.AcknowledgeUnverifiedTarget = false
	if _, err := a.StartRSPBridge(context.Background(), q); err == nil || !strings.Contains(err.Error(), "unverified") {
		t.Fatalf("readiness: %v", err)
	}
	if _, active, _ := a.LeaseStatus(s.ID()); active {
		t.Fatal("unconfirmed target acquired lease")
	}
	q.AcknowledgeUnverifiedTarget = true
	s.emitOutput([]byte("old shell history"))
	result, err := a.StartRSPBridge(context.Background(), q)
	if err != nil {
		t.Fatal(err)
	}
	if result.Status.TargetState != "unverified" || result.Status.Warning == "" {
		t.Fatal(result.Status)
	}
	if len(s.writtenData()) != 0 {
		t.Fatal("start sent target commands")
	}
	if _, err = a.WriteSession(context.Background(), s.ID(), session.WriteRequest{Actor: session.ActorUser, Data: []byte("bad")}); !errors.Is(err, ErrSessionBusy) {
		t.Fatalf("lease bypass: %v", err)
	}
	if err := a.ReleaseLease(s.ID(), a.bridges.runs[result.Status.BridgeID].owner); err == nil {
		t.Fatal("managed lease released")
	}
	client, err := debugger.DialBridge(context.Background(), *result.Connection)
	if err != nil {
		t.Fatal(err)
	}
	defer client.Close()
	waitBridgeState(t, a, result.Status.BridgeID, "connected")
	payload := []byte{0, 0xff, '$', '}', '#', '+', 3}
	s.emitOutput(payload)
	client.SetReadDeadline(time.Now().Add(time.Second))
	got := make([]byte, len(payload))
	if _, err := io.ReadFull(client, got); err != nil || !bytes.Equal(payload, got) {
		t.Fatalf("relay replayed/changed bytes: %x %v", got, err)
	}
	if _, err := client.Write(payload); err != nil {
		t.Fatal(err)
	}
	deadline := time.Now().Add(time.Second)
	for !bytes.Equal(s.writtenData(), payload) {
		if time.Now().After(deadline) {
			t.Fatalf("request changed: %x", s.writtenData())
		}
		time.Sleep(time.Millisecond)
	}
	client.Close()
	status := waitBridgeState(t, a, result.Status.BridgeID, "disconnected")
	if status.TargetState != "unverified" {
		t.Fatal("disconnect claimed target recovery")
	}
	if _, active, _ := a.LeaseStatus(s.ID()); !active {
		t.Fatal("disconnect released lease")
	}
	if _, err := a.RestoreRSPBridge(status.BridgeID, bridgeTestOwner, "saved", false); err == nil {
		t.Fatal("unconfirmed shell probed")
	}
	if !bytes.Equal(s.writtenData(), payload) {
		t.Fatal("disconnect/failed restore injected commands")
	}
	status, err = a.CloseRSPBridge(status.BridgeID, bridgeTestOwner)
	if err != nil || status.State != "closed" || s.State() != session.StateClosed {
		t.Fatalf("close: %+v %v", status, err)
	}
	if _, err = a.CloseRSPBridge(status.BridgeID, bridgeTestOwner); err != nil {
		t.Fatal(err)
	}
	assertBridgePortReleased(t, status.Address)
}

func assertBridgePortReleased(t *testing.T, address string) {
	t.Helper()
	listener, err := net.Listen("tcp", address)
	if err != nil {
		t.Fatalf("bridge port retained after finalization: %v", err)
	}
	listener.Close()
}

func TestRSPBridgeAuthorizationRetryAndExpiry(t *testing.T) {
	a, s := bridgeFixture(t)
	q := bridgeRequest()
	first, err := a.StartRSPBridge(context.Background(), q)
	if err != nil {
		t.Fatal(err)
	}
	again, err := a.StartRSPBridge(context.Background(), q)
	if err != nil || first.Status.BridgeID != again.Status.BridgeID || *first.Connection != *again.Connection {
		t.Fatalf("retry changed operation: %v", err)
	}
	q.AllowedPeer = "127.0.0.1"
	if _, err = a.StartRSPBridge(context.Background(), q); err == nil {
		t.Fatal("conflicting retry accepted")
	}
	q.RequestID = "other"
	if _, err = a.StartRSPBridge(context.Background(), q); !errors.Is(err, ErrSessionBusy) {
		t.Fatalf("second bridge: %v", err)
	}
	id := first.Status.BridgeID
	if _, err := a.CloseRSPBridge(id, "wrong"); err == nil {
		t.Fatal("wrong close owner")
	}
	if _, err := a.RSPBridgeStatus(id, "wrong"); err == nil {
		t.Fatal("wrong read owner")
	}
	if _, err := a.RenewRSPBridge(id, "wrong"); err == nil {
		t.Fatal("wrong renew owner")
	}
	renewed, err := a.RenewRSPBridge(id, bridgeTestOwner)
	if err != nil || !renewed.ExpiresAt.After(first.Status.ExpiresAt) {
		t.Fatalf("renew: %v", err)
	}
	statusJSON, _ := json.Marshal(renewed)
	events, _ := s.ReadRecentEvents(100)
	eventJSON, _ := json.Marshal(events)
	for _, secret := range []string{bridgeTestOwner, first.Connection.Token, a.bridges.runs[id].owner} {
		if strings.Contains(string(statusJSON), secret) || strings.Contains(string(eventJSON), secret) {
			t.Fatal("secret leaked in status/events")
		}
	}
	r := a.bridges.runs[id]
	r.mu.Lock()
	r.status.ExpiresAt = time.Now().Add(-time.Second)
	r.mu.Unlock()
	if _, err := a.RenewRSPBridge(id, bridgeTestOwner); err == nil {
		t.Fatal("expired heartbeat resurrected")
	}
	waitBridgeState(t, a, id, "failed")
	if s.State() != session.StateClosed {
		t.Fatal("expiry left serial open")
	}
}

func TestRSPBridgeVerifiedRestorationAndEchoFailure(t *testing.T) {
	for _, verified := range []bool{true, false} {
		t.Run(map[bool]string{true: "verified", false: "echo-is-not-proof"}[verified], func(t *testing.T) {
			a, s := bridgeFixture(t)
			pattern := regexp.MustCompile(`'([0-9a-f]{12})''([0-9a-f]{12})'`)
			s.onWrite = func(q session.WriteRequest) {
				if verified {
					parts := pattern.FindSubmatch(q.Data)
					if len(parts) == 3 {
						s.emitOutput([]byte("\x1e\x1f" + string(parts[1]) + string(parts[2]) + "\x1f\x1e"))
					}
				} else {
					s.emitOutput(q.Data)
				}
			}
			result, err := a.StartRSPBridge(context.Background(), bridgeRequest())
			if err != nil {
				t.Fatal(err)
			}
			client, err := debugger.DialBridge(context.Background(), *result.Connection)
			if err != nil {
				t.Fatal(err)
			}
			waitBridgeState(t, a, result.Status.BridgeID, "connected")
			if _, err = a.RestoreRSPBridge(result.Status.BridgeID, bridgeTestOwner, "1:2:3", true); err == nil {
				t.Fatal("restored while external client active")
			}
			client.Close()
			waitBridgeState(t, a, result.Status.BridgeID, "disconnected")
			status, err := a.RestoreRSPBridge(result.Status.BridgeID, bridgeTestOwner, "1:2:3", true)
			if verified {
				if err != nil || status.State != "restored" || status.TargetState != "shell_verified" {
					t.Fatalf("restore: %+v %v", status, err)
				}
				if _, err := a.WriteSession(context.Background(), s.ID(), session.WriteRequest{Actor: session.ActorUser, Data: []byte("safe\n")}); err != nil {
					t.Fatal(err)
				}
				if _, err := a.RestoreRSPBridge(status.BridgeID, bridgeTestOwner, "1:2:3", true); err != nil {
					t.Fatal(err)
				}
			} else if err == nil || status.State != "failed" || s.State() != session.StateClosed {
				t.Fatalf("echo falsely recovered: %+v %v", status, err)
			}
			assertBridgePortReleased(t, status.Address)
		})
	}
}

func TestRSPBridgeSerialLossAndPreflightFailure(t *testing.T) {
	a, s := bridgeFixture(t)
	q := bridgeRequest()
	q.ListenAddress = "0.0.0.0:0"
	if _, err := a.StartRSPBridge(context.Background(), q); err == nil {
		t.Fatal("unsafe listener")
	}
	if _, active, _ := a.LeaseStatus(s.ID()); active {
		t.Fatal("failed setup retained lease")
	}
	s.Write(session.WriteRequest{Actor: session.ActorUser, Data: []byte("pending")})
	if _, err := a.StartRSPBridge(context.Background(), bridgeRequest()); !errors.Is(err, ErrTerminalCommandInputPending) {
		t.Fatalf("pending input: %v", err)
	}
	s.Write(session.WriteRequest{Actor: session.ActorUser, Data: []byte("\n")})
	result, err := a.StartRSPBridge(context.Background(), bridgeRequest())
	if err != nil {
		t.Fatal(err)
	}
	s.Close()
	waitBridgeState(t, a, result.Status.BridgeID, "failed")
}

func TestRSPBridgeCloseInterruptsBlockedSerialWrite(t *testing.T) {
	m := session.NewManager()
	defer m.Close()
	s := newBlockingWriteSession("bridge-serial")
	s.Connect(context.Background())
	if err := m.RegisterWithMetadata(s, session.SessionMetadata{Transport: "serial"}); err != nil {
		t.Fatal(err)
	}
	a, err := New(Dependencies{Manager: m})
	if err != nil {
		t.Fatal(err)
	}
	result, err := a.StartRSPBridge(context.Background(), bridgeRequest())
	if err != nil {
		t.Fatal(err)
	}
	defer a.CloseRSPBridge(result.Status.BridgeID, bridgeTestOwner)
	client, err := debugger.DialBridge(context.Background(), *result.Connection)
	if err != nil {
		t.Fatal(err)
	}
	defer client.Close()
	client.Write([]byte("blocked"))
	select {
	case <-s.writeStarted:
	case <-time.After(time.Second):
		t.Fatal("write did not start")
	}
	done := make(chan error, 1)
	go func() { _, err := a.CloseRSPBridge(result.Status.BridgeID, bridgeTestOwner); done <- err }()
	select {
	case err := <-done:
		if err != nil {
			t.Fatal(err)
		}
	case <-time.After(time.Second):
		t.Fatal("close did not release blocked relay")
	}
}

type lostBridgeSession struct{ *fakeConnectedSession }

func (s lostBridgeSession) ReadOutput(context.Context, session.OutputCursor, int) (session.OutputChunk, error) {
	return session.OutputChunk{Dropped: true}, nil
}

func TestRSPBridgeLostBytesFailClosed(t *testing.T) {
	m := session.NewManager()
	defer m.Close()
	s := lostBridgeSession{newFakeConnectedSession("bridge-serial")}
	s.Connect(context.Background())
	if err := m.RegisterWithMetadata(s, session.SessionMetadata{Transport: "serial"}); err != nil {
		t.Fatal(err)
	}
	a, err := New(Dependencies{Manager: m})
	if err != nil {
		t.Fatal(err)
	}
	result, err := a.StartRSPBridge(context.Background(), bridgeRequest())
	if err != nil {
		t.Fatal(err)
	}
	defer a.CloseRSPBridge(result.Status.BridgeID, bridgeTestOwner)
	client, err := debugger.DialBridge(context.Background(), *result.Connection)
	if err != nil {
		t.Fatal(err)
	}
	defer client.Close()
	status := waitBridgeState(t, a, result.Status.BridgeID, "failed")
	if !strings.Contains(status.Error, "data lost") || s.State() != session.StateClosed {
		t.Fatalf("data loss: %+v", status)
	}
}
