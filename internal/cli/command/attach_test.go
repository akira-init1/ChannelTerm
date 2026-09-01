package command

import (
	"bytes"
	"context"
	"encoding/base64"
	"encoding/json"
	"errors"
	"io"
	"net/http/httptest"
	"reflect"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/akira-init1/ChannelTerm/internal/cli/interactive"
	"github.com/akira-init1/ChannelTerm/internal/core/channel"
	"github.com/akira-init1/ChannelTerm/internal/core/session"
	mcpadapter "github.com/akira-init1/ChannelTerm/internal/mcp"
	protocol "github.com/modelcontextprotocol/go-sdk/mcp"
)

func TestMCPAttachFindsExistingSessionAndForwardsInput(t *testing.T) {
	host := newAttachTestHost(t)
	defer host.close()

	attached, err := newMCPAttachSession(context.Background(), host.server.URL, "board")
	if err != nil {
		t.Fatalf("newMCPAttachSession() error = %v", err)
	}
	defer func() { _ = attached.Close() }()

	host.device.emit([]byte("boot> "))
	ctx, cancel := context.WithTimeout(context.Background(), time.Second)
	defer cancel()
	chunk, err := attached.ReadOutput(ctx, 0, 128)
	if err != nil {
		t.Fatalf("ReadOutput() error = %v", err)
	}
	if got := string(chunk.Data); got != "boot> " {
		t.Errorf("ReadOutput() data = %q, want boot prompt", got)
	}
	if _, err := attached.Write(session.WriteRequest{Actor: session.ActorUser, Data: []byte("uname -a\n")}); err != nil {
		t.Fatalf("Write() error = %v", err)
	}
	if got := string(host.device.waitWritten(t)); got != "uname -a\n" {
		t.Errorf("device input = %q, want uname command", got)
	}
}

func TestMCPAttachRejectsMissingSession(t *testing.T) {
	host := newAttachTestHost(t)
	defer host.close()

	attached, err := newMCPAttachSession(context.Background(), host.server.URL, "missing")
	if attached != nil {
		_ = attached.Close()
		t.Error("newMCPAttachSession() returned a client for a missing Session")
	}
	if !errors.Is(err, ErrAttachedSessionNotFound) {
		t.Errorf("newMCPAttachSession() error = %v, want ErrAttachedSessionNotFound", err)
	}
}

func TestMCPAttachConsumersKeepIndependentCursors(t *testing.T) {
	host := newAttachTestHost(t)
	defer host.close()
	cli := newAttachedForTest(t, host.server.URL)
	defer func() { _ = cli.Close() }()
	agent, closeAgent := newAttachTestAgent(t, host.server.URL)
	defer closeAgent()

	host.device.emit([]byte("shared output\n"))
	ctx, cancel := context.WithTimeout(context.Background(), time.Second)
	defer cancel()
	cliChunk, err := cli.ReadOutput(ctx, 0, 128)
	if err != nil {
		t.Fatalf("CLI ReadOutput() error = %v", err)
	}
	result, err := agent.CallTool(ctx, &protocol.CallToolParams{Name: "terminal_wait", Arguments: map[string]any{
		"session_id": "board", "cursor": 0, "max_bytes": 128, "encoding": "base64",
	}})
	if err != nil || result.IsError {
		t.Fatalf("Agent terminal_wait = %#v, %v", result, err)
	}
	var agentChunk struct {
		Data string `json:"data"`
		Next uint64 `json:"next"`
	}
	decodeStructured(t, result.StructuredContent, &agentChunk)
	agentData, err := base64.StdEncoding.DecodeString(agentChunk.Data)
	if err != nil {
		t.Fatalf("decode Agent output: %v", err)
	}
	if got := string(cliChunk.Data); got != "shared output\n" {
		t.Errorf("CLI consumer data = %q, want shared output", got)
	}
	if got := string(agentData); got != "shared output\n" {
		t.Errorf("Agent consumer data = %q, want shared output", got)
	}
	wantNext := session.OutputCursor(len("shared output\n"))
	if cliChunk.Next != wantNext || agentChunk.Next != uint64(wantNext) {
		t.Errorf("independent cursors = %d/%d, want %d", cliChunk.Next, agentChunk.Next, wantNext)
	}
}

