# MCP Tool Reference

The current MCP adapter exposes 16 tool names. Thirteen protocol-neutral tools are registered from `internal/mcp/terminal`; the MCP adapter adds the three cursor-required wait names `terminal_wait`, `terminal_wait_activity`, and `terminal_wait_device_event` over the corresponding read implementations.

Successful calls return both structured content and an equivalent JSON text content item. Recoverable tool failures return an MCP tool result with `isError: true` and text beginning `<tool-name> failed:`. Structured inputs decoded by the terminal adapter reject unknown fields.

## Common cursor rules

- Output, activity, and device-event cursors are independent monotonically increasing positions.
- A read without `cursor` returns a recent snapshot immediately.
- A read with `cursor` waits until data is available, the request is cancelled, the source closes, or `timeout_ms` expires.
- `timeout_ms` requires `cursor`, must be positive, and cannot exceed `86400000` (24 hours).
- A `terminal_wait*` tool additionally requires `cursor` in its public MCP schema.
- `dropped: true` means the requested cursor predates the bounded retention window. Continue with the returned `next`, but treat older data as lost.

Session-addressed tools accept either an opaque `session_id` or a current short Session reference such as `SER-1` in the property named `session_id`. Labels and `device_id` values are not Session addresses.

## `terminal_list_serial_ports`

Purpose: enumerate serial endpoints currently reported by the operating system without opening them.

- Required input: none; use `{}`.
- Optional input: none.
- Result: `ports`, an array of objects containing `name`, `vid`, `pid`, `usb_serial`, `manufacturer`, `product`, and `usb_path`. Metadata strings are best effort and may be empty.

```json
{}
```

```json
{"ports":[{"name":"COM8","vid":"0403","pid":"6010","usb_serial":"ABC123","manufacturer":"FTDI","product":"USB UART","usb_path":"1-2"}]}
```

Important errors: operating-system enumeration failures and request cancellation. Safety: read-only; it does not load profiles, change Device Registry state, create a Session, or send bytes.

## `terminal_list_devices`

Purpose: return the Device Registry's current presence and identity snapshot.

- Required input: none; use `{}`.
- Optional input: none.
- Result: `devices`, an array with `device_id`, `identity_method`, `persistent`, `transport`, `endpoint`, `state`, USB metadata fields, `first_seen`, and `last_seen`.

```json
{}
```

```json
{"devices":[{"device_id":"dev_0123456789abcdef0123456789abcdef","identity_method":"usb_serial","persistent":true,"transport":"serial","endpoint":"COM8","state":"present","vid":"0403","pid":"6010","usb_serial":"ABC123","manufacturer":"FTDI","product":"USB UART","usb_path":"1-2","first_seen":"2026-08-28T09:00:00Z","last_seen":"2026-08-28T09:00:03Z"}]}
```

Important errors: cancellation or an unavailable/uninitialized application Device Registry. Safety: read-only; presence does not imply an open Session.

## `terminal_read_device_events`

Purpose: read retained appearance/disappearance events immediately, or wait after a supplied cursor.

- Required input: none.
- Optional input: `cursor` (integer), `max_events` (integer; 0 means 1024), `timeout_ms`.
- Result: `events` array with `timestamp`, `type` (`appeared` or `disappeared`), `transport`, and `endpoint`; plus `next` and `dropped`.

```json
{"cursor":0,"max_events":16,"timeout_ms":30000}
```

```json
{"events":[{"timestamp":"2026-08-28T09:01:00Z","type":"appeared","transport":"serial","endpoint":"COM11"}],"next":1,"dropped":false}
```

Important errors: non-positive effective limits, invalid timeout, timeout without cursor, cancellation, timeout, or closed Registry. Safety: events describe discovery only. After an `appeared` event, evaluate `terminal_get_connection_decision` before a discovery-driven open.

## `terminal_wait_device_event`

Purpose: wait for device events after a known device-event cursor. This MCP name invokes the same implementation and returns the same fields as `terminal_read_device_events`.

- Required input: `cursor`.
- Optional input: `max_events`, `timeout_ms`.
- Result: `events`, `next`, `dropped`.

```json
{"cursor":1,"timeout_ms":30000}
```

Important errors: missing/null cursor, invalid timeout or limit, cancellation, timeout, or Registry closure. Safety: waiting never opens a port or creates a Session; use the decision tool after an appearance event.

## `terminal_get_connection_decision`

Purpose: evaluate the configured discovery policy for one exact runtime endpoint without taking the action.

- Required input: non-empty `transport` and `endpoint` strings.
- Optional input: none.
- Result always includes `transport`, `endpoint`, `present`, `connected`, `policy`, and `action`.
- When absent, result also includes `reason: "device_not_present"`.
- When an active Session already owns the endpoint, result also includes `session_id`, `session_ref`, and `reason: "already_connected"`.

