package command

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/akira-init1/ChannelTerm/internal/cli/interactive"
	"github.com/akira-init1/ChannelTerm/internal/core/app"
	"github.com/akira-init1/ChannelTerm/internal/core/session"
	protocol "github.com/modelcontextprotocol/go-sdk/mcp"
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

func TestNextAvailableLocalDirectoryUsesFirstFreeSibling(t *testing.T) {
	directory := t.TempDir()
	for _, name := range []string{"release", "release_1"} {
		if err := os.Mkdir(filepath.Join(directory, name), 0o755); err != nil {
			t.Fatal(err)
		}
	}
	got, err := nextAvailableLocalDirectory(filepath.Join(directory, "release"))
	if err != nil {
		t.Fatalf("nextAvailableLocalDirectory() error = %v", err)
	}
	if want := filepath.Join(directory, "release_2"); got != want {
		t.Errorf("nextAvailableLocalDirectory() = %q, want %q", got, want)
	}
}

func TestCreateLocalDirectoryStagingIsSibling(t *testing.T) {
	destination := filepath.Join(t.TempDir(), "release")
	staging, err := createLocalDirectoryStaging(destination)
	if err != nil {
		t.Fatalf("createLocalDirectoryStaging() error = %v", err)
	}
	defer os.RemoveAll(staging)
	if filepath.Dir(staging) != filepath.Dir(destination) || !strings.HasPrefix(filepath.Base(staging), ".release.cterm-part-") {
		t.Errorf("staging = %q, want same-directory .release.cterm-part-*", staging)
	}
}

func TestReplaceReceivedDirectoryRefusesAppearingDestination(t *testing.T) {
	directory := t.TempDir()
	staging := filepath.Join(directory, ".release.cterm-part-test")
	destination := filepath.Join(directory, "release")
	if err := os.Mkdir(staging, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.Mkdir(destination, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := replaceReceivedDirectory(staging, destination); err == nil || !strings.Contains(err.Error(), "appeared during transfer") {
		t.Fatalf("replaceReceivedDirectory() error = %v, want collision refusal", err)
	}
	if _, err := os.Stat(staging); err != nil {
		t.Errorf("staging directory was changed after refusal: %v", err)
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
	if got := output.String(); !strings.Contains(got, "File transfer:") || !strings.Contains(got, "Select: ") || !strings.Contains(got, "File transfer cancelled") {
		t.Errorf("shortcut output = %q, want menu Select prompt and local cancellation", got)
	}
}

func TestAttachFileShortcutSelectsSendAndReceive(t *testing.T) {
	tests := []struct {
		name       string
		input      string
		wantPrompt string
	}{
		{name: "send", input: "s\x1b", wantPrompt: "Local path:"},
		{name: "receive", input: "r\x1b", wantPrompt: "Remote path:"},
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
			if got := output.String(); !strings.Contains(got, "Select: "+tt.input[:1]+"\r\n"+tt.wantPrompt) || strings.Contains(got, "\r\n"+tt.input[:1]+"\r\n") || !strings.Contains(got, "File transfer cancelled") {
				t.Errorf("shortcut output = %q, want immediate selected %q, prompt %q, and cancellation", got, tt.input[:1], tt.wantPrompt)
			}
		})
	}
}

func TestAttachFileShortcutPathPromptsNeverWriteSession(t *testing.T) {
	tests := []struct {
		name       string
		input      []byte
		wantOutput []string
	}{
		{
			name:       "send accepts pasted remote path before local Esc cancellation",
			input:      []byte("sbuild/firmware.bin\n/home/root/firmware.bin\x1b"),
			wantOutput: []string{"Local path: build/firmware.bin", "Remote path [/tmp/cterm/user-files/firmware.bin]: /home/root/firmware.bin"},
		},
		{
			name:       "receive accepts pasted remote path before local path cancellation",
			input:      []byte("r/var/log/app.log\n\x1b"),
			wantOutput: []string{"Remote path: /var/log/app.log", "Local path [" + defaultLocalTransferPath("/var/log/app.log") + "]:"},
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			attached := &fakeAttachSession{}
			pump := newAttachInputPump(bytes.NewReader(tt.input))
			var output bytes.Buffer
			if ok := runAttachFileShortcut(context.Background(), &pump, attached, func(data []byte) error {
				_, err := output.Write(data)
				return err
			}); !ok {
				t.Fatal("runAttachFileShortcut() returned false")
			}
			for _, want := range tt.wantOutput {
				if !strings.Contains(output.String(), want) {
					t.Errorf("output = %q, want %q", output.String(), want)
				}
			}
			if written := attached.writtenData(); len(written) != 0 {
				t.Errorf("path prompts wrote Session data %q, want none", written)
			}
			if strings.Contains(output.String(), "Session") || strings.Contains(output.String(), "write failed") {
				t.Errorf("path prompt output contains a Session write failure: %q", output.String())
			}
		})
	}
}

