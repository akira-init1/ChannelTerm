package mcp

import (
	"context"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"net/http/httptest"
	"reflect"
	"strings"
	"sync"
	"testing"
	"time"
	"unicode/utf8"

	"github.com/akira-init1/ChannelTerm/internal/core/channel"
	"github.com/akira-init1/ChannelTerm/internal/core/connectionpolicy"
	"github.com/akira-init1/ChannelTerm/internal/core/device"
	"github.com/akira-init1/ChannelTerm/internal/core/session"
	"github.com/akira-init1/ChannelTerm/internal/core/tool"
	"github.com/akira-init1/ChannelTerm/internal/mcp/terminal"
	"github.com/google/jsonschema-go/jsonschema"
	protocol "github.com/modelcontextprotocol/go-sdk/mcp"
)

func TestServerInstructionsCoverRoutingAndSafety(t *testing.T) {
	if characters := utf8.RuneCountInString(serverInstructions); characters > 512 {
		t.Errorf("serverInstructions length = %d characters, want no more than 512", characters)
	}
	for _, required := range []string{
		"cterm means ChannelTerm",
		"terminal_list_sessions",
		"terminal_exec",
		"terminal_write",
		"terminal_wait_file_transfer",
		"For discovered devices",
		"terminal_get_connection_decision",
		"ask requires approval",
		"terminal_close closes the Session for all clients",
		"Lease and file-transfer controls are not for normal use",
	} {
		if !strings.Contains(serverInstructions, required) {
			t.Errorf("serverInstructions = %q, want guidance containing %q", serverInstructions, required)
		}
	}
}

