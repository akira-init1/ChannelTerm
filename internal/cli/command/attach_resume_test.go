package command

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"io"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/akira-init1/ChannelTerm/internal/core/app"
	"github.com/akira-init1/ChannelTerm/internal/core/config"
)

type checkpointShortcutSession struct {
	fakeAttachSession
	target   mcpListedSession
	calls    int
	change   bool
	leaseErr error
}

type checkpointOutputFailure struct{}

func (checkpointOutputFailure) Write([]byte) (int, error) { return 0, io.ErrClosedPipe }

func (s *checkpointShortcutSession) fileTransferTarget(context.Context) (string, mcpListedSession, error) {
	s.calls++
	target := s.target
	if s.change && s.calls > 1 {
		target.Endpoint = "different-port"
	}
	return defaultMCPEndpoint, target, nil
}
func (s *checkpointShortcutSession) AcquireFileTransferLease(context.Context) error {
	return s.leaseErr
}
func (*checkpointShortcutSession) ReleaseFileTransferLease(context.Context) error { return nil }

func isolateShortcutCheckpoints(t *testing.T) string {
	t.Helper()
	directory := t.TempDir()
	for _, key := range []string{"APPDATA", "XDG_CONFIG_HOME", "HOME"} {
		t.Setenv(key, directory)
	}
	root, err := config.DefaultTransferPath()
	if err != nil {
		t.Fatal(err)
	}
	return root
}

func TestAttachCheckpointMenuKeepsDefaultsAndDiscardsCancelledJobInput(t *testing.T) {
	var output bytes.Buffer
	terminal := &fakeAttachSession{}
	d := newAttachInputDispatcherWithPump(context.Background(), &attachInputPump{}, terminal, func(data []byte) error { _, err := output.Write(data); return err }, func() error { return nil }, func() {}, nil)
	if !d.enterFileMenu() || d.keepCheckpoint {
		t.Fatal("changed default")
	}
	d.dispatchFileMenuByte('k')
	if !d.keepCheckpoint || !strings.Contains(output.String(), "ON") {
		t.Fatal("retention not explicit")
	}
	d.enterFileMenu()
	if d.keepCheckpoint {
		t.Fatal("retention persisted into another operation")
	}
	d.mode = attachInputModeResumeJob
	d.dispatch([]byte("\x1bmust-not-replay\n"))
	if len(terminal.writtenData()) != 0 || d.mode != attachInputModeAttach {
		t.Fatal("cancelled job input reached device")
	}
	d.mode = attachInputModeResumeJob
	d.dispatch([]byte("\rmust-not-replay\n"))
	if len(terminal.writtenData()) != 0 || d.mode != attachInputModeAttach {
		t.Fatal("empty job input replayed paste tail")
	}
	d.mode = attachInputModeResumeJob
	d.dispatch([]byte(strings.Repeat("a", 1000) + "\n"))
	if d.mode != attachInputModeFileMenu || len(d.line) != 0 || len(terminal.writtenData()) != 0 {
		t.Fatal("oversized job accepted or replayed")
	}
}

func TestAttachCheckpointWorkerPropagatesExplicitIntentAndListsBeforePrompt(t *testing.T) {
	var output bytes.Buffer
	observed := make(chan string, 1)
	d := newAttachInputDispatcherWithPump(context.Background(), &attachInputPump{}, &fakeAttachSession{}, func(data []byte) error { _, err := output.Write(data); return err }, func() error { return nil }, func() {}, func(_ context.Context, attached attachSession, _ io.Writer, _ func() bool, direction, first, _ string) error {
		keep := attached.(interface{ keepFileCheckpoint() bool }).keepFileCheckpoint()
		if direction == "send" && !keep {
			return errors.New("retention lost")
		}
		observed <- direction + ":" + first
		return nil
	})
	d.enterFileMenu()
	d.dispatchFileMenuByte('k')
	d.startTransfer("send", "local", "/tmp/remote")
	if err := <-d.transferDone; err != nil {
		t.Fatal(err)
	} else {
		d.finishTransfer(err)
	}
	if got := <-observed; got != "send:local" {
		t.Fatal(got)
	}
	d.enterFileMenu()
	d.dispatchFileMenuByte('j')
	if d.mode != attachInputModeFileTransfer {
		t.Fatal("prompt opened before listing")
	}
	d.finishTransfer(<-d.transferDone)
	if got := <-observed; got != "jobs:" || d.mode != attachInputModeResumeJob {
		t.Fatalf("%s %v", got, d.mode)
	}
	job := strings.Repeat("a", 24)
	d.dispatch([]byte(strings.ToUpper(job) + "\n"))
	d.finishTransfer(<-d.transferDone)
	if got := <-observed; got != "resume:"+job {
		t.Fatal(got)
	}
}