func TestAttachFileShortcutIgnoresInvalidMenuKeys(t *testing.T) {
	attached := &fakeAttachSession{}
	pump := newAttachInputPump(strings.NewReader("x\x1b"))
	var output bytes.Buffer
	if ok := runAttachFileShortcut(context.Background(), &pump, attached, func(data []byte) error {
		_, err := output.Write(data)
		return err
	}); !ok {
		t.Fatal("runAttachFileShortcut() returned false")
	}
	if got := output.String(); strings.Contains(got, "Local path:") || strings.Contains(got, "Remote path:") || strings.Contains(got, "\r\nx") || !strings.Contains(got, "Select: ") || !strings.Contains(got, "File transfer cancelled") {
		t.Errorf("shortcut output = %q, want ignored local invalid key and cancellation", got)
	}
	if written := attached.writtenData(); len(written) != 0 {
		t.Errorf("invalid menu key wrote Session data %q, want none", written)
	}
}

func TestAttachFileShortcutReturnsToNormalInput(t *testing.T) {
	pump := newAttachInputPump(strings.NewReader(""))
	attached := &fakeAttachSession{}
	var output bytes.Buffer
	controller := interactive.NewController(interactive.DefaultEscapeByte)
	writeLocal := func(data []byte) error {
		_, err := output.Write(data)
		return err
	}
	if ok := processAttachInput(context.Background(), controller, []byte{interactive.DefaultEscapeByte, 'f', 0x1b}, &pump, attached, writeLocal, func() error { return nil }, func() {}); !ok {
		t.Fatal("Ctrl+] f menu did not return to attach")
	}
	if !strings.Contains(output.String(), "Select: ") || !strings.Contains(output.String(), "File transfer cancelled") {
		t.Errorf("shortcut output = %q, want Ctrl+] f menu and local cancellation", output.String())
	}
	if ok := processAttachInput(context.Background(), controller, []byte("echo ok\r"), &pump, attached, writeLocal, func() error { return nil }, func() {}); !ok {
		t.Fatal("normal attach input did not resume")
	}
	if got := string(attached.writtenData()); got != "echo ok\r" {
		t.Errorf("Session data after menu = %q, want normal attach input", got)
	}
}

func TestAttachFileShortcutDefaultPathsAndCustomPaths(t *testing.T) {
	if got := defaultRemoteTransferPath(filepath.Join("build", "firmware.bin")); got != "/tmp/cterm/user-files/firmware.bin" {
		t.Errorf("default user remote path = %q, want /tmp/cterm/user-files/firmware.bin", got)
	}
	if got := defaultMCPRemoteTransferPath(filepath.Join("build", "firmware.bin")); got != "/tmp/cterm/mcp-files/firmware.bin" {
		t.Errorf("default MCP remote path = %q, want /tmp/cterm/mcp-files/firmware.bin", got)
	}
	if got := defaultLocalTransferPath("/tmp/crash.log"); got != "."+string(filepath.Separator)+"crash.log" {
		t.Errorf("default local path = %q", got)
	}
	if got := "/opt/app/firmware.bin"; got == defaultRemoteTransferPath(filepath.Join("build", "firmware.bin")) {
		t.Fatal("custom remote path unexpectedly changed")
	}
}

func TestResolveFileSendPathsUsesMCPDirectoryOnlyWhenRemotePathIsOmitted(t *testing.T) {
	tests := []struct {
		name        string
		args        []string
		wantRemote  string
		wantOptions []string
	}{
		{name: "default", args: []string{"build/firmware.bin"}, wantRemote: "/tmp/cterm/mcp-files/firmware.bin"},
		{name: "default before options", args: []string{"build/firmware.bin", "--session", "SER-1"}, wantRemote: "/tmp/cterm/mcp-files/firmware.bin", wantOptions: []string{"--session", "SER-1"}},
		{name: "explicit", args: []string{"build/firmware.bin", "/opt/app/firmware.bin", "--session", "SER-1"}, wantRemote: "/opt/app/firmware.bin", wantOptions: []string{"--session", "SER-1"}},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			local, remote, options := resolveFileSendPaths(tt.args)
			if local != tt.args[0] || remote != tt.wantRemote || len(options) != len(tt.wantOptions) || (len(options) > 0 && !reflect.DeepEqual(options, tt.wantOptions)) {
				t.Errorf("resolveFileSendPaths() = (%q, %q, %v), want (%q, %q, %v)", local, remote, options, tt.args[0], tt.wantRemote, tt.wantOptions)
			}
		})
	}
}

func TestFileSendUsageDocumentsOptionalMCPDestination(t *testing.T) {
	var output bytes.Buffer
	writeFileSendUsage(&output)
	if got := output.String(); !strings.Contains(got, "LOCAL_PATH [REMOTE_PATH]") || !strings.Contains(got, "/tmp/cterm/mcp-files/") {
		t.Errorf("file send usage = %q, want optional remote path and MCP directory", got)
	}
}

