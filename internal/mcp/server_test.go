package mcp

import (
	"context"
	"errors"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/akira-init1/ChannelTerm/internal/core/channel"
	"github.com/akira-init1/ChannelTerm/internal/core/connectionpolicy"
	"github.com/akira-init1/ChannelTerm/internal/core/device"
	"github.com/akira-init1/ChannelTerm/internal/core/session"
	"github.com/akira-init1/ChannelTerm/internal/core/tool"
	"github.com/akira-init1/ChannelTerm/internal/mcp/terminal"
	protocol "github.com/modelcontextprotocol/go-sdk/mcp"
)

func TestServerListsAndUsesTerminalTools(t *testing.T) {
	manager, registry, device := newTestRegistry(t)
	defer func() { _ = manager.Close() }()
	client, closeClient := connectTestClient(t, registry)
	defer closeClient()

	listed, err := client.ListTools(context.Background(), nil)
	if err != nil {
		t.Fatalf("ListTools() error = %v", err)
	}
	want := map[string]bool{
		"terminal_close": true, "terminal_list_serial_ports": true, "terminal_list_sessions": true,
		"terminal_open_serial": true, "terminal_read": true, "terminal_read_activity": true,
		"terminal_wait": true, "terminal_wait_activity": true, "terminal_write": true,
		"terminal_list_devices": true, "terminal_read_device_events": true, "terminal_wait_device_event": true, "terminal_get_connection_decision": true,
	}
	if len(listed.Tools) != len(want) {
		t.Fatalf("tools count = %d, want %d", len(listed.Tools), len(want))
	}
	for _, registered := range listed.Tools {
		if !want[registered.Name] {
			t.Errorf("unexpected MCP tool %q", registered.Name)
		}
		if registered.Name == "terminal_wait_device_event" && !strings.Contains(registered.Description, "terminal_get_connection_decision") {
			t.Errorf("terminal_wait_device_event description = %q, want connection-decision guidance", registered.Description)
		}
	}
	sessions := callTool(t, client, "terminal_list_sessions", map[string]any{})
	if sessions.IsError || !strings.Contains(resultText(t, sessions), "board") {
		t.Errorf("terminal_list_sessions result = %#v, want board session", sessions)
	}

	device.emit([]byte("boot> "))
	read := callTool(t, client, "terminal_read", map[string]any{"session_id": "board", "max_bytes": 128})
	if read.IsError {
		t.Fatalf("terminal_read result = %#v", read)
	}
	if got := resultString(t, read, "data"); got != "boot> " {
		t.Errorf("terminal_read data = %q, want boot prompt", got)
	}
	write := callTool(t, client, "terminal_write", map[string]any{"session_id": "board", "data": "uname -a\n"})
	if write.IsError {
		t.Fatalf("terminal_write result = %#v", write)
	}
	if got := string(device.writtenData()); got != "uname -a\n" {
		t.Errorf("written data = %q, want uname command", got)
	}
}

func TestStreamableHTTPServerListsAndUsesTerminalTools(t *testing.T) {
	manager, registry, device := newTestRegistry(t)
	defer func() { _ = manager.Close() }()
	client, closeClient := connectHTTPTestClient(t, registry)
	defer closeClient()

	for range 2 {
		listed, err := client.ListTools(context.Background(), nil)
		if err != nil {
			t.Fatalf("ListTools() error = %v", err)
		}
		want := map[string]bool{
			"terminal_close": true, "terminal_list_serial_ports": true, "terminal_list_sessions": true,
			"terminal_open_serial": true, "terminal_read": true, "terminal_read_activity": true,
			"terminal_wait": true, "terminal_wait_activity": true, "terminal_write": true,
			"terminal_list_devices": true, "terminal_read_device_events": true, "terminal_wait_device_event": true, "terminal_get_connection_decision": true,
		}
		if len(listed.Tools) != len(want) {
			t.Fatalf("tools count = %d, want %d", len(listed.Tools), len(want))
		}
		for _, registered := range listed.Tools {
			if !want[registered.Name] {
				t.Errorf("unexpected MCP tool %q", registered.Name)
			}
		}
	}

	device.emit([]byte("boot> "))
	read := callTool(t, client, "terminal_read", map[string]any{"session_id": "board", "max_bytes": 128})
	if read.IsError || resultString(t, read, "data") != "boot> " {
		t.Errorf("terminal_read result = %#v, want HTTP call through the shared Registry", read)
	}
	write := callTool(t, client, "terminal_write", map[string]any{"session_id": "board", "data": "uname -a\n"})
	if write.IsError {
		t.Fatalf("terminal_write result = %#v", write)
	}
	if got := string(device.writtenData()); got != "uname -a\n" {
		t.Errorf("written data = %q, want uname command", got)
	}
}

