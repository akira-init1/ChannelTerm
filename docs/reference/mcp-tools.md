# MCP Tool Reference

The MCP adapter exposes terminal, command execution, discovery, lease, activity, file-transfer, user-space debug, and
structured Session-event tools. It also provides cursor-required wait tools for raw output,
activity, device events, and file-transfer terminal results.

The MCP initialization response reports the same ChannelTerm build version as `channelterm version`.
Ordinary source builds report `devel`; tagged release artifacts report their injected release
version. Its server instructions tell clients that users may refer to ChannelTerm as `cterm`,
distinguish single non-interactive Bash commands from raw or interactive writes, select the matching
output or file-transfer wait tool, apply the discovery connection decision before a discovery-driven
open, obtain explicit approval for an `ask` decision, treat Session close as shared-client
destructive, reserve lease and file-transfer control tools for managed multi-step operations, and
select debug_start/command/events/stop for native Linux user-space GDB.

Every tool advertised by `tools/list` includes an `outputSchema` for its structured result. Required
properties are present on every successful result; optional properties describe conditional data,
such as connection-decision reasons, active leases, and completed file-transfer paths. Successful
calls return both schema-matching structured content and an equivalent JSON text content item.
Recoverable tool failures return an MCP tool result with `isError: true` and text beginning
`<tool-name> failed:`. They also include `structuredContent.error` with `code`, `tool`, and
`message`; an operation can include partial evidence in `structuredContent.result`. Check
`isError` before decoding content: error text is not JSON. Codes include `timeout`, `cancelled`,
`session_closed`, `invalid_utf8`, and fallback `tool_error`; U-Boot adds its operation states,
`invalid_input`, `session_busy`, and `session_not_found`. Successful result shapes are unchanged.
Structured inputs decoded by the terminal adapter reject unknown fields.

Every advertised tool also includes MCP behavioral annotations. These are planning hints, not
authorization or safety enforcement:

- `readOnlyHint: true` applies to listing, reading, waiting, connection-decision, and file-transfer
  checkpoint tools because they only observe retained or managed state.
- `destructiveHint: true` applies to serial open, terminal command/write tools, transfer cancellation
  resolution, lease release, and Session close. Other state-changing tools explicitly report it as
  `false`. Serial open is conservative because it can save configuration and opening or waking a
  physical endpoint can have target-specific effects.
- `openWorldHint: false` applies to tools confined to ChannelTerm-managed Sessions, discovery state,
  and leases. `terminal_open_serial`, `terminal_exec`, `terminal_write`, and
  `terminal_write_leased` report `true` because they can interact with a physical target or arbitrary
  terminal behavior beyond ChannelTerm's managed state.
- `terminal_release_lease` reports `idempotentHint: true`; releasing an already absent lease succeeds
  without another state change.

Every advertised tool has a human-readable top-level `title` for tool pickers, approval dialogs,
and logs. The `terminal_*` name remains the stable machine-readable compatibility key.

| Tool name | Display title |
| --- | --- |
| `terminal_enter_uboot` | Enter U-Boot |
| `terminal_export_diagnostics` | Export Session Diagnostics |
| `terminal_read_diagnostic` | Download Diagnostic Archive |
| `terminal_list_sessions` | List Terminal Sessions |
| `terminal_read` | Read Terminal Output |
| `terminal_read_activity` | Read Session Activity |
| `terminal_session_events` | Read Session Events |
| `terminal_wait_file_transfer` | Wait for File Transfer Result |
| `terminal_session_attach` | Attach to Terminal Session |
| `terminal_session_detach` | Detach from Terminal Session |
| `terminal_report_file_transfer` | Report File Transfer Event |
| `terminal_exec` | Execute Terminal Command |
| `terminal_write` | Write Raw Terminal Input |
| `terminal_write_leased` | Write Leased Terminal Input |
| `terminal_acquire_lease` | Acquire Session Lease |
| `terminal_renew_lease` | Renew Session Lease |
| `terminal_begin_file_transfer_cancel` | Begin File Transfer Cancellation |
| `terminal_resolve_file_transfer_cancel` | Resolve File Transfer Cancellation |
| `terminal_file_transfer_checkpoint` | Check File Transfer Control |
| `terminal_release_lease` | Release Session Lease |
| `terminal_wait` | Wait for Terminal Output |
| `terminal_wait_activity` | Wait for Session Activity |
| `terminal_open_serial` | Open Serial Session |
| `terminal_list_serial_ports` | List Serial Ports |
| `terminal_list_devices` | List Discovered Devices |
| `terminal_read_device_events` | Read Device Events |
| `terminal_wait_device_event` | Wait for Device Event |
| `terminal_get_connection_decision` | Get Connection Decision |
| `terminal_close` | Close Terminal Session |
| `debug_start` | Start User-Space Debugging |
| `debug_command` | Control GDB |
| `debug_events` | Read Debug Events |
| `debug_stop` | End User-Space Debugging |
| `debug_bridge_list` | List External GDB Bridge Ports |
| `debug_bridge_start` | Open External GDB Bridge |
| `debug_bridge_status` | Read External GDB Bridge Status |
| `debug_bridge_renew` | Renew External GDB Bridge |
| `debug_bridge_close` | Close External GDB Bridge and Session |
| `debug_bridge_restore` | Verify Terminal Restoration after RSP |
| `debug_bridge_report` | Report External GDB Activity from Codex |

Streamable HTTP requests require `Authorization: Bearer <token>`. An automatically started,
attachment-owned Host returns `X-ChannelTerm-Host-Lifetime: attachment` on every authenticated MCP
response so clients can distinguish it from a separately started persistent Host. The header does
not change tool schemas or result objects.

## Common cursor rules

- Output, activity, Session-event, and device-event cursors are independent monotonically increasing
  positions.
- A read without `cursor` returns a recent snapshot immediately.
- A read with `cursor` waits until data is available, the request is cancelled, the source closes,
  or `timeout_ms` expires.
- Cancelling a Streamable HTTP wait leaves that same MCP client connection usable for later calls;
  resume with the saved cursor.
- `timeout_ms` requires `cursor`, must be positive, and cannot exceed `86400000` (24 hours).
- A `terminal_wait*` tool additionally requires `cursor` in its public MCP schema.
- `dropped: true` means the requested cursor predates the bounded retention window. Continue with
  the returned `next`, but treat older data as lost.

Session-addressed tools accept either an opaque `session_id` or a current short Session reference
such as `SER-1` in the property named `session_id`. Their schemas describe this transport-neutral
input as an active Session ID or short reference. These tools are transport-neutral once a Session
is active; `terminal_open_serial` is currently the only MCP tool that creates a Session.
The CLI `ssh` command can host SSH Sessions accessible through the existing generic Session
tools, including `SSH-N` references. There is no MCP tool for opening SSH connections. Labels and
`device_id` values are not Session addresses.

Dedicated `sftp`/`scp` CLI operations do not create terminal Sessions and are not new MCP tools.
They use independent authenticated file connections rather than `terminal_write` protocol bytes.

## `terminal_list_serial_ports`

Purpose: enumerate serial endpoints currently reported by the operating system without opening them.