func TestAttachShortcutControlCCancelsTransferAndRestoresInput(t *testing.T) {
	inputReader, inputWriter := io.Pipe()
	defer inputReader.Close()
	defer inputWriter.Close()
	pump := newAttachInputPump(inputReader)
	attached := &cancellableShortcutSession{writeStarted: make(chan struct{})}
	attachCtx, cancelAttach := context.WithCancel(context.Background())
	defer cancelAttach()
	var output bytes.Buffer
	done := make(chan bool, 1)
	go func() {
		done <- runAttachShortcutTransferWithRunner(attachCtx, &pump, attached, func(data []byte) error {
			_, err := output.Write(data)
			return err
		}, "send", "firmware.bin", "/tmp/firmware.bin", func(workerCtx context.Context, transfer attachSession, workerOutput io.Writer) error {
			lease := transfer.(fileLeaseSession)
			if err := lease.AcquireFileTransferLease(workerCtx); err != nil {
				return err
			}
			defer func() { _ = lease.ReleaseFileTransferLease(context.Background()) }()
			if _, err := transfer.Write(session.WriteRequest{Actor: session.ActorUser, Data: []byte("current chunk")}); err != nil {
				return err
			}
			wrapped := transfer.(nonClosingAttachSession)
			<-wrapped.cancellation.ch
			if err := workerCtx.Err(); err != nil {
				return fmt.Errorf("Ctrl+C cancelled attach context: %w", err)
			}
			if _, err := transfer.Write(session.WriteRequest{Actor: session.ActorUser, Data: []byte("chunk ACK cleanup")}); err != nil {
				return err
			}
			if _, err := fmt.Fprintln(workerOutput, "Transfer cancelled."); err != nil {
				return err
			}
			return context.Canceled
		})
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
	if err := attachCtx.Err(); err != nil {
		t.Errorf("attach context after Ctrl+C = %v, want active", err)
	}
	if attached.acquires != 1 || attached.releases != 1 {
		t.Errorf("lease acquire/release = %d/%d, want 1/1", attached.acquires, attached.releases)
	}
	if got := output.String(); strings.Count(got, "Transfer cancelled.") != 1 || strings.Contains(got, "File transfer cancelled") {
		t.Errorf("output = %q, want one final cancellation message after worker exit", got)
	}
	if got := attached.lifecycle(); len(got) != 4 || got[0] != "acquire" || got[1] != "write" || got[2] != "write" || got[3] != "release" {
		t.Errorf("transfer lifecycle = %v, want current chunk completion before lease release", got)
	}
	if attached.closeCount() != 0 {
		t.Errorf("shared attach close count = %d, want 0", attached.closeCount())
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
	lifecycleLog []string
}

func (s *cancellableShortcutSession) Write(request session.WriteRequest) (int, error) {
	s.once.Do(func() { close(s.writeStarted) })
	s.mu.Lock()
	s.lifecycleLog = append(s.lifecycleLog, "write")
	s.mu.Unlock()
	return s.fakeAttachSession.Write(request)
}

func (s *cancellableShortcutSession) AcquireFileTransferLease(context.Context) error {
	s.mu.Lock()
	s.acquires++
	s.lifecycleLog = append(s.lifecycleLog, "acquire")
	s.mu.Unlock()
	return nil
}

func (s *cancellableShortcutSession) ReleaseFileTransferLease(context.Context) error {
	s.mu.Lock()
	s.releases++
	s.lifecycleLog = append(s.lifecycleLog, "release")
	s.mu.Unlock()
	return nil
}

func (s *cancellableShortcutSession) lifecycle() []string {
	s.mu.Lock()
	defer s.mu.Unlock()
	return append([]string(nil), s.lifecycleLog...)
}

func TestWithFileTransferLeaseReleasesAfterFailure(t *testing.T) {
	attached := &leaseTrackingAttachSession{}
	want := errors.New("transfer failed")
	err := withFileTransferLease(context.Background(), attached, "SER-1", func(context.Context) error { return want })
	if !errors.Is(err, want) {
		t.Fatalf("withFileTransferLease() error = %v, want transfer failure", err)
	}
	if attached.acquires != 1 || attached.releases != 1 {
		t.Errorf("lease acquire/release = %d/%d, want 1/1", attached.acquires, attached.releases)
	}
}

func TestWithFileTransferLeaseCancelsOperationWhenHeartbeatFails(t *testing.T) {
	attached := &failingLeaseHeartbeatSession{}
	err := withFileTransferLease(context.Background(), attached, "SER-1", func(ctx context.Context) error {
		<-ctx.Done()
		return ctx.Err()
	})
	if err == nil || !strings.Contains(err.Error(), "renew file transfer lease") {
		t.Fatalf("withFileTransferLease() error = %v, want heartbeat failure", err)
	}
	if attached.acquires != 1 || attached.renews != 1 || attached.releases != 1 {
		t.Errorf("lease lifecycle = acquire:%d renew:%d release:%d, want 1/1/1", attached.acquires, attached.renews, attached.releases)
	}
}

func TestInternalFileTransferSessionWritesAsSystem(t *testing.T) {
	attached := &fakeAttachSession{}
	terminal := internalFileTransferSession{attachSession: attached}
	if _, err := terminal.Write(session.WriteRequest{Actor: session.ActorUser, Data: []byte("internal command")}); err != nil {
		t.Fatal(err)
	}
	if got := attached.writeActors(); !reflect.DeepEqual(got, []session.Actor{session.ActorSystem}) {
		t.Errorf("internal write actors = %v, want [system]", got)
	}
}

type leaseTrackingAttachSession struct {
	fakeAttachSession
	acquires int
	releases int
}

type failingLeaseHeartbeatSession struct {
	leaseTrackingAttachSession
	renews int
}

func (s *failingLeaseHeartbeatSession) RenewFileTransferLease(context.Context) error {
	s.renews++
	return errors.New("injected heartbeat failure")
}

func TestFileTransferProgressPublishesStructuredProgress(t *testing.T) {
	attached := &eventReportingAttachSession{}
	metadata := map[string]any{"direction": "send", "requested_path": "/tmp/firmware.bin"}
	progress := newFileTransferProgress(context.Background(), io.Discard, attached, metadata)
	if err := progress.Report(622592, 1048576); err != nil {
		t.Fatalf("progress.Report() error = %v", err)
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
	operationErr := errors.New("serial connection lost")
	reportFailedFileTransfer(context.Background(), attached, metadata, &operationErr)
	if len(attached.events) != 2 {
		t.Fatalf("events after failure = %d, want 2", len(attached.events))
	}
	failure := attached.events[1]
	if failure.typ != session.EventFileTransferFailed || failure.metadata["sent"] != int64(622592) || failure.metadata["total"] != int64(1048576) || failure.metadata["percent"] != float64(59.375) {
		t.Errorf("failure event = %+v, want last confirmed transfer progress", failure)
	}
}

func TestFileTransferFailureMetadataReportsUserCancellation(t *testing.T) {
	metadata := fileTransferFailureMetadata(map[string]any{"sent": int64(8192), "total": int64(16384), "percent": 50.0}, fmt.Errorf("stop transfer: %w", context.Canceled))
	if metadata["error"] != fileTransferUserCancelled || metadata["reason"] != fileTransferUserCancelled {
		t.Errorf("cancellation metadata = %#v, want stable user_cancelled result", metadata)
	}
}

func TestResolvedTransferPathMetadataDistinguishesRequestedAndActualPaths(t *testing.T) {
	tests := []struct {
		name      string
		requested string
		resolved  string
		renamed   bool
	}{
		{name: "unchanged", requested: "/tmp/cterm/mcp-files/app.bin", resolved: "/tmp/cterm/mcp-files/app.bin"},
		{name: "collision", requested: "/tmp/cterm/mcp-files/app.bin", resolved: "/tmp/cterm/mcp-files/app_1.bin", renamed: true},
		{name: "directory trailing slash", requested: "/tmp/cterm/mcp-files/release/", resolved: "/tmp/cterm/mcp-files/release"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			metadata := map[string]any{"source_path": "source.bin", "requested_path": tt.requested}
			setResolvedTransferPathMetadata(metadata, tt.requested, tt.resolved, tt.renamed)
			if metadata["source_path"] != "source.bin" || metadata["requested_path"] != tt.requested || metadata["resolved_path"] != tt.resolved || metadata["renamed"] != tt.renamed {
				t.Errorf("resolved metadata = %#v, want requested=%q resolved=%q renamed=%t", metadata, tt.requested, tt.resolved, tt.renamed)
			}
			if _, ok := metadata["local_path"]; ok {
				t.Errorf("resolved metadata unexpectedly contains removed local_path: %#v", metadata)
			}
			if _, ok := metadata["remote_path"]; ok {
				t.Errorf("resolved metadata unexpectedly contains removed remote_path: %#v", metadata)
			}
		})
	}
}

func TestFileSendCollisionResultReachesMCPWaitAfterLeaseRelease(t *testing.T) {
	host := newAttachTestHost(t)
	defer host.close()
	agent, closeAgent := newAttachTestAgent(t, host.server.URL)
	defer closeAgent()

	initial, err := agent.CallTool(context.Background(), &protocol.CallToolParams{Name: "terminal_session_events", Arguments: map[string]any{"session_id": "board"}})
	if err != nil || initial == nil || initial.IsError {
		t.Fatalf("initial terminal_session_events = %#v, %v", initial, err)
	}
	var initialEvents struct {
		Next uint64 `json:"next"`
	}
	decodeStructured(t, initial.StructuredContent, &initialEvents)

	source := filepath.Join(t.TempDir(), "app.bin")
	if err := os.WriteFile(source, []byte("data"), 0o600); err != nil {
		t.Fatal(err)
	}
	requested := "/tmp/cterm/mcp-files/app.bin"
	resolved := "/tmp/cterm/mcp-files/app_1.bin"
	dependencies := fileCommandDependencies{
		newAttach: func(ctx context.Context, endpoint, identifier string) (attachSession, error) {
			return newMCPAttachSession(ctx, endpoint, identifier)
		},
		listSessions: func(context.Context, string) ([]mcpListedSession, error) {
			return nil, nil
		},
		sendFile: func(_ context.Context, _ app.FileTransferSession, _ io.Reader, size int64, remotePath string, _ app.FileTransferCancelRequested, progress app.FileTransferProgress) (app.FileTransferResult, error) {
			if remotePath != requested {
				t.Errorf("remote path = %q, want %q", remotePath, requested)
			}
			if progress != nil {
				if err := progress(size, size); err != nil {
					return app.FileTransferResult{}, err
				}
			}
			return app.FileTransferResult{Size: size, SHA256: strings.Repeat("a", 64), RemotePath: resolved}, nil
		},
	}
	var output bytes.Buffer
	if err := runFileSend(context.Background(), []string{source, requested, "--session", "board", "--endpoint", host.server.URL}, &output, dependencies); err != nil {
		t.Fatalf("runFileSend() error = %v", err)
	}

	observed, err := agent.CallTool(context.Background(), &protocol.CallToolParams{Name: "terminal_session_events", Arguments: map[string]any{"session_id": "board", "cursor": initialEvents.Next, "max_events": 32, "timeout_ms": 1000}})
	if err != nil || observed == nil || observed.IsError {
		t.Fatalf("terminal_session_events after send = %#v, %v", observed, err)
	}
	var eventResult struct {
		Events []struct {
			Type     string         `json:"type"`
			Metadata map[string]any `json:"metadata"`
		} `json:"events"`
	}
	decodeStructured(t, observed.StructuredContent, &eventResult)
	transferID := ""
	completedSeen := false
	releasedSeen := false
	for _, event := range eventResult.Events {
		if event.Type == string(session.EventFileTransferStarted) {
			transferID, _ = event.Metadata["transfer_id"].(string)
		}
		if event.Type == string(session.EventFileTransferCompleted) && event.Metadata["resolved_path"] == resolved {
			completedSeen = true
		}
		if event.Type == string(session.EventLeaseReleased) && event.Metadata["type"] == string(app.LeaseTypeFileTransfer) {
			releasedSeen = true
		}
	}
	if transferID == "" || !completedSeen || !releasedSeen {
		t.Fatalf("transfer events = %#v, want one identified completed and released transfer", eventResult.Events)
	}

	waited, err := agent.CallTool(context.Background(), &protocol.CallToolParams{Name: "terminal_wait_file_transfer", Arguments: map[string]any{"session_id": "board", "transfer_id": transferID, "cursor": initialEvents.Next, "timeout_ms": 1000}})
	if err != nil || waited == nil || waited.IsError {
		t.Fatalf("terminal_wait_file_transfer = %#v, %v", waited, err)
	}
	var result struct {
		State         string `json:"state"`
		TransferID    string `json:"transfer_id"`
		ResolvedPath  string `json:"resolved_path"`
		Renamed       bool   `json:"renamed"`
		LeaseReleased bool   `json:"lease_released"`
	}
	decodeStructured(t, waited.StructuredContent, &result)
	if result.State != "completed" || result.TransferID != transferID || result.ResolvedPath != resolved || !result.Renamed || !result.LeaseReleased {
		t.Fatalf("wait result = %#v", result)
	}
	write, err := agent.CallTool(context.Background(), &protocol.CallToolParams{Name: "terminal_write", Arguments: map[string]any{"session_id": "board", "data": "ready"}})
	if err != nil || write == nil || write.IsError {
		t.Fatalf("terminal_write immediately after wait = %#v, %v", write, err)
	}
}

func TestFileTransferPathMetadataHasDirectionIndependentSemantics(t *testing.T) {
	tests := []struct {
		name      string
		direction string
		source    string
		requested string
	}{
		{name: "send", direction: "send", source: "./app.bin", requested: "/tmp/cterm/mcp-files/app.bin"},
		{name: "receive", direction: "receive", source: "/var/log/app.log", requested: "./app.log"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			metadata := fileTransferPathMetadata(tt.direction, tt.source, tt.requested)
			if metadata["direction"] != tt.direction || metadata["source_path"] != tt.source || metadata["requested_path"] != tt.requested {
				t.Errorf("path metadata = %#v", metadata)
			}
			if _, localOK := metadata["local_path"]; localOK {
				t.Errorf("path metadata unexpectedly contains local_path: %#v", metadata)
			}
			if _, remoteOK := metadata["remote_path"]; remoteOK {
				t.Errorf("path metadata unexpectedly contains remote_path: %#v", metadata)
			}
		})
	}
}

func TestFormatFileTransferProgressBars(t *testing.T) {
	tests := []struct {
		name        string
		snapshot    fileTransferSnapshot
		wantBar     string
		wantPercent string
	}{
		{name: "zero", snapshot: fileTransferSnapshot{total: 100, speed: 1}, wantBar: "--------------------", wantPercent: "0.0%"},
		{name: "half", snapshot: fileTransferSnapshot{transferred: 50, total: 100, percent: 50, speed: 1}, wantBar: "##########----------", wantPercent: "50.0%"},
		{name: "complete", snapshot: fileTransferSnapshot{transferred: 100, total: 100, percent: 100, speed: 1}, wantBar: "####################", wantPercent: "100.0%"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := formatFileTransferProgress(tt.snapshot)
			if !strings.Contains(got, "["+tt.wantBar+"]") {
				t.Errorf("progress = %q, want bar %q", got, tt.wantBar)
			}
			if !strings.Contains(got, tt.wantPercent) {
				t.Errorf("progress = %q, want percent %q", got, tt.wantPercent)
			}
			start := strings.IndexByte(got, '[')
			end := strings.IndexByte(got, ']')
			if start < 0 || end-start-1 != fileTransferProgressBarWidth {
				t.Errorf("progress bar width = %d, want %d: %q", end-start-1, fileTransferProgressBarWidth, got)
			}
		})
	}
}

