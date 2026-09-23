package mcp

import (
	"context"
	"strings"
	"testing"

	protocol "github.com/modelcontextprotocol/go-sdk/mcp"
)

func TestToolSelectionBoundaries(t *testing.T) {
	manager, registry, _ := newTestRegistry(t)
	defer func() { _ = manager.Close() }()
	client, closeClient := connectTestClient(t, registry)
	defer closeClient()

	listed, err := client.ListTools(context.Background(), nil)
	if err != nil {
		t.Fatalf("ListTools() error = %v", err)
	}
	tools := make(map[string]*protocol.Tool, len(listed.Tools))
	for _, registered := range listed.Tools {
		tools[registered.Name] = registered
	}

	assertDescriptionContains(t, tools, "terminal_exec", "one non-interactive command", "idle Bash prompt", "terminal_write", "raw keys", "interactive programs")
	assertDescriptionContains(t, tools, "terminal_write", "raw", "keys", "interactive programs", "target history", "terminal_exec", "idle Bash prompt")
	assertDescriptionContains(t, tools, "terminal_read", "immediately", "cursor is omitted", "terminal_wait", "known cursor")
	assertDescriptionContains(t, tools, "terminal_wait", "known cursor", "terminal_read without cursor", "immediate snapshot", "does not return file-transfer status", "terminal_wait_file_transfer")
	assertDescriptionContains(t, tools, "terminal_read_activity", "immediately", "cursor is omitted", "terminal_wait_activity", "known activity cursor")
	assertDescriptionContains(t, tools, "terminal_wait_activity", "known cursor", "terminal_read_activity without cursor", "immediate snapshot")
	assertDescriptionContains(t, tools, "terminal_read_device_events", "immediately", "cursor is omitted", "terminal_wait_device_event", "known cursor", "terminal_get_connection_decision")
	assertDescriptionContains(t, tools, "terminal_wait_device_event", "known cursor", "terminal_read_device_events without cursor", "immediate snapshot", "terminal_get_connection_decision")
	assertDescriptionContains(t, tools, "terminal_wait_file_transfer", "transfer_id", "completes", "cancelled", "fails", "lease has been released", "instead of terminal_wait")
	assertDescriptionContains(t, tools, "terminal_close", "every attached client", "terminal_session_detach", "without closing")
	assertDescriptionContains(t, tools, "terminal_session_detach", "one client detachment", "other clients", "terminal_close", "everyone")
	assertDescriptionContains(t, tools, "terminal_write_leased", "active Session lease", "managed multi-step operation", "terminal_write")

	assertInputBoundary(t, tools, "terminal_exec", []string{"command"}, []string{"data", "owner"}, []string{"command"}, nil)
	assertInputBoundary(t, tools, "terminal_write", []string{"data"}, []string{"command", "owner"}, []string{"data"}, nil)
	assertInputBoundary(t, tools, "terminal_write_leased", []string{"data", "owner"}, []string{"command"}, []string{"data", "owner"}, nil)
	assertInputBoundary(t, tools, "terminal_read", []string{"cursor"}, nil, nil, []string{"cursor"})
	assertInputBoundary(t, tools, "terminal_wait", []string{"cursor"}, nil, []string{"cursor"}, nil)
	assertInputBoundary(t, tools, "terminal_read_activity", []string{"cursor"}, nil, nil, []string{"cursor"})
	assertInputBoundary(t, tools, "terminal_wait_activity", []string{"cursor"}, nil, []string{"cursor"}, nil)
	assertInputBoundary(t, tools, "terminal_read_device_events", []string{"cursor"}, nil, nil, []string{"cursor"})
	assertInputBoundary(t, tools, "terminal_wait_device_event", []string{"cursor"}, nil, []string{"cursor"}, nil)
	assertInputBoundary(t, tools, "terminal_wait_file_transfer", []string{"cursor", "transfer_id"}, nil, []string{"cursor", "transfer_id"}, nil)

	assertDestructiveBoundary(t, tools, "terminal_close", true)
	assertDestructiveBoundary(t, tools, "terminal_session_detach", false)
}

func assertDescriptionContains(t *testing.T, tools map[string]*protocol.Tool, name string, phrases ...string) {
	t.Helper()
	registered := tools[name]
	if registered == nil {
		t.Fatalf("tool %q is not registered", name)
	}
	for _, phrase := range phrases {
		if !strings.Contains(registered.Description, phrase) {
			t.Errorf("%s description = %q, want selection guidance containing %q", name, registered.Description, phrase)
		}
	}
}

func assertInputBoundary(t *testing.T, tools map[string]*protocol.Tool, name string, present, absent, required, optional []string) {
	t.Helper()
	registered := tools[name]
	if registered == nil {
		t.Fatalf("tool %q is not registered", name)
	}
	schema, ok := registered.InputSchema.(map[string]any)
	if !ok {
		t.Fatalf("%s input schema = %#v, want JSON object schema", name, registered.InputSchema)
	}
	properties, ok := schema["properties"].(map[string]any)
	if !ok {
		t.Fatalf("%s input properties = %#v, want object", name, schema["properties"])
	}
	for _, property := range present {
		if _, ok := properties[property]; !ok {
			t.Errorf("%s input properties omit %q", name, property)
		}
	}
	for _, property := range absent {
		if _, ok := properties[property]; ok {
			t.Errorf("%s input properties unexpectedly include %q", name, property)
		}
	}
	for _, property := range required {
		if !schemaListContains(schema["required"], property) {
			t.Errorf("%s required fields = %#v, want %q", name, schema["required"], property)
		}
	}
	for _, property := range optional {
		if schemaListContains(schema["required"], property) {
			t.Errorf("%s required fields = %#v, want %q optional", name, schema["required"], property)
		}
	}
}

func assertDestructiveBoundary(t *testing.T, tools map[string]*protocol.Tool, name string, destructive bool) {
	t.Helper()
	registered := tools[name]
	if registered == nil {
		t.Fatalf("tool %q is not registered", name)
	}
	if registered.Annotations == nil || registered.Annotations.DestructiveHint == nil {
		t.Fatalf("%s destructiveHint is not defined", name)
	}
	if *registered.Annotations.DestructiveHint != destructive {
		t.Errorf("%s destructiveHint = %t, want %t", name, *registered.Annotations.DestructiveHint, destructive)
	}
}