func TestMCPAttachDetachDoesNotCloseManagedSession(t *testing.T) {
	host := newAttachTestHost(t)
	defer host.close()
	attached := newAttachedForTest(t, host.server.URL)
	if err := attached.Close(); err != nil {
		t.Fatalf("attach Close() error = %v", err)
	}

	managed, ok := host.manager.Get("board")
	if !ok {
		t.Fatal("attach Close() removed the Manager-owned Session")
	}
	if got := managed.State(); got != session.StateOpen {
		t.Errorf("Session state after detach = %s, want open", got)
	}
}

func TestMCPAttachSessionCloseReleasesWait(t *testing.T) {
	host := newAttachTestHost(t)
	defer host.close()
	attached := newAttachedForTest(t, host.server.URL)
	defer func() { _ = attached.Close() }()

	returned := make(chan error, 1)
	go func() {
		_, err := attached.ReadOutput(context.Background(), 0, 128)
		returned <- err
	}()
	time.Sleep(20 * time.Millisecond)
	if err := host.manager.Close(); err != nil {
		t.Fatalf("Manager.Close() error = %v", err)
	}
	select {
	case err := <-returned:
		if err == nil {
			t.Error("ReadOutput() error = nil after Session close")
		}
	case <-time.After(time.Second):
		t.Fatal("Session close did not release the attach wait")
	}
}

func TestMCPAttachMultipleConsumersReceiveSameOutput(t *testing.T) {
	host := newAttachTestHost(t)
	defer host.close()
	consumers := []attachSession{
		newAttachedForTest(t, host.server.URL),
		newAttachedForTest(t, host.server.URL),
		newAttachedForTest(t, host.server.URL),
	}
	for _, consumer := range consumers {
		defer func(client attachSession) { _ = client.Close() }(consumer)
	}

	host.device.emit([]byte("fanout\n"))
	for index, consumer := range consumers {
		ctx, cancel := context.WithTimeout(context.Background(), time.Second)
		chunk, err := consumer.ReadOutput(ctx, 0, 128)
		cancel()
		if err != nil {
			t.Fatalf("consumer %d ReadOutput() error = %v", index, err)
		}
		if got := string(chunk.Data); got != "fanout\n" {
			t.Errorf("consumer %d data = %q, want fanout", index, got)
		}
	}
}

func TestMCPAttachWriteAndAgentWriteAreAtomic(t *testing.T) {
	host := newAttachTestHost(t)
	defer host.close()
	host.device.shortWrites = true
	attached := newAttachedForTest(t, host.server.URL)
	defer func() { _ = attached.Close() }()
	agent, closeAgent := newAttachTestAgent(t, host.server.URL)
	defer closeAgent()

	cliPayload := []byte("AAAAAAAA")
	agentPayload := "BBBBBBBB"
	start := make(chan struct{})
	var group sync.WaitGroup
	group.Add(2)
	go func() {
		defer group.Done()
		<-start
		if _, err := attached.Write(session.WriteRequest{Actor: session.ActorUser, Data: cliPayload}); err != nil {
			t.Errorf("CLI Write() error = %v", err)
		}
	}()
	go func() {
		defer group.Done()
		<-start
		result, err := agent.CallTool(context.Background(), &protocol.CallToolParams{Name: "terminal_write", Arguments: map[string]any{
			"session_id": "board", "data": agentPayload,
		}})
		if err != nil || result.IsError {
			t.Errorf("Agent terminal_write = %#v, %v", result, err)
		}
	}()
	close(start)
	group.Wait()

	got := string(host.device.waitWrittenLength(t, len(cliPayload)+len(agentPayload)))
	if got != string(cliPayload)+agentPayload && got != agentPayload+string(cliPayload) {
		t.Errorf("concurrent payloads interleaved: %q", got)
	}
}