func TestFormatFileTransferProgressFieldOrder(t *testing.T) {
	got := formatFileTransferProgress(fileTransferSnapshot{
		transferred: 352 * 1024,
		total:       512 * 1024,
		percent:     68.75,
		speed:       8.7 * 1024,
	})
	want := "[##############------] 68.8%  352 KiB / 512 KiB  8.7 KiB/s  ETA 18s"
	if got != want {
		t.Errorf("progress = %q, want %q", got, want)
	}
}

func TestFormatFileTransferBytes(t *testing.T) {
	tests := []struct {
		value int64
		want  string
	}{
		{value: 0, want: "0 B"},
		{value: 1023, want: "1023 B"},
		{value: 608 * 1024, want: "608 KiB"},
		{value: 1024 * 1024, want: "1 MiB"},
		{value: 32 * 1024 * 1024, want: "32 MiB"},
		{value: 1024 * 1024 * 1024, want: "1 GiB"},
	}
	for _, tt := range tests {
		if got := formatFileTransferBytes(tt.value); got != tt.want {
			t.Errorf("formatFileTransferBytes(%d) = %q, want %q", tt.value, got, tt.want)
		}
	}
}

func TestFormatFileTransferSpeed(t *testing.T) {
	tests := []struct {
		value float64
		want  string
	}{
		{value: 850 * 1024, want: "850 KiB/s"},
		{value: 8.7 * 1024, want: "8.7 KiB/s"},
		{value: 1.25 * 1024 * 1024, want: "1.25 MiB/s"},
	}
	for _, tt := range tests {
		if got := formatFileTransferSpeed(tt.value); got != tt.want {
			t.Errorf("formatFileTransferSpeed(%v) = %q, want %q", tt.value, got, tt.want)
		}
	}
}