func TestStreamableHTTPWaitCancelsAndAllowsReconnect(t *testing.T) {
	manager, registry, device := newTestRegistry(t)
	defer func() { _ = manager.Close() }()
	client, closeClient := connectHTTPTestClient(t, registry)
	defer closeClient()

	device.emit([]byte("ready"))
	read := callTool(t, client, "terminal_read", map[string]any{"session_id": "board"})
	cursor := resultNumber(t, read, "next")
	returned := make(chan *protocol.CallToolResult, 1)
	returnError := make(chan error, 1)
	go func() {
		result, err := client.CallTool(context.Background(), &protocol.CallToolParams{Name: "terminal_wait", Arguments: map[string]any{"session_id": "board", "cursor": cursor}})
		if err != nil {
			returnError <- err
			return
		}
		returned <- result
	}()
	time.Sleep(20 * time.Millisecond)
	device.emit([]byte("\nlogin: "))
	select {
	case err := <-returnError:
		t.Fatalf("terminal_wait error = %v", err)
	case result := <-returned:
		if result.IsError || resultString(t, result, "data") != "\nlogin: " {
			t.Errorf("terminal_wait result = %#v, want new terminal output", result)
		}
	case <-time.After(time.Second):
		t.Fatal("terminal_wait did not return after new HTTP output")
	}
	cursor += float64(len("\nlogin: "))

	ctx, cancel := context.WithCancel(context.Background())
	waitDone := make(chan error, 1)
	go func() {
		_, err := client.CallTool(ctx, &protocol.CallToolParams{Name: "terminal_wait", Arguments: map[string]any{"session_id": "board", "cursor": cursor}})
		waitDone <- err
	}()
	time.Sleep(20 * time.Millisecond)
	cancel()
	select {
	case err := <-waitDone:
		if !errors.Is(err, context.Canceled) {
			t.Errorf("terminal_wait cancellation error = %v, want context.Canceled", err)
		}
	case <-time.After(time.Second):
		t.Fatal("terminal_wait remained blocked after HTTP client cancellation")
	}

	if err := client.Close(); err != nil {
		t.Fatalf("HTTP client Close() error = %v", err)
	}
	reconnected, closeReconnected := connectHTTPTestClient(t, registry)
	defer closeReconnected()
	listed, err := reconnected.ListTools(context.Background(), nil)
	if err != nil || len(listed.Tools) != 13 {
		t.Errorf("ListTools() after reconnect = %#v, %v; want thirteen tools", listed, err)
	}
	if terminal, ok := manager.Get("board"); !ok || terminal.State() != session.StateOpen {
		t.Errorf("session after HTTP disconnect = %v, registered = %t; want open managed session", terminal, ok)
	}
}

func TestStreamableHTTPReportsInvalidAndClosedSessionsWithoutPanic(t *testing.T) {
	manager, registry, _ := newTestRegistry(t)
	defer func() { _ = manager.Close() }()
	client, closeClient := connectHTTPTestClient(t, registry)
	defer closeClient()

	missing := callTool(t, client, "terminal_write", map[string]any{"session_id": "missing", "data": "status"})
	if !missing.IsError || !strings.Contains(errorText(t, missing), "session not found") {
		t.Errorf("missing session result = %#v, want readable not-found error", missing)
	}
	invalid := callTool(t, client, "terminal_write", map[string]any{"session_id": "board", "data": "status", "unexpected": true})
	if !invalid.IsError || !strings.Contains(errorText(t, invalid), "decode tool input") {
		t.Errorf("invalid parameter result = %#v, want readable validation error", invalid)
	}
	terminal, ok := manager.Get("board")
	if !ok {
		t.Fatal("manager lost board session")
	}
	if err := terminal.Close(); err != nil {
		t.Fatalf("Close() error = %v", err)
	}
	closed := callTool(t, client, "terminal_read", map[string]any{"session_id": "board"})
	if !closed.IsError || !strings.Contains(errorText(t, closed), "session is not open") {
		t.Errorf("closed session result = %#v, want readable closed-session error", closed)
	}
}