func TestServerListsAndUsesTerminalTools(t *testing.T) {
	manager, registry, device := newTestRegistry(t)
	defer func() { _ = manager.Close() }()
	client, closeClient := connectTestClient(t, registry)
	defer closeClient()
	initialized := client.InitializeResult()
	if initialized == nil || initialized.ServerInfo == nil || initialized.ServerInfo.Version != "test" {
		t.Fatalf("InitializeResult server info = %#v, want injected test version", initialized)
	}
	if initialized.Instructions != serverInstructions {
		t.Errorf("InitializeResult instructions = %q, want %q", initialized.Instructions, serverInstructions)
	}

	listed, err := client.ListTools(context.Background(), nil)
	if err != nil {
		t.Fatalf("ListTools() error = %v", err)
	}
	want := map[string]bool{
		"terminal_close": true, "terminal_list_serial_ports": true, "terminal_list_sessions": true,
		"terminal_open_serial": true, "terminal_read": true, "terminal_read_activity": true,
		"terminal_session_events": true, "terminal_wait_file_transfer": true, "terminal_session_attach": true, "terminal_session_detach": true, "terminal_report_file_transfer": true,
		"terminal_wait": true, "terminal_wait_activity": true, "terminal_exec": true, "terminal_write": true,
		"terminal_write_leased": true, "terminal_acquire_lease": true, "terminal_renew_lease": true, "terminal_release_lease": true,
		"terminal_begin_file_transfer_cancel": true, "terminal_resolve_file_transfer_cancel": true, "terminal_file_transfer_checkpoint": true,
		"terminal_list_devices": true, "terminal_read_device_events": true, "terminal_wait_device_event": true, "terminal_get_connection_decision": true,
	}
	wantOutputProperty := map[string]string{
		"terminal_close":                        "closed",
		"terminal_list_serial_ports":            "ports",
		"terminal_list_sessions":                "sessions",
		"terminal_open_serial":                  "session_id",
		"terminal_read":                         "data",
		"terminal_read_activity":                "events",
		"terminal_session_events":               "events",
		"terminal_wait_file_transfer":           "state",
		"terminal_session_attach":               "attached",
		"terminal_session_detach":               "detached",
		"terminal_report_file_transfer":         "published",
		"terminal_wait":                         "data",
		"terminal_wait_activity":                "events",
		"terminal_exec":                         "command_id",
		"terminal_write":                        "bytes_written",
		"terminal_write_leased":                 "bytes_written",
		"terminal_acquire_lease":                "expires_at",
		"terminal_renew_lease":                  "expires_at",
		"terminal_release_lease":                "released",
		"terminal_begin_file_transfer_cancel":   "request_id",
		"terminal_resolve_file_transfer_cancel": "percent",
		"terminal_file_transfer_checkpoint":     "action",
		"terminal_list_devices":                 "devices",
		"terminal_read_device_events":           "events",
		"terminal_wait_device_event":            "events",
		"terminal_get_connection_decision":      "action",
	}
	wantTitle := map[string]string{
		"terminal_close":                        "Close Terminal Session",
		"terminal_list_serial_ports":            "List Serial Ports",
		"terminal_list_sessions":                "List Terminal Sessions",
		"terminal_open_serial":                  "Open Serial Session",
		"terminal_read":                         "Read Terminal Output",
		"terminal_read_activity":                "Read Session Activity",
		"terminal_session_events":               "Read Session Events",
		"terminal_wait_file_transfer":           "Wait for File Transfer Result",
		"terminal_session_attach":               "Attach to Terminal Session",
		"terminal_session_detach":               "Detach from Terminal Session",
		"terminal_report_file_transfer":         "Report File Transfer Event",
		"terminal_wait":                         "Wait for Terminal Output",
		"terminal_wait_activity":                "Wait for Session Activity",
		"terminal_exec":                         "Execute Terminal Command",
		"terminal_write":                        "Write Raw Terminal Input",
		"terminal_write_leased":                 "Write Leased Terminal Input",
		"terminal_acquire_lease":                "Acquire Session Lease",
		"terminal_renew_lease":                  "Renew Session Lease",
		"terminal_release_lease":                "Release Session Lease",
		"terminal_begin_file_transfer_cancel":   "Begin File Transfer Cancellation",
		"terminal_resolve_file_transfer_cancel": "Resolve File Transfer Cancellation",
		"terminal_file_transfer_checkpoint":     "Check File Transfer Control",
		"terminal_list_devices":                 "List Discovered Devices",
		"terminal_read_device_events":           "Read Device Events",
		"terminal_wait_device_event":            "Wait for Device Event",
		"terminal_get_connection_decision":      "Get Connection Decision",
	}
	readOnlyAnnotations := map[string]bool{
		"terminal_list_sessions": true, "terminal_read": true, "terminal_read_activity": true,
		"terminal_session_events": true, "terminal_wait_file_transfer": true,
		"terminal_wait": true, "terminal_wait_activity": true,
		"terminal_file_transfer_checkpoint": true, "terminal_list_serial_ports": true,
		"terminal_list_devices": true, "terminal_read_device_events": true,
		"terminal_wait_device_event": true, "terminal_get_connection_decision": true,
	}
	destructiveAnnotations := map[string]bool{
		"terminal_open_serial": true, "terminal_exec": true, "terminal_write": true, "terminal_write_leased": true,
		"terminal_resolve_file_transfer_cancel": true, "terminal_release_lease": true,
		"terminal_close": true,
	}
	openWorldAnnotations := map[string]bool{
		"terminal_open_serial": true, "terminal_exec": true, "terminal_write": true, "terminal_write_leased": true,
	}
	if len(listed.Tools) != len(want) {
		t.Fatalf("tools count = %d, want %d", len(listed.Tools), len(want))
	}
	seenTitles := make(map[string]string, len(listed.Tools))
	for _, registered := range listed.Tools {
		if !want[registered.Name] {
			t.Errorf("unexpected MCP tool %q", registered.Name)
		}
		if registered.Name == "terminal_wait_device_event" && !strings.Contains(registered.Description, "terminal_get_connection_decision") {
			t.Errorf("terminal_wait_device_event description = %q, want connection-decision guidance", registered.Description)
		}
		if registered.Name == "terminal_wait" && !strings.Contains(registered.Description, "terminal_wait_file_transfer") {
			t.Errorf("terminal_wait description = %q, want file-transfer wait guidance", registered.Description)
		}
		assertOutputSchema(t, registered, wantOutputProperty[registered.Name])
		assertToolAnnotations(t, registered, readOnlyAnnotations[registered.Name], destructiveAnnotations[registered.Name], openWorldAnnotations[registered.Name], registered.Name == "terminal_release_lease")
		if registered.Title != wantTitle[registered.Name] {
			t.Errorf("%s title = %q, want %q", registered.Name, registered.Title, wantTitle[registered.Name])
		}
		if previous, duplicate := seenTitles[registered.Title]; duplicate {
			t.Errorf("%s and %s share title %q", previous, registered.Name, registered.Title)
		}
		seenTitles[registered.Title] = registered.Name
	}
	sessions := callTool(t, client, "terminal_list_sessions", map[string]any{})
	if sessions.IsError || !strings.Contains(resultText(t, sessions), "board") {
		t.Errorf("terminal_list_sessions result = %#v, want board session", sessions)
	}

	device.emit([]byte("boot> "))
	read := callTool(t, client, "terminal_read", map[string]any{"session_id": "board", "cursor": 0, "max_bytes": 128, "timeout_ms": 1000})
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

func assertToolAnnotations(t *testing.T, registered *protocol.Tool, readOnly, destructive, openWorld, idempotent bool) {
	t.Helper()
	annotations := registered.Annotations
	if annotations == nil {
		t.Errorf("%s annotations = nil", registered.Name)
		return
	}
	if annotations.ReadOnlyHint != readOnly {
		t.Errorf("%s readOnlyHint = %t, want %t", registered.Name, annotations.ReadOnlyHint, readOnly)
	}
	if readOnly {
		if annotations.DestructiveHint != nil {
			t.Errorf("%s destructiveHint = %t, want omitted for read-only tool", registered.Name, *annotations.DestructiveHint)
		}
	} else if annotations.DestructiveHint == nil || *annotations.DestructiveHint != destructive {
		t.Errorf("%s destructiveHint = %v, want %t", registered.Name, annotations.DestructiveHint, destructive)
	}
	if annotations.OpenWorldHint == nil || *annotations.OpenWorldHint != openWorld {
		t.Errorf("%s openWorldHint = %v, want %t", registered.Name, annotations.OpenWorldHint, openWorld)
	}
	if annotations.IdempotentHint != idempotent {
		t.Errorf("%s idempotentHint = %t, want %t", registered.Name, annotations.IdempotentHint, idempotent)
	}
}

func assertOutputSchema(t *testing.T, registered *protocol.Tool, expectedProperty string) {
	t.Helper()
	resolveOutputSchema(t, registered.Name, registered.OutputSchema)
	schema, ok := registered.OutputSchema.(map[string]any)
	if !ok {
		t.Errorf("%s output schema = %#v, want JSON object schema", registered.Name, registered.OutputSchema)
		return
	}
	if schema["type"] != "object" {
		t.Errorf("%s output schema type = %#v, want object", registered.Name, schema["type"])
	}
	properties, ok := schema["properties"].(map[string]any)
	if !ok {
		t.Errorf("%s output schema properties = %#v, want object", registered.Name, schema["properties"])
		return
	}
	if _, ok := properties[expectedProperty]; !ok {
		t.Errorf("%s output schema properties = %#v, want %q", registered.Name, properties, expectedProperty)
	}
	if !schemaListContains(schema["required"], expectedProperty) {
		t.Errorf("%s output schema required = %#v, want %q", registered.Name, schema["required"], expectedProperty)
	}
}

func schemaListContains(value any, expected string) bool {
	switch values := value.(type) {
	case []any:
		for _, value := range values {
			if value == expected {
				return true
			}
		}
	case []string:
		for _, value := range values {
			if value == expected {
				return true
			}
		}
	}
	return false
}

func TestTerminalOutputSchemaAliasesMatch(t *testing.T) {
	for _, names := range [][2]string{
		{"terminal_read", "terminal_wait"},
		{"terminal_read_activity", "terminal_wait_activity"},
		{"terminal_read_device_events", "terminal_wait_device_event"},
	} {
		left, leftOK := terminalOutputSchema(names[0])
		right, rightOK := terminalOutputSchema(names[1])
		if !leftOK || !rightOK {
			t.Fatalf("terminalOutputSchema(%q, %q) registered = %t, %t; want both", names[0], names[1], leftOK, rightOK)
		}
		if !reflect.DeepEqual(left, right) {
			t.Errorf("output schemas for %q and %q differ", names[0], names[1])
		}
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
			"terminal_session_events": true, "terminal_wait_file_transfer": true, "terminal_session_attach": true, "terminal_session_detach": true, "terminal_report_file_transfer": true,
			"terminal_wait": true, "terminal_wait_activity": true, "terminal_exec": true, "terminal_write": true,
			"terminal_write_leased": true, "terminal_acquire_lease": true, "terminal_renew_lease": true, "terminal_release_lease": true,
			"terminal_begin_file_transfer_cancel": true, "terminal_resolve_file_transfer_cancel": true, "terminal_file_transfer_checkpoint": true,
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
	read := callTool(t, client, "terminal_read", map[string]any{"session_id": "board", "cursor": 0, "max_bytes": 128, "timeout_ms": 1000})
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

// TestStreamableHTTPWaitCancellationKeepsClientUsable verifies an HTTP
// cancellation notification does not poison later calls on the same client.
func TestStreamableHTTPWaitCancellationKeepsClientUsable(t *testing.T) {
	manager, registry, device := newTestRegistry(t)
	defer func() { _ = manager.Close() }()
	client, closeClient := connectHTTPTestClient(t, registry)
	defer closeClient()

	device.emit([]byte("ready"))
	read := callTool(t, client, "terminal_read", map[string]any{"session_id": "board", "cursor": 0, "timeout_ms": 1000})
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
		assertStructuredResultMatchesOutputSchema(t, "terminal_wait", result)
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

	write, err := client.CallTool(context.Background(), &protocol.CallToolParams{Name: "terminal_write", Arguments: map[string]any{"session_id": "board", "data": "still connected\n"}})
	if err != nil || write.IsError {
		t.Fatalf("terminal_write after cancelled wait = %#v, %v; want success on the same client", write, err)
	}
	assertStructuredResultMatchesOutputSchema(t, "terminal_write", write)
	if got := string(device.writtenData()); got != "still connected\n" {
		t.Errorf("device input = %q, want connection to remain usable", got)
	}
	if terminal, ok := manager.Get("board"); !ok || terminal.State() != session.StateOpen {
		t.Errorf("session after HTTP cancellation = %v, registered = %t; want open managed session", terminal, ok)
	}
}

func TestStreamableHTTPWaitFileTransferReturnsCancellationResult(t *testing.T) {
	manager, registry, _ := newTestRegistry(t)
	defer func() { _ = manager.Close() }()
	client, closeClient := connectHTTPTestClient(t, registry)
	defer closeClient()

	events := callTool(t, client, "terminal_session_events", map[string]any{"session_id": "board"})
	cursor := resultNumber(t, events, "next")
	type callResult struct {
		result *protocol.CallToolResult
		err    error
	}
	returned := make(chan callResult, 1)
	go func() {
		result, err := client.CallTool(context.Background(), &protocol.CallToolParams{Name: "terminal_wait_file_transfer", Arguments: map[string]any{
			"session_id": "board", "transfer_id": "FT-cancel", "cursor": cursor, "timeout_ms": 1000,
		}})
		returned <- callResult{result: result, err: err}
	}()
	terminal, ok := manager.Get("board")
	if !ok {
		t.Fatal("managed Session board was not found")
	}
	terminal.PublishEvent(session.Event{Type: session.EventFileTransferProgress, Metadata: map[string]any{"sent": 32768, "total": 98304, "percent": 33.3}})
	select {
	case result := <-returned:
		t.Fatalf("file-transfer wait returned for progress: %#v, %v", result.result, result.err)
	case <-time.After(20 * time.Millisecond):
	}
	terminal.PublishEvent(session.Event{Type: session.EventFileTransferCancelled, Metadata: map[string]any{
		"transfer_id": "FT-cancel", "reason": "user_cancelled", "transferred": 32768, "total": 98304, "percent": 33.3, "lease_released": true,
	}})

	select {
	case result := <-returned:
		if result.err != nil || result.result == nil || result.result.IsError {
			t.Fatalf("terminal_wait_file_transfer result = %#v, %v", result.result, result.err)
		}
		assertStructuredResultMatchesOutputSchema(t, "terminal_wait_file_transfer", result.result)
		if state := resultString(t, result.result, "state"); state != "cancelled" {
			t.Fatalf("terminal_wait_file_transfer state = %q, want cancelled", state)
		}
		structured := result.result.StructuredContent.(map[string]any)
		event, ok := structured["event"].(map[string]any)
		if !ok || event["type"] != string(session.EventFileTransferCancelled) {
			t.Fatalf("terminal_wait_file_transfer event = %#v, want FILE_TRANSFER_CANCELLED", structured["event"])
		}
	case <-time.After(time.Second):
		t.Fatal("terminal_wait_file_transfer did not return after cancellation")
	}
}

func TestStreamableHTTPWaitFileTransferReturnsResolvedSendPath(t *testing.T) {
	manager, registry, _ := newTestRegistry(t)
	defer func() { _ = manager.Close() }()
	client, closeClient := connectHTTPTestClient(t, registry)
	defer closeClient()

	events := callTool(t, client, "terminal_session_events", map[string]any{"session_id": "board"})
	cursor := resultNumber(t, events, "next")
	terminal, ok := manager.Get("board")
	if !ok {
		t.Fatal("managed Session board was not found")
	}
	terminal.PublishEvent(session.Event{Type: session.EventFileTransferCompleted, Metadata: map[string]any{
		"transfer_id":    "FT-send",
		"source_path":    "app.bin",
		"requested_path": "/tmp/cterm/mcp-files/app.bin",
		"resolved_path":  "/tmp/cterm/mcp-files/app_1.bin",
		"renamed":        true,
		"sha256":         "0123456789abcdef0123456789abcdef0123456789abcdef0123456789abcdef",
	}})
	terminal.PublishEvent(session.Event{Type: session.EventLeaseReleased, Metadata: map[string]any{"type": "file-transfer", "transfer_id": "FT-send"}})
	result, err := client.CallTool(context.Background(), &protocol.CallToolParams{Name: "terminal_wait_file_transfer", Arguments: map[string]any{
		"session_id": "board", "transfer_id": "FT-send", "cursor": cursor, "timeout_ms": 1000,
	}})
	if err != nil || result == nil || result.IsError {
		t.Fatalf("terminal_wait_file_transfer result = %#v, %v", result, err)
	}
	assertStructuredResultMatchesOutputSchema(t, "terminal_wait_file_transfer", result)
	structured := result.StructuredContent.(map[string]any)
	if structured["state"] != "completed" || structured["transfer_id"] != "FT-send" || structured["lease_released"] != true || structured["source_path"] != "app.bin" || structured["requested_path"] != "/tmp/cterm/mcp-files/app.bin" || structured["resolved_path"] != "/tmp/cterm/mcp-files/app_1.bin" || structured["renamed"] != true || structured["sha256"] != "0123456789abcdef0123456789abcdef0123456789abcdef0123456789abcdef" {
		t.Fatalf("terminal_wait_file_transfer structured result = %#v", structured)
	}
	event := structured["event"].(map[string]any)
	metadata := event["metadata"].(map[string]any)
	if _, localOK := metadata["local_path"]; localOK {
		t.Fatalf("completed transfer metadata unexpectedly contains removed local_path: %#v", metadata)
	}
	if _, remoteOK := metadata["remote_path"]; remoteOK {
		t.Fatalf("completed transfer metadata unexpectedly contains removed remote_path: %#v", metadata)
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
	read := callTool(t, client, "terminal_read", map[string]any{"session_id": "board", "cursor": 0, "timeout_ms": 1000})
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
		assertStructuredResultMatchesOutputSchema(t, "terminal_wait", result)
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
		assertStructuredResultMatchesOutputSchema(t, "terminal_wait_activity", result)
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
		assertStructuredResultMatchesOutputSchema(t, "terminal_wait_device_event", result)
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
	go func() { runDone <- Run(context.Background(), registry, "test", serverTransport) }()

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
	server, err := NewServer(registry, "test")
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
	handler, err := NewStreamableHTTPHandler(registry, "test")
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
	assertStructuredResultMatchesOutputSchema(t, name, result)
	return result
}

func assertStructuredResultMatchesOutputSchema(t *testing.T, name string, result *protocol.CallToolResult) {
	t.Helper()
	if result == nil || result.IsError {
		return
	}
	schema, ok := terminalOutputSchema(name)
	if !ok {
		t.Fatalf("terminalOutputSchema(%q) is not registered", name)
	}
	resolved := resolveOutputSchema(t, name, schema)
	if err := resolved.Validate(result.StructuredContent); err != nil {
		t.Errorf("%s structured result does not match output schema: %v\nresult: %#v", name, err, result.StructuredContent)
	}
}

func resolveOutputSchema(t *testing.T, name string, schema any) *jsonschema.Resolved {
	t.Helper()
	encoded, err := json.Marshal(schema)
	if err != nil {
		t.Fatalf("marshal %s output schema: %v", name, err)
	}
	var parsed jsonschema.Schema
	if err := json.Unmarshal(encoded, &parsed); err != nil {
		t.Fatalf("decode %s output schema: %v", name, err)
	}
	resolved, err := parsed.Resolve(nil)
	if err != nil {
		t.Fatalf("resolve %s output schema: %v", name, err)
	}
	return resolved
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
