# Tool Registry Module

`internal/core/tool` defines a small protocol-neutral callable Tool boundary. It allows Core operations to be registered without importing MCP or another model/protocol SDK.

A Tool supplies a stable name, human description, JSON-object input schema, and cancellable call returning JSON-serializable structured data. The schema subset supports object properties, primitive types, descriptions, enums, and required fields.

## Registry behavior

`Registry` supports concurrent registration, lookup, sorted listing, and invocation. Names are trimmed for validation but stored and looked up by the Tool's stable name. Empty names, nil/typed-nil tools, duplicate registration, and unknown calls are errors.

Registry holds its lock only while accessing the map. `Tool.Call` runs after the lock is released, so a slow or waiting terminal operation does not block discovery or unrelated calls. Context is checked before dispatch and then passed to the Tool.

Registry does not replace existing Tools, own Sessions, know MCP result types, or format user-facing output.

## MCP adaptation

`internal/mcp/terminal` implements ten registered Core Tool names for serial, Session, device, and decision use cases. `internal/mcp/server.go` requires those names and exposes them over MCP, adding three public wait aliases with `cursor` made required.

```text
terminal_read                -> terminal_read + terminal_wait
terminal_read_activity       -> terminal_read_activity + terminal_wait_activity
terminal_read_device_events  -> terminal_read_device_events + terminal_wait_device_event
```

The MCP adapter reports Tool errors as recoverable error results and supplies successful structured content plus JSON text. It owns MCP-facing compatibility: public names, descriptions, exposed schemas, and result translation. The Core Registry remains protocol-neutral.

See [MCP tools](../reference/mcp-tools.md) for the actual 13-name public surface.