- Required input: none; use `{}`.
- Optional input: none.
- Result: `ports`, an array of objects containing `name`, `vid`, `pid`, `usb_serial`,
  `manufacturer`, `product`, and `usb_path`. Metadata strings are best effort and may be empty.

```json
{}
```

```json
{"ports":[{"name":"COM8","vid":"0403","pid":"6010","usb_serial":"ABC123","manufacturer":"FTDI","product":"USB UART","usb_path":"1-2"}]}
```

Important errors: operating-system enumeration failures and request cancellation. Safety: read-only;
it does not load profiles, change Device Registry state, create a Session, or send bytes.

## `terminal_list_devices`

Purpose: return the Device Registry's current presence and identity snapshot.

- Required input: none; use `{}`.
- Optional input: none.
- Result: `devices`, an array with `device_id`, `identity_method`, `persistent`, `transport`,
  `endpoint`, `state`, USB metadata fields, `first_seen`, and `last_seen`.

```json
{}
```

```json
{"devices":[{"device_id":"dev_0123456789abcdef0123456789abcdef","identity_method":"usb_serial","persistent":true,"transport":"serial","endpoint":"COM8","state":"present","vid":"0403","pid":"6010","usb_serial":"ABC123","manufacturer":"FTDI","product":"USB UART","usb_path":"1-2","first_seen":"2026-08-28T09:00:00Z","last_seen":"2026-08-28T09:00:03Z"}]}
```

Important errors: cancellation or an unavailable/uninitialized application Device Registry. Safety:
read-only; presence does not imply an open Session.

## `terminal_read_device_events`

Purpose: read retained appearance/disappearance events immediately, or wait after a supplied cursor.

- Required input: none.
- Optional input: `cursor` (integer), `max_events` (integer; 0 means 1024), `timeout_ms`.
- Result: `events` array with `timestamp`, `type` (`appeared` or `disappeared`), `transport`, and
  `endpoint`; plus `next` and `dropped`.

```json
{"cursor":0,"max_events":16,"timeout_ms":30000}
```

```json
{"events":[{"timestamp":"2026-08-28T09:01:00Z","type":"appeared","transport":"serial","endpoint":"COM11"}],"next":1,"dropped":false}
```

Important errors: non-positive effective limits, invalid timeout, timeout without cursor,
cancellation, timeout, or closed Registry. Safety: events describe discovery only. After an
`appeared` event, evaluate `terminal_get_connection_decision` before a discovery-driven open.

## `terminal_wait_device_event`

Purpose: wait for device events after a known device-event cursor. This MCP name invokes the same
implementation and returns the same fields as `terminal_read_device_events`.

- Required input: `cursor`.
- Optional input: `max_events`, `timeout_ms`.
- Result: `events`, `next`, `dropped`.

```json
{"cursor":1,"timeout_ms":30000}
```

Important errors: missing/null cursor, invalid timeout or limit, cancellation, timeout, or Registry
closure. Safety: waiting never opens a port or creates a Session; use the decision tool after an
appearance event.

## `terminal_get_connection_decision`

Purpose: evaluate the configured discovery policy for one exact runtime endpoint without taking the
action.

- Required input: non-empty `transport` and `endpoint` strings.
- Optional input: none.
- Result always includes `transport`, `endpoint`, `present`, `connected`, `policy`, and `action`.
- When absent, result also includes `reason: "device_not_present"`.
- When an active Session already owns the endpoint, result also includes `session_id`,
  `session_ref`, and `reason: "already_connected"`.

```json
{"transport":"serial","endpoint":"COM8"}
```

```json
{"transport":"serial","endpoint":"COM8","present":true,"connected":false,"policy":"ask","action":"ask"}
```

Actions are `none`, `ask`, `connect`, or `deny`. `new`, `connecting`, `open`, and `closing` Sessions
count as connected; `failed` and `closed` Sessions do not.

Important errors: missing/blank fields, cancellation, or unavailable Device Registry. Safety: the
tool never prompts, opens, closes, or writes. `ask` requires client-obtained user approval;
`connect` permits a client-controlled open only when connection settings are known. `deny` affects
discovery-driven behavior, not an explicit user request.

## `terminal_open_serial`

Purpose: resolve serial configuration, optionally save it, connect or reuse a Session, and register
it with the host Manager.

- Required input: no schema field is always required, but profile resolution and overrides must
  produce a non-empty port.
- Optional input: `profile`, `config_path`, `save`, `port`, `baud`, `data_bits`, `parity`,
  `stop_bits`, `flow_control`, `wake`, and `label`.
- Accepted enums: parity `none|odd|even|mark|space`; stop bits `1|1.5|2`; flow control
  `none|software|hardware`.
- Result: `session_id`, `session_ref`, and `reused`.

```json
{"port":"COM8","baud":115200,"data_bits":8,"parity":"none","stop_bits":"1","flow_control":"none","wake":false,"label":"board"}
```

```json
{"session_id":"0123456789abcdef0123456789abcdef","session_ref":"SER-1","reused":false}
```

Within one Manager, ChannelTerm shares one active Session for an exact transport/endpoint pair. A
repeated open returns the original Session and metadata with `reused: true`. A new Session sends no
input unless `wake` resolves to true, in which case one system-attributed carriage return is written
after connect. `save` persists the resolved profile before the physical open.

Important errors: missing port after resolution, unknown profile, invalid serial settings,
unsupported non-`none` flow control, control characters in `label`, configuration read/write
failure, busy/missing/inaccessible port, cancellation, connection failure, or repeated ID collision.
Safety: this tool can open real hardware and `wake` can transmit data. For a discovery-driven open,
call it only after user approval or a decision action of `connect`.

## `terminal_list_sessions`

Purpose: list the host Manager's current Session snapshot.

- Required input: none; use `{}`.
- Optional input: none.
- Result: `sessions`, sorted by short reference. Each object contains `session_id`, `session_ref`,
  `transport`, `endpoint`, `label`, and `state`. An actively leased Session also has `lease` with
  `type`, `created_at`, `expires_at`, and `state`; a file-transfer lease adds `transfer_id`. Owner
  capabilities are never returned.

```json
{}
```

```json
{"sessions":[{"session_id":"0123456789abcdef0123456789abcdef","session_ref":"SER-1","transport":"serial","endpoint":"COM8","label":"board","state":"open"}]}
```

Important errors: request cancellation. Safety: read-only snapshot; state can change immediately
after the call. Empty and duplicate labels are valid and are not lookup keys.

## `terminal_read`

Purpose: read recent raw Session output, or read/wait from an output cursor.

- Required input: `session_id`.
- Optional input: `cursor`, `max_bytes` (0 means 65536), `encoding` (`utf8`, `hex`, or `base64`;
  default `utf8`), and `timeout_ms`.
- Result: `data`, `encoding`, `bytes_read`, `next`, and `dropped`.

```json
{"session_id":"SER-1","max_bytes":4096,"encoding":"utf8"}
```

```json
{"data":"boot> ","encoding":"utf8","bytes_read":6,"next":42,"dropped":false}
```

`bytes_read` and `next` count raw bytes regardless of result encoding. UTF-8 mode fails on malformed
bytes rather than replacing them; retry with hex or Base64 for binary output.

