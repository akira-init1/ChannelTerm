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
	for _, name := range []string{"debug_bridge_start", "debug_bridge_status", "debug_bridge_renew", "debug_bridge_close", "debug_bridge_restore", "debug_bridge_report"} {
		out = append(out, &bridgeTool{application: a, name: name})
	}
	return out
}

// Name identifies the bridge management or reporting operation.
func (t *bridgeTool) Name() string { return t.name }

// Description states the operation's ownership, liveness, and recovery limits.
func (t *bridgeTool) Description() string {
	switch t.name {
	case "debug_bridge_report":
		return "Report actual externally executed GDB commands, results, or observed states as Codex reports, not a complete GDB audit. Publishes only independent Session events visible in attach/events; never executes commands, forwards RSP, verifies target state, or renews the heartbeat. Omit raw RSP and secrets. Reporting failure must not interrupt GDB: continue debugging; renewal is independent of reporting. Sequence increases per bridge (gaps allowed); retry only the latest identical report."
	case "debug_bridge_start":
		return "Bridge an already prepared serial Session to one authorized external GDB via pinned TLS/TCP. Does not start or control GDB/gdbserver or probe RSP. Target readiness cannot be inferred from silence: explicit acknowledgement is required after preparing the target. Returns a secret connection descriptor for channelterm rsp-client. Defaults to loopback; a specific non-loopback bind requires allowed_peer. Reuse owner/request_id and all parameters exactly for retries. Current rsp-client connections maintain the management lease automatically, even without RSP traffic. Renew every 30 seconds before connecting, after disconnect, or with legacy helpers. All bridges have a 24-hour maximum lifetime. Use debug_bridge_report for actual externally executed commands, results, and observed states (Codex reports, not a complete audit)."
	case "debug_bridge_status":
		return "Read external RSP bridge status without renewing its heartbeat or disclosing connection secrets. Disconnected retains exclusive ownership and does not mean the target or shell has recovered."
	case "debug_bridge_renew":
		return "Renew the external RSP bridge's management heartbeat and internal lease, including while listening or disconnected. Current authenticated rsp-client connections renew automatically. The 60-second grace and 24-hour absolute maximum cannot be resurrected. Expiry closes the serial Session without claiming target restoration."
	case "debug_bridge_restore":
		return "After external GDB disconnects and the target returns to its shell, explicitly confirm readiness and supply the pre-debug stty -g value. Applies and reads back that value, then verifies a fresh shell marker before releasing the lease. Never injects RSP detach. Failure or ten-second timeout closes the Session."
	default:
		return "Close the external RSP bridge AND its serial Session. Does not detach, kill a target, or claim shell restoration. Use debug_bridge_restore after externally ending RSP if the Session should be preserved. Idempotent."
	}
}

// InputSchema exposes operation-specific arguments and separate capabilities.
func (t *bridgeTool) InputSchema() tool.InputSchema {
	props := map[string]tool.InputProperty{"owner": {Type: "string", Description: "Secret management capability, 32..128 characters; distinct from the generated TCP connection token."}}
	required := []string{"owner"}
	if t.name == "debug_bridge_start" {
		props["session_id"] = tool.InputProperty{Type: "string", Description: activeSessionIDDescription + " Must be an open serial Session."}
		props["request_id"] = tool.InputProperty{Type: "string", Description: "Unique start request ID, 1..128 characters; exact retries return the original bridge."}
		props["listen_address"] = tool.InputProperty{Type: "string", Description: "Specific numeric IP:port. Omit to rotate through unallocated loopback ports 37100..37999 then fall back to an OS dynamic port. Explicit ports never fall back; explicit zero requests an OS dynamic port. No wildcard binds."}
		props["strict_port_pool"] = tool.InputProperty{Type: "boolean", Description: "When listen_address is omitted, fail if ports 37100..37999 are exhausted instead of using an OS dynamic port. Default false. Explicit listen_address overrides the pool policy."}
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
	if t.name == "debug_bridge_report" {
		props["sequence"] = tool.InputProperty{Type: "integer", Description: "Increasing per-bridge sequence, 1..9007199254740991. Gaps are allowed; latest exact retry is deduplicated."}
		props["kind"] = tool.InputProperty{Type: "string", Enum: []string{"command", "result", "state"}, Description: "Actual command executed externally, its result, or observed debugger state. This never changes bridge state."}
		props["text"] = tool.InputProperty{Type: "string", Description: "Nonempty UTF-8 text, at most 4096 bytes. Select/redact before reporting: no raw RSP, credentials, or connection descriptors. Visible to all Session observers."}
		required = append(required, "sequence", "kind", "text")
	}
	return tool.InputSchema{Type: "object", Properties: props, Required: required}
}

// Call validates input and delegates lifecycle and reporting to Application.
func (t *bridgeTool) Call(ctx context.Context, input json.RawMessage) (tool.Result, error) {
	if t.name == "debug_bridge_report" {
		var q app.BridgeReportRequest
		if err := decodeDebug(input, &q); err != nil {
			return nil, err
		}
		result, err := t.application.ReportRSPBridge(ctx, q)
		return tool.Result{"bridge_id": result.BridgeID, "sequence": result.Sequence}, err
	}
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
