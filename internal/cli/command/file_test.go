package command

import (
	"context"
	"errors"
	"io"
	"os"
	"path/filepath"
	"testing"

	"github.com/akira-init1/ChannelTerm/internal/core/session"
)

func TestWithFileTransferLeaseReleasesAfterFailure(t *testing.T) {
	attached := &leaseTrackingAttachSession{}
	want := errors.New("transfer failed")
	err := withFileTransferLease(context.Background(), attached, "SER-1", func() error { return want })
	if !errors.Is(err, want) {
		t.Fatalf("withFileTransferLease() error = %v, want transfer failure", err)
	}
	if attached.acquires != 1 || attached.releases != 1 {
		t.Errorf("lease acquire/release = %d/%d, want 1/1", attached.acquires, attached.releases)
	}
}

type leaseTrackingAttachSession struct {
	fakeAttachSession
	acquires int
	releases int
}

func TestFileTransferProgressPublishesStructuredProgress(t *testing.T) {
	attached := &eventReportingAttachSession{}
	progress := fileTransferProgress(context.Background(), io.Discard, attached, map[string]any{"direction": "send", "remote_path": "/tmp/firmware.bin"})
	if err := progress(622592, 1048576); err != nil {
		t.Fatalf("progress() error = %v", err)
	}
	if len(attached.events) != 1 {
		t.Fatalf("events = %d, want 1", len(attached.events))
	}
	event := attached.events[0]
	if event.typ != session.EventFileTransferProgress || event.metadata["sent"] != int64(622592) || event.metadata["total"] != int64(1048576) || event.metadata["percent"] != float64(59.375) {
		t.Errorf("progress event = %+v, want structured confirmed transfer progress", event)
	}
	if _, ok := event.metadata["speed"].(float64); !ok {
		t.Errorf("progress speed = %#v, want float64", event.metadata["speed"])
	}
}

type reportedFileTransferEvent struct {
	typ      session.EventType
	metadata map[string]any
}

type eventReportingAttachSession struct {
	fakeAttachSession
	events []reportedFileTransferEvent
}

func (s *eventReportingAttachSession) ReportFileTransferEvent(_ context.Context, typ session.EventType, metadata map[string]any) error {
	s.events = append(s.events, reportedFileTransferEvent{typ: typ, metadata: metadata})
	return nil
}

func (s *leaseTrackingAttachSession) AcquireFileTransferLease(context.Context) error {
	s.acquires++
	return nil
}

func (s *leaseTrackingAttachSession) ReleaseFileTransferLease(context.Context) error {
	s.releases++
	return nil
}

// TestAttachFileSessionSelectsOnlyOpenSession verifies the example command can
// omit --session without selecting a closed Session.
func TestAttachFileSessionSelectsOnlyOpenSession(t *testing.T) {
	wantClient := &fakeAttachSession{}
	attached, identifier, err := attachFileSession(context.Background(), fileOptions{endpoint: "test-endpoint"}, fileCommandDependencies{
		listSessions: func(_ context.Context, endpoint string) ([]mcpListedSession, error) {
			if endpoint != "test-endpoint" {
				t.Errorf("list endpoint = %q, want test-endpoint", endpoint)
			}
			return []mcpListedSession{{ID: "closed-id", Reference: "SER-1", State: "closed"}, {ID: "open-id", Reference: "SER-2", State: "open"}}, nil
		},
		newAttach: func(_ context.Context, endpoint, id string) (attachSession, error) {
			if endpoint != "test-endpoint" || id != "SER-2" {
				t.Errorf("attach target = %q/%q, want test-endpoint/SER-2", endpoint, id)
			}
			return wantClient, nil
		},
	})
	if err != nil {
		t.Fatalf("attachFileSession() error = %v", err)
	}
	if attached != wantClient || identifier != "SER-2" {
		t.Errorf("attachFileSession() = %T/%q, want selected client/SER-2", attached, identifier)
	}
}

// TestAttachFileSessionRequiresExplicitSelectionWhenAmbiguous prevents a file
// from being sent to an arbitrary board when several Sessions are open.
func TestAttachFileSessionRequiresExplicitSelectionWhenAmbiguous(t *testing.T) {
	_, _, err := attachFileSession(context.Background(), fileOptions{endpoint: "test-endpoint"}, fileCommandDependencies{
		listSessions: func(context.Context, string) ([]mcpListedSession, error) {
			return []mcpListedSession{{Reference: "SER-1", State: "open"}, {Reference: "SER-2", State: "open"}}, nil
		},
		newAttach: func(context.Context, string, string) (attachSession, error) {
			t.Fatal("newAttach called for ambiguous selection")
			return nil, nil
		},
	})
	if !errors.Is(err, ErrFileSessionAmbiguous) {
		t.Fatalf("attachFileSession() error = %v, want ErrFileSessionAmbiguous", err)
	}
}

// TestReplaceReceivedFileReplacesExistingContent verifies the cross-platform
// backup-and-move path installs verified content and cleans its backup.
func TestReplaceReceivedFileReplacesExistingContent(t *testing.T) {
	directory := t.TempDir()
	destination := filepath.Join(directory, "log.txt")
	temporary := filepath.Join(directory, ".channelterm-receive-test")
	if err := os.WriteFile(destination, []byte("old"), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(temporary, []byte("verified new"), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := replaceReceivedFile(temporary, destination); err != nil {
		t.Fatalf("replaceReceivedFile() error = %v", err)
	}
	content, err := os.ReadFile(destination)
	if err != nil {
		t.Fatal(err)
	}
	if got := string(content); got != "verified new" {
		t.Errorf("destination content = %q, want verified new", got)
	}
	if _, err := os.Stat(temporary + ".previous"); !errors.Is(err, os.ErrNotExist) {
		t.Errorf("backup still exists or stat failed: %v", err)
	}
}