Important errors: missing/unknown Session, Session not open, invalid/non-positive limit, unsupported
encoding, invalid UTF-8, timeout without cursor, invalid timeout, cancellation, deadline, EOF after
close, or underlying reader failure. Safety: read-only; each caller owns its cursor and does not
consume another caller's output.

## `terminal_wait`

Purpose: wait for raw terminal output after a known cursor. It invokes `terminal_read` but prevents
an accidental recent snapshot. It does not return file-transfer state; use
`terminal_wait_file_transfer` for completion, cancellation, or failure.

- Required input: `session_id`, `cursor`.
- Optional input: `max_bytes`, `encoding`, `timeout_ms`.
- Result: `data`, `encoding`, `bytes_read`, `next`, `dropped`.

```json
{"session_id":"SER-1","cursor":42,"timeout_ms":5000}
```

Important errors: missing/null cursor and every `terminal_read` error. Safety: cancellation releases
the wait and does not close the Session; the same HTTP client can continue with its saved cursor.

## `terminal_read_activity`

Purpose: read recent Session operation metadata, or read/wait from an activity cursor. Activity is
separate from remote terminal output.

- Required input: `session_id`.
- Optional input: `cursor`, `max_events` (0 means 1024), `timeout_ms`.
- Result: `events`, `next`, and `dropped`. Each event contains `timestamp`, `actor`, `operation`,
  `data`, and `encoding`.

```json
{"session_id":"SER-1"}
```

```json
{"events":[{"timestamp":"2026-08-28T09:02:00Z","actor":"agent","operation":"write","data":"c3RhdHVzXHI=","encoding":"base64"}],"next":7,"dropped":false}
```

The current operation is `write`; event data is always Base64 and contains only bytes confirmed
written by the Transport.

Important errors: missing/unknown Session, Session not open, invalid limit or timeout, timeout
without cursor, cancellation, deadline, EOF, or activity-buffer failure. Safety: reading activity
never reads or alters remote output cursors.

## `terminal_wait_activity`

Purpose: wait for Session activity after a known activity cursor. It invokes
`terminal_read_activity` with a required cursor.

- Required input: `session_id`, `cursor`.
- Optional input: `max_events`, `timeout_ms`.
- Result: `events`, `next`, `dropped`.

```json
{"session_id":"SER-1","cursor":7,"timeout_ms":5000}
```

Important errors: missing/null cursor and every `terminal_read_activity` error. Safety: cancellation
ends only this wait and does not affect the Session or another consumer.

## `terminal_session_events`

Purpose: read retained structured Session state, or wait after an event cursor. It is read-only:
events do not include or consume terminal output.

- Required input: `session_id`.
- Optional input: `cursor`, `max_events` (0 means 1024), `timeout_ms`.
- Result: `events`, `next`, and `dropped`. Each event has `id`, `timestamp`, `session_id`, `type`,
  `actor`, and optional JSON `metadata`.

```json
{"session_id":"SER-1","cursor":3,"max_events":32,"timeout_ms":30000}
```

```json
{"events":[{"id":3,"timestamp":"2026-09-02T09:02:00Z","session_id":"0123456789abcdef0123456789abcdef","type":"FILE_TRANSFER_PROGRESS","actor":"user","metadata":{"transfer_id":"FT-123","sent":622592,"total":1048576,"percent":59.4,"speed":850000}}],"next":4,"dropped":false}
```

Current event types are `SESSION_CREATED`, `SESSION_ATTACHED`, `SESSION_DETACHED`, `LEASE_ACQUIRED`,
`LEASE_RELEASED`, `FILE_TRANSFER_STARTED`, `FILE_TRANSFER_PROGRESS`, `FILE_TRANSFER_COMPLETED`,
`FILE_TRANSFER_CANCELLED`, `FILE_TRANSFER_FAILED`, `TERMINAL_COMMAND_STARTED`,
`TERMINAL_COMMAND_OUTPUT_STARTED`, `TERMINAL_COMMAND_COMPLETED`, and `TERMINAL_COMMAND_FAILED`.
Every event belonging to one file transfer, including its `file-transfer` lease acquire/release
events, carries the same `transfer_id`. Terminal-command events carry a non-secret `command_id`; the
started event includes the original command and bootstrap start cursor, the output-started event
identifies the first command-output cursor, and completion includes the exit code plus the internal
completion-marker range. A lease event also includes an `output_cursor` metadata snapshot for the
bundled attach client's local presentation; it does not change or consume raw Session output.
`FILE_TRANSFER_CANCELLED` is emitted only after cleanup releases the lease and includes
`reason: user_cancelled`, last confirmed progress, and `lease_released: true`, so an observing AI
can distinguish cancellation from transfer failure. A slow observer receives `dropped: true` if its
cursor predates bounded retention; it cannot block the Session reader or writer. Important errors:
missing/unknown Session, Session not open, invalid limit or timeout, timeout without cursor,
cancellation, deadline, EOF, or event-buffer failure.

## `terminal_wait_file_transfer`

Purpose: wait through intermediate Session events and return only when the identified file transfer
completes, is cancelled, or fails and its matching `file-transfer` lease has been released. Use this
tool rather than `terminal_wait` for a transfer outcome; `terminal_wait` observes raw terminal bytes
and a cancellation does not guarantee another byte or shell prompt.

- Required input: `session_id`, `transfer_id`, `cursor`.
- Optional input: `max_events` (0 means 1024), `timeout_ms`.
- Result: `state` (`completed`, `cancelled`, or `failed`), `transfer_id`, `lease_released: true`,
  the terminal `event`, `next`, and `dropped`. Every completed transfer also returns top-level
  `source_path`, `requested_path`, `resolved_path`, and `renamed`; regular files additionally return
  `sha256`. `resolved_path` is the authoritative saved path after collision handling.

The three path fields have direction-independent meanings:

| Field | Meaning |
| --- | --- |
| `source_path` | Original file or directory location. |
| `requested_path` | Destination requested by the user or Agent, before collision handling. |
| `resolved_path` | Final destination actually saved by ChannelTerm. |

For a PC-to-board send these fields identify the local source, requested board destination, and
resolved board destination. For a board-to-PC receive they identify the board source, requested
local destination, and resolved local destination.

Capture the Session event cursor before starting the transfer, then obtain `transfer_id` from the
matching `FILE_TRANSFER_STARTED` event or active file-transfer lease. The cursor bounds retained
history while `transfer_id` prevents another transfer's terminal event or lease release from
satisfying the wait.

```json
{"session_id":"SER-1","transfer_id":"FT-123","cursor":7,"timeout_ms":30000}
```

```json
{"state":"cancelled","transfer_id":"FT-123","lease_released":true,"event":{"id":11,"timestamp":"2026-09-15T08:51:51Z","session_id":"0123456789abcdef0123456789abcdef","type":"FILE_TRANSFER_CANCELLED","actor":"user","metadata":{"transfer_id":"FT-123","transferred":32768,"total":98304,"percent":33.3,"reason":"user_cancelled","lease_released":true}},"next":12,"dropped":false}
```

A successful send whose requested name already exists returns the resolved name directly:

