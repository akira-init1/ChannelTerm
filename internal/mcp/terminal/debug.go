package terminal

import (
	"bytes"
	"context"
	"encoding/base64"
	"encoding/json"
	"errors"
	"io"
	"time"
	"unicode/utf8"

	"github.com/akira-init1/ChannelTerm/internal/core/app"
	"github.com/akira-init1/ChannelTerm/internal/core/debugger"
	"github.com/akira-init1/ChannelTerm/internal/core/tool"
)

type debugTool struct {
	application *app.Application
	name        string
}

func debugToolsForApplication(a *app.Application) []tool.Tool {
	var out []tool.Tool
	for _, name := range []string{"debug_start", "debug_command", "debug_events", "debug_stop"} {
		out = append(out, &debugTool{application: a, name: name})
	}
	return out
}
func (t *debugTool) Name() string { return t.name }
func (t *debugTool) Description() string {
	switch t.name {
	case "debug_start":
		return "Start native Linux user-space GDB/MI on an idle Bash Session using an exclusive managed lease. ChannelTerm, GDB, gdbserver, and ELF must share one Linux environment. PID omitted launches with isolated I/O; attach rejects controlling terminals, pipes, sockets, or shared character devices. Single inferior, multithreaded, all-stop only. Reuse owner and request_id unchanged to retry."
	case "debug_command":
		return "Execute one supported GDB action, or renew the 60-second debug heartbeat. Reuse request_id and parameters unchanged after a lost response. Execution is asynchronous: consume debug_events for stops. Raw MI/console and multi-inferior control are unavailable."
	case "debug_events":
		return "Wait for ordered MI records and lifecycle events after a debug cursor; renews the client heartbeat. A stale cursor is an error, never silently skipped. Invalid UTF-8 MI values use explicit base64 objects."
	default:
		return "End debugging, detach by default, wait for protocol completion, restore original terminal modes and confirm the shell with a unique harmless probe. terminate is allowed only for launched processes. Recovery failure closes the Session and returns an error. Safe to retry."
	}
}
func (t *debugTool) InputSchema() tool.InputSchema {
	props := map[string]tool.InputProperty{"owner": {Type: "string", Description: "Secret random capability, 32..128 characters; retain for retries and all calls. Never publish it in events."}}
	required := []string{"owner"}
	if t.name == "debug_start" {
		props["session_id"] = tool.InputProperty{Type: "string", Description: activeSessionIDDescription}
		props["request_id"] = tool.InputProperty{Type: "string", Description: "Unique start request ID, 1..128 characters. Conflicting reuse is rejected."}
		props["elf"] = tool.InputProperty{Type: "string", Description: "Absolute executable ELF path; must match native Linux architecture and attached /proc/PID/exe."}
		props["args"] = tool.InputProperty{Type: "array", Items: &tool.InputProperty{Type: "string"}, Description: "Launch arguments; at most 128. No shell expansion."}
		props["pid"] = tool.InputProperty{Type: "integer", Description: "Existing isolated process PID; omit or zero to launch. Attach never permits terminate."}
		for _, key := range []string{"gdb", "gdbserver"} {
			props[key] = tool.InputProperty{Type: "string", Description: "Absolute executable path; defaults to executable found on Host PATH."}
		}
		required = append(required, "session_id", "request_id", "elf")
	} else {
		props["debug_id"] = tool.InputProperty{Type: "string", Description: "Opaque debug_id returned by debug_start."}
		required = append(required, "debug_id")
	}
	switch t.name {
	case "debug_command":
		props["request_id"] = tool.InputProperty{Type: "string", Description: "Unique command request ID; exact retries return the cached response."}
		props["action"] = tool.InputProperty{Type: "string", Enum: []string{"break_insert", "break_delete", "continue", "next", "step", "finish", "interrupt", "stack", "arguments", "variables", "threads", "thread", "frame", "registers", "register_names", "evaluate", "renew"}}
		props["argument"] = tool.InputProperty{Type: "string", Description: "One breakpoint location, expression, or numeric breakpoint/thread/frame ID; other actions take no argument. Function calls are disabled."}
		required = append(required, "request_id", "action")
	case "debug_events":
		props["cursor"] = tool.InputProperty{Type: "integer", Description: "Next event cursor; begin at zero to include startup events."}
		props["timeout_ms"] = tool.InputProperty{Type: "integer", Description: "Wait duration 1..30000 ms; default 10000. Timeout is an error and does not end debugging."}
		required = append(required, "cursor")
	case "debug_stop":
		props["terminate"] = tool.InputProperty{Type: "boolean", Description: "Default false detaches and leaves the inferior alive; true kills only a launched inferior."}
	}
	return tool.InputSchema{Type: "object", Properties: props, Required: required}
}
func decodeDebug(input json.RawMessage, out any) error {
	if len(input) > 32*1024 {
		return errors.New("debug request exceeds 32 KiB")
	}
	d := json.NewDecoder(bytes.NewReader(input))
	d.DisallowUnknownFields()
	if e := d.Decode(out); e != nil {
		return e
	}
	if e := d.Decode(new(any)); e != io.EOF {
		return errors.New("expected one debug request object")
	}
	return nil
}
func (t *debugTool) Call(ctx context.Context, input json.RawMessage) (tool.Result, error) {
	switch t.name {
	case "debug_start":
		var q app.DebugStartRequest
		if e := decodeDebug(input, &q); e != nil {
			return nil, e
		}
		s, e := t.application.StartDebug(ctx, q)
		return tool.Result{"status": s}, e
	case "debug_command":
		var q app.DebugCommandRequest
		if e := decodeDebug(input, &q); e != nil {
			return nil, e
		}
		r, e := t.application.DebugCommand(ctx, q)
		return tool.Result{"record": debugRecord(r)}, e
	case "debug_stop":
		var q struct {
			DebugID   string `json:"debug_id"`
			Owner     string `json:"owner"`
			Terminate bool   `json:"terminate"`
		}
		if e := decodeDebug(input, &q); e != nil {
			return nil, e
		}
		s, e := t.application.StopDebug(ctx, q.DebugID, q.Owner, q.Terminate)
		return tool.Result{"status": s}, e
	default:
		var q struct {
			DebugID   string  `json:"debug_id"`
			Owner     string  `json:"owner"`
			Cursor    *uint64 `json:"cursor"`
			TimeoutMS *int    `json:"timeout_ms"`
		}
		if e := decodeDebug(input, &q); e != nil {
			return nil, e
		}
		if q.Cursor == nil {
			return nil, errors.New("cursor is required")
		}
		timeout := 10000
		if q.TimeoutMS != nil {
			timeout = *q.TimeoutMS
		}
		if timeout < 1 || timeout > 30000 {
			return nil, errors.New("timeout_ms must be 1..30000")
		}
		ctx, cancel := context.WithTimeout(ctx, time.Duration(timeout)*time.Millisecond)
		defer cancel()
		out, e := t.application.ReadDebugEvents(ctx, q.DebugID, q.Owner, *q.Cursor)
		if e != nil {
			return nil, e
		}
		events := []any{}
		for _, event := range out.Events {
			events = append(events, map[string]any{"sequence": event.Sequence, "record": debugRecord(event.Record)})
		}
		return tool.Result{"status": out.Status, "events": events, "next": out.Next}, nil
	}
}
func debugRecord(r debugger.Record) map[string]any {
	return map[string]any{"kind": r.Kind, "class": r.Class, "token": r.Token, "results": debugJSONValue(r.Results), "text": debugJSONValue(r.Text)}
}
func debugJSONValue(v any) any {
	switch x := v.(type) {
	case string:
		if !utf8.ValidString(x) {
			return map[string]any{"encoding": "base64", "data": base64.StdEncoding.EncodeToString([]byte(x))}
		}
		return x
	case map[string]any:
		out := map[string]any{}
		for k, v := range x {
			out[k] = debugJSONValue(v)
		}
		return out
	case []any:
		out := make([]any, len(x))
		for i, v := range x {
			out[i] = debugJSONValue(v)
		}
		return out
	default:
		return v
	}
}