func TestRenderAgentActivityShowsCommandsButSkipsUserAndLineEndings(t *testing.T) {
	timestamp := time.Date(2026, time.August, 23, 18, 52, 31, 0, time.Local)
	block := renderAgentActivity(session.SessionEvent{
		Timestamp: timestamp,
		Actor:     session.ActorAgent,
		Operation: session.OperationWrite,
		Data:      []byte("ls /tmp"),
	})
	if got, want := string(block), "\r\n──────── AI ────────\r\n[18:52:31] >> ls /tmp\r\n────────────────────\r\n"; got != want {
		t.Errorf("rendered Agent block = %q, want %q", got, want)
	}
	for _, event := range []session.SessionEvent{
		{Actor: session.ActorUser, Operation: session.OperationWrite, Data: []byte("ls")},
		{Actor: session.ActorAgent, Operation: session.OperationWrite, Data: []byte("\r\n")},
		{Actor: session.ActorAgent, Operation: session.OperationWrite, Data: []byte("\r")},
	} {
		if got := renderAgentActivity(event); len(got) != 0 {
			t.Errorf("renderAgentActivity(%+v) = %q, want no block", event, got)
		}
	}
}

func TestRunAttachEscapeQuitDetachesWithoutClosingSession(t *testing.T) {
	client := &fakeAttachSession{}
	var output bytes.Buffer
	err := runAttach(context.Background(), []string{"board"}, strings.NewReader("status\n\x03\x1dq"), &output, func(_ context.Context, endpoint, id string) (attachSession, error) {
		if endpoint != defaultMCPEndpoint || id != "board" {
			t.Errorf("attach target = %q/%q, want default endpoint/board", endpoint, id)
		}
		return client, nil
	})
	if err != nil {
		t.Fatalf("runAttach() error = %v", err)
	}
	if got := string(client.writtenData()); got != "status\n\x03" {
		t.Errorf("forwarded input = %q, want input and Ctrl+C before local quit", got)
	}
	if got := client.writeActors(); !reflect.DeepEqual(got, []session.Actor{session.ActorUser}) {
		t.Errorf("forwarded actors = %v, want [user]", got)
	}
	if client.closeCount() != 1 {
		t.Errorf("attach Close calls = %d, want 1", client.closeCount())
	}
	if got := output.String(); got != string(escapePendingText)+"Detached.\r\n" {
		t.Errorf("CLI output = %q, want escape feedback followed by detach status", got)
	}
}

func TestRunAttachContextCancellationDetaches(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	client := &fakeAttachSession{onReadOutput: cancel}
	var output bytes.Buffer
	if err := runAttach(ctx, []string{"board"}, strings.NewReader(""), &output, func(context.Context, string, string) (attachSession, error) {
		return client, nil
	}); err != nil {
		t.Fatalf("runAttach() error = %v", err)
	}
	if client.closeCount() != 1 || !strings.Contains(output.String(), "Detached.") {
		t.Errorf("cancellation did not detach cleanly: closes=%d output=%q", client.closeCount(), output.String())
	}
}