func TestFormatFileTransferETA(t *testing.T) {
	tests := []struct {
		name               string
		transferred, total int64
		speed              float64
		want               string
	}{
		{name: "seconds", transferred: 85, total: 100, speed: 1, want: "ETA 15s"},
		{name: "minutes", transferred: 15, total: 100, speed: 1, want: "ETA 1m25s"},
		{name: "hours", transferred: 0, total: 3661, speed: 1, want: "ETA 1h1m1s"},
		{name: "non-zero partial second", transferred: 99, total: 100, speed: 2, want: "ETA 1s"},
		{name: "complete", transferred: 100, total: 100, speed: 1, want: "ETA 0s"},
		{name: "zero speed", transferred: 50, total: 100, speed: 0, want: "ETA --"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := formatFileTransferETA(tt.transferred, tt.total, tt.speed); got != tt.want {
				t.Errorf("formatFileTransferETA() = %q, want %q", got, tt.want)
			}
		})
	}
}

func TestFileTransferProgressClearsStaleETAText(t *testing.T) {
	var output bytes.Buffer
	reporter := newFileTransferProgress(context.Background(), &output, &fakeAttachSession{}, nil)
	if err := reporter.render(fileTransferSnapshot{transferred: 0, total: 125, percent: 0, speed: 1}); err != nil {
		t.Fatalf("render(long ETA) error = %v", err)
	}
	if err := reporter.render(fileTransferSnapshot{transferred: 100, total: 125, percent: 80, speed: 1}); err != nil {
		t.Fatalf("render(short ETA) error = %v", err)
	}
	if got := output.String(); !strings.Contains(got, "ETA 25s\x1b[K") || strings.Contains(got, "ETA 25ss") {
		t.Errorf("progress rendering = %q, want cleared single-unit ETA", got)
	}
}

