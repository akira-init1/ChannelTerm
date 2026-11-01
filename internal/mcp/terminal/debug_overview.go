package terminal

import (
	"context"
	"encoding/json"

	"github.com/akira-init1/ChannelTerm/internal/core/app"
	"github.com/akira-init1/ChannelTerm/internal/core/tool"
)

type debugObservationTool struct {
	application *app.Application
	name        string
}

func debugObservationTools(a *app.Application) []tool.Tool {
	return []tool.Tool{&debugObservationTool{a, "debug_overview"}, &debugObservationTool{a, "debug_observation_register"}, &debugObservationTool{a, "debug_observation_report"}}
}
func (t *debugObservationTool) Name() string { return t.name }
func (t *debugObservationTool) Description() string {
	switch t.name {
	case "debug_overview":
		return "Read one bounded, capability-free Host snapshot of registered serial bridges, managed GDB and TCP/SSH debug observations. No target I/O, process/port scanning, lease renewal or debugger control. Client reports are labelled and stale after 30 seconds; missing evidence is unknown."
	case "debug_observation_register":
		return "Register external TCP, SSH tunnel, SSH stdio or SSH-terminal GDB metadata without starting or controlling anything. Bind reports to this authenticated client and a private owner. manual registrations remain unknown; mi executors can report their owned structured MI stream. Full optional Session ID associates context only, without an input lease. Maximum 128 retained registrations; reuse request_id and all parameters exactly."
	default:
		return "Report one registered executor's connection snapshot and optional GDB/MI record. Requires the registering credential and owner. Monotonic sequence; latest exact retry is idempotent without refreshing freshness. Does not store raw MI text, execute commands, verify client claims or renew a lease. Include current observed endpoints; omitted evidence becomes unknown."
	}
}
func (t *debugObservationTool) InputSchema() tool.InputSchema {
	props := map[string]tool.InputProperty{}
	required := []string{}
	if t.name != "debug_overview" {
		props["owner"] = tool.InputProperty{Type: "string", Description: "Private capability, 32..128 characters. Never include it in public metadata."}
		props["ports"] = tool.InputProperty{Type: "array", Items: &tool.InputProperty{Type: "object"}, Description: "At most three role objects: role (target/ssh/local_forward), configured host:port, optional observed address host:port, port, perspective and state. Registration only accepts configured endpoints. stdio/SSH local debugging has no target TCP port. See debug observation reference."}
		required = []string{"owner"}
	}
	if t.name == "debug_observation_register" {
		props["request_id"] = tool.InputProperty{Type: "string", Description: "1..128 characters; exact registration retry key."}
		props["name"] = tool.InputProperty{Type: "string", Description: "Non-secret display name, 1..128 UTF-8 bytes."}
		props["session_id"] = tool.InputProperty{Type: "string", Description: "Optional full Session ID. Required for ssh_terminal modes; never a short reference."}
		props["mode"] = tool.InputProperty{Type: "string", Enum: []string{"tcp", "ssh_tunnel", "ssh_stdio", "ssh_terminal", "ssh_terminal_tcp"}}
		props["executor"] = tool.InputProperty{Type: "string", Enum: []string{"manual", "mi"}}
		required = append(required, "request_id", "name", "mode", "executor")
	} else if t.name == "debug_observation_report" {
		props["id"] = tool.InputProperty{Type: "string", Description: "Observation ID returned by registration, not a bridge ID."}
		props["sequence"] = tool.InputProperty{Type: "integer", Description: "Increasing 1..9007199254740991; gaps allowed."}
		props["connection"] = tool.InputProperty{Type: "string", Enum: []string{"unknown", "connecting", "connected", "disconnected"}}
		props["tunnel_state"] = tool.InputProperty{Type: "string", Enum: []string{"unknown", "connecting", "connected", "disconnected", "not_applicable"}, Description: "Required for ssh_tunnel, independent of debugger connection."}
		props["mi"] = tool.InputProperty{Type: "string", Description: "Optional single structured GDB/MI record, at most 8192 bytes. Not terminal text or a command."}
		required = append(required, "id", "sequence", "connection")
	}
	return tool.InputSchema{Type: "object", Properties: props, Required: required}
}
func (t *debugObservationTool) Call(ctx context.Context, input json.RawMessage) (tool.Result, error) {
	switch t.name {
	case "debug_overview":
		var q struct{}
		if err := decodeDebug(input, &q); err != nil {
			return nil, err
		}
		s, err := t.application.DebugOverview(ctx)
		return tool.Result{"observed_at": s.ObservedAt, "operations": s.Operations}, err
	case "debug_observation_register":
		var q app.DebugRegistration
		if err := decodeDebug(input, &q); err != nil {
			return nil, err
		}
		s, err := t.application.RegisterDebugObservation(ctx, q)
		return tool.Result{"operation": s}, err
	default:
		var q app.DebugObservationReport
		if err := decodeDebug(input, &q); err != nil {
			return nil, err
		}
		s, err := t.application.ReportDebugObservation(ctx, q)
		return tool.Result{"operation": s}, err
	}
}