// TestRunAttachKeepsEscapePendingWhileSessionOutputArrives verifies that
// output forwarding cannot alter input-controller state: a literal Ctrl+]
// command remains available after the Session displays remote output.
func TestRunAttachKeepsEscapePendingWhileSessionOutputArrives(t *testing.T) {
	inputReader, inputWriter := io.Pipe()
	defer inputReader.Close()
	defer inputWriter.Close()
	output := newSignalBuffer()
	outputDelivered := make(chan struct{})
	client := &fakeAttachSession{
		nextOutput: []byte("device output\r\n"),
		onReadOutput: func() {
			select {
			case <-output.firstWrite:
			case <-time.After(time.Second):
			}
		},
		onOutputDelivered: func() {
			close(outputDelivered)
		},
	}
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	done := make(chan error, 1)
	go func() {
		done <- runAttach(ctx, []string{"board"}, inputReader, output, func(context.Context, string, string) (attachSession, error) {
			return client, nil
		})
	}()
	if _, err := inputWriter.Write([]byte{interactive.DefaultEscapeByte}); err != nil {
		t.Fatalf("write escape prefix: %v", err)
	}
	waitForSignal(t, output.firstWrite, "local escape feedback")
	waitForSignal(t, outputDelivered, "remote Session output")
	if _, err := inputWriter.Write([]byte{']'}); err != nil {
		t.Fatalf("write literal escape command: %v", err)
	}
	if err := inputWriter.Close(); err != nil {
		t.Fatalf("close input: %v", err)
	}
	if got := waitForAttachWrite(t, client); !bytes.Equal(got, []byte{interactive.DefaultEscapeByte}) {
		t.Errorf("remote input after output = %x, want literal Ctrl+]", got)
	}
	cancel()
	if err := <-done; err != nil {
		t.Fatalf("runAttach() error = %v", err)
	}
	if got := output.String(); !strings.Contains(got, string(escapePendingText)) || !strings.Contains(got, "device output\r\n") {
		t.Errorf("CLI output = %q, want local escape feedback and unchanged remote output", got)
	}
}

// TestRunAttachCleansUpAfterSessionErrorDuringEscapePending verifies that an
// attachment error still follows the normal defer cleanup path after local
// escape mode is entered.
func TestRunAttachCleansUpAfterSessionErrorDuringEscapePending(t *testing.T) {
	inputReader, inputWriter := io.Pipe()
	defer inputReader.Close()
	defer inputWriter.Close()
	output := newSignalBuffer()
	sessionErr := errors.New("session disconnected")
	client := &fakeAttachSession{
		readOutputErr: sessionErr,
		onReadOutput: func() {
			select {
			case <-output.firstWrite:
			case <-time.After(time.Second):
			}
		},
	}
	done := make(chan error, 1)
	go func() {
		done <- runAttach(context.Background(), []string{"board"}, inputReader, output, func(context.Context, string, string) (attachSession, error) {
			return client, nil
		})
	}()
	if _, err := inputWriter.Write([]byte{interactive.DefaultEscapeByte}); err != nil {
		t.Fatalf("write escape prefix: %v", err)
	}
	if err := inputWriter.Close(); err != nil {
		t.Fatalf("close input: %v", err)
	}
	err := <-done
	if !errors.Is(err, sessionErr) {
		t.Fatalf("runAttach() error = %v, want session error", err)
	}
	if client.closeCount() != 1 {
		t.Errorf("attach Close calls = %d, want 1", client.closeCount())
	}
	if got := client.writtenData(); len(got) != 0 {
		t.Errorf("remote input = %x, want no escape-prefix bytes", got)
	}
	if got := output.String(); !strings.Contains(got, string(escapePendingText)) {
		t.Errorf("CLI output = %q, want local escape feedback before cleanup", got)
	}
}

func TestRunAttachHighlightAlwaysStylesManagedSessionOutput(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	client := &fakeAttachSession{recentData: []byte("driver is not open\n"), onReadOutput: cancel}
	var output bytes.Buffer
	if err := runAttach(ctx, []string{"board", "--highlight", "always"}, strings.NewReader(""), &output, func(context.Context, string, string) (attachSession, error) {
		return client, nil
	}); err != nil {
		t.Fatalf("runAttach() error = %v", err)
	}
	const highlighted = "driver is \x1b[1;91mnot open\x1b[0m\n"
	if !strings.Contains(output.String(), highlighted) {
		t.Errorf("output = %q, want highlighted managed output %q", output.String(), highlighted)
	}
}

