package terminal

import (
	"context"
	"encoding/json"

	"github.com/akira-init1/ChannelTerm/internal/core/app"
	"github.com/akira-init1/ChannelTerm/internal/core/tool"
)

type clientInstanceTool struct{ application *app.Application }

func (*clientInstanceTool) Name() string { return "client_instance" }
func (*clientInstanceTool) Description() string {
	return "Manage bounded online-instance presence: register, renew, end, list, use, unuse or delegate. Not a lease or write grant. Registration returns a private credential bound to the authenticated client. Renew every 20 seconds; expires after 60 seconds. Explicit observe/monitor/operation uses name exact Sessions; list polling uses none. Optional one-use delegation links a child to a parent operation. Public IDs cannot authorize calls. Entry/name are unverified display claims. Old callers remain instance-unknown."
}
func (*clientInstanceTool) InputSchema() tool.InputSchema {
	return tool.InputSchema{Type: "object", Required: []string{"action"}, Properties: map[string]tool.InputProperty{
		"action":     {Type: "string", Enum: []string{"register", "renew", "end", "list", "use", "unuse", "delegate"}},
		"name":       {Type: "string", Description: "Registration display claim; 1..128 UTF-8 bytes, no controls."},
		"entry":      {Type: "string", Description: "Unverified entry label, 1..32 UTF-8 bytes; for example cli, electron or agent."},
		"credential": {Type: "object", Description: "Private registration result {id, secret}; never expose in activity, UI, logs or command arguments."},
		"delegation": {Type: "string", Description: "Optional one-use private registration capability, valid 30 seconds under the parent's credential and Session."},
		"key":        {Type: "string", Description: "Idempotent per-instance usage key, 1..128 UTF-8 bytes."},
		"session_id": {Type: "string", Description: "Session target resolved by Host; stored as full ID."},
		"kind":       {Type: "string", Enum: []string{"observe", "monitor", "operation"}},
		"use_id":     {Type: "string", Description: "Use ID returned by use; delegate requires an operation use."},
	}}
}
func (t *clientInstanceTool) Call(ctx context.Context, input json.RawMessage) (result tool.Result, err error) {
	var q struct {
		Action     string            `json:"action"`
		Name       string            `json:"name"`
		Entry      string            `json:"entry"`
		Credential app.InstanceProof `json:"credential"`
		Delegation string            `json:"delegation"`
		Key        string            `json:"key"`
		SessionID  string            `json:"session_id"`
		Kind       string            `json:"kind"`
		UseID      string            `json:"use_id"`
	}
	if len(input) > 4096 {
		return nil, app.ErrClientInstanceInput
	}
	if err := decodeDebug(input, &q); err != nil {
		return nil, err
	}
	defer func() {
		if result != nil {
			result["action"] = q.Action
		}
	}()
	switch q.Action {
	case "register":
		view, proof, err := t.application.RegisterClientInstance(ctx, app.InstanceRegistration{Name: q.Name, Entry: q.Entry, Delegation: q.Delegation})
		return tool.Result{"instance": view, "credential": proof}, err
	case "renew":
		view, err := t.application.RenewClientInstance(ctx, q.Credential)
		return tool.Result{"instance": view}, err
	case "end":
		return tool.Result{"ended": true}, t.application.EndClientInstance(ctx, q.Credential)
	case "list":
		views, err := t.application.ListClientInstances(ctx)
		return tool.Result{"instances": views}, err
	case "use":
		use, err := t.application.SetClientUse(ctx, q.Credential, q.Key, q.SessionID, q.Kind)
		return tool.Result{"use": use}, err
	case "unuse":
		return tool.Result{"ended": true}, t.application.EndClientUse(ctx, q.Credential, q.UseID)
	case "delegate":
		secret, err := t.application.DelegateClientInstance(ctx, q.Credential, q.UseID)
		return tool.Result{"delegation": secret}, err
	default:
		return nil, app.ErrClientInstanceInput
	}
}