func TestFormatFileTransferProgressZeroByteFile(t *testing.T) {
	got := formatFileTransferProgress(fileTransferSnapshot{total: 0, percent: 100})
	want := "[####################] 100.0%  0 B / 0 B  0 B/s  ETA --"
	if got != want {
		t.Errorf("zero-byte progress = %q, want %q", got, want)
	}
}

func TestFileTransferProgressRendersSendAndReceive(t *testing.T) {
	for _, direction := range []string{"send", "receive"} {
		t.Run(direction, func(t *testing.T) {
			attached := &eventReportingAttachSession{}
			var output bytes.Buffer
			progress := newFileTransferProgress(context.Background(), &output, attached, map[string]any{"direction": direction})
			if err := progress.Report(512, 1024); err != nil {
				t.Fatalf("progress.Report() error = %v", err)
			}
			if got := output.String(); !strings.HasPrefix(got, "\r[##########----------] 50.0%") {
				t.Errorf("progress output = %q, want one-line 50%% update beginning with carriage return", got)
			}
			if len(attached.events) != 1 {
				t.Fatalf("events = %d, want 1", len(attached.events))
			}
			if direction == "send" && attached.events[0].metadata["sent"] != int64(512) {
				t.Errorf("send metadata = %#v, want sent=512", attached.events[0].metadata)
			}
			if direction == "receive" && attached.events[0].metadata["received"] != int64(512) {
				t.Errorf("receive metadata = %#v, want received=512", attached.events[0].metadata)
			}
		})
	}
}

