package mcp

import protocol "github.com/modelcontextprotocol/go-sdk/mcp"

// terminalToolAnnotations returns behavioral hints for one public MCP tool.
// The hints describe effects rather than enforcing authorization or safety.
func terminalToolAnnotations(name string) (*protocol.ToolAnnotations, bool) {
	readOnly := false
	destructive := false
	openWorld := false
	idempotent := false

	switch name {
	case "terminal_list_sessions",
		"terminal_read",
		"terminal_read_activity",
		"terminal_session_events",
		"terminal_wait_file_transfer",
		"terminal_wait",
		"terminal_wait_activity",
		"terminal_file_transfer_checkpoint",
		"terminal_list_serial_ports",
		"terminal_list_devices",
		"terminal_read_device_events",
		"terminal_wait_device_event",
		"terminal_get_connection_decision":
		readOnly = true
	case "terminal_open_serial", "terminal_exec", "terminal_write", "terminal_write_leased":
		destructive = true
		openWorld = true
	case "terminal_resolve_file_transfer_cancel", "terminal_release_lease", "terminal_close":
		destructive = true
		idempotent = name == "terminal_release_lease"
	case "terminal_session_attach",
		"terminal_session_detach",
		"terminal_report_file_transfer",
		"terminal_acquire_lease",
		"terminal_renew_lease",
		"terminal_begin_file_transfer_cancel":
		// These tools change bounded ChannelTerm state without deleting data or
		// invoking arbitrary behavior on the connected terminal.
	default:
		return nil, false
	}

	annotations := &protocol.ToolAnnotations{
		IdempotentHint: idempotent,
		OpenWorldHint:  boolPointer(openWorld),
		ReadOnlyHint:   readOnly,
	}
	if !readOnly {
		annotations.DestructiveHint = boolPointer(destructive)
	}
	return annotations, true
}

func boolPointer(value bool) *bool { return &value }