```json
{"state":"completed","transfer_id":"FT-123","lease_released":true,"source_path":"app.bin","requested_path":"/tmp/cterm/mcp-files/app.bin","resolved_path":"/tmp/cterm/mcp-files/app_1.bin","renamed":true,"sha256":"0123456789abcdef0123456789abcdef0123456789abcdef0123456789abcdef","event":{"type":"FILE_TRANSFER_COMPLETED","metadata":{"transfer_id":"FT-123","source_path":"app.bin","requested_path":"/tmp/cterm/mcp-files/app.bin","resolved_path":"/tmp/cterm/mcp-files/app_1.bin","renamed":true,"sha256":"0123456789abcdef0123456789abcdef0123456789abcdef0123456789abcdef"}},"next":14,"dropped":false}
```

Clients should always use the top-level `resolved_path` for subsequent operations on the saved file.
Completed receive results expose the same fields and use `resolved_path` for the actual local
destination. Cancellation and failure results do not promote path fields to the top level.

This is a breaking event-schema replacement with no compatibility aliases. File-transfer events no
longer publish `local_path` or `remote_path`. During start and progress, consumers can use
`source_path` and `requested_path`; after `terminal_wait_file_transfer` returns
`state: "completed"`, they must use the top-level `resolved_path` as the final saved destination.

Progress, attachment, and unrelated lifecycle events are consumed internally. Completion and failure
events occur before lease release, so the Tool retains the matching terminal event and waits for the
matching `LEASE_RELEASED`; cancellation already follows release. `next` points immediately after the
later event required to establish both outcome and release. Once the Tool returns, a subsequent
ordinary Session write does not race the completed transfer's lease. `dropped` is true if any part
of the inspected range was no longer retained. Important errors: missing/null cursor, missing/empty
`transfer_id`, malformed completed-event path metadata, missing/unknown Session, Session not open,
invalid limit or timeout, cancellation, deadline, EOF, or event-buffer failure.

## Attachment and file-transfer reporting tools

`terminal_session_attach` and `terminal_session_detach` record a CLI attachment lifecycle event.
They require `session_id` and accept an optional `actor`; they neither acquire a lease nor write or
close a Session. The bundled CLI calls them while opening and closing an MCP attachment.

`terminal_report_file_transfer` is the bundled CLI's status bridge to the host-owned event stream.
It requires `session_id`, the active lease's opaque `owner`, its `transfer_id`, and `type`
(`FILE_TRANSFER_STARTED`, `FILE_TRANSFER_PROGRESS`, `FILE_TRANSFER_COMPLETED`, or
`FILE_TRANSFER_FAILED`); it accepts optional `actor` and JSON-compatible `metadata`. The Host
rejects a missing lease, non-owner reporter, or mismatched transfer ID. It never writes terminal
bytes. Both directions use `source_path` for the origin and `requested_path` for the requested
destination. Completion is rejected unless non-empty string `source_path`, `requested_path`, and
`resolved_path` plus boolean `renamed` are present; a regular file also records `sha256`.
File-transfer events do not use `local_path` or `remote_path`. `FILE_TRANSFER_CANCELLED` is
Host-generated after a confirmed cross-process cancellation releases its lease and cannot be forged
through this reporting tool. AI clients can inspect all state through `terminal_session_events` or
wait for one terminal outcome through `terminal_wait_file_transfer`; they should not treat report
calls as a substitute for the existing file protocol or lease tools.

Bundled CLI progress metadata carries `sent` or `received`, `total`, `percent`, and best-effort
`speed`. A failed event retains those last confirmed progress values. User-confirmed cancellation
uses the stable `reason` value `user_cancelled`. A successful regular-file completion additionally
carries full lowercase `local_sha256` and `remote_sha256` values; because completion follows
verification, they match. Directory completion uses the same fields for the verified tar-stream
digest.

## `terminal_enter_uboot`

Purpose: interrupt one serial boot countdown in the Host and confirm a fresh U-Boot prompt.
This is a write-capable, non-idempotent operation that can reboot the target. It uses the existing
terminal lease and refuses competing managed operations. Stay within explicit user authorization;
an existing request to enter U-Boot need not be reconfirmed, but does not authorize repeated resets.