func TestFileTransferProgressStartsNonEmptySendAndReceiveWithSafePlaceholders(t *testing.T) {
	const total = 537 * 1024
	for _, direction := range []string{"send", "receive"} {
		t.Run(direction, func(t *testing.T) {
			attached := &eventReportingAttachSession{}
			var output bytes.Buffer
			progress := newFileTransferProgress(context.Background(), &output, attached, map[string]any{"direction": direction})
			var err error
			if direction == "send" {
				err = progress.Start(total)
			} else {
				err = progress.Report(0, total)
			}
			if err != nil {
				t.Fatalf("initial progress error = %v", err)
			}
			if got := output.String(); got != "\r[--------------------] 0.0%  0 B / 537 KiB  0 B/s  ETA --\x1b[K" {
				t.Errorf("initial progress = %q, want immediate zero-progress frame", got)
			}
			if got := output.String(); !strings.Contains(got, "0 B/s  ETA --") {
				t.Errorf("initial progress = %q, want safe speed and ETA placeholders", got)
			}
			if len(attached.events) != 0 {
				t.Errorf("initial progress events = %#v, want none", attached.events)
			}

			progress.started = time.Now().Add(-time.Second)
			if err := progress.Report(32*1024, total); err != nil {
				t.Fatalf("progress.Report() error = %v", err)
			}
			if got := output.String(); !strings.Contains(got, "KiB/s") || !strings.Contains(got, "ETA ") {
				t.Errorf("first acknowledged progress = %q, want speed and ETA", got)
			}
		})
	}
}

func TestFileTransferProgressCompleteReusesFinalEventSnapshot(t *testing.T) {
	attached := &eventReportingAttachSession{}
	var output bytes.Buffer
	progress := newFileTransferProgress(context.Background(), &output, attached, map[string]any{"direction": "send"})
	if err := progress.Report(100, 100); err != nil {
		t.Fatalf("progress.Report() error = %v", err)
	}
	if got := output.String(); got != "" {
		t.Errorf("final protocol update rendered early: %q", got)
	}
	if err := progress.Complete(100); err != nil {
		t.Fatalf("progress.Complete() error = %v", err)
	}
	speed, ok := attached.events[0].metadata["speed"].(float64)
	if !ok {
		t.Fatalf("event speed = %#v, want float64", attached.events[0].metadata["speed"])
	}
	want := "\r" + formatFileTransferProgress(fileTransferSnapshot{transferred: 100, total: 100, percent: 100, speed: speed}) + "\x1b[K"
	if got := output.String(); got != want {
		t.Errorf("completed output = %q, want final event snapshot %q", got, want)
	}
}

func TestFinishFileTransferPresentation(t *testing.T) {
	tests := []struct {
		name       string
		operation  error
		wantStatus string
		want100    bool
		holdFinal  bool
	}{
		{name: "success", want100: true},
		{name: "cancelled", operation: context.Canceled, wantStatus: "Transfer cancelled.\n"},
		{name: "failure before payload", operation: errors.New("remote setup failed"), wantStatus: "Transfer failed: remote setup failed\n"},
		{name: "failure after final protocol progress", operation: errors.New("remote checksum failed"), wantStatus: "Transfer failed: remote checksum failed\n", holdFinal: true},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			var output bytes.Buffer
			progress := newFileTransferProgress(context.Background(), &output, nil, map[string]any{"direction": "send"})
			if err := progress.Start(100); err != nil {
				t.Fatalf("progress.Start() error = %v", err)
			}
			if tt.operation == nil {
				if err := progress.Report(40, 100); err != nil {
					t.Fatalf("progress.Report() error = %v", err)
				}
			}
			if tt.holdFinal {
				if err := progress.Report(100, 100); err != nil {
					t.Fatalf("final progress.Report() error = %v", err)
				}
			}
			if tt.want100 {
				if err := progress.Complete(100); err != nil {
					t.Fatalf("progress.Complete() error = %v", err)
				}
			}
			if got := finishFileTransferPresentation(&output, progress, tt.operation); !errors.Is(got, tt.operation) {
				t.Errorf("finishFileTransferPresentation() = %v, want %v", got, tt.operation)
			}
			got := output.String()
			if !strings.Contains(got, "\n"+tt.wantStatus) {
				t.Errorf("output = %q, want progress line terminated before %q", got, tt.wantStatus)
			}
			if tt.want100 && !strings.Contains(got, "[####################] 100.0%") {
				t.Errorf("successful output = %q, want final 100%% progress", got)
			}
			if !tt.want100 && strings.Contains(got, "100.0%") {
				t.Errorf("unsuccessful output = %q, must not show 100%%", got)
			}
			if tt.operation != nil && !strings.Contains(got, "[--------------------] 0.0%") {
				t.Errorf("unsuccessful output = %q, want initial zero-progress line", got)
			}
			wantNewlines := 1
			if tt.operation != nil {
				wantNewlines = 2
			}
			if newlines := strings.Count(got, "\n"); newlines != wantNewlines {
				t.Errorf("output newlines = %d, want %d: %q", newlines, wantNewlines, got)
			}
		})
	}
}

