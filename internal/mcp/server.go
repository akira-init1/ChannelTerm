// Package mcp adapts ChannelTerm Tools to the Model Context Protocol.
//
// This package is a protocol boundary. It depends only on the protocol-neutral
// Tool Registry and never owns, creates, or accesses a terminal Transport.
package mcp

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"strings"

	"github.com/akira-init1/ChannelTerm/internal/core/tool"
	protocol "github.com/modelcontextprotocol/go-sdk/mcp"
)

var (
	// ErrNilRegistry is returned when an MCP Server has no Tool Registry to adapt.
	ErrNilRegistry = errors.New("tool registry must not be nil")
	// ErrRequiredCursor is returned when an MCP wait tool has no cursor.
	ErrRequiredCursor = errors.New("cursor is required for wait tools")
)

// NewServer creates an MCP Server backed by registry.
//
// The returned Server has no ownership of registry or of the Sessions reached
// through it. In particular, disconnecting an MCP client only closes the MCP
// connection; the embedding application decides when shared Sessions close.
func NewServer(registry *tool.Registry) (*protocol.Server, error) {
	adapter, err := newAdapter(registry)
	if err != nil {
		return nil, err
	}
	server := protocol.NewServer(&protocol.Implementation{
		Name:        "channelterm",
		Title:       "ChannelTerm",
		Description: "Operate active ChannelTerm terminal sessions and inspect local devices.",
		Version:     "0.1.0",
	}, &protocol.ServerOptions{Capabilities: &protocol.ServerCapabilities{Tools: &protocol.ToolCapabilities{}}})
	for _, exposed := range adapter.exposed {
		exposed := exposed
		server.AddTool(&protocol.Tool{Name: exposed.name, Description: exposed.description, InputSchema: exposed.schema}, adapter.handler(exposed))
	}
	return server, nil
}

// NewStreamableHTTPHandler creates a stateless Streamable HTTP handler backed
// by registry.
//
// The handler returns the same MCP Server for every request, so HTTP and stdio
// expose the identical Tool Registry. Stateless mode is required for current
// MCP HTTP clients and makes an interrupted request release only its temporary
// protocol session; it never closes Registry-owned terminal Sessions.
func NewStreamableHTTPHandler(registry *tool.Registry) (http.Handler, error) {
	server, err := NewServer(registry)
	if err != nil {
		return nil, err
	}
	return protocol.NewStreamableHTTPHandler(func(*http.Request) *protocol.Server {
		return server
	}, &protocol.StreamableHTTPOptions{
		Stateless:                    true,
		PropagateRequestCancellation: true,
	}), nil
}

// Run serves one MCP client over transport until it disconnects or ctx ends.
//
// A normal client disconnect and context cancellation are successful shutdowns.
// Run deliberately does not close registry or its Sessions, because both can be
// shared with CLI or future GUI callers outside this protocol adapter.
func Run(ctx context.Context, registry *tool.Registry, transport protocol.Transport) error {
	server, err := NewServer(registry)
	if err != nil {
		return err
	}
	if err := server.Run(ctx, transport); err != nil && !isNormalDisconnect(err) {
		return fmt.Errorf("run MCP server: %w", err)
	}
	return nil
}

// isNormalDisconnect recognizes both direct EOF and the SDK's current wrapped
// EOF form. The latter is returned when a stdio client closes input while the
// SDK is concurrently finishing a request.
func isNormalDisconnect(err error) bool {
	return errors.Is(err, io.EOF) || errors.Is(err, context.Canceled) || strings.Contains(err.Error(), "server is closing: EOF")
}

type adapter struct {
	registry *tool.Registry
	exposed  []exposedTool
}

type exposedTool struct {
	name          string
	target        string
	description   string
	schema        tool.InputSchema
	requireCursor bool
}

