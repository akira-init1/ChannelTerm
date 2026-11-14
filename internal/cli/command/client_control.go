package command

import (
	"context"
	"encoding/json"
	"sync"

	"github.com/akira-init1/ChannelTerm/internal/core/app"
	protocol "github.com/modelcontextprotocol/go-sdk/mcp"
)

// commandControls pins multi-call scopes to this invocation. A revoked scope is
// never refreshed or restarted automatically; each new invocation is explicit work.
type capturedInputControlKey struct{}

type commandControls struct {
	mu        sync.Mutex
	available bool
	kind      string
	scopes    map[string]app.ControlProof
	leases    map[string]app.ControlProof
	targets   map[string]string
}

func (c *instanceMCPClient) controlCall(ctx context.Context, args map[string]any, out any) error {
	return callMCPTool(ctx, controlProofClient{ClientSession: c.ClientSession, proof: c.proof}, "terminal_control", args, out)
}

type controlProofClient struct {
	*protocol.ClientSession
	proof app.InstanceProof
}

func (c controlProofClient) CallTool(ctx context.Context, q *protocol.CallToolParams) (*protocol.CallToolResult, error) {
	copy := *q
	copy.Meta = protocol.Meta{"io.channelterm/instance": c.proof}
	return c.ClientSession.CallTool(ctx, &copy)
}

func (c *instanceMCPClient) controlMetadata(ctx context.Context, q *protocol.CallToolParams) (string, error) {
	if !c.controls.available || q.Name == "terminal_control" || q.Name == "client_instance" {
		return "", nil
	}
	encoded, err := json.Marshal(q.Arguments)
	if err != nil {
		return "", err
	}
	var args struct {
		SessionID string `json:"session_id"`
		DebugID   string `json:"debug_id"`
		BridgeID  string `json:"bridge_id"`
	}
	if json.Unmarshal(encoded, &args) != nil {
		return "", nil
	}
	if args.SessionID == "" {
		c.controls.mu.Lock()
		args.SessionID = c.controls.targets[args.DebugID]
		if args.SessionID == "" {
			args.SessionID = c.controls.targets[args.BridgeID]
		}
		c.controls.mu.Unlock()
	}
	if args.SessionID == "" {
		return "", nil
	}
	// Reads never start/renew scopes or require generation metadata.
	switch q.Name {
	case "debug_start", "debug_command", "debug_stop", "debug_events", "debug_bridge_start", "debug_bridge_renew", "debug_bridge_close", "debug_bridge_restore", "debug_bridge_report", "terminal_write", "terminal_write_leased", "terminal_acquire_lease", "terminal_renew_lease", "terminal_release_lease", "terminal_report_file_transfer", "terminal_file_transfer_checkpoint", "terminal_exec", "terminal_enter_uboot", "terminal_close", "terminal_begin_file_transfer_cancel", "terminal_resolve_file_transfer_cancel", "terminal_export_diagnostics":
	default:
		return "", nil
	}
	if captured, ok := ctx.Value(capturedInputControlKey{}).(app.ControlProof); ok {
		if q.Meta == nil {
			q.Meta = protocol.Meta{}
		}
		q.Meta["io.channelterm/control"] = captured
		return args.SessionID, nil
	}
	c.controls.mu.Lock()
	defer c.controls.mu.Unlock()
	p, ok := c.controls.leases[args.SessionID]
	if !ok {
		p, ok = c.controls.scopes[args.SessionID]
	}
	if !ok {
		var result struct {
			Control app.ControlStatus `json:"control"`
		}
		if err = c.controlCall(ctx, map[string]any{"action": "status", "session_id": args.SessionID}, &result); err != nil {
			return "", err
		}
		p = app.ControlProof{Version: result.Control.Version, OperationID: result.Control.OperationID}
		if q.Name == "terminal_write" && c.controls.kind == "agent" || q.Name == "debug_start" || q.Name == "debug_bridge_start" {
			if err = c.controlCall(ctx, map[string]any{"action": "begin", "session_id": args.SessionID, "version": p.Version, "operation_id": p.OperationID}, &result); err != nil {
				return "", err
			}
			p = app.ControlProof{Version: result.Control.Version, OperationID: result.Control.OperationID}
			c.controls.scopes[args.SessionID] = p
		}
	}
	if q.Name == "terminal_acquire_lease" || q.Name == "debug_start" || q.Name == "debug_bridge_start" {
		c.controls.leases[args.SessionID] = p
	}
	if q.Meta == nil {
		q.Meta = protocol.Meta{}
	}
	q.Meta["io.channelterm/control"] = p
	return args.SessionID, nil
}

func (c *instanceMCPClient) renewControls(ctx context.Context) error {
	c.controls.mu.Lock()
	defer c.controls.mu.Unlock()
	for id, p := range c.controls.scopes {
		if err := c.controlCall(ctx, map[string]any{"action": "renew", "session_id": id, "version": p.Version, "operation_id": p.OperationID}, nil); err != nil {
			return err
		}
	}
	return nil
}

func (c *instanceMCPClient) endControls(ctx context.Context) {
	c.controls.mu.Lock()
	defer c.controls.mu.Unlock()
	for id, p := range c.controls.scopes {
		if c.controlCall(ctx, map[string]any{"action": "end", "session_id": id, "version": p.Version, "operation_id": p.OperationID}, nil) == nil {
			delete(c.controls.scopes, id)
		}
	}
}

// captureControlInput pins queued foreground input before it waits behind I/O.
func (s *mcpAttachSession) captureControlInput(ctx context.Context) (context.Context, error) {
	c, ok := s.client.(*instanceMCPClient)
	if !ok || !c.controls.available {
		return ctx, nil
	}
	q := &protocol.CallToolParams{Name: "terminal_write", Arguments: map[string]any{"session_id": s.id}}
	if _, err := c.controlMetadata(ctx, q); err != nil {
		return ctx, err
	}
	p, ok := q.Meta["io.channelterm/control"].(app.ControlProof)
	if !ok {
		return ctx, app.ErrControlRequired
	}
	return context.WithValue(ctx, capturedInputControlKey{}, p), nil
}
