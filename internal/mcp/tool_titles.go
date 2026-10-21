package mcp

// terminalToolTitle returns the human-readable display name for one public
// MCP tool. Machine-readable terminal_* names remain the compatibility key.
func terminalToolTitle(name string) (string, bool) {
	switch name {
	case "terminal_export_diagnostics":
		return "Export Session Diagnostics", true
	case "terminal_read_diagnostic":
		return "Download Diagnostic Archive", true
	case "debug_bridge_list":
		return "List External GDB Bridge Ports", true
	case "debug_bridge_report":
		return "Report External GDB Activity from Codex", true
	case "debug_bridge_start":
		return "Open External GDB Bridge", true
	case "debug_bridge_status":
		return "Read External GDB Bridge Status", true
	case "debug_bridge_renew":
		return "Renew External GDB Bridge", true
	case "debug_bridge_close":
		return "Close External GDB Bridge and Session", true
	case "debug_bridge_restore":
		return "Verify Terminal Restoration after RSP", true
	case "debug_start":
		return "Start User-Space Debugging", true
	case "debug_command":
		return "Control GDB", true
	case "debug_events":
		return "Read Debug Events", true
	case "debug_stop":
		return "End User-Space Debugging", true
	case "terminal_list_sessions":
		return "List Terminal Sessions", true
	case "terminal_read":
		return "Read Terminal Output", true
	case "terminal_read_activity":
		return "Read Session Activity", true
	case "terminal_session_events":
		return "Read Session Events", true
	case "terminal_wait_file_transfer":
		return "Wait for File Transfer Result", true
	case "terminal_session_attach":
		return "Attach to Terminal Session", true
	case "terminal_session_detach":
		return "Detach from Terminal Session", true
	case "terminal_report_file_transfer":
		return "Report File Transfer Event", true
	case "terminal_exec":
		return "Execute Terminal Command", true
	case "terminal_write":
		return "Write Raw Terminal Input", true
	case "terminal_write_leased":
		return "Write Leased Terminal Input", true
	case "terminal_acquire_lease":
		return "Acquire Session Lease", true
	case "terminal_renew_lease":
		return "Renew Session Lease", true
	case "terminal_begin_file_transfer_cancel":
		return "Begin File Transfer Cancellation", true
	case "terminal_resolve_file_transfer_cancel":
		return "Resolve File Transfer Cancellation", true
	case "terminal_file_transfer_checkpoint":
		return "Check File Transfer Control", true
	case "terminal_release_lease":
		return "Release Session Lease", true
	case "terminal_wait":
		return "Wait for Terminal Output", true
	case "terminal_wait_activity":
		return "Wait for Session Activity", true
	case "terminal_open_serial":
		return "Open Serial Session", true
	case "terminal_list_serial_ports":
		return "List Serial Ports", true
	case "terminal_list_devices":
		return "List Discovered Devices", true
	case "terminal_read_device_events":
		return "Read Device Events", true
	case "terminal_wait_device_event":
		return "Wait for Device Event", true
	case "terminal_get_connection_decision":
		return "Get Connection Decision", true
	case "terminal_close":
		return "Close Terminal Session", true
	default:
		return "", false
	}
}
