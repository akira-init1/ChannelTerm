package mcp

import (
	"reflect"
	"testing"
)

func TestDeviceOutputSchemaRequiresFiniteScanHealth(t *testing.T) {
	schema, ok := terminalOutputSchema("terminal_list_devices")
	if !ok || !reflect.DeepEqual(schema.Required, []string{"devices", "scan_state", "last_scan_at"}) {
		t.Fatalf("device output schema = %#v, registered=%t", schema, ok)
	}
	if !reflect.DeepEqual(schema.Properties["scan_state"].Enum, []string{"current", "failed", "stale", "unknown"}) {
		t.Fatalf("scan state schema = %#v", schema.Properties["scan_state"])
	}
	// Empty is meaningful before any successful enumeration, so date-time alone is too strict.
	if got := schema.Properties["last_scan_at"]; got.Type != "string" || got.Format != "" {
		t.Fatalf("last scan schema = %#v", got)
	}
}