```json
{"transport":"serial","endpoint":"COM8"}
```

```json
{"transport":"serial","endpoint":"COM8","present":true,"connected":false,"policy":"ask","action":"ask"}
```

Actions are `none`, `ask`, `connect`, or `deny`. `new`, `connecting`, `open`, and `closing` Sessions count as connected; `failed` and `closed` Sessions do not.

Important errors: missing/blank fields, cancellation, or unavailable Device Registry. Safety: the tool never prompts, opens, closes, or writes. `ask` requires client-obtained user approval; `connect` permits a client-controlled open only when connection settings are known. `deny` affects discovery-driven behavior, not an explicit user request.

## `terminal_open_serial`

Purpose: resolve serial configuration, optionally save it, connect or reuse a Session, and register it with the host Manager.

- Required input: no schema field is always required, but profile resolution and overrides must produce a non-empty port.
- Optional input: `profile`, `config_path`, `save`, `port`, `baud`, `data_bits`, `parity`, `stop_bits`, `flow_control`, `wake`, and `label`.
- Accepted enums: parity `none|odd|even|mark|space`; stop bits `1|1.5|2`; flow control `none|software|hardware`.
- Result: `session_id`, `session_ref`, and `reused`.

```json
{"port":"COM8","baud":115200,"data_bits":8,"parity":"none","stop_bits":"1","flow_control":"none","wake":false,"label":"board"}
```

```json
{"session_id":"0123456789abcdef0123456789abcdef","session_ref":"SER-1","reused":false}
```

Within one Manager, ChannelTerm shares one active Session for an exact transport/endpoint pair. A repeated open returns the original Session and metadata with `reused: true`. A new Session sends no input unless `wake` resolves to true, in which case one system-attributed carriage return is written after connect. `save` persists the resolved profile before the physical open.

Important errors: missing port after resolution, unknown profile, invalid serial settings, unsupported non-`none` flow control, control characters in `label`, configuration read/write failure, busy/missing/inaccessible port, cancellation, connection failure, or repeated ID collision. Safety: this tool can open real hardware and `wake` can transmit data. For a discovery-driven open, call it only after user approval or a decision action of `connect`.

## `terminal_list_sessions`

Purpose: list the host Manager's current Session snapshot.

- Required input: none; use `{}`.
- Optional input: none.
- Result: `sessions`, sorted by short reference. Each object contains `session_id`, `session_ref`, `transport`, `endpoint`, `label`, and `state`. An actively leased Session also has `lease` with `type`, `created_at`, and `state`; owner capabilities are never returned.

```json
{}
```

```json
{"sessions":[{"session_id":"0123456789abcdef0123456789abcdef","session_ref":"SER-1","transport":"serial","endpoint":"COM8","label":"board","state":"open"}]}
```

Important errors: request cancellation. Safety: read-only snapshot; state can change immediately after the call. Empty and duplicate labels are valid and are not lookup keys.

## `terminal_read`

Purpose: read recent raw Session output, or read/wait from an output cursor.

- Required input: `session_id`.
- Optional input: `cursor`, `max_bytes` (0 means 65536), `encoding` (`utf8`, `hex`, or `base64`; default `utf8`), and `timeout_ms`.
- Result: `data`, `encoding`, `bytes_read`, `next`, and `dropped`.

```json
{"session_id":"SER-1","max_bytes":4096,"encoding":"utf8"}
```

```json
{"data":"boot> ","encoding":"utf8","bytes_read":6,"next":42,"dropped":false}
```

`bytes_read` and `next` count raw bytes regardless of result encoding. UTF-8 mode fails on malformed bytes rather than replacing them; retry with hex or Base64 for binary output.

Important errors: missing/unknown Session, Session not open, invalid/non-positive limit, unsupported encoding, invalid UTF-8, timeout without cursor, invalid timeout, cancellation, deadline, EOF after close, or underlying reader failure. Safety: read-only; each caller owns its cursor and does not consume another caller's output.

## `terminal_wait`

Purpose: wait for output after a known cursor. It invokes `terminal_read` but prevents an accidental recent snapshot.

- Required input: `session_id`, `cursor`.
- Optional input: `max_bytes`, `encoding`, `timeout_ms`.
- Result: `data`, `encoding`, `bytes_read`, `next`, `dropped`.

```json
{"session_id":"SER-1","cursor":42,"timeout_ms":5000}
```

Important errors: missing/null cursor and every `terminal_read` error. Safety: cancellation releases the wait and does not close the Session; an interrupted HTTP request can reconnect and continue with its saved cursor.

## `terminal_read_activity`