func TestServerWaitReturnsNewOutputAndSupportsTimeout(t *testing.T) {
	manager, registry, device := newTestRegistry(t)
	defer func() { _ = manager.Close() }()
	client, closeClient := connectTestClient(t, registry)
	defer closeClient()

	device.emit([]byte("ready"))
	read := callTool(t, client, "terminal_read", map[string]any{"session_id": "board"})
	cursor := resultNumber(t, read, "next")

	timedOut, err := client.CallTool(context.Background(), &protocol.CallToolParams{Name: "terminal_wait", Arguments: map[string]any{"session_id": "board", "cursor": cursor, "timeout_ms": 40}})
	if err != nil && !errors.Is(err, context.DeadlineExceeded) {
		t.Fatalf("terminal_wait timeout error = %v", err)
	}
	if err == nil && (!timedOut.IsError || !strings.Contains(errorText(t, timedOut), "deadline exceeded")) {
		t.Errorf("terminal_wait timeout result = %#v, want readable deadline error", timedOut)
	}

	waitResult := make(chan *protocol.CallToolResult, 1)
	waitError := make(chan error, 1)
	go func() {
		result, err := client.CallTool(context.Background(), &protocol.CallToolParams{Name: "terminal_wait", Arguments: map[string]any{"session_id": "board", "cursor": cursor}})
		if err != nil {
			waitError <- err
			return
		}
		waitResult <- result
	}()
	device.emit([]byte("\nlogin: "))
	select {
	case err := <-waitError:
		t.Fatalf("terminal_wait error = %v", err)
	case result := <-waitResult:
		if result.IsError || resultString(t, result, "data") != "\nlogin: " {
			t.Errorf("terminal_wait result = %#v, want new terminal output", result)
		}
	case <-time.After(time.Second):
		t.Fatal("terminal_wait did not return after new output")
	}

	cancelled, cancelCall := context.WithCancel(context.Background())
	cancelCall()
	_, err = client.CallTool(cancelled, &protocol.CallToolParams{Name: "terminal_wait", Arguments: map[string]any{"session_id": "board", "cursor": cursor}})
	if !errors.Is(err, context.Canceled) {
		t.Errorf("terminal_wait cancelled call error = %v, want context.Canceled", err)
	}
}

func TestServerWaitActivityReturnsUserWrite(t *testing.T) {
	manager, registry, _ := newTestRegistry(t)
	defer func() { _ = manager.Close() }()
	client, closeClient := connectTestClient(t, registry)
	defer closeClient()

	initial := callTool(t, client, "terminal_read_activity", map[string]any{"session_id": "board"})
	if initial.IsError {
		t.Fatalf("terminal_read_activity result = %#v", initial)
	}
	cursor := resultNumber(t, initial, "next")
	returned := make(chan *protocol.CallToolResult, 1)
	returnError := make(chan error, 1)
	go func() {
		result, err := client.CallTool(context.Background(), &protocol.CallToolParams{Name: "terminal_wait_activity", Arguments: map[string]any{"session_id": "board", "cursor": cursor}})
		if err != nil {
			returnError <- err
			return
		}
		returned <- result
	}()
	time.Sleep(20 * time.Millisecond)
	write := callTool(t, client, "terminal_write", map[string]any{"session_id": "board", "data": "ls", "actor": "user"})
	if write.IsError {
		t.Fatalf("terminal_write result = %#v", write)
	}
	select {
	case err := <-returnError:
		t.Fatalf("terminal_wait_activity error = %v", err)
	case result := <-returned:
		text := resultText(t, result)
		if result.IsError || !strings.Contains(text, `"actor":"user"`) || !strings.Contains(text, `"operation":"write"`) || !strings.Contains(text, `"data":"bHM="`) {
			t.Errorf("terminal_wait_activity result = %#v, want user write event for ls", result)
		}
	case <-time.After(time.Second):
		t.Fatal("terminal_wait_activity did not return after user write")
	}
}