// newAdapter records the public MCP names and their Registry targets.
// terminal_wait reuses terminal_read's implementation while requiring a cursor,
// so waiting does not duplicate the underlying Tool logic.
func newAdapter(registry *tool.Registry) (*adapter, error) {
	if registry == nil {
		return nil, ErrNilRegistry
	}
	lookup := func(name string) (tool.Tool, error) {
		registered, ok := registry.Get(name)
		if !ok {
			return nil, fmt.Errorf("required tool %q is not registered", name)
		}
		return registered, nil
	}
	list, err := lookup("terminal_list_sessions")
	if err != nil {
		return nil, err
	}
	read, err := lookup("terminal_read")
	if err != nil {
		return nil, err
	}
	readActivity, err := lookup("terminal_read_activity")
	if err != nil {
		return nil, err
	}
	write, err := lookup("terminal_write")
	if err != nil {
		return nil, err
	}
	writeLeased, err := lookup("terminal_write_leased")
	if err != nil {
		return nil, err
	}
	acquireLease, err := lookup("terminal_acquire_lease")
	if err != nil {
		return nil, err
	}
	releaseLease, err := lookup("terminal_release_lease")
	if err != nil {
		return nil, err
	}
	open, err := lookup("terminal_open_serial")
	if err != nil {
		return nil, err
	}
	listSerialPorts, err := lookup("terminal_list_serial_ports")
	if err != nil {
		return nil, err
	}
	listDevices, err := lookup("terminal_list_devices")
	if err != nil {
		return nil, err
	}
	readDeviceEvents, err := lookup("terminal_read_device_events")
	if err != nil {
		return nil, err
	}
	connectionDecision, err := lookup("terminal_get_connection_decision")
	if err != nil {
		return nil, err
	}
	closeTool, err := lookup("terminal_close")
	if err != nil {
		return nil, err
	}
	return &adapter{registry: registry, exposed: []exposedTool{
		{name: "terminal_list_sessions", target: list.Name(), description: "List active terminal sessions and their lifecycle states.", schema: list.InputSchema()},
		{name: "terminal_read", target: read.Name(), description: read.Description(), schema: read.InputSchema()},
		{name: "terminal_read_activity", target: readActivity.Name(), description: readActivity.Description(), schema: readActivity.InputSchema()},
		{name: "terminal_write", target: write.Name(), description: write.Description(), schema: write.InputSchema()},
		{name: "terminal_write_leased", target: writeLeased.Name(), description: writeLeased.Description(), schema: writeLeased.InputSchema()},
		{name: "terminal_acquire_lease", target: acquireLease.Name(), description: acquireLease.Description(), schema: acquireLease.InputSchema()},
		{name: "terminal_release_lease", target: releaseLease.Name(), description: releaseLease.Description(), schema: releaseLease.InputSchema()},
		{name: "terminal_wait", target: read.Name(), description: "Wait for terminal output after cursor and return the next output chunk.", schema: waitSchema(read.InputSchema()), requireCursor: true},
		{name: "terminal_wait_activity", target: readActivity.Name(), description: "Wait for Session activity events after cursor and return the next event chunk.", schema: waitSchema(readActivity.InputSchema()), requireCursor: true},
		{name: "terminal_open_serial", target: open.Name(), description: open.Description(), schema: open.InputSchema()},
		{name: "terminal_list_serial_ports", target: listSerialPorts.Name(), description: listSerialPorts.Description(), schema: listSerialPorts.InputSchema()},
		{name: "terminal_list_devices", target: listDevices.Name(), description: listDevices.Description(), schema: listDevices.InputSchema()},
		{name: "terminal_read_device_events", target: readDeviceEvents.Name(), description: readDeviceEvents.Description(), schema: readDeviceEvents.InputSchema()},
		{name: "terminal_wait_device_event", target: readDeviceEvents.Name(), description: "Wait for a device appearance or disappearance event after cursor. After appeared, call terminal_get_connection_decision before terminal_open_serial.", schema: waitSchema(readDeviceEvents.InputSchema()), requireCursor: true},
		{name: "terminal_get_connection_decision", target: connectionDecision.Name(), description: connectionDecision.Description(), schema: connectionDecision.InputSchema()},
		{name: "terminal_close", target: closeTool.Name(), description: closeTool.Description(), schema: closeTool.InputSchema()},
	}}, nil
}

// waitSchema adds cursor to the required fields without changing the Tool's
// schema, because terminal_wait must never silently degrade into a recent read.
func waitSchema(schema tool.InputSchema) tool.InputSchema {
	required := append([]string(nil), schema.Required...)
	for _, field := range required {
		if field == "cursor" {
			return schema
		}
	}
	schema.Required = append(required, "cursor")
	return schema
}

// handler converts one MCP request to a Registry call and returns all Tool
// failures as MCP tool results. This keeps recoverable terminal errors visible
// to an Agent instead of hiding them behind a protocol-level JSON-RPC failure.
func (a *adapter) handler(exposed exposedTool) protocol.ToolHandler {
	return func(ctx context.Context, request *protocol.CallToolRequest) (*protocol.CallToolResult, error) {
		var input json.RawMessage
		if request != nil && request.Params != nil {
			input = request.Params.Arguments
		}
		if exposed.requireCursor {
			if err := validateWaitInput(exposed.name, input); err != nil {
				return errorResult(exposed.name, err), nil
			}
		}
		result, err := a.registry.Call(ctx, exposed.target, input)
		if err != nil {
			return errorResult(exposed.name, err), nil
		}
		return successResult(result)
	}
}

// validateWaitInput gives a clear error before terminal_read could interpret an
// absent cursor as a non-blocking recent-output request.
func validateWaitInput(toolName string, input json.RawMessage) error {
	var fields map[string]json.RawMessage
	if err := json.Unmarshal(input, &fields); err != nil {
		return fmt.Errorf("decode %s input: %w", toolName, err)
	}
	cursor, ok := fields["cursor"]
	if !ok || string(cursor) == "null" {
		return ErrRequiredCursor
	}
	return nil
}

// successResult supplies both structured data and readable JSON text for MCP
// clients that use either result representation.
func successResult(result tool.Result) (*protocol.CallToolResult, error) {
	encoded, err := json.Marshal(result)
	if err != nil {
		return nil, fmt.Errorf("encode tool result: %w", err)
	}
	return &protocol.CallToolResult{
		Content:           []protocol.Content{&protocol.TextContent{Text: string(encoded)}},
		StructuredContent: result,
	}, nil
}

// errorResult preserves the underlying error in concise Agent-facing text.
func errorResult(name string, err error) *protocol.CallToolResult {
	return &protocol.CallToolResult{
		Content: []protocol.Content{&protocol.TextContent{Text: fmt.Sprintf("%s failed: %v", name, err)}},
		IsError: true,
	}
}
