package app

import (
	"context"
	"errors"
	"strings"
	"testing"
	"time"

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
	for _, event := range events.Events[len(events.Events)-2:] {
		if _, ok := event.Metadata["output_cursor"].(uint64); !ok {
			t.Errorf("lease event metadata = %+v, want output_cursor uint64", event.Metadata)
		}
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

func TestFileTransferCancellationConfirmationPausesResumesAndCancels(t *testing.T) {
	manager := session.NewManager()
	terminal := newFakeConnectedSession("first")
	if err := terminal.Connect(context.Background()); err != nil {
		t.Fatal(err)
	}
	if err := manager.RegisterWithMetadata(terminal, session.SessionMetadata{Transport: "serial", Endpoint: "COM1"}); err != nil {
		t.Fatal(err)
	}
	application, err := New(Dependencies{Manager: manager})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := application.AcquireLease("SER-1", "transfer-owner", LeaseTypeFileTransfer); err != nil {
		t.Fatal(err)
	}
	if err := application.ReportFileTransferEvent("SER-1", session.EventFileTransferProgress, "user", map[string]any{"sent": int64(24576), "total": int64(65536), "percent": 37.5}); err != nil {
		t.Fatal(err)
	}

	request, err := application.BeginFileTransferCancel("SER-1")
	if err != nil || !request.Active || request.RequestID == "" || request.State != "confirming" {
		t.Fatalf("BeginFileTransferCancel() = %#v, %v", request, err)
	}
	checkpoint := make(chan FileTransferControlAction, 1)
	go func() {
		action, checkpointErr := application.FileTransferCheckpoint(context.Background(), "SER-1", "transfer-owner")
		if checkpointErr != nil {
			checkpoint <- FileTransferControlAction(checkpointErr.Error())
			return
		}
		checkpoint <- action
	}()
	select {
	case action := <-checkpoint:
		t.Fatalf("checkpoint returned %q while confirmation was pending", action)
	case <-time.After(20 * time.Millisecond):
	}
	resumed, err := application.ResolveFileTransferCancel(context.Background(), "SER-1", request.RequestID, false)
	if err != nil || resumed.State != "resumed" {
		t.Fatalf("ResolveFileTransferCancel(false) = %#v, %v", resumed, err)
	}
	if action := <-checkpoint; action != FileTransferContinue {
		t.Fatalf("checkpoint action = %q, want continue", action)
	}

	request, err = application.BeginFileTransferCancel("SER-1")
	if err != nil {
		t.Fatal(err)
	}
	type resolutionResult struct {
		resolution FileTransferCancelResolution
		err        error
	}
	resolved := make(chan resolutionResult, 1)
	go func() {
		resolution, resolveErr := application.ResolveFileTransferCancel(context.Background(), "SER-1", request.RequestID, true)
		resolved <- resolutionResult{resolution: resolution, err: resolveErr}
	}()
	action, err := application.FileTransferCheckpoint(context.Background(), "SER-1", "transfer-owner")
	if err != nil || action != FileTransferCancel {
		t.Fatalf("FileTransferCheckpoint() = %q, %v, want cancel", action, err)
	}
	if err := application.ReportFileTransferEvent("SER-1", session.EventFileTransferFailed, "user", map[string]any{"error": "user_cancelled", "sent": int64(24576), "total": int64(65536), "percent": 37.5}); err != nil {
		t.Fatal(err)
	}
	if err := application.ReleaseLease("SER-1", "transfer-owner"); err != nil {
		t.Fatal(err)
	}
	result := <-resolved
	if result.err != nil || result.resolution.State != "cancelled" || result.resolution.Transferred != 24576 || result.resolution.Total != 65536 || result.resolution.Percent != 37.5 {
		t.Fatalf("ResolveFileTransferCancel(true) = %#v, %v", result.resolution, result.err)
	}
	events, err := application.ReadSessionEvents(context.Background(), "SER-1", nil, 16)
	if err != nil {
		t.Fatal(err)
	}
	if got := events.Events[len(events.Events)-1]; got.Type != session.EventFileTransferCancelled || got.Metadata["reason"] != "user_cancelled" || got.Metadata["lease_released"] != true {
		t.Fatalf("last event = %#v, want released cancellation status", got)
	}
}