func TestAttachSerialOpenArgumentsRetainExplicitSettings(t *testing.T) {
	arguments, err := attachSerialOpenArguments([]string{"--baud", "921600", "--wake", "--label", "board"}, "COM8")
	if err != nil {
		t.Fatalf("attachSerialOpenArguments() error = %v", err)
	}
	want := map[string]any{"port": "COM8", "baud": 921600, "wake": true, "label": "board"}
	if !reflect.DeepEqual(arguments, want) {
		t.Errorf("attachSerialOpenArguments() = %#v, want %#v", arguments, want)
	}
}

func TestAttachReferenceClassification(t *testing.T) {
	for _, tt := range []struct {
		value       string
		wantSession bool
		wantTarget  bool
	}{
		{value: "SER-1", wantSession: true, wantTarget: true},
		{value: "SER-COM8", wantTarget: true},
		{value: "SSH-1", wantSession: true},
		{value: "ee7fd9f7b2d06688e1ad125580c25bc0"},
	} {
		if got := isShortSessionReference(tt.value); got != tt.wantSession {
			t.Errorf("isShortSessionReference(%q) = %t, want %t", tt.value, got, tt.wantSession)
		}
		if got := isSerialTargetReference(tt.value); got != tt.wantTarget {
			t.Errorf("isSerialTargetReference(%q) = %t, want %t", tt.value, got, tt.wantTarget)
		}
	}
}

// attachTestHost exposes one Manager-owned Core through the same Streamable
// HTTP adapter used by production MCP. Its transport has no physical device,
// so CLI client tests exercise the real shared-session boundary deterministically.
type attachTestHost struct {
	manager *session.Manager
	device  *attachTestTransport
	server  *httptest.Server
}

// newAttachTestHost creates an open board Session before serving MCP requests.
func newAttachTestHost(t *testing.T) *attachTestHost {
	t.Helper()
	manager := session.NewManager()
	device := newAttachTestTransport()
	terminal, err := session.New("board", device)
	if err != nil {
		t.Fatalf("session.New() error = %v", err)
	}
	if err := terminal.Connect(context.Background()); err != nil {
		t.Fatalf("Session.Connect() error = %v", err)
	}
	if err := manager.Register(terminal); err != nil {
		t.Fatalf("Manager.Register() error = %v", err)
	}
	registry, err := newMCPRegistry(manager, newTestDeviceRegistry(t))
	if err != nil {
		t.Fatalf("newMCPRegistry() error = %v", err)
	}
	handler, err := mcpadapter.NewStreamableHTTPHandler(registry)
	if err != nil {
		t.Fatalf("NewStreamableHTTPHandler() error = %v", err)
	}
	return &attachTestHost{manager: manager, device: device, server: httptest.NewServer(handler)}
}

// close shuts down the HTTP client entry point before releasing the Manager's
// Session. The order prevents a late test request from racing cleanup.
func (h *attachTestHost) close() {
	h.server.Close()
	_ = h.manager.Close()
}

// newAttachedForTest attaches to the host's single pre-registered Session.
func newAttachedForTest(t *testing.T, endpoint string) attachSession {
	t.Helper()
	attached, err := newMCPAttachSession(context.Background(), endpoint, "board")
	if err != nil {
		t.Fatalf("newMCPAttachSession() error = %v", err)
	}
	return attached
}

// newAttachTestAgent creates an independent MCP cursor and writer, modeling
// the Agent side of a shared Session without opening another Transport.
func newAttachTestAgent(t *testing.T, endpoint string) (*protocol.ClientSession, func()) {
	t.Helper()
	client := protocol.NewClient(&protocol.Implementation{Name: "attach-test-agent", Version: "test"}, nil)
	attached, err := client.Connect(context.Background(), &protocol.StreamableClientTransport{Endpoint: endpoint, DisableStandaloneSSE: true}, nil)
	if err != nil {
		t.Fatalf("Agent Connect() error = %v", err)
	}
	return attached, func() { _ = attached.Close() }
}

// attachTestTransport provides one blocking read path for Core's dedicated
// reader and records writes. Short writes force Core's write lock to cover more
// than one Transport.Write call, exposing byte-interleaving regressions.
type attachTestTransport struct {
	mu          sync.Mutex
	reads       chan []byte
	closed      chan struct{}
	closeOnce   sync.Once
	written     []byte
	shortWrites bool
}