func TestServerWaitDeviceEventDoesNotCreateSession(t *testing.T) {
	manager := session.NewManager()
	defer func() { _ = manager.Close() }()
	scanner := &mutableDeviceScanner{}
	devices, err := device.NewRegistry(scanner)
	if err != nil {
		t.Fatalf("NewRegistry() error = %v", err)
	}
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	defer devices.Close()
	if err := devices.Start(ctx); err != nil {
		t.Fatalf("Device Registry Start() error = %v", err)
	}

	registry := tool.NewRegistry()
	serialTools, err := terminal.NewSerialTools(manager)
	if err != nil {
		t.Fatalf("NewSerialTools() error = %v", err)
	}
	deviceTools, err := terminal.NewDeviceTools(devices)
	if err != nil {
		t.Fatalf("NewDeviceTools() error = %v", err)
	}
	decisionTools, err := terminal.NewConnectionDecisionTools(manager, devices, connectionpolicy.PolicyAsk)
	if err != nil {
		t.Fatalf("NewConnectionDecisionTools() error = %v", err)
	}
	for _, registered := range append(append(serialTools, deviceTools...), decisionTools...) {
		if err := registry.Register(registered); err != nil {
			t.Fatalf("Register(%q) error = %v", registered.Name(), err)
		}
	}
	client, closeClient := connectTestClient(t, registry)
	defer closeClient()

	returned := make(chan *protocol.CallToolResult, 1)
	returnedError := make(chan error, 1)
	go func() {
		result, err := client.CallTool(context.Background(), &protocol.CallToolParams{Name: "terminal_wait_device_event", Arguments: map[string]any{"cursor": 0}})
		if err != nil {
			returnedError <- err
			return
		}
		returned <- result
	}()
	scanner.set([]device.Endpoint{{Transport: "serial", Endpoint: "COM11"}})
	select {
	case err := <-returnedError:
		t.Fatalf("terminal_wait_device_event error = %v", err)
	case result := <-returned:
		text := resultText(t, result)
		if result.IsError || !strings.Contains(text, `"type":"appeared"`) || !strings.Contains(text, `"transport":"serial"`) || !strings.Contains(text, `"endpoint":"COM11"`) {
			t.Errorf("terminal_wait_device_event result = %#v, want COM11 appeared", result)
		}
	case <-time.After(2 * time.Second):
		t.Fatal("terminal_wait_device_event did not return after device appeared")
	}
	if sessions := callTool(t, client, "terminal_list_sessions", map[string]any{}); sessions.IsError || !strings.Contains(resultText(t, sessions), `"sessions":[]`) {
		t.Errorf("terminal_list_sessions after discovery = %#v, want no automatically opened session", sessions)
	}
	decision := callTool(t, client, "terminal_get_connection_decision", map[string]any{"transport": "serial", "endpoint": "COM11"})
	if decision.IsError || !strings.Contains(resultText(t, decision), `"present":true`) || !strings.Contains(resultText(t, decision), `"policy":"ask"`) || !strings.Contains(resultText(t, decision), `"action":"ask"`) {
		t.Errorf("terminal_get_connection_decision after discovery = %#v, want ask without opening a session", decision)
	}
}

func TestServerReportsInvalidAndClosedSessionsWithoutPanic(t *testing.T) {
	manager, registry, _ := newTestRegistry(t)
	defer func() { _ = manager.Close() }()
	client, closeClient := connectTestClient(t, registry)
	defer closeClient()

	missing := callTool(t, client, "terminal_write", map[string]any{"session_id": "missing", "data": "status"})
	if !missing.IsError || !strings.Contains(errorText(t, missing), "session not found") {
		t.Errorf("missing session result = %#v, want readable not-found error", missing)
	}
	terminal, ok := manager.Get("board")
	if !ok {
		t.Fatal("manager lost board session")
	}
	if err := terminal.Close(); err != nil {
		t.Fatalf("Close() error = %v", err)
	}
	closed := callTool(t, client, "terminal_read", map[string]any{"session_id": "board"})
	if !closed.IsError || !strings.Contains(errorText(t, closed), "session is not open") {
		t.Errorf("closed session result = %#v, want readable closed-session error", closed)
	}
}

