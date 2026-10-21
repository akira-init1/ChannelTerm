package terminal

import (
	"bytes"
	"context"
	"encoding/base64"
	"encoding/json"
	"errors"

	"github.com/akira-init1/ChannelTerm/internal/core/tool"
)

type exportDiagnosticsTool struct{ *sessionTools }

// Name returns the evidence export identifier.
func (*exportDiagnosticsTool) Name() string { return "terminal_export_diagnostics" }

// Description explains retained snapshots and sensitive-content opt-in.
func (*exportDiagnosticsTool) Description() string {
	return "Export a bounded ZIP snapshot of retained Session evidence. include_raw=true explicitly includes sensitive terminal/output/write bytes. Default exports metadata only. Returns ID, size and SHA256; download with terminal_read_diagnostic. Host memory retains at most four archives for 15 minutes, evicting oldest first. No filesystem path is exposed."
}

// InputSchema exposes no arbitrary Host paths.
func (*exportDiagnosticsTool) InputSchema() tool.InputSchema {
	return tool.InputSchema{Type: "object", Properties: map[string]tool.InputProperty{"session_id": {Type: "string", Description: activeSessionIDDescription}, "include_raw": {Type: "boolean", Description: "Explicitly include terminal contents; default false."}}, Required: []string{"session_id"}}
}

// Call exports the Application-owned snapshot.
func (t *exportDiagnosticsTool) Call(ctx context.Context, input json.RawMessage) (tool.Result, error) {
	var args struct {
		SessionID  string `json:"session_id"`
		IncludeRaw bool   `json:"include_raw"`
	}
	if err := decodeInput(input, &args); err != nil {
		return nil, err
	}
	r, err := t.application.ExportDiagnostics(ctx, args.SessionID, args.IncludeRaw)
	return jsonToolResult(r), err
}

type readDiagnosticTool struct{ *sessionTools }

// Name returns the bounded evidence download identifier.
func (*readDiagnosticTool) Name() string { return "terminal_read_diagnostic" }

// Description distinguishes archive offsets from Session cursors.
func (*readDiagnosticTool) Description() string {
	return "Read up to 65536 ZIP bytes by diagnostic_id and archive byte offset as base64. This offset is not a Session cursor. Download before expiry/eviction; verify the exported SHA256."
}

// InputSchema describes bounded archive offsets.
func (*readDiagnosticTool) InputSchema() tool.InputSchema {
	return tool.InputSchema{Type: "object", Properties: map[string]tool.InputProperty{"diagnostic_id": {Type: "string"}, "offset": {Type: "integer"}, "max_bytes": {Type: "integer", Description: "1..65536; default 65536."}}, Required: []string{"diagnostic_id", "offset"}}
}

// Call returns lossless archive bytes without terminal writes.
func (t *readDiagnosticTool) Call(ctx context.Context, input json.RawMessage) (tool.Result, error) {
	var args struct {
		ID       string `json:"diagnostic_id"`
		Offset   *int   `json:"offset"`
		MaxBytes *int   `json:"max_bytes"`
	}
	if err := decodeInput(input, &args); err != nil {
		return nil, err
	}
	if args.Offset == nil {
		return nil, errors.New("offset is required")
	}
	limit := 65536
	if args.MaxBytes != nil {
		limit = *args.MaxBytes
	}
	data, next, eof, err := t.application.ReadDiagnostic(ctx, args.ID, *args.Offset, limit)
	if err != nil {
		return nil, err
	}
	info, err := t.application.DiagnosticInfo(args.ID)
	if err != nil {
		return nil, err
	}
	return tool.Result{"data": base64.StdEncoding.EncodeToString(data), "encoding": "base64", "next": next, "eof": eof, "bytes_read": len(data), "size": info.Size, "sha256": info.SHA256}, nil
}

// jsonToolResult is used only with the JSON-safe operation/archive structs.
// Number tokens preserve raw cursors above the floating-point integer range.
func jsonToolResult(value any) tool.Result {
	encoded, _ := json.Marshal(value)
	var result tool.Result
	decoder := json.NewDecoder(bytes.NewReader(encoded))
	decoder.UseNumber() // Preserve full 64-bit raw cursors through the adapter.
	_ = decoder.Decode(&result)
	return result
}