// newAttachTestTransport constructs a transport with buffered injected output.
func newAttachTestTransport() *attachTestTransport {
	return &attachTestTransport{reads: make(chan []byte, 16), closed: make(chan struct{})}
}

// Connect satisfies transport.Transport without contacting a physical device.
func (t *attachTestTransport) Connect(context.Context) (channel.Channel, error) { return t, nil }

// Read blocks until injected output arrives or Close releases Core's reader.
func (t *attachTestTransport) Read(p []byte) (int, error) {
	select {
	case data := <-t.reads:
		return copy(p, data), nil
	case <-t.closed:
		return 0, io.EOF
	}
}

// Write records one complete or intentionally short transport write.
func (t *attachTestTransport) Write(data []byte) (int, error) {
	t.mu.Lock()
	defer t.mu.Unlock()
	count := len(data)
	if t.shortWrites && count > 1 {
		count = 1
	}
	t.written = append(t.written, data[:count]...)
	return count, nil
}

// Resize is unsupported by the fake serial-style transport.
func (*attachTestTransport) Resize(uint16, uint16) error { return errors.New("resize unsupported") }

// State reports the established test Channel lifecycle.
func (*attachTestTransport) State() channel.State { return channel.StateOpen }

// Close unblocks the only Core reader and is safe when Manager cleanup repeats it.
func (t *attachTestTransport) Close() error {
	t.closeOnce.Do(func() { close(t.closed) })
	return nil
}

// emit queues terminal output for the Core reader without granting consumers
// direct access to the transport.
func (t *attachTestTransport) emit(data []byte) {
	t.reads <- append([]byte(nil), data...)
}

// waitWritten waits until at least one write has reached the shared transport.
func (t *attachTestTransport) waitWritten(tb testing.TB) []byte {
	return t.waitWrittenLength(tb, 1)
}

// waitWrittenLength waits for a known complete byte count while avoiding an
// unbounded test hang if an MCP request fails before reaching Session.Write.
func (t *attachTestTransport) waitWrittenLength(tb testing.TB, length int) []byte {
	tb.Helper()
	deadline := time.Now().Add(time.Second)
	for time.Now().Before(deadline) {
		t.mu.Lock()
		data := append([]byte(nil), t.written...)
		t.mu.Unlock()
		if len(data) >= length {
			return data
		}
		time.Sleep(time.Millisecond)
	}
	tb.Fatalf("transport received %d bytes, want at least %d", len(t.written), length)
	return nil
}

// fakeAttachSession lets runAttach tests exercise local escape handling and
// context cancellation without an HTTP server or a real Session lifecycle.
type fakeAttachSession struct {
	mu                sync.Mutex
	written           []byte
	actors            []session.Actor
	closes            int
	recentData        []byte
	nextOutput        []byte
	readOutputErr     error
	onReadOutput      func()
	onOutputDelivered func()
}

// ReadRecent starts the CLI cursor with optional already-retained terminal data.
func (s *fakeAttachSession) ReadRecent(context.Context, int) (session.OutputChunk, error) {
	return session.OutputChunk{Data: append([]byte(nil), s.recentData...), Next: session.OutputCursor(len(s.recentData))}, nil
}

// ReadRecentActivity starts the fake activity cursor at an empty stream tail.
func (*fakeAttachSession) ReadRecentActivity(context.Context, int) (session.ActivityChunk, error) {
	return session.ActivityChunk{}, nil
}

// ReadActivity blocks until detach cancellation so runAttach tests also verify
// that the Activity observer releases with the local attach context.
func (*fakeAttachSession) ReadActivity(ctx context.Context, _ session.ActivityCursor, _ int) (session.ActivityChunk, error) {
	<-ctx.Done()
	return session.ActivityChunk{}, ctx.Err()
}