Purpose: read recent Session operation metadata, or read/wait from an activity cursor. Activity is separate from remote terminal output.

- Required input: `session_id`.
- Optional input: `cursor`, `max_events` (0 means 1024), `timeout_ms`.
- Result: `events`, `next`, and `dropped`. Each event contains `timestamp`, `actor`, `operation`, `data`, and `encoding`.

```json
{"session_id":"SER-1"}
```

```json
{"events":[{"timestamp":"2026-08-28T09:02:00Z","actor":"agent","operation":"write","data":"c3RhdHVzXHI=","encoding":"base64"}],"next":7,"dropped":false}
```

The current operation is `write`; event data is always Base64 and contains only bytes confirmed written by the Transport.

Important errors: missing/unknown Session, Session not open, invalid limit or timeout, timeout without cursor, cancellation, deadline, EOF, or activity-buffer failure. Safety: reading activity never reads or alters remote output cursors.

## `terminal_wait_activity`

Purpose: wait for Session activity after a known activity cursor. It invokes `terminal_read_activity` with a required cursor.

- Required input: `session_id`, `cursor`.
- Optional input: `max_events`, `timeout_ms`.
- Result: `events`, `next`, `dropped`.

```json
{"session_id":"SER-1","cursor":7,"timeout_ms":5000}
```

Important errors: missing/null cursor and every `terminal_read_activity` error. Safety: cancellation ends only this wait and does not affect the Session or another consumer.

## `terminal_write`

Purpose: write an explicitly encoded payload to an active Session without adding delimiters.

- Required input: `session_id`, `data`.
- Optional input: `encoding` (`utf8`, `hex`, or `base64`; default `utf8`) and `actor` (`user`, `agent`, or `system`; default `agent`).
- Result: `bytes_written`.

```json
{"session_id":"SER-1","data":"status\r","encoding":"utf8","actor":"agent"}
```

```json
{"bytes_written":7}
```

Hex input may contain whitespace. Base64 uses the standard encoding. The entire encoded payload is validated before Session write, so malformed hex or Base64 writes nothing. The actor is retained only in the activity buffer and is never inserted into the device byte stream.

Important errors: missing/unknown Session, Session not open, invalid encoding or encoded data, invalid actor, cancellation before or during application retries, short write, and transport write failure. Safety: this tool controls the remote terminal. Include `\r` or `\n` only when the target protocol requires it; ChannelTerm adds neither automatically.

When another operation owns an exclusive lease, this unchanged tool returns a busy error such as `Session SER-1 is locked by file-transfer`. It never waits for the lease or accepts an owner capability.

## `terminal_acquire_lease`

Purpose: acquire one exclusive application-level writer lease for an active Session.

- Required input: `session_id`, caller-generated opaque `owner`, and `type` (`terminal`, `file-transfer`, or reserved `debug`).
- Optional input: none.
- Result: canonical `session_id`, `type`, UTC `created_at`, and `state` (`active`). The owner capability is not echoed.

```json
{"session_id":"SER-1","owner":"file-transfer-opaque-capability","type":"file-transfer"}
```

Only one lease may be active for a Session. Readers and their cursors continue normally. Other ordinary writers fail immediately; a separate Session is unaffected. Important errors: missing Session, invalid owner/type, or an already active lease. Safety: an owner is a bearer capability and should be generated randomly, retained only for the operation, and never logged.

## `terminal_write_leased`

Purpose: write through an active lease without changing the stable `terminal_write` schema.

- Required input: `session_id`, `owner`, and `data`.
- Optional input: `encoding` and `actor`, with the same semantics as `terminal_write`.
- Result: `bytes_written`.

The owner must exactly match the active lease for this Session. This tool exists for multi-step operations such as the CLI file transfer; normal terminal clients should continue to use `terminal_write`.

## `terminal_release_lease`

Purpose: release an exclusive Session lease after its operation completes or fails.

- Required input: `session_id`, `owner`.
- Optional input: none.
- Result: `released: true`.

Release is idempotent when no lease remains, but a different owner receives an ownership error. Closing a Session also discards its lease state.

## `terminal_close`

Purpose: remove one Session from the Manager, close its Transport, and release the endpoint.

- Required input: non-empty `session_id` (opaque ID or short Session reference).
- Optional input: none.
- Result: the canonical `session_id`, `session_ref`, and `closed: true`.

```json
{"session_id":"SER-1"}
```

```json
{"session_id":"0123456789abcdef0123456789abcdef","session_ref":"SER-1","closed":true}
```

Important errors: missing, unknown, or already removed Session; cancellation before invocation; transport close failure. Safety: this is a lifecycle-destructive operation for every attached client. Detach a single CLI by closing its MCP client instead when the shared Session must remain open.
