package terminal

import (
	"context"
	"encoding/json"
	"errors"

	"github.com/akira-init1/ChannelTerm/internal/core/app"
	"github.com/akira-init1/ChannelTerm/internal/core/tool"
)

type controlTool struct{ application *app.Application }

func (*controlTool) Name() string { return "terminal_control" }
func (*controlTool) Description() string {
	return "Read default-on Session control or explicitly begin/renew/end a multi-call operation. Writes require a live authenticated instance and current _meta io.channelterm/control version; agent raw writes also require operation_id. Scope TTL is 30 seconds. Human user credentials with Manage may take over an exact version/operation, then explicitly allow new AI work. Takeover revokes old writes before bounded recovery; unknown external/raw execution can close the Session. Poll result with the original version/operation even after closure. No input replay or automatic retry; a closed connection does not prove all remote processes stopped."
}
func (*controlTool) InputSchema() tool.InputSchema {
	return tool.InputSchema{Type: "object", Properties: map[string]tool.InputProperty{
		"action":       {Type: "string", Enum: []string{"status", "begin", "renew", "end", "takeover", "allow", "result"}},
		"session_id":   {Type: "string", Description: "Full opaque Session ID; result lookup also works after closure."},
		"version":      {Type: "string", Description: "Exact version returned by status; required except for status."},
		"operation_id": {Type: "string", Description: "Exact operation from status, or omitted when idle/human. Never substitute a successor."},
	}, Required: []string{"action", "session_id"}}
}
func (t *controlTool) Call(ctx context.Context, input json.RawMessage) (tool.Result, error) {
	var q struct {
		Action    string `json:"action"`
		SessionID string `json:"session_id"`
		app.ControlProof
	}
	if err := decodeDebug(input, &q); err != nil {
		return nil, err
	}
	if q.SessionID == "" || len(q.SessionID) > 512 || len(q.Version) > 128 || len(q.OperationID) > 128 {
		return nil, errors.New("invalid control target or version")
	}
	if q.Action != "status" && q.Version == "" {
		return nil, app.ErrControlRequired
	}
	var status app.ControlStatus
	var err error
	switch q.Action {
	case "status":
		status, err = t.application.SessionControl(ctx, q.SessionID)
	case "begin":
		status, err = t.application.BeginControl(ctx, q.SessionID, q.ControlProof)
	case "renew":
		status, err = t.application.RenewControl(ctx, q.SessionID, q.ControlProof)
	case "end":
		status, err = t.application.EndControl(ctx, q.SessionID, q.ControlProof)
	case "allow":
		status, err = t.application.AllowAI(ctx, q.SessionID, q.ControlProof)
	case "takeover":
		result, e := t.application.TakeControl(ctx, q.SessionID, q.ControlProof)
		return tool.Result{"action": q.Action, "result": result}, e
	case "result":
		result, e := t.application.ControlOutcome(ctx, q.SessionID, q.ControlProof)
		return tool.Result{"action": q.Action, "result": result}, e
	default:
		return nil, errors.New("invalid control action")
	}
	return tool.Result{"action": q.Action, "control": status}, err
}
