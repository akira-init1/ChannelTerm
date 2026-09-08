package command

import (
	"bytes"
	"context"
	"errors"
	"io"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/akira-init1/ChannelTerm/internal/cli/interactive"
	"github.com/akira-init1/ChannelTerm/internal/core/session"
)

func TestNextAvailableLocalPathPreservesExtensionsAndAvoidsOverwrite(t *testing.T) {
	directory := t.TempDir()
	tests := []struct {
		name string
		want string
	}{
		{name: "binary", want: "firmware_3.bin"},
		{name: "log", want: "app_3.log"},
		{name: "no extension", want: "README_3"},
		{name: "dotfile", want: ".env_3"},
		{name: "compound extension", want: "backup_3.tar.gz"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			base := strings.Replace(tt.want, "_3", "", 1)
			for _, name := range []string{base, strings.Replace(tt.want, "_3", "_1", 1), strings.Replace(tt.want, "_3", "_2", 1)} {
				if err := os.WriteFile(filepath.Join(directory, name), []byte(name), 0o600); err != nil {
					t.Fatal(err)
				}
			}
			got, err := nextAvailableLocalPath(filepath.Join(directory, base))
			if err != nil {
				t.Fatal(err)
			}
			if got != filepath.Join(directory, tt.want) {
				t.Errorf("nextAvailableLocalPath() = %q, want %q", got, filepath.Join(directory, tt.want))
			}
		})
	}
}

func TestAttachFileShortcutMenuEscStaysLocal(t *testing.T) {
	pump := newAttachInputPump(bytes.NewReader([]byte{0x1b}))
	var output bytes.Buffer
	if ok := runAttachFileShortcut(context.Background(), &pump, &fakeAttachSession{}, func(data []byte) error {
		_, err := output.Write(data)
		return err
	}); !ok {
		t.Fatal("runAttachFileShortcut() returned false")
	}
	if got := output.String(); !strings.Contains(got, "File transfer:") || !strings.Contains(got, "File transfer cancelled") {
		t.Errorf("shortcut output = %q, want menu and local cancellation", got)
	}
}

func TestAttachFileShortcutSelectsSendAndReceive(t *testing.T) {
	tests := []struct {
		name       string
		input      string
		wantPrompt string
	}{
		{name: "send", input: "s\n\n", wantPrompt: "Local file:"},
		{name: "receive", input: "r\n\n", wantPrompt: "Remote file:"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			pump := newAttachInputPump(strings.NewReader(tt.input))
			var output bytes.Buffer
			if ok := runAttachFileShortcut(context.Background(), &pump, &fakeAttachSession{}, func(data []byte) error {
				_, err := output.Write(data)
				return err
			}); !ok {
				t.Fatal("runAttachFileShortcut() returned false")
			}
			if got := output.String(); !strings.Contains(got, tt.wantPrompt) || !strings.Contains(got, "File transfer cancelled") {
				t.Errorf("shortcut output = %q, want %q and cancellation", got, tt.wantPrompt)
			}
		})
	}
}

func TestAttachFileShortcutDefaultPathsAndCustomPaths(t *testing.T) {
	if got := defaultRemoteTransferPath(filepath.Join("build", "firmware.bin")); got != "/tmp/firmware.bin" {
		t.Errorf("default remote path = %q, want /tmp/firmware.bin", got)
	}
	if got := defaultLocalTransferPath("/tmp/crash.log"); got != "."+string(filepath.Separator)+"crash.log" {
		t.Errorf("default local path = %q", got)
	}
	if got := "/opt/app/firmware.bin"; got == defaultRemoteTransferPath(filepath.Join("build", "firmware.bin")) {
		t.Fatal("custom remote path unexpectedly changed")
	}
}

func TestAttachShortcutControlCCancelsTransferAndRestoresInput(t *testing.T) {
	localPath := filepath.Join(t.TempDir(), "firmware.bin")
	if err := os.WriteFile(localPath, []byte("payload"), 0o600); err != nil {
		t.Fatal(err)
	}
	inputReader, inputWriter := io.Pipe()
	defer inputReader.Close()
	defer inputWriter.Close()
	pump := newAttachInputPump(inputReader)
	attached := &cancellableShortcutSession{writeStarted: make(chan struct{})}
	var output bytes.Buffer
	done := make(chan bool, 1)
	go func() {
		done <- runAttachShortcutTransfer(context.Background(), &pump, attached, func(data []byte) error {
			_, err := output.Write(data)
			return err
		}, "send", localPath, "/tmp/firmware.bin")
	}()
	select {
	case <-attached.writeStarted:
	case <-time.After(time.Second):
		t.Fatal("transfer did not start")
	}
	if _, err := inputWriter.Write([]byte{0x03}); err != nil {
		t.Fatal(err)
	}
	select {
	case ok := <-done:
		if !ok {
			t.Fatal("shortcut did not return to attach input")
		}
	case <-time.After(time.Second):
		t.Fatal("Ctrl+C did not cancel transfer")
	}
	if attached.acquires != 1 || attached.releases != 1 {
		t.Errorf("lease acquire/release = %d/%d, want 1/1", attached.acquires, attached.releases)
	}
	if !strings.Contains(output.String(), "File transfer cancelled") {
		t.Errorf("output = %q, want transfer cancellation", output.String())
	}
	controller := interactive.NewController(interactive.DefaultEscapeByte)
	if ok := processAttachInput(context.Background(), controller, []byte("echo ok\r"), &pump, attached, func([]byte) error { return nil }, func() error { return nil }, func() {}); !ok {
		t.Fatal("normal attach input did not resume")
	}
	if !strings.HasSuffix(string(attached.writtenData()), "echo ok\r") {
		t.Errorf("normal input after cancellation = %q", attached.writtenData())
	}
}

type cancellableShortcutSession struct {
	fakeAttachSession
	mu           sync.Mutex
	acquires     int
	releases     int
	writeStarted chan struct{}
	once         sync.Once
}

func (s *cancellableShortcutSession) Write(request session.WriteRequest) (int, error) {
	s.once.Do(func() { close(s.writeStarted) })
	return s.fakeAttachSession.Write(request)
}

func (s *cancellableShortcutSession) AcquireFileTransferLease(context.Context) error {
	s.mu.Lock()
	s.acquires++
	s.mu.Unlock()
	return nil
}

func (s *cancellableShortcutSession) ReleaseFileTransferLease(context.Context) error {
	s.mu.Lock()
	s.releases++
	s.mu.Unlock()
	return nil
}

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

// TestReplaceReceivedFileRefusesExistingContent verifies a late competing file
// is never silently replaced after the destination name was selected.
func TestReplaceReceivedFileRefusesExistingContent(t *testing.T) {
	directory := t.TempDir()
	destination := filepath.Join(directory, "log.txt")
	temporary := filepath.Join(directory, ".channelterm-receive-test")
	if err := os.WriteFile(destination, []byte("old"), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(temporary, []byte("verified new"), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := replaceReceivedFile(temporary, destination); err == nil {
		t.Fatal("replaceReceivedFile() error = nil, want overwrite refusal")
	}
	content, err := os.ReadFile(destination)
	if err != nil {
		t.Fatal(err)
	}
	if got := string(content); got != "old" {
		t.Errorf("destination content = %q, want old", got)
	}
	if content, err := os.ReadFile(temporary); err != nil || string(content) != "verified new" {
		t.Errorf("temporary content = %q, %v; want retained verified data", content, err)
	}
}
