package terminal

import (
	"context"
	"encoding/json"

	"github.com/akira-init1/ChannelTerm/internal/core/app"
	"github.com/akira-init1/ChannelTerm/internal/core/tool"
)

type bridgeTool struct {
	application *app.Application
	name        string
}

func bridgeToolsForApplication(a *app.Application) []tool.Tool {
	var out []tool.Tool
	for _, name := range []string{"debug_bridge_start", "debug_bridge_status", "debug_bridge_renew", "debug_bridge_close", "debug_bridge_restore"} {
		out = append(out, &bridgeTool{application: a, name: name})
	}
	return out
}

func (t *bridgeTool) Name() string { return t.name }

func (t *bridgeTool) Description() string {
	switch t.name {
	case "debug_bridge_start":
		return "Bridge an already prepared serial Session to one authorized external GDB via pinned TLS/TCP. Does not start or control GDB/gdbserver or probe RSP. Target readiness cannot be inferred from silence: explicit acknowledgement is required after preparing the target. Returns a secret connection descriptor for channelterm rsp-client. Defaults to loopback; a specific non-loopback bind requires allowed_peer. Reuse owner/request_id and all parameters exactly for retries. Renew every 30 seconds."
	case "debug_bridge_status":
		return "Read external RSP bridge status without renewing its heartbeat or disclosing connection secrets. Disconnected retains exclusive ownership and does not mean the target or shell has recovered."
	case "debug_bridge_renew":
		return "Renew the external RSP bridge's 60-second management heartbeat, including while disconnected. Expiry closes the serial Session without claiming target restoration."
	case "debug_bridge_restore":
		return "After external GDB disconnects and the target returns to its shell, explicitly confirm readiness and supply the pre-debug stty -g value. Applies and reads back that value, then verifies a fresh shell marker before releasing the lease. Never injects RSP detach. Failure or ten-second timeout closes the Session."
	default:
		return "Close the external RSP bridge AND its serial Session. Does not detach, kill a target, or claim shell restoration. Use debug_bridge_restore after externally ending RSP if the Session should be preserved. Idempotent."
	}
}

func (t *bridgeTool) InputSchema() tool.InputSchema {
	props := map[string]tool.InputProperty{"owner": {Type: "string", Description: "Secret management capability, 32..128 characters; distinct from the generated TCP connection token."}}
	required := []string{"owner"}
	if t.name == "debug_bridge_start" {
		props["session_id"] = tool.InputProperty{Type: "string", Description: activeSessionIDDescription + " Must be an open serial Session."}
		props["request_id"] = tool.InputProperty{Type: "string", Description: "Unique start request ID, 1..128 characters; exact retries return the original bridge."}
		props["listen_address"] = tool.InputProperty{Type: "string", Description: "Specific numeric IP:port; default 127.0.0.1:0. No wildcard binds. Port zero allocates an ephemeral port."}
		props["allowed_peer"] = tool.InputProperty{Type: "string", Description: "One numeric client IP. Required for non-loopback binding. Authentication is still mandatory."}
		props["acknowledge_unverified_target"] = tool.InputProperty{Type: "boolean", Description: "Must be true after preparing the target for raw RSP and saving its stty -g state. This acknowledges that passive checks cannot prove gdbserver readiness."}
		required = append(required, "session_id", "request_id", "acknowledge_unverified_target")
	} else {
		props["bridge_id"] = tool.InputProperty{Type: "string", Description: "Opaque bridge_id returned by debug_bridge_start; not a Session reference."}
		required = append(required, "bridge_id")
	}
	if t.name == "debug_bridge_restore" {
		props["confirm_shell_ready"] = tool.InputProperty{Type: "boolean", Description: "Must be true only after independently ending RSP and returning to the target shell. The probe itself is shell input."}
		props["tty_state"] = tool.InputProperty{Type: "string", Description: "Exact stty -g value recorded before preparing the target, 1..512 printable bytes. No guessed defaults."}
		required = append(required, "confirm_shell_ready", "tty_state")
	}
	return tool.InputSchema{Type: "object", Properties: props, Required: required}
}

func (t *bridgeTool) Call(ctx context.Context, input json.RawMessage) (tool.Result, error) {
	if t.name == "debug_bridge_start" {
		var q app.BridgeStartRequest
		if err := decodeDebug(input, &q); err != nil {
			return nil, err
		}
		result, err := t.application.StartRSPBridge(ctx, q)
		return tool.Result{"status": result.Status, "connection": result.Connection}, err
	}
	var q struct {
		BridgeID string `json:"bridge_id"`
		Owner    string `json:"owner"`
	}
	if t.name == "debug_bridge_restore" {
		var restore struct {
			BridgeID          string `json:"bridge_id"`
			Owner             string `json:"owner"`
			TTYState          string `json:"tty_state"`
			ConfirmShellReady bool   `json:"confirm_shell_ready"`
		}
		if err := decodeDebug(input, &restore); err != nil {
			return nil, err
		}
		status, err := t.application.RestoreRSPBridge(restore.BridgeID, restore.Owner, restore.TTYState, restore.ConfirmShellReady)
		return tool.Result{"status": status}, err
	}
	if err := decodeDebug(input, &q); err != nil {
		return nil, err
	}
	var status app.BridgeStatus
	var err error
	switch t.name {
	case "debug_bridge_status":
		status, err = t.application.RSPBridgeStatus(q.BridgeID, q.Owner)
	case "debug_bridge_renew":
		status, err = t.application.RenewRSPBridge(q.BridgeID, q.Owner)
	default:
		status, err = t.application.CloseRSPBridge(q.BridgeID, q.Owner)
	}
	return tool.Result{"status": status}, err
}