func TestAttachCheckpointListFailureAndCancellationNeverOpenResumePrompt(t *testing.T) {
	for _, cancelled := range []bool{false, true} {
		var output bytes.Buffer
		d := newAttachInputDispatcherWithPump(context.Background(), &attachInputPump{}, &fakeAttachSession{}, func(data []byte) error { _, err := output.Write(data); return err }, func() error { return nil }, func() {}, nil)
		d.mode = attachInputModeFileTransfer
		d.listingCheckpoints = true
		d.transferCancellation = newFileTransferCancellation()
		failure := errors.New("listing unavailable")
		if cancelled {
			d.transferCancellation.Request()
			failure = context.Canceled
		}
		d.finishTransfer(failure)
		if d.mode != attachInputModeAttach || d.listingCheckpoints || strings.Contains(output.String(), "Checkpoint ID") || strings.Contains(output.String(), "transfer completed") {
			t.Fatalf("cancelled=%v mode=%v output=%s", cancelled, d.mode, output.String())
		}
	}
}

func TestAttachCheckpointUsesSavedBindingAndRefusesChangedReadOnlyOrNonSerialTargets(t *testing.T) {
	root := isolateShortcutCheckpoints(t)
	original := &checkpointShortcutSession{target: mcpListedSession{ID: "original-id", Reference: "SER-1", Transport: "serial", Endpoint: "COM-FIXTURE", State: "open"}}
	wrapped := nonClosingAttachSession{attachSession: original, keepCheckpoint: true}
	if err := runAttachShortcutFileTransfer(context.Background(), wrapped, checkpointOutputFailure{}, func() bool { return false }, "jobs", "", ""); !errors.Is(err, io.ErrClosedPipe) {
		t.Fatalf("empty listing output failure: %v", err)
	}
	source := filepath.Join(t.TempDir(), "source.bin")
	if err := os.WriteFile(source, []byte("snapshot"), 0600); err != nil {
		t.Fatal(err)
	}
	peer, _, err := fileCheckpointPeer(context.Background(), fileOptions{session: original.target.ID, endpoint: defaultMCPEndpoint}, fileCommandDependencies{listSessions: func(context.Context, string) ([]mcpListedSession, error) {
		return []mcpListedSession{original.target}, nil
	}})
	if err != nil {
		t.Fatal(err)
	}
	saved, err := app.CreateTransferCheckpoint(context.Background(), root, "send", source, "/tmp/out", peer, app.FileTransferWireBase64)
	if err != nil {
		t.Fatal(err)
	}
	var output bytes.Buffer
	if err := runAttachShortcutFileTransfer(context.Background(), wrapped, &output, func() bool { return false }, "jobs", "", ""); err != nil || !strings.Contains(output.String(), saved.ID) {
		t.Fatalf("%s %v", output.String(), err)
	}
	if err := runAttachShortcutFileTransfer(context.Background(), wrapped, checkpointOutputFailure{}, func() bool { return false }, "jobs", "", ""); !errors.Is(err, io.ErrClosedPipe) {
		t.Fatalf("listing output failure: %v", err)
	}
	original.calls = 0
	original.change = true
	if err := runAttachShortcutFileTransfer(context.Background(), wrapped, io.Discard, func() bool { return false }, "resume", saved.ID, ""); err == nil || !strings.Contains(err.Error(), "changed") {
		t.Fatal(err)
	}
	original.change = false
	original.leaseErr = os.ErrPermission
	if err := runAttachShortcutFileTransfer(context.Background(), wrapped, io.Discard, func() bool { return false }, "resume", saved.ID, ""); !errors.Is(err, os.ErrPermission) {
		t.Fatal(err)
	}
	if len(original.writtenData()) != 0 {
		t.Fatal("rejected resume wrote device bytes")
	}
	original.target.Transport = "ssh"
	if err := runAttachShortcutFileTransfer(context.Background(), wrapped, io.Discard, func() bool { return false }, "jobs", "", ""); err == nil || !strings.Contains(err.Error(), "sftp") {
		t.Fatal(err)
	}
	original.target.Transport = "serial"
	wrapped.directoryMode = "files"
	if err := runAttachShortcutFileTransfer(context.Background(), wrapped, io.Discard, func() bool { return false }, "send", source, "/tmp/out"); err == nil || !strings.Contains(err.Error(), "per-file") {
		t.Fatal(err)
	}
	saved.Phase = "publishing"
	data, _ := json.Marshal(saved)
	if err := os.WriteFile(filepath.Join(root, saved.ID, "checkpoint.json"), data, 0600); err != nil {
		t.Fatal(err)
	}
	if err := runAttachShortcutFileTransfer(context.Background(), wrapped, io.Discard, func() bool { return false }, "resume", saved.ID, ""); !errors.Is(err, app.ErrTransferPublicationUncertain) {
		t.Fatal(err)
	}
}

func TestMCPCheckpointTargetKeepsOriginalRegistration(t *testing.T) {
	host := newAttachTestHost(t)
	defer host.close()
	attached, err := newMCPAttachSession(context.Background(), host.server.URL, "board")
	if err != nil {
		t.Fatal(err)
	}
	defer attached.Close()
	source := attached.(shortcutCheckpointTarget)
	endpoint, target, err := source.fileTransferTarget(context.Background())
	if err != nil || endpoint != host.server.URL || target.ID != "board" {
		t.Fatalf("%s %+v %v", endpoint, target, err)
	}
	concrete := attached.(*mcpAttachSession)
	concrete.recoveryReference = "different-registration"
	if _, _, err := source.fileTransferTarget(context.Background()); !errors.Is(err, ErrAttachedSessionNotFound) {
		t.Fatal(err)
	}
}