func TestFinishFileTransferPresentationDefersAttachResultStatus(t *testing.T) {
	var output bytes.Buffer
	writer := localOutputWriter{write: func(data []byte) error {
		_, err := output.Write(data)
		return err
	}, suppressFileTransferResultText: true}
	progress := newFileTransferProgress(context.Background(), writer, nil, map[string]any{"direction": "send"})
	if err := progress.Start(100); err != nil {
		t.Fatal(err)
	}
	operationErr := errors.New("remote setup failed")
	if got := finishFileTransferPresentation(writer, progress, operationErr); !errors.Is(got, operationErr) {
		t.Errorf("finishFileTransferPresentation() = %v, want %v", got, operationErr)
	}
	if got := output.String(); strings.Contains(got, "\n") || strings.Contains(got, "Transfer failed:") {
		t.Errorf("attach progress output = %q, want line termination and result status deferred to the dispatcher", got)
	}
}

func TestConfirmedAttachCancellationEndsProgressLineExactlyOnce(t *testing.T) {
	var output bytes.Buffer
	lineState := newPresentationLineState()
	writeLocal := func(data []byte) error {
		_, err := output.Write(data)
		lineState.observe(data)
		return err
	}
	writeStatus := func(data []byte) error {
		if prefix := lineState.lineStartPrefix(); len(prefix) > 0 {
			if err := writeLocal(prefix); err != nil {
				return err
			}
		}
		return writeLocal(data)
	}
	writer := localOutputWriter{write: writeLocal, suppressFileTransferResultText: true}
	progress := newFileTransferProgress(context.Background(), writer, nil, map[string]any{"direction": "send"})
	if err := progress.Start(100); err != nil {
		t.Fatal(err)
	}
	if err := writeLocal(fileTransferCancelConfirmationText); err != nil {
		t.Fatal(err)
	}
	if err := writeLocal([]byte("\r\n")); err != nil {
		t.Fatal(err)
	}
	if err := finishFileTransferPresentation(writer, progress, context.Canceled); !errors.Is(err, context.Canceled) {
		t.Fatalf("finishFileTransferPresentation() = %v, want context.Canceled", err)
	}
	timestamp := time.Date(2026, time.September, 15, 12, 0, 0, 0, time.Local)
	if err := writeStatus(fileTransferCancelledSummaryText(timestamp, fileTransferCancellationSnapshot{})); err != nil {
		t.Fatal(err)
	}

	got := output.String()
	if !strings.Contains(got, "\x1b[K\r\n[ChannelTerm] Cancel file transfer? [y/N]: \r\n[12:00:00]") {
		t.Errorf("confirmed cancellation output = %q, want one boundary after progress and one after prompt", got)
	}
	if strings.Contains(got, "[y/N]: \r\n\r\n[12:00:00]") {
		t.Errorf("confirmed cancellation output = %q, contains a blank line before final status", got)
	}
}

func TestFileTransferSuccessSummaryStartsAfterCompletedProgressLine(t *testing.T) {
	var output bytes.Buffer
	progress := newFileTransferProgress(context.Background(), &output, nil, map[string]any{"direction": "send"})
	if err := progress.Start(100); err != nil {
		t.Fatalf("progress.Start() error = %v", err)
	}
	if err := progress.Report(100, 100); err != nil {
		t.Fatalf("progress.Report() error = %v", err)
	}
	if err := progress.Complete(100); err != nil {
		t.Fatalf("progress.Complete() error = %v", err)
	}
	if err := progress.finish(); err != nil {
		t.Fatalf("progress.finish() error = %v", err)
	}
	digest := strings.Repeat("ab", 32)
	if err := writeFileTransferVerificationSummary(&output, digest, "/tmp/system.dts"); err != nil {
		t.Fatalf("write summary error = %v", err)
	}
	wantSummary := "\n  Local SHA-256 : " + digest + "\n" +
		"  Remote SHA-256: " + digest + "\n" +
		"  Verify        : MATCH\n" +
		"  Saved         : /tmp/system.dts\n"
	if got := output.String(); !strings.Contains(got, "[--------------------] 0.0%") || !strings.Contains(got, "[####################] 100.0%") || !strings.Contains(got, wantSummary) {
		t.Errorf("success output = %q, want completed line followed by a separate summary", got)
	}
}

func TestLocalOutputWriterUsesCRLFForFileTransferSummary(t *testing.T) {
	var output bytes.Buffer
	writer := localOutputWriter{write: func(data []byte) error {
		_, err := output.Write(data)
		return err
	}}
	if _, err := fmt.Fprint(writer, "\r[####################] 100.0%  0 B / 0 B  0 B/s  ETA --\x1b[K"); err != nil {
		t.Fatalf("write progress: %v", err)
	}
	if _, err := fmt.Fprintln(writer); err != nil {
		t.Fatalf("finish progress line: %v", err)
	}
	if err := writeFileTransferVerificationSummary(writer, "digest", "/tmp/test123"); err != nil {
		t.Fatalf("write verification summary: %v", err)
	}
	want := "\r[####################] 100.0%  0 B / 0 B  0 B/s  ETA --\x1b[K\r\n" +
		"  Local SHA-256 : digest\r\n" +
		"  Remote SHA-256: digest\r\n" +
		"  Verify        : MATCH\r\n" +
		"  Saved         : /tmp/test123\r\n"
	if got := output.String(); got != want {
		t.Errorf("local output = %q, want %q", got, want)
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
