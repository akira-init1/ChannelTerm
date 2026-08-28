// Package tool defines protocol-neutral callable tools for ChannelTerm.
//
// Tool definitions intentionally use a small JSON-Schema-shaped input model so
// future MCP or model adapters can expose the same terminal operations without
// making the Session Core depend on either integration.
package tool

import (
	"context"
	"encoding/json"
)

// Tool describes one named operation that can be exposed by a Tool Registry.
//
// Call receives a JSON object matching InputSchema and returns structured data.
// Implementations must not write user-facing output to process streams; callers
// decide how to render Result and errors.
type Tool interface {
	// Name returns the unique stable identifier used by Registry lookups.
	Name() string
	// Description explains the operation to a human or a future tool adapter.
	Description() string
	// InputSchema describes the JSON object accepted by Call.
	InputSchema() InputSchema
	// Call executes the operation. ctx controls cancellation and any deadline.
	Call(ctx context.Context, input json.RawMessage) (Result, error)
}

// InputSchema describes a JSON object accepted by a Tool.
//
// It deliberately mirrors the useful subset of JSON Schema used by MCP and
// function calling: object properties, their primitive types, descriptions,
// and required fields. Protocol adapters can serialize it directly.
type InputSchema struct {
	Type       string                   `json:"type"`
	Properties map[string]InputProperty `json:"properties,omitempty"`
	Required   []string                 `json:"required,omitempty"`
}

// InputProperty describes a single property within an InputSchema object.
type InputProperty struct {
	Type        string   `json:"type"`
	Description string   `json:"description,omitempty"`
	Enum        []string `json:"enum,omitempty"`
}

// Result contains structured data returned by a Tool call.
//
// Values must remain JSON-serializable so future protocol adapters can forward
// them without parsing console-oriented text.
type Result map[string]any
