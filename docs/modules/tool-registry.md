# Tool Registry Module

`internal/core/tool` defines a small protocol-neutral callable Tool boundary. It allows Core
operations to be registered without importing MCP or another model/protocol SDK.

A Tool supplies a stable name, human description, JSON-object input schema, and cancellable call
returning JSON-serializable structured data. The schema subset supports object properties, primitive
types, array item schemas, descriptions, enums, and required fields.

## Registry behavior

`Registry` supports concurrent registration, lookup, sorted listing, and invocation. Names are
trimmed for validation but stored and looked up by the Tool's stable name. Empty names,
nil/typed-nil tools, duplicate registration, and unknown calls are errors.

Registry holds its lock only while accessing the map. `Tool.Call` runs after the lock is released,
so a slow or waiting terminal operation does not block discovery or unrelated calls. Context is
checked before dispatch and then passed to the Tool.

Registry does not replace existing Tools, own Sessions, know MCP result types, or format user-facing
output.

## MCP adaptation

`internal/mcp/terminal` implements registered Tool names for serial, Session, file-transfer, device,
decision, and user-space debug use cases. `internal/mcp/server.go` requires those names and exposes them over MCP. The
three generic wait tools below are public aliases with `cursor` made required; the focused
`terminal_wait_file_transfer` Tool is registered directly and filters the Session event stream by
`transfer_id` until both one terminal transfer result and its file-transfer lease release are
present.

```text
terminal_read                -> terminal_read + terminal_wait
terminal_read_activity       -> terminal_read_activity + terminal_wait_activity
terminal_read_device_events  -> terminal_read_device_events + terminal_wait_device_event
terminal_wait_file_transfer  -> terminal_wait_file_transfer
```

The MCP adapter reports Tool errors as recoverable error results and supplies successful structured
content plus JSON text. It attaches an output schema to every public MCP name, including the wait
aliases, without adding MCP-specific schema requirements to the protocol-neutral Core Tool
interface. The adapter also owns human-readable tool titles and MCP behavioral annotations for
read-only, destructive, idempotent, and open-world hints; these hints do not change authorization or
enforce safety. It owns MCP-facing compatibility: public names, titles, descriptions, exposed
schemas, annotations, and result translation. The Core Registry remains protocol-neutral.

See [MCP tools](../reference/mcp-tools.md) for the actual public surface.


The `debug_bridge_*` adapters expose the separate external bridge Application operations, including
read-only status, explicit heartbeat renewal, Session-closing abort, and verified restoration.
Connection secrets are returned only to an authorized start caller. The relay invokes Session
services directly; RSP bytes do not travel through MCP tool calls. The Core Registry remains unaware
of TLS, target readiness, and debugger process ownership.

`debug_bridge_report` translates caller-supplied command/result/state observations into Application
report publication. It is an authenticated event-write tool, not GDB control. Its receipt confirms
report acceptance only; observers use the existing `terminal_session_events` cursor interface.
