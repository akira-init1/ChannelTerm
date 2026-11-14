package app

import (
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/akira-init1/ChannelTerm/internal/core/session"
)

func TestTransferSummaryPublicProjectionDoesNotGrantPrivateStatus(t *testing.T) {
	root := t.TempDir()
	mustWriteDirectoryTestFile(t, filepath.Join(root, "secret-source.txt"), []byte("test"))
	a := managedTestApplication(t, root, managedFileServer(t, 1<<30, false))
	creator := WithClientIdentity(context.Background(), session.ClientIdentity{ID: "CL-agent", Name: "Agent", Kind: "agent", InstanceID: "CI-origin", OperationID: "CU-origin", Entry: "mcp", DeclaredEntry: "cli"})
	// Direct composition uses a real registered instance, as adapters do.
	_, proof := registerInstance(t, a, creator)
	creator, err := a.BindClientInstance(WithInstanceProof(creator, proof), "mcp")
	if err != nil {
		t.Fatal(err)
	}
	status := managedWait(t, a, creator, TransferRequest{RequestID: "source", Direction: "send", Transport: "sftp", SSHProfile: "board", Sources: []string{"secret-source.txt"}, Destination: "/private-destination", Mode: "files"})
	reader := WithClientAccess(WithClientIdentity(context.Background(), session.ClientIdentity{ID: "CL-reader", Kind: "agent"}), ClientAccess{})
	summaries, err := a.ListFileTransfers(reader)
	if err != nil || len(summaries) != 1 || summaries[0].Client != status.Client || summaries[0].ID != status.ID {
		t.Fatalf("summary: %+v %v", summaries, err)
	}
	data, _ := json.Marshal(summaries)
	for _, secret := range []string{"secret-source", "private-destination", root, "ssh_profile", proof.Secret} {
		if strings.Contains(string(data), secret) {
			t.Fatalf("leaked %q", secret)
		}
	}
	if _, err = a.FileTransferStatus(reader, status.ID, false, 0); err == nil {
		t.Fatal("public listing granted private status")
	}
	if _, err = a.FileTransferStatus(reader, status.ID, true, 0); err == nil {
		t.Fatal("public listing granted cancellation")
	}
	summaries[0].Client.Name = "mutated"
	again, _ := a.ListFileTransfers(reader)
	if again[0].Client.Name == "mutated" {
		t.Fatal("snapshot aliases state")
	}
}

func TestTransferReporterKeepsLeaseInitiatorAcrossRequests(t *testing.T) {
	a, ctx, _ := instanceFixture(t)
	origin := ClientIdentityFromContext(ctx)
	origin.InstanceID = "CI-original"
	origin.OperationID = "CU-original"
	ctx = WithClientIdentity(ctx, origin)
	lease, err := a.AcquireLeaseForClient(ctx, "board", "owner", LeaseTypeFileTransfer)
	if err != nil {
		t.Fatal(err)
	}
	defer a.ReleaseLease("board", "owner")
	later := origin
	later.InstanceID = "CI-later"
	later.OperationID = "CU-later"
	err = a.ReportFileTransferEventForClient(WithClientIdentity(ctx, later), "board", "owner", lease.TransferID, session.EventFileTransferProgress, "user", map[string]any{"sent": int64(1)})
	if err != nil {
		t.Fatal(err)
	}
	events, err := a.ReadSessionEvents(ctx, "board", nil, 20)
	if err != nil || events.Events[len(events.Events)-1].Client != origin {
		t.Fatalf("origin replaced: %+v %v", events, err)
	}
	metadata := map[string]any{"checkpoint_id": strings.Repeat("a", 24), "checkpoint_origin": map[string]any{"client_id": "CL-earlier", "kind": "user", "secret": "private-secret"}, "resumed": true}
	if err = a.ReportFileTransferEventForClient(WithClientIdentity(ctx, later), "board", "owner", lease.TransferID, session.EventFileTransferProgress, "user", metadata); err != nil {
		t.Fatal(err)
	}
	events, _ = a.ReadSessionEvents(ctx, "board", nil, 20)
	last := events.Events[len(events.Events)-1]
	if last.Client != origin {
		t.Fatal("checkpoint report replaced current initiator")
	}
	encoded, _ := json.Marshal(last)
	if strings.Contains(string(encoded), "private-secret") || !strings.Contains(string(encoded), "CL-earlier") {
		t.Fatal("unsafe checkpoint projection")
	}
}

func TestCheckpointOriginSurvivesReloadAndRejectsUnboundedClaims(t *testing.T) {
	root := filepath.Join(t.TempDir(), "jobs")
	source := filepath.Join(t.TempDir(), "source")
	if err := os.WriteFile(source, []byte("content"), 0600); err != nil {
		t.Fatal(err)
	}
	origin := session.ClientIdentity{ID: "CL-agent", Name: "Original", Kind: "agent", InstanceID: "CI-original", OperationID: "CU-original"}
	ctx := WithClientIdentity(context.Background(), origin)
	c, err := CreateTransferCheckpoint(ctx, root, "send", source, "/tmp/output", "test-peer", FileTransferWireBase64)
	if err != nil {
		t.Fatal(err)
	}
	saved, err := ReadTransferCheckpoint(root, c.ID)
	if err != nil || saved.Origin != origin {
		t.Fatalf("origin lost: %+v %v", saved, err)
	}
	saved.Origin.Name = strings.Repeat("x", 129)
	if err = saveTransferCheckpoint(filepath.Join(root, c.ID), saved); err != nil {
		t.Fatal(err)
	}
	if _, err = ReadTransferCheckpoint(root, c.ID); err == nil {
		t.Fatal("oversize local claim accepted")
	}
	saved.Origin = session.ClientIdentity{}
	if err = saveTransferCheckpoint(filepath.Join(root, c.ID), saved); err != nil {
		t.Fatal(err)
	}
	if old, err := ReadTransferCheckpoint(root, c.ID); err != nil || old.Origin != (session.ClientIdentity{}) {
		t.Fatal("legacy unknown origin", err)
	}
}

func TestCheckpointEventMetadataRejectsMalformedClaims(t *testing.T) {
	for name, metadata := range map[string]map[string]any{
		"invalid ID":         {"checkpoint_id": "bad"},
		"invalid resumed":    {"resumed": "true"},
		"missing checkpoint": {"checkpoint_origin": session.ClientIdentity{}},
		"wrong origin type":  {"checkpoint_id": strings.Repeat("a", 24), "checkpoint_origin": "bad"},
		"unbounded origin":   {"checkpoint_id": strings.Repeat("a", 24), "checkpoint_origin": map[string]any{"name": strings.Repeat("x", 129)}},
		"unsafe label":       {"checkpoint_id": strings.Repeat("a", 24), "checkpoint_origin": map[string]any{"name": "bad\nlabel"}},
	} {
		t.Run(name, func(t *testing.T) {
			if err := normalizeCheckpointEventMetadata(metadata); err == nil {
				t.Fatal("malformed checkpoint claim accepted")
			}
		})
	}
}