// ReadOutput returns configured output once, then invokes the optional hook
// before returning a configured error or waiting for caller cancellation.
func (s *fakeAttachSession) ReadOutput(ctx context.Context, cursor session.OutputCursor, _ int) (session.OutputChunk, error) {
	s.mu.Lock()
	data := append([]byte(nil), s.nextOutput...)
	s.nextOutput = nil
	onReadOutput := s.onReadOutput
	onOutputDelivered := s.onOutputDelivered
	readOutputErr := s.readOutputErr
	s.mu.Unlock()
	if onReadOutput != nil {
		onReadOutput()
	}
	if len(data) > 0 {
		if onOutputDelivered != nil {
			onOutputDelivered()
		}
		return session.OutputChunk{Data: data, Next: cursor + session.OutputCursor(len(data))}, nil
	}
	if readOutputErr != nil {
		return session.OutputChunk{}, readOutputErr
	}
	<-ctx.Done()
	return session.OutputChunk{}, ctx.Err()
}

// Write records local input forwarded before an escape command detaches the client.
func (s *fakeAttachSession) Write(request session.WriteRequest) (int, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.actors = append(s.actors, request.Actor)
	s.written = append(s.written, request.Data...)
	return len(request.Data), nil
}

// Close records client detach and intentionally has no backing Session to close.
func (s *fakeAttachSession) Close() error {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.closes++
	return nil
}

// writtenData snapshots the local input stream for assertions.
func (s *fakeAttachSession) writtenData() []byte {
	s.mu.Lock()
	defer s.mu.Unlock()
	return append([]byte(nil), s.written...)
}

// writeActors snapshots the sources passed by the local CLI input bridge.
func (s *fakeAttachSession) writeActors() []session.Actor {
	s.mu.Lock()
	defer s.mu.Unlock()
	return append([]session.Actor(nil), s.actors...)
}

// closeCount reports how many local detach cleanups ran.
func (s *fakeAttachSession) closeCount() int {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.closes
}

// signalBuffer records concurrent CLI output and signals when its first write
// confirms that local escape feedback is visible.
type signalBuffer struct {
	mu         sync.Mutex
	data       bytes.Buffer
	firstWrite chan struct{}
	once       sync.Once
}

func newSignalBuffer() *signalBuffer {
	return &signalBuffer{firstWrite: make(chan struct{})}
}

func (b *signalBuffer) Write(data []byte) (int, error) {
	b.mu.Lock()
	n, err := b.data.Write(data)
	b.mu.Unlock()
	b.once.Do(func() { close(b.firstWrite) })
	return n, err
}

func (b *signalBuffer) String() string {
	b.mu.Lock()
	defer b.mu.Unlock()
	return b.data.String()
}

// waitForAttachWrite waits for the input bridge to forward a complete local
// command without relying on a fixed scheduling delay.
func waitForAttachWrite(tb testing.TB, client *fakeAttachSession) []byte {
	tb.Helper()
	deadline := time.Now().Add(time.Second)
	for time.Now().Before(deadline) {
		if data := client.writtenData(); len(data) > 0 {
			return data
		}
		time.Sleep(time.Millisecond)
	}
	tb.Fatal("input bridge did not forward a Session write")
	return nil
}

// waitForSignal bounds synchronization with asynchronous input and output
// loops, so a failing test reports the missing event instead of hanging.
func waitForSignal(tb testing.TB, signal <-chan struct{}, description string) {
	tb.Helper()
	select {
	case <-signal:
	case <-time.After(time.Second):
		tb.Fatalf("did not receive %s", description)
	}
}

// decodeStructured is retained to ensure tests fail clearly if an MCP result
// changes shape while diagnosing shared cursor behavior.
func decodeStructured(t *testing.T, value any, destination any) {
	t.Helper()
	encoded, err := json.Marshal(value)
	if err != nil {
		t.Fatalf("marshal structured result: %v", err)
	}
	if err := json.Unmarshal(encoded, destination); err != nil {
		t.Fatalf("unmarshal structured result: %v", err)
	}
}
