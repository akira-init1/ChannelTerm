package terminal

import (
	"context"
	"encoding/json"

	"github.com/akira-init1/ChannelTerm/internal/core/app"
	"github.com/akira-init1/ChannelTerm/internal/core/tool"
)

// authorizedTool enforces the application access contract for each invocation.
// MCP readOnlyHint annotations are descriptive and are never authorization.
type authorizedTool struct{ tool.Tool }

func (t authorizedTool) Call(ctx context.Context, input json.RawMessage) (tool.Result, error) {
	if err := app.CheckClientAccess(ctx, clientEffect(t.Name())); err != nil {
		return nil, err
	}
	return t.Tool.Call(ctx, input)
}

// clientEffect is deliberately an allowlist. Newly added tools require management
// authorization until their actual side effects have been reviewed here.
func clientEffect(name string) app.ClientEffect {
	switch name {
	case "terminal_list_sessions", "terminal_read", "terminal_read_activity", "terminal_session_events", "terminal_wait_file_transfer",
		"terminal_list_serial_ports", "terminal_list_devices", "terminal_read_device_events", "terminal_get_connection_decision",
		"terminal_read_diagnostic", "debug_bridge_list", "debug_bridge_status", "terminal_session_attach", "terminal_session_detach":
		return app.ClientRead
	case "terminal_write", "terminal_write_leased", "terminal_exec", "terminal_open_serial", "terminal_enter_uboot",
		"terminal_report_file_transfer", "terminal_acquire_lease", "terminal_renew_lease",
		"terminal_release_lease", "terminal_file_transfer_checkpoint", "terminal_export_diagnostics",
		"debug_start", "debug_command", "debug_events", "debug_stop", "debug_bridge_start", "debug_bridge_renew", "debug_bridge_close", "debug_bridge_restore", "debug_bridge_report":
		return app.ClientWrite
	default:
		return app.ClientManage
	}
}

func authorizeTools(tools []tool.Tool) []tool.Tool {
	for i := range tools {
		tools[i] = authorizedTool{Tool: tools[i]}
	}
	return tools
}
