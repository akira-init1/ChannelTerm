package app

import (
	"context"
	"errors"
	"strings"
	"testing"

	"github.com/akira-init1/ChannelTerm/internal/core/session"
)

func TestApplicationLeaseBlocksOtherWritersAndPreservesOtherSessions(t *testing.T) {
	manager := session.NewManager()
	first := newFakeConnectedSession("first")
	second := newFakeConnectedSession("second")
	if err := first.Connect(context.Background()); err != nil {
		t.Fatal(err)
	}
	if err := second.Connect(context.Background()); err != nil {
		t.Fatal(err)
	}
	if err := manager.RegisterWithMetadata(first, session.SessionMetadata{Transport: "serial", Endpoint: "COM1"}); err != nil {
		t.Fatal(err)
	}
	if err := manager.RegisterWithMetadata(second, session.SessionMetadata{Transport: "serial", Endpoint: "COM2"}); err != nil {
		t.Fatal(err)
	}
	application, err := New(Dependencies{Manager: manager})
	if err != nil {
		t.Fatal(err)
	}

	lease, err := application.AcquireLease("SER-1", "file-transfer-owner", LeaseTypeFileTransfer)
	if err != nil {
		t.Fatalf("AcquireLease() error = %v", err)
	}
	if lease.SessionID != "first" || lease.Type != LeaseTypeFileTransfer || lease.State != "active" || lease.CreatedAt.IsZero() {
		t.Errorf("AcquireLease() = %#v, want active lease for first", lease)
	}
	if _, err := application.WriteSession(context.Background(), "SER-1", session.WriteRequest{Actor: session.ActorUser, Data: []byte("blocked")}); !errors.Is(err, ErrSessionBusy) || !strings.Contains(err.Error(), "Session SER-1 is locked by file-transfer") {
		t.Errorf("WriteSession() error = %v, want friendly file-transfer busy error", err)
	}
	if got := string(first.writtenData()); got != "" {
		t.Errorf("blocked Session bytes = %q, want none", got)
	}
	if _, err := application.WriteSessionWithLease(context.Background(), "SER-1", "file-transfer-owner", session.WriteRequest{Actor: session.ActorUser, Data: []byte("transfer")}); err != nil {
		t.Fatalf("WriteSessionWithLease() error = %v", err)
	}
	if _, err := application.WriteSession(context.Background(), "SER-2", session.WriteRequest{Actor: session.ActorAgent, Data: []byte("other")}); err != nil {
		t.Fatalf("WriteSession(other Session) error = %v", err)
	}
	if got := string(second.writtenData()); got != "other" {
		t.Errorf("other Session bytes = %q, want other", got)
	}
	if err := application.ReleaseLease("SER-1", "wrong-owner"); !errors.Is(err, ErrLeaseNotOwned) {
		t.Errorf("ReleaseLease(wrong owner) error = %v, want ErrLeaseNotOwned", err)
	}
	if err := application.ReleaseLease("SER-1", "file-transfer-owner"); err != nil {
		t.Fatalf("ReleaseLease() error = %v", err)
	}
	events, err := application.ReadSessionEvents(context.Background(), "SER-1", nil, 8)
	if err != nil {
		t.Fatalf("ReadSessionEvents() error = %v", err)
	}
	if len(events.Events) < 3 || events.Events[len(events.Events)-2].Type != session.EventLeaseAcquired || events.Events[len(events.Events)-1].Type != session.EventLeaseReleased {
		t.Errorf("lease events = %+v, want acquired then released", events.Events)
	}
	if events.Events[len(events.Events)-2].Metadata["type"] != string(LeaseTypeFileTransfer) || events.Events[len(events.Events)-1].Metadata["state"] != "released" {
		t.Errorf("lease event metadata = %+v", events.Events[len(events.Events)-2:])
	}
	if _, active, err := application.LeaseStatus("SER-1"); err != nil || active {
		t.Errorf("LeaseStatus() = active:%t err:%v, want inactive nil", active, err)
	}
	if _, err := application.WriteSession(context.Background(), "SER-1", session.WriteRequest{Actor: session.ActorUser, Data: []byte("restored")}); err != nil {
		t.Fatalf("WriteSession(after release) error = %v", err)
	}
	if got := string(first.writtenData()); got != "transferrestored" {
		t.Errorf("first Session bytes = %q, want transferrestored", got)
	}
}