Required inputs: `session_id`, `mode` (`wait_reset` or `reboot`), `countdown_pattern`,
`prompt_pattern`, `boot_started_patterns`, and `interrupt_data_hex`. Patterns are literal strings
of 1..256 bytes; the prompt is one exact line including spaces. Supply 1..8 boot-start patterns.
The interrupt is 1..32 bytes encoded as complete hexadecimal pairs without whitespace.
It may be a single key or a complete non-secret string; there is no default key or appended CR/LF.
Matching uses raw bytes, not rendered terminal text, regular expressions, or wildcards.
The client must derive settings from observed output or verified board/firmware configuration,
and ask the user about missing settings before calling. Countdown text alone does not identify
the required interrupt input. See [parameter selection examples](../getting-started/serial-terminal.md#enter-u-boot-and-export-evidence).
The tool does not auto-detect settings, ask questions during execution, or provide a dedicated
credential-protection workflow. Already-idle prompts and silent countdowns are outside its fresh
countdown-then-prompt confirmation flow.
Tool discovery includes this preparation and clarification guidance without requiring an installed
skill. `terminal_read`, `terminal_write`, and `terminal_exec` descriptions also distinguish countdown
interruption from commands at an already-idle U-Boot prompt. Use raw writes for the latter, never
Bash execution; an empty Enter is not a neutral probe because it may repeat the previous command.

Optional inputs: `reboot_command` (required only in reboot mode; one printable Bash command),
`timeout_ms` (1..180000, default 60000), and `diagnostics` (`metadata`, default, or `capture`).
Reboot requires a caller-verified idle interactive Bash prompt. No command is assumed by default.
The bootstrap preserves echo, removes its own Bash history entry, and does not wait for a shell
completion marker after reboot. A failed or non-rebooting command can therefore time out.

```json
{"session_id":"SER-1","mode":"wait_reset","countdown_pattern":"Hit any key","prompt_pattern":"Zynq> ","boot_started_patterns":["Starting kernel"],"interrupt_data_hex":"20","timeout_ms":60000,"diagnostics":"capture"}
```

This example is board-dependent. Before manual reset, observe the matching `OPERATION_STATE`
event with `operation: "uboot"`, `status: "ready"`, `operation_id`, and `output_cursor` through
`terminal_session_events`. Readiness means the observer cursor is armed, not that the device reset.
Clients must allow the requested call duration and can observe events in a separate request.
Save the event cursor before starting the operation and ignore older readiness events. Completion
events include the diagnostic ID when capture succeeded, allowing another client to retrieve it
after request cancellation. The archive's independently sampled event snapshot can precede this
completion event; its manifest records the operation outcome directly.

Result fields: `operation_id`, full `session_id`, `status`, `output_start`, `output_end`,
`countdown_cursor`, `prompt_cursor`, `countdown_matched`, `prompt_matched`, `bytes_written`, `elapsed_ms`, `dropped`, and optional
`diagnostic_id` / `diagnostic_error`. Bytes written include the reboot bootstrap when used. Cursors
count raw bytes; matched flags distinguish a real match at cursor zero from no match.
Only `entered` is confirmed entry.
Failure statuses include `boot_started`, `timeout`, `cancelled`, `session_closed`, `output_lost`,
`write_failed`, `input_pending`, `unsupported_shell`, `cleanup_failed`, and fallback `failed`.
Failed calls carry partial results under `structuredContent.result` with `isError: true`.

Old buffered prompts cannot succeed. Binary noise and split markers are handled without UTF-8
decoding. Retained backlog is drained before acting; observed boot-start evidence takes precedence
over countdown/prompt matches. Output loss stops automatic writes. The Host sends the configured
interrupt at most once. A successful write without a fresh prompt is not success. Device/USB/OS
latency can still miss a window, and firmware may disable interruption entirely.

Cancellation/timeout stop further writes and release ownership without Ctrl-C, Enter, automatic
retry, persistent environment changes, or closing a healthy shared Session. Already submitted
reboot commands cannot be undone. Raw capture is opt-in and bounded; archive creation failure is
reported separately from the boot outcome. Metadata mode records operation events/results only.

## `terminal_export_diagnostics`

Purpose: create a ZIP of retained Session evidence. Required: `session_id`. Optional:
`include_raw` (default false). It never writes to the device. Raw output and write payload can
contain sensitive data; true explicitly opts into retaining them. Event metadata uses an allowlist
and excludes command text, arbitrary errors, credentials and lease owner capabilities.

Returns `diagnostic_id`, ZIP `size`, lowercase `sha256`, and UTC `expires_at`. Archives are held in
the owning Application's memory, shared under the existing Host authorization. There are no new
listeners, arbitrary Host path arguments, or automatic uploads. At most four archives, each at most
16 MiB, are retained for 15 minutes with oldest eviction. Expired data is reclaimed on store access;
expired IDs cannot be read. Host exit loses all archives. Download promptly to persist evidence.

Archive contents:

- `manifest.json`: format version, Host build/OS/architecture, known Session metadata, unknown
  serial parameters explicitly marked, per-file hashes, cursor ranges and completeness limits.
- `activity.jsonl`: at most 256 recent writes; actual payload is included only with raw opt-in,
  up to 256 KiB total, with per-record truncation flags.
- `events.jsonl`: at most 256 recent lifecycle/operation events with bounded allowlisted metadata.
- `errors.jsonl`: operation status for a captured U-Boot failure; otherwise empty. No historical
  MCP error journal is implied; `historical_errors_available` is false.
- With raw opt-in, `terminal.bin`, `terminal.hex` (pure hexadecimal), and `terminal.log` (derived
  view escaping controls and invalid UTF-8 with bin offsets). Output is capped at 2 MiB.
- U-Boot capture additionally supplies `rx-index.jsonl` with file-offset/Session-cursor segments
  and application observation times. There are at most 4096 segments. Snapshot export supplies
  cursor mapping in the manifest and does not invent reception times.

Buffers are sampled independently, not as one transaction. Gaps, byte/segment limits, unavailable
buffers, retained-start cursors and payload truncation are explicit. A snapshot is not a complete
recording. Operation capture survives a later unavailable Session buffer, preserving bytes already
observed. Raw archives are lossless within their declared retained ranges, not automatically
redacted shareable reports. No transport timing or hardware health is inferred from them.

## `terminal_read_diagnostic`

Required: `diagnostic_id`, `offset` (nonnegative ZIP byte offset). Optional `max_bytes`
(1..65536, default 65536). Returns `data` (Base64), `encoding: "base64"`, `bytes_read`, `next`,
`eof`, total `size`, and `sha256`. Unknown/expired/evicted IDs and invalid ranges fail. Offset
equal to archive size returns an empty EOF chunk. These offsets are not Session cursors.

Use `next` until EOF, then verify total size and SHA-256. A CLI on another machine can save the
same archive with `channelterm diagnostics --diagnostic-id ID --output capture.zip --endpoint URL`.

## `terminal_exec`

Purpose: execute one non-interactive command through an idle interactive Bash prompt without adding
the command or ChannelTerm's bootstrap to Bash history.

- Required input: `session_id`, `command`.
- Optional input: `timeout_ms` (default `30000`, maximum `86400000`).
- Result: canonical `session_id`, `command_id`, `exit_code`, `output_start`, and `output_end`.

```json
{"session_id":"SER-1","command":"sha256sum /tmp/app.bin","timeout_ms":30000}
```

```json
{"session_id":"8f6f3c76a3903ca393b6ace8812f91fe","command_id":"CMD-0123456789abcdef01234567","exit_code":0,"output_start":420,"output_end":512}
```

The command must be one non-empty UTF-8 line of at most 64 KiB and cannot contain control
characters. ChannelTerm acquires a `terminal` lease, checks retained write activity for unsubmitted
interactive input, hides the echoed internal bootstrap through cursor metadata, and runs the command
in a non-interactive child Bash without profile or rc files. The bundled attachment renders the
original command once in its local `AI` block; raw command output remains in the Session between
`output_start` and `output_end`. Internal wrapper writes use the `system` actor and do not create a
second Agent activity block.

The parent prompt must be interactive Bash so it can delete the current bootstrap history entry. The
child cannot persist `cd`, `export`, aliases, shell functions, or other state into the human shell
or a later `terminal_exec` call. Use one compound command when state must be shared within a call.
Interactive/full-screen programs, password prompts, arbitrary key input, multiline scripts, and
deliberately persistent background jobs must use an explicitly managed raw-terminal workflow
instead.

The lease blocks other ChannelTerm writers until completion and is renewed internally; callers do
not use the public lease tools for `terminal_exec`. If retained activity indicates a partial human
or Agent input line, the call fails before writing any bootstrap bytes. ChannelTerm cannot portably
prove that an arbitrary terminal is showing a shell prompt, so clients must call this tool only at a
known idle prompt. Cancellation or timeout sends Ctrl+C, waits up to five seconds for remote echo
restoration, then releases the lease. If restoration cannot be confirmed, the Host closes the
Session instead of returning it in an unknown TTY state.

Important errors: empty, multiline, control-character, invalid UTF-8, or oversized command;
unknown/non-open Session; active lease; pending interactive input; non-Bash parent; timeout or MCP
cancellation; lost output marker; transport failure; or failed recovery. Do not include passwords,
tokens, or other secrets in `command`: the original text is intentionally retained in structured
Session events for observable `AI` presentation.

## `terminal_write`

Purpose: write an explicitly encoded payload to an active Session without adding delimiters.

- Required input: `session_id`, `data`.
- Optional input: `encoding` (`utf8`, `hex`, or `base64`; default `utf8`) and `actor` (`user`,
  `agent`, or `system`; default `agent`).
- Result: `bytes_written`.

```json
{"session_id":"SER-1","data":"status\r","encoding":"utf8","actor":"agent"}
```

```json
{"bytes_written":7}
```

Hex input may contain whitespace. Base64 uses the standard encoding. The entire encoded payload is
validated before Session write, so malformed hex or Base64 writes nothing. A single call accepts at
most 1 MiB after decoding, regardless of the MCP transport or selected encoding. Input that cannot
fit is rejected before allocating its decoded copy. The actor is retained only in the activity
buffer and is never inserted into the device byte stream.

Important errors: payload larger than 1 MiB after decoding, missing/unknown Session, Session not
open, invalid encoding or encoded data, invalid actor, cancellation before or during application
retries, short write, and transport write failure. Safety: this tool controls the remote terminal.
Include `\r` or `\n` only when the target protocol requires it; ChannelTerm adds neither
automatically. A shell line sent with this raw-byte tool follows the remote shell's normal history
behavior. Agents should use `terminal_exec` for an idle Bash command that must stay out of history.

For files and debugging ELFs, run `channelterm file send` / `receive` from an environment that can
read the local file and reach the Session's HTTP Host. Select `--session` and the owning `--endpoint`,
then observe `terminal_wait_file_transfer` as described in the [transfer workflow](../getting-started/file-transfer.md).
Do not construct Base64/heredoc uploads with `terminal_write`: they bypass managed transfer progress
and payload suppression. If CLI or file access is unavailable, report that limitation. MCP provides
transfer control and observation, not a client-file upload tool; the CLI opens no additional serial port.

When another operation owns an exclusive lease, this unchanged tool returns a busy error such as
`Session SER-1 is locked by file-transfer`. It never waits for the lease or accepts an owner
capability.

## `terminal_acquire_lease`

Purpose: acquire one exclusive application-level writer lease for an active Session.

- Required input: `session_id`, caller-generated opaque `owner`, and `type` (`terminal`,
  `file-transfer`, or `debug`).
- Optional input: none.
- Result: canonical `session_id`, `type`, UTC `created_at`, UTC `expires_at`, and `state`
  (`active`). A `file-transfer` lease additionally returns its non-secret `transfer_id`. The owner
  capability is not echoed.

```json
{"session_id":"SER-1","owner":"file-transfer-opaque-capability","type":"file-transfer"}
```

Only one lease may be active for a Session. Readers and their cursors continue normally. Other
ordinary writers fail immediately; a separate Session is unaffected. A lease expires 30 seconds
after acquire or its latest successful renewal.

A `file-transfer` acquisition rejects unsubmitted interactive input, including a recalled shell
history line. The error instructs the user to clear the line with `Ctrl+C` at the normal prompt
before retrying. Rejection writes no terminal bytes and leaves no active lease.

Expiry releases writer ownership without the original owner capability. For file transfer, it also
publishes a failed terminal result with `reason: lease_expired` before
`LEASE_RELEASED(state=expired)`. File-transfer expiry blocks ordinary and stale leased writes and
new leases before closing/removing the original Session, including when no write is in flight.
This replaces the previous behavior that allowed another operation after idle file-transfer expiry.
A release received after that deadline cannot turn expiry into normal unlock. Debug expiry also
closes the Session; terminal expiry retains its existing in-flight-write policy. Closure is local
access revocation, not proof of remote program exit, TTY restoration, or safe reconnection. Final
events may be lost to observers when closure releases the retained buffers; `lease_released` does
not mean the Session is reusable.

Important errors: missing Session, invalid owner/type, or an already active lease. Safety: an owner
is a bearer capability and should be generated randomly, retained only for the operation, and never
logged.

## `terminal_renew_lease`

Purpose: extend an active exclusive lease before its Host-side TTL elapses.

- Required input: `session_id`, `owner`.
- Optional input: none.
- Result: the same non-secret lease state as acquisition, with an updated `expires_at`.

Only the current owner capability can renew. A missing, expired, or replacement lease returns an
ownership error. Long-running operations must renew more frequently than the 30-second TTL; the
bundled CLI renews every 10 seconds.

## File-transfer cancellation control tools

`terminal_begin_file_transfer_cancel` requires `session_id`. It returns `active: false` and
`state: "inactive"` when no `file-transfer` lease exists, preserving ordinary terminal Ctrl+C
behavior. For an active transfer it returns `active: true`, `state: "confirming"`, and an opaque
`request_id`; a second concurrent confirmation is rejected.

`terminal_resolve_file_transfer_cancel` requires `session_id`, that `request_id`, and boolean
`cancel`. `cancel: false` returns `state: "resumed"` immediately. `cancel: true` signals the lease
owner and normally returns `state: "cancelled"` with last confirmed `transferred`, `total`, and
`percent` after lease release. If the owner disappeared, TTL expiry returns `state: "expired"`
instead of waiting indefinitely. A stale request ID is rejected. Only a direct user `y`/`Y` answer
should set `cancel: true`.

`terminal_file_transfer_checkpoint` requires `session_id` and the active lease `owner`. The bundled
transfer process calls it only at safe protocol block boundaries. It returns `action: "continue"`,
blocks while confirmation is pending, or returns `action: "cancel"`; a non-owner is rejected. These
three tools carry control state only, never terminal bytes. The bundled attachment uses them for
Ctrl+C. AI clients should wait for the final result with `terminal_wait_file_transfer` rather than
initiating the user interaction themselves or waiting for raw terminal output.

## `terminal_write_leased`

Purpose: write through an active lease without changing the stable `terminal_write` schema.

- Required input: `session_id`, `owner`, and `data`.
- Optional input: `encoding` and `actor`, with the same semantics as `terminal_write`.
- Result: `bytes_written`.

The owner must exactly match the active lease for this Session. The same 1 MiB decoded-payload limit
and encoding validation as `terminal_write` apply. This tool exists for multi-step operations such
as the CLI file transfer; normal terminal clients should continue to use `terminal_write`.

## `terminal_release_lease`

Purpose: release an exclusive Session lease after its operation completes or fails.

- Required input: `session_id`, `owner`.
- Optional input: none.
- Result: `released: true`.

Release is idempotent when no lease remains, but a different owner receives an ownership error.
Closing a Session also discards its lease state.

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

Important errors: missing, unknown, or already removed Session; cancellation before invocation;
transport close failure. Safety: this is a lifecycle-destructive operation for every attached
client. Detach a single CLI by closing its MCP client instead when the shared Session must remain
open.

## Debug tools

`debug_start`, `debug_command`, `debug_events`, and `debug_stop` implement native Linux user-space
GDB/MI. The Host, GDB, gdbserver, ELF, and idle Bash Session must share one Linux environment.
Only one inferior, multiple threads, and all-stop execution are supported. See the
[workflow and examples](../getting-started/gdb-debugging.md) and
[Application lifecycle contract](../modules/application.md#user-space-debug-operations).

All four tools require a secret `owner` supplied at start (32..128 characters; generate it randomly).
Debug IDs alone do not authorize access. The internal lease owner is distinct and never returned.
Existing HTTP bearer authentication and loopback defaults remain unchanged. `debug_start`,
`debug_command`, and `debug_stop` are destructive, open-world, and idempotent-with-identical-request
operations. `debug_events` changes the client heartbeat and therefore is not annotated read-only.

| Tool | Required additional parameters | Optional parameters | Successful result |
| --- | --- | --- | --- |
| `debug_start` | `session_id`, `request_id`, `elf` | `args` string array, `pid`, absolute `gdb` / `gdbserver` paths | `status` |
| `debug_command` | `debug_id`, `request_id`, `action` | `argument` | parsed MI `record` |
| `debug_events` | `debug_id`, integer `cursor` | `timeout_ms` (default 10000, range 1..30000) | `status`, ordered `events`, `next` |
| `debug_stop` | `debug_id` | `terminate` (default false) | `status` |

`status` contains `debug_id`, canonical `session_id`, `state`, current event-tail `next`, client
heartbeat `expires_at`, and optional `error`. States are `starting`, `stopped`, `running`, `exited`,
`recovering`, `closed`, and `failed`. `closed` means shell restoration was verified. A failed call
uses the ordinary MCP `isError` result, not a successful status object; subsequent authenticated
event reads can still inspect the retained failure.

`elf` is an absolute native amd64/arm64 executable/PIE path. Start accepts at most 128 launch
arguments and 8192 combined path/argument bytes; NUL and newlines are rejected. The resulting
bootstrap must also fit Linux's canonical input line limit (4095 bytes); shorten paths/arguments
if it does not. `pid: 0` or omission launches. Positive PID attaches, disallows arguments, requires
a matching `/proc/PID/exe`, no controlling terminal, and no potentially shared character-device
descriptors. `/dev/null`, `/dev/zero`, `/dev/random`, and `/dev/urandom` are allowed. Unreadable process
state is refused. Pipes and sockets are rejected because their peers could forward Session I/O.
The target must not reopen terminal devices during debugging.

Start and command request IDs are 1..128 characters. Reusing the same start ID and exact arguments
returns the same operation; reusing a command ID returns its cached response/error. Changed
arguments are an error. No state-mutating command is automatically replayed after a timeout.
Start/MI commands have a 30-second maximum; uncertain execution fails closed. Stop uses an
independent 10-second recovery context even if its caller disconnects. A repeat successful stop
succeeds; a repeat failed stop remains an error. `terminate: true` is forbidden for attached PIDs.

Supported command actions:

| Actions | Argument |
| --- | --- |
| `break_insert` | One location, such as `add` or `/tmp/addition.c:2` |
| `break_delete` | Numeric breakpoint ID |
| `continue`, `next`, `step`, `finish`, `interrupt` | None; wait for subsequent stop/exit events |
| `stack`, `arguments`, `variables`, `threads`, `registers`, `register_names` | None |
| `thread`, `frame` | Numeric thread/frame ID |
| `evaluate` | One expression; function calls disabled, assignments may modify values |
| `renew` | None; refresh the 60-second client heartbeat |

Arguments are MI-quoted, limited to 4096 bytes, and cannot contain NUL/newlines. Raw MI/console,
non-stop mode, multi-inferior operations, and kernel debugging are not exposed. Each MI `record`
contains `kind`, `class`, `token`, `results`, and `text`. Nested tuples/lists retain their MI shape;
a string with non-UTF-8 bytes becomes `{"encoding":"base64","data":"..."}` rather than replacement
characters. Events contain `sequence` and `record`; lifecycle events use `kind: "lifecycle"`.
The record schema allows these nested MI values, whose fields depend on the selected GDB action.

Debug event cursors begin at zero and are separate from terminal cursors. Reads return at most 128
records. A stale/future cursor is an explicit error; no silent skip occurs. Timeout is an error and
does not end the operation. Event reads and new commands renew the 60-second client heartbeat;
use a new request ID for each explicit `renew`. Idle expiry attempts detach/restoration. Internal
lease expiry, transport loss, overwritten RSP bytes, or failed restoration closes the Session.
Application retains at most 128 operations, 512 events / 4 MiB per operation, and 4096 cached commands
/ approximately 16 MiB per operation. Exhausted retention requires ending debugging; Host restart
clears retained operation IDs. No persistent debug log or credentials are created.


## External RSP bridge tools

These tools manage an independent byte bridge over an already-open serial Session on any supported
Host OS. They never launch/control GDB or gdbserver. The existing native Linux `debug_start`,
`debug_command`, `debug_events`, and `debug_stop` contracts remain separate.
The [target workflow](../getting-started/gdb-debugging.md#external-gdb-over-a-prepared-serial-session)
supports Linux boards with a compatible gdbserver over serial; it has no board-model or target-CPU
restriction. Bare-metal debugging is outside scope.

`debug_bridge_list` accepts an empty argument object and returns `bridges`, an array of all
unfinished external bridges in this Host, ordered by numeric port, address, then bridge ID.
Each entry contains `bridge_id`, canonical `session_id`, `session_ref`, serial `target`, actual
bound `address`, integer `port`, `state`, `expires_at`, and `auto_keepalive`. Session metadata can
be empty during concurrent removal. Empty inventory is `[]`, never `null`. Listed states are
`listening`, `connected`, `disconnected`, and `recovering`; terminal `closed`, `failed`, and
`restored` records are excluded. A concurrent transition can immediately supersede the snapshot.
It does not scan OS ports or include managed MI operations, other Hosts, or standalone gdbservers.

The list is read-only: it sends no target bytes and renews no heartbeat or lease. Cancellation
affects only the query. Security impact: clients authorized to access the Host can observe bridge
IDs, addresses, and the listed Session metadata without individual bridge owners. The result omits
all capabilities, connection descriptors, fingerprints, error messages, and terminal/debugger output. Existing HTTP
authentication and bridge mutation authorization remain unchanged. The CLI exposes this observation
as [`gdb-list`](cli.md#gdb-list).

The management, single-status, and reporting calls below require the secret management `owner`
(32..128 characters on start). Management ownership is distinct from the generated connection
token and internal debug lease capability.

| Tool | Required arguments besides `owner` | Optional arguments | Result |
| --- | --- | --- | --- |
| `debug_bridge_start` | `session_id`, `request_id`, `acknowledge_unverified_target: true` | `listen_address`, `allowed_peer`, `strict_port_pool` | `status`, private `connection` |
| `debug_bridge_status` | `bridge_id` | none | `status`; does not renew |
| `debug_bridge_renew` | `bridge_id` | none | `status` with extended `expires_at` |
| `debug_bridge_close` | `bridge_id` | none | `status`; closes bridge **and Session** |
| `debug_bridge_restore` | `bridge_id`, `confirm_shell_ready: true`, `tty_state` | none | `status`; preserves Session only after verification |
| `debug_bridge_report` | `bridge_id`, `sequence`, `kind`, `text` | none | `bridge_id`, `sequence`; acknowledges reporting only |

`status` contains `bridge_id`, canonical `session_id`, `state`, bound `address`, `expires_at`, `max_expires_at`,
`auto_keepalive`, `target_state`, and optional `warning`/`error`. States are `listening`, `connected`, `disconnected`,
`recovering`, `restored`, `closed`, and `failed`. Target state is `unverified` until successful shell
restoration changes it to `shell_verified`; neither connection nor EOF proves target readiness.
`DEBUG_STATE` Session events contain `bridge_id` and `state`, without secrets. The secret connection
object appears only in successful start responses and contains `address`, SHA-256 certificate
`fingerprint` (hex), and random `token` (64 hex characters).

With `listen_address` omitted, each Host rotates through `127.0.0.1:37100` through `37999`.
A synchronized cursor starts at 37100, advances across requests, and wraps after 37999; closing a
bridge does not reset it. Active logical reservations are skipped without a bind attempt. Each
remaining candidate is allocated by an actual TCP bind; unavailable ports advance the cursor,
with at most one pass per request. A pool fully reserved by this Host needs no scan. Exhaustion
falls back to an OS dynamic port (`:0`), or returns an error with `strict_port_pool: true`.
Explicit `listen_address` overrides this pool policy: a fixed port either binds or returns an error;
explicit port zero requests an OS dynamic port. `status.address` and `connection.address` always
contain the actual bound port. The 900 preferred ports are an allocation policy, not a device limit
or evidence of 900 concurrent debugging Sessions. Existing operation retention limits still apply.
MCP HTTP retains its separate default port **37099**.

Only a specific numeric IP with port 0..65535 is accepted;
no wildcard bind, hostname, or implicit all-interface fallback. A non-loopback bind requires one
numeric `allowed_peer` IP. Without an explicit peer, only loopback clients are eligible. All clients
must authenticate using TLS 1.3, certificate pinning, and the connection token. Exactly one
successful data client is admitted; no reconnect. After authentication the listener stays bound.
A current helper may authenticate one additional control socket on the same port; further
data/control sockets are rejected. The Host separately reserves the logical IP/port until final close,
failure, or verified restoration, including while disconnected. Closing a listener alone must not
be treated as OS reservation of that port. Conversely, releasing the logical reservation does not
guarantee the OS will immediately allow rebinding; allocation always checks actual bind results.
Authentication attempts have a three-second deadline
and are processed sequentially. Use the `rsp-client` CLI helper, not an unauthenticated raw TCP GDB
connection. The MCP listener's authentication/configuration is unchanged.

Start `request_id` is 1..128 characters, scoped to the supplied owner. Exact retries return the
retained operation/descriptor, including its current terminal state. Reusing that request ID with
different arguments is an error. At most 128 bridges are retained per Host lifetime. Requests are
limited to 32 KiB and reject unknown fields. Start refuses non-serial/closed Sessions, conflicting
leases, pending interactive input, and incomplete input history when no persistent summary exists.
Target preparation is external; passive checks cannot prove RSP readiness, so missing explicit
acknowledgement returns an actionable risk error. Bytes are read from a new Session cursor.

The internal debug lease has a 30-second TTL. The one-second monitor checks it and renews when
at most twenty seconds remain. Only after confirming that lease can the monitor extend the
60-second management TTL for a connected helper with fresh control challenge/response evidence.
`auto_keepalive` reports this automatic renewal; `expires_at` is the current management deadline.
A paused target, breakpoint wait, or lack of RSP traffic does not prevent renewal.

Current `rsp-client` uses a second TLS connection on the **same listener and port** for control,
with the same pinned certificate, bridge token, and peer restriction. Only one data client and its
one control connection are admitted. The Host issues a fresh random challenge every ten seconds;
a missing control connection or response has a thirty-second deadline (loss is detected within
approximately forty seconds of the last successful response). TCP keepalive additionally probes
idle data sockets (10-second idle, 5-second interval, 3 probes, subject to OS support). Control
failure closes data; data EOF closes control. No heartbeat bytes enter GDB, Session, or standard
RSP, and no MCP Host token or management owner is exposed to the helper. The connection descriptor
format is unchanged. Use matching current Host/helper versions for automatic renewal. Legacy
data-only helpers remain accepted but require explicit management renewal.

Control liveness proves helper responsiveness, not target execution or acknowledgement of every RSP
write. A failure isolated to the data socket with unacknowledged traffic can still follow the OS's
TCP retransmission timeout; idle keepalive is not a per-write delivery deadline. The absolute
lifetime below remains a bound. Validate packet-loss behavior on the deployed OS/network separately.

Call `debug_bridge_renew` every thirty seconds **while listening, arranging recovery after a
disconnect, or using a legacy helper**. It also renews the internal lease and fails closed if that
lease has expired. Status reads, reports, and RSP traffic alone do not renew management time.
Every operation has an absolute 24-hour maximum (`max_expires_at`), including manually renewed ones;
finish and restore/close before that deadline. Neither deadline can be resurrected. This caps a
forgotten but responsive helper; lost control cannot indefinitely reserve a serial Session.

Disconnect stops automatic renewal but retains the lease until recovery or the remaining management
grace expires. It sends no target bytes and does not prove detach or gdbserver exit. Data loss,
serial failure, either lease's expiry, and the absolute maximum close the Session. Ordinary
lease-release tools cannot bypass managed bridge recovery.

Restore requires a disconnected client, explicit confirmation that the target returned to its
shell, and the exact pre-debug `stty -g` snapshot (1..512 printable bytes). It sends a quoted shell
command to apply/read back that state and obtain a fresh split-token response. It does not synthesize
RSP detach/kill. The independent ten-second recovery deadline also closes a blocked serial write.
In interactive Bash, the probe removes only its own current history entry, if recorded; it does
not clear user history or change history settings. Other shells retain their normal history
behavior. This does not remove earlier raw setup commands sent through `terminal_write`.
Only verified restoration releases the lease while keeping the Session open. Close always ends an
unfinished bridge by closing its Session. Close and successful restoration are idempotent; terminal
states cannot be resurrected. A later close of an already restored bridge preserves that Session.

Start/close/restore have destructive and open-world annotations; start is deduplicated, while
close/restore are idempotent. Status is read-only; renew changes expiry. See the
[external GDB workflow](../getting-started/gdb-debugging.md#external-gdb-over-a-prepared-serial-session)
for target preparation, Windows/WSL networking, secret handling, and verification boundaries.

### External debugger reports

`debug_bridge_report` publishes selected, actual external GDB commands, their results, or observed
states into the existing Session event stream. `kind` is `command`, `result`, or `state`; `text` is
nonempty UTF-8, at most 4096 bytes. It is never executed, sent to the serial port, or interpreted as
RSP. Report after executing/observing the action externally. A `state` report cannot change the
Host's bridge/target state or prove recovery. No GDB log file is watched.

`sequence` is an increasing integer from 1 through 9007199254740991, scoped to one bridge.
Gaps are allowed. An exact retry of the latest accepted sequence returns the same receipt without
another event, including after the bridge ends. Older sequences and conflicting retries fail.
New reports require a nonexpired bridge in an open Session, before recovery/termination. Reporting
does not renew the heartbeat. Invalid input, wrong owner, cancellation, and reporting failures never
close the bridge or Session. External drivers must continue GDB independently; keepalive and any needed manual renewal remain separate;
do not gate debugging on delivery or re-execute a GDB command to retry its report.

Each `DEBUG_CLIENT_REPORT` has actor `agent` and metadata `bridge_id`, `sequence`, `kind`, `text`,
`reported_by: "codex"`, and `complete_audit: false`. This is caller-supplied reporting, not attested
execution or a complete audit of manual/automatic GDB actions. Read it through
`terminal_session_events` or `channelterm events`; each observer keeps an independent cursor.
Retention shares the Session's 1024-event ring; overwritten entries report `dropped: true` and
history is unavailable after Session removal or Host exit. Attach shows bounded live previews and
recent retained reports; see [CLI](cli.md#attach).

Reports are visible to every authorized Session observer. The caller must omit raw RSP, connection
descriptors, credentials, and sensitive program values. Known management/lease/TCP capabilities are
rejected if included in text, but this is not a general secret detector. The relay never generates
reports from protocol bytes. CLI previews escape control characters. The tool is non-destructive,
closed-world, and idempotent with respect to sequence retries; it is not read-only because it
publishes events. MCP transport authentication and the bridge owner capability are required.
