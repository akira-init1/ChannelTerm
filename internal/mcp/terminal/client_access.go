package terminal

import (
	"context"
	"encoding/json"

	"github.com/akira-init1/ChannelTerm/internal/core/app"
	"github.com/akira-init1/ChannelTerm/internal/core/tool"
)

// authorizedTool enforces the application access contract for each invocation.
// MCP readOnlyHint annotations are descriptive and are never authorization.
type authorizedTool struct {
	tool.Tool
	application *app.Application
}

func (t authorizedTool) Call(ctx context.Context, input json.RawMessage) (result tool.Result, callErr error) {
	if err := app.CheckClientAccess(ctx, clientEffect(t.Name())); err != nil {
		return nil, err
	}
	if t.application != nil && t.Name() != "client_instance" {
		var err error
		ctx, err = t.application.BindClientInstance(ctx, "mcp")
		if err != nil {
			return nil, err
		}
		// Only explicit Session targets create call-use records. Lists and discovery
		// do not implicitly keep every device in use. Business input is unchanged.
		var target struct {
			SessionID string `json:"session_id"`
			DebugID   string `json:"debug_id"`
			BridgeID  string `json:"bridge_id"`
		}
		_ = json.Unmarshal(input, &target)
		guardTarget := target.SessionID
		if guardTarget == "" && target.DebugID != "" {
			guardTarget = t.application.OperationSessionID(target.DebugID)
		}
		if guardTarget == "" && target.BridgeID != "" {
			guardTarget = t.application.OperationSessionID(target.BridgeID)
		}
		var finish func() error
		ctx, finish, err = t.application.GuardSessionCall(ctx, guardTarget)
		if err != nil {
			return nil, err
		}
		guarded := ctx
		defer func() {
			if denied := finish(); denied != nil {
				result = nil
				callErr = denied
			} else if callErr != nil {
				if notice, ok := t.application.InterruptedNoticeForRequest(guarded, guardTarget); ok && notice.Status != "allowed" {
					reply := localizeWait(notice)
					result = tool.Result{"wait": reply}
					callErr = &pausedTaskError{reply}
				}
			}
		}()
		if t.Name() != "terminal_control" && clientEffect(t.Name()) != app.ClientRead {
			if err = app.RequireControlIdentity(ctx); err != nil {
				return nil, err
			}
			if target.SessionID != "" {
				if t.Name() == "terminal_close" || t.Name() == "terminal_begin_file_transfer_cancel" || t.Name() == "terminal_resolve_file_transfer_cancel" {
					err = t.application.CheckControlGeneration(ctx, target.SessionID)
				} else {
					err = t.application.CheckSessionControl(ctx, target.SessionID)
				}
			} else if target.DebugID != "" {
				err = t.application.CheckOperationControl(ctx, target.DebugID)
			} else if target.BridgeID != "" {
				err = t.application.CheckOperationControl(ctx, target.BridgeID)
			}
			if err != nil {
				return nil, err
			}
		}
		var done func()
		ctx, done, err = t.application.BeginClientCall(ctx, target.SessionID, t.Name(), clientEffect(t.Name()) != app.ClientRead)
		if err != nil {
			return nil, err
		}
		defer done()
	}
	return t.Tool.Call(ctx, input)
}

// clientEffect is deliberately an allowlist. Newly added tools require management
// authorization until their actual side effects have been reviewed here.
func clientEffect(name string) app.ClientEffect {
	switch name {
	case "client_instance", "terminal_control", "file_transfer", "file_transfer_list", "host_idle_status":
		// Application checks start/cancel write grants and task ownership;
		// status remains available after a client's write grant is revoked.
		return app.ClientRead
	case "terminal_list_sessions", "terminal_read", "terminal_read_activity", "terminal_session_events", "terminal_wait_file_transfer",
		"terminal_list_serial_ports", "terminal_list_devices", "terminal_read_device_events", "terminal_get_connection_decision",
		"terminal_read_diagnostic", "debug_overview", "debug_bridge_list", "debug_bridge_status", "terminal_session_attach", "terminal_session_detach":
		return app.ClientRead
	case "terminal_write", "terminal_write_leased", "terminal_exec", "terminal_open_serial", "terminal_enter_uboot",
		"terminal_report_file_transfer", "terminal_acquire_lease", "terminal_renew_lease",
		"terminal_release_lease", "terminal_file_transfer_checkpoint", "terminal_export_diagnostics",
		"debug_observation_register", "debug_observation_report", "debug_start", "debug_command", "debug_events", "debug_stop", "debug_bridge_start", "debug_bridge_renew", "debug_bridge_close", "debug_bridge_restore", "debug_bridge_report":
		return app.ClientWrite
	default:
		return app.ClientManage
	}
}

func authorizeTools(tools []tool.Tool, application *app.Application) []tool.Tool {
	for i := range tools {
		tools[i] = authorizedTool{Tool: tools[i], application: application}
	}
	return tools
}
