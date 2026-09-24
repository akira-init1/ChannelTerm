---
name: channelterm-debug
description: Operate ChannelTerm (cterm) shared serial Sessions for MCU and embedded-Linux diagnosis. Use when a user wants to inspect boot logs, interact with a UART or serial console, run a command at a known idle Bash prompt, or let a human and AI share one device Session. Do not use for JTAG, SWD, GDB, or an unrelated local shell.
---

# ChannelTerm Debug

Treat `cterm` as an alias for ChannelTerm. Use ChannelTerm's MCP tools to work through an existing shared Session whenever possible so a human attachment can observe and intervene.

## Establish the Session

1. Call `terminal_list_sessions` when no active Session is known. Reuse the user's intended Session instead of opening the same endpoint independently.
2. If an explicit native serial target must be opened, obtain the actual port and communication settings. Never guess the baud rate, data bits, parity, stop bits, flow control, wiring, or voltage level.
3. For an endpoint learned from a device-appearance event, call `terminal_get_connection_decision` before `terminal_open_serial`. An `ask` action requires explicit user approval; respect `deny` for discovery-driven opens.
4. Leave `wake` disabled unless the target is known to be at an idle interactive prompt and sending one carriage return is intended. Opening an unknown bootloader or MCU must not send probe input automatically.
5. Record the returned opaque `session_id` and short reference such as `SER-1`. A label, `device_id`, or native port is not a Session identifier.

## Observe Before Writing

- Use `terminal_read` without a cursor for an immediate recent snapshot. Save its `next` cursor, then use `terminal_wait` with that cursor for new bytes.
- Treat `dropped: true` as lost earlier history; continue from `next` without reconstructing or inventing the missing output.
- Use `terminal_read_activity` or `terminal_wait_activity` when attribution matters. Activity and raw-output cursors are independent.
- If UTF-8 decoding fails or the protocol is binary, read and write with `hex` or `base64` rather than replacing bytes.

## Choose the Interaction Path

- Use `terminal_exec` only for one non-interactive command at a known idle Bash prompt. Each call uses a fresh child, so combine dependent shell steps in one command. Do not use it while a foreground or interactive program owns the terminal.
- Use `terminal_write` for a bare MCU, RTOS console, bootloader, REPL, password prompt, interactive program, raw keys, control characters, or binary protocol.
- Observe the response before choosing the next action. Do not invent device-specific commands.
- Obtain explicit approval before an operation that may reset a device, enter a bootloader, erase or flash storage, change persistent configuration, or otherwise interrupt the user's hardware workflow.
- Use leases and `terminal_write_leased` only for a deliberately managed multi-step operation. Renew and release an acquired lease; do not add lease machinery to ordinary reads or writes.

## Preserve Shared Ownership

- Assume other clients may be attached to the same Session. A complete write payload is serialized, but ordinary writers do not receive semantic shell ownership.
- Prefer leaving the Session open after diagnosis. `terminal_session_detach` removes one client attachment; `terminal_close` closes the shared Session for every client and requires clear user intent.
- Use `terminal_wait_file_transfer` only for a transfer identified by `transfer_id`; ordinary terminal output waits do not report transfer completion.
- File and directory transfer requires an idle POSIX-style shell and target-side utilities. It is not a generic bare-MCU firmware flashing mechanism.

## Current Capability Boundary

ChannelTerm currently provides serial terminal I/O, retained output, shared Sessions, activity, discovery, command execution at Bash, and managed file transfer for suitable embedded Linux targets. It does not provide JTAG/SWD debugging, GDB breakpoints or stepping, register or memory inspection, manual RTS/DTR control, JSON serial macros, or general firmware flashing. The portable serial backend currently supports only `flow_control: none`.

## Report Evidence

Report the selected endpoint and known serial settings, the Session reference, actions taken, and the relevant device output. Distinguish automated or local-command evidence from real-hardware confirmation. If no physical device was exercised, say so explicitly.