func TestRunClientDisconnectKeepsSessionOwnedByManager(t *testing.T) {
	manager, registry, _ := newTestRegistry(t)
	defer func() { _ = manager.Close() }()
	serverTransport, clientTransport := protocol.NewInMemoryTransports()
	runDone := make(chan error, 1)
	go func() { runDone <- Run(context.Background(), registry, serverTransport) }()

	client := protocol.NewClient(&protocol.Implementation{Name: "test-client", Version: "1.0"}, nil)
	clientSession, err := client.Connect(context.Background(), clientTransport, nil)
	if err != nil {
		t.Fatalf("Connect() error = %v", err)
	}
	if err := clientSession.Close(); err != nil {
		t.Fatalf("client Close() error = %v", err)
	}
	select {
	case err := <-runDone:
		if err != nil {
			t.Errorf("Run() after client disconnect = %v, want nil", err)
		}
	case <-time.After(time.Second):
		t.Fatal("Run() did not finish after client disconnect")
	}
	if terminal, ok := manager.Get("board"); !ok || terminal.State() != session.StateOpen {
		t.Errorf("session after MCP disconnect = %v, registered = %t; want open managed session", terminal, ok)
	}
}

func newTestRegistry(t *testing.T) (*session.Manager, *tool.Registry, *fakeTransport) {
	t.Helper()
	manager := session.NewManager()
	transport := newFakeTransport()
	core, err := session.New("board", transport, session.WithReceiveBufferCapacity(1024))
	if err != nil {
		t.Fatalf("session.New() error = %v", err)
	}
	if err := core.Connect(context.Background()); err != nil {
		t.Fatalf("Connect() error = %v", err)
	}
	if err := manager.Register(core); err != nil {
		t.Fatalf("Register() error = %v", err)
	}

	registry := tool.NewRegistry()
	registered, err := terminal.NewSerialTools(manager)
	if err != nil {
		t.Fatalf("NewSerialTools() error = %v", err)
	}
	for _, current := range registered {
		if err := registry.Register(current); err != nil {
			t.Fatalf("Register(%q) error = %v", current.Name(), err)
		}
	}
	devices, err := device.NewRegistry(device.ScannerFunc(func(context.Context) ([]device.Endpoint, error) {
		return nil, nil
	}))
	if err != nil {
		t.Fatalf("NewRegistry() error = %v", err)
	}
	t.Cleanup(devices.Close)
	deviceTools, err := terminal.NewDeviceTools(devices)
	if err != nil {
		t.Fatalf("NewDeviceTools() error = %v", err)
	}
	for _, current := range deviceTools {
		if err := registry.Register(current); err != nil {
			t.Fatalf("Register(%q) error = %v", current.Name(), err)
		}
	}
	decisionTools, err := terminal.NewConnectionDecisionTools(manager, devices, connectionpolicy.PolicyAsk)
	if err != nil {
		t.Fatalf("NewConnectionDecisionTools() error = %v", err)
	}
	for _, current := range decisionTools {
		if err := registry.Register(current); err != nil {
			t.Fatalf("Register(%q) error = %v", current.Name(), err)
		}
	}
	return manager, registry, transport
}

// mutableDeviceScanner lets the device-event MCP test simulate a physical
// endpoint appearing without opening the endpoint or creating a Session.
type mutableDeviceScanner struct {
	mu        sync.Mutex
	endpoints []device.Endpoint
}

// Scan returns a caller-owned endpoint snapshot for one Registry scan.
func (s *mutableDeviceScanner) Scan(context.Context) ([]device.Endpoint, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	return append([]device.Endpoint(nil), s.endpoints...), nil
}

// set replaces the next scan snapshot while retaining caller ownership.
func (s *mutableDeviceScanner) set(endpoints []device.Endpoint) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.endpoints = append([]device.Endpoint(nil), endpoints...)
}

func connectTestClient(t *testing.T, registry *tool.Registry) (*protocol.ClientSession, func()) {
	t.Helper()
	server, err := NewServer(registry)
	if err != nil {
		t.Fatalf("NewServer() error = %v", err)
	}
	serverTransport, clientTransport := protocol.NewInMemoryTransports()
	serverSession, err := server.Connect(context.Background(), serverTransport, nil)
	if err != nil {
		t.Fatalf("server Connect() error = %v", err)
	}
	client := protocol.NewClient(&protocol.Implementation{Name: "test-client", Version: "1.0"}, nil)
	clientSession, err := client.Connect(context.Background(), clientTransport, nil)
	if err != nil {
		_ = serverSession.Close()
		t.Fatalf("client Connect() error = %v", err)
	}
	return clientSession, func() {
		_ = clientSession.Close()
		_ = serverSession.Close()
	}
}

