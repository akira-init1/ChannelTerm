---
name: cterm-serial-debug
description: Operate ChannelTerm (cterm) shared serial Sessions for MCU and embedded-Linux diagnosis. Use when a user wants to inspect boot logs, interact with a UART or serial console, run a command at a known idle Bash prompt, or let a human and AI share one device Session. Do not use for JTAG, SWD, GDB, firmware flashing, or an unrelated local shell.
---

# ChannelTerm Serial Debug

Treat `cterm` as an alias for ChannelTerm. Use ChannelTerm's MCP tools to work through an existing shared Session whenever possible so a human attachment can observe and intervene.

## Establish the Session

1. Call `terminal_list_sessions` when no active Session is known. Reuse the user's intended Session instead of opening the same endpoint independently.
2. Distinguish the two ownership paths. When a human has already opened a shared attachment, reuse that Session. When the AI is asked to create the Session through HTTP, the user first configures the client with `channelterm init --mcp` and then starts the persistent Host with `channelterm mcp --transport http`; after the Host is available, call `terminal_list_serial_ports` and open the explicitly selected endpoint. Starting an MCP Host alone does not open a serial port.
3. If an explicit native serial target must be opened, obtain the actual port and communication settings. Never guess the baud rate, data bits, parity, stop bits, flow control, wiring, or voltage level.
4. For an endpoint learned from a device-appearance event, call `terminal_get_connection_decision` before `terminal_open_serial`. An `ask` action requires explicit user approval; respect `deny` for discovery-driven opens. A `connect` action permits the client-controlled open but does not open the endpoint by itself.
5. Leave `wake` disabled unless the target is known to be at an idle interactive prompt and sending one carriage return is intended. Opening an unknown bootloader or MCU must not send probe input automatically.
6. Record the returned opaque `session_id` and short reference such as `SER-1` together with the owning MCP connection. Session references are local to that Host. A label, `device_id`, or native port is not a Session identifier.
7. For an HTTP Session, confirm its Host URL before giving a human a join command. Use `channelterm attach SER-1 --endpoint <HOST_URL>`, substituting the actual reference and reachable URL. Omit `--endpoint` only for the default HTTP Host. A stdio Session belongs to its own server process and cannot be joined with `channelterm attach`; use a shared HTTP Host when human CLI access is required.

## Observe Before Writing

- Use `terminal_read` without a cursor for an immediate recent snapshot. Save its `next` cursor, then use `terminal_wait` with that cursor for new bytes.
- Treat `dropped: true` as lost earlier history; continue from `next` without reconstructing or inventing the missing output.
- Use `terminal_read_activity` or `terminal_wait_activity` when attribution matters. Activity and raw-output cursors are independent.
- If UTF-8 decoding fails or the protocol is binary, read and write with `hex` or `base64` rather than replacing bytes.

## Choose the Interaction Path

- Use `terminal_exec` only for one non-interactive command at a known idle Bash prompt. Each call uses a fresh child, so combine dependent shell steps in one command. Do not use it while a foreground or interactive program owns the terminal.
- Use `terminal_write` for a bare MCU, RTOS console, bootloader, REPL, password prompt, interactive program, raw keys, control characters, or binary protocol.
- For files or debugging ELFs, follow `cterm-file-transfer` and run `channelterm file send/receive` against the existing Session. Do not implement Base64/heredoc uploads with raw writes. If execution, local-file, or Host access is unavailable, report the limitation.
- For Linux source-level debugging, use `cterm-gdb-debug` when installed; it covers gdbserver checks, target preparation, bridge ownership, independent GDB control, reporting, and recovery. Preserve the intended Session and Host. Otherwise consult the [debugging workflow](../../docs/getting-started/gdb-debugging.md#external-gdb-over-a-prepared-serial-session) and installed MCP schemas before taking over the serial stream.
- Observe the response before choosing the next action. Do not invent device-specific commands.
- Obtain explicit approval before an operation that may reset a device, enter a bootloader, erase or flash storage, change persistent configuration, or otherwise interrupt the user's hardware workflow.
- Use leases and `terminal_write_leased` only for a deliberately managed multi-step operation. Renew and release an acquired lease; do not add lease machinery to ordinary reads or writes.

## Preserve Shared Ownership

- Assume other clients may be attached to the same Session. A complete write payload is serialized, but ordinary writers do not receive semantic shell ownership.
- Prefer leaving the Session open after diagnosis. `terminal_session_detach` only records a client departure on the Session event stream. The client must stop its own attachment I/O through its exit or detach action; calling the tool does not disconnect it. `terminal_close` closes the shared Session for every client and requires clear user intent.
- Before advising a CLI attachment to exit, check whether it created a temporary Host. Exiting that creating attachment also stops the Host and closes all of its Sessions, even if other clients remain attached. Keep it open while those Sessions are needed. Use a separately started persistent HTTP Host from the outset when Sessions must outlive CLI attachments.
- Do not stop a persistent HTTP Host unless the user asks to end it. The normal foreground-process exit is `Ctrl+C`; stopping it closes the Sessions owned by that Host.

## File Transfer Handoff

For uploads/downloads, use `cterm-file-transfer` when installed. It owns protocol selection, paths, verification, and cancellation. Preserve the selected Session and Host when handing off. If that skill is unavailable, consult `channelterm file send --help` / `file receive --help` and the [file-transfer guide](../../docs/getting-started/file-transfer.md) when working from a source checkout.

Managed serial transfer requires an idle Linux shell and the target-side utilities; it is not firmware flashing. The CLI initiates transfers and manages their leases. MCP can observe a matching `transfer_id` through `terminal_wait_file_transfer`, but cannot accept client-side files. Keep the Session owner alive and wait for transfer cleanup before resuming terminal writes.

## Current Capability Boundary

This skill covers serial terminal diagnosis and file-transfer handoff. ChannelTerm's separate GDB workflows support Linux user-space breakpoints, stepping, and inspection; use the GDB skill or guide above. JTAG/SWD, bare-metal GDB, manual RTS/DTR control, JSON serial macros, and general firmware flashing are outside this workflow. The portable serial backend currently supports only `flow_control: none`.

## Report Evidence

Report the selected endpoint and known serial settings, the Session reference, actions taken, and the relevant device output. Distinguish automated or local-command evidence from real-hardware confirmation. If no physical device was exercised, say so explicitly.