// connectHTTPTestClient serves the production handler at /mcp and connects the
// official Streamable HTTP client, exercising the complete network protocol.
func connectHTTPTestClient(t *testing.T, registry *tool.Registry) (*protocol.ClientSession, func()) {
	t.Helper()
	handler, err := NewStreamableHTTPHandler(registry)
	if err != nil {
		t.Fatalf("NewStreamableHTTPHandler() error = %v", err)
	}
	mux := http.NewServeMux()
	mux.Handle("/mcp", handler)
	httpServer := httptest.NewServer(mux)
	client := protocol.NewClient(&protocol.Implementation{Name: "test-client", Version: "1.0"}, nil)
	clientSession, err := client.Connect(context.Background(), &protocol.StreamableClientTransport{Endpoint: httpServer.URL + "/mcp"}, nil)
	if err != nil {
		httpServer.Close()
		t.Fatalf("Connect() error = %v", err)
	}
	return clientSession, func() {
		_ = clientSession.Close()
		httpServer.Close()
	}
}

func callTool(t *testing.T, client *protocol.ClientSession, name string, arguments map[string]any) *protocol.CallToolResult {
	t.Helper()
	result, err := client.CallTool(context.Background(), &protocol.CallToolParams{Name: name, Arguments: arguments})
	if err != nil {
		t.Fatalf("%s CallTool() error = %v", name, err)
	}
	return result
}

func resultString(t *testing.T, result *protocol.CallToolResult, key string) string {
	t.Helper()
	value, ok := result.StructuredContent.(map[string]any)[key].(string)
	if !ok {
		t.Fatalf("structured result %q = %#v, want string", key, result.StructuredContent)
	}
	return value
}

func resultNumber(t *testing.T, result *protocol.CallToolResult, key string) float64 {
	t.Helper()
	value, ok := result.StructuredContent.(map[string]any)[key].(float64)
	if !ok {
		t.Fatalf("structured result %q = %#v, want number", key, result.StructuredContent)
	}
	return value
}

func errorText(t *testing.T, result *protocol.CallToolResult) string {
	t.Helper()
	if len(result.Content) != 1 {
		t.Fatalf("error content = %#v, want one text item", result.Content)
	}
	text, ok := result.Content[0].(*protocol.TextContent)
	if !ok {
		t.Fatalf("error content type = %T, want *TextContent", result.Content[0])
	}
	return text.Text
}

func resultText(t *testing.T, result *protocol.CallToolResult) string {
	t.Helper()
	if len(result.Content) != 1 {
		t.Fatalf("result content = %#v, want one text item", result.Content)
	}
	text, ok := result.Content[0].(*protocol.TextContent)
	if !ok {
		t.Fatalf("result content type = %T, want *TextContent", result.Content[0])
	}
	return text.Text
}

// fakeTransport gives the real Session Core deterministic input and records
// writes, so MCP tests never open a physical serial port.
type fakeTransport struct {
	mu     sync.Mutex
	output chan []byte
	closed chan struct{}
	writes []byte
	once   sync.Once
}

func newFakeTransport() *fakeTransport {
	return &fakeTransport{output: make(chan []byte, 8), closed: make(chan struct{})}
}

func (t *fakeTransport) Connect(context.Context) (channel.Channel, error) { return t, nil }

func (t *fakeTransport) Read(buffer []byte) (int, error) {
	select {
	case data := <-t.output:
		return copy(buffer, data), nil
	case <-t.closed:
		return 0, io.EOF
	}
}

func (t *fakeTransport) Write(data []byte) (int, error) {
	t.mu.Lock()
	defer t.mu.Unlock()
	t.writes = append(t.writes, data...)
	return len(data), nil
}

func (*fakeTransport) Resize(uint16, uint16) error { return nil }
func (*fakeTransport) State() channel.State        { return channel.StateOpen }

func (t *fakeTransport) Close() error {
	t.once.Do(func() { close(t.closed) })
	return nil
}

func (t *fakeTransport) emit(data []byte) { t.output <- append([]byte(nil), data...) }

func (t *fakeTransport) writtenData() []byte {
	t.mu.Lock()
	defer t.mu.Unlock()
	return append([]byte(nil), t.writes...)
}
