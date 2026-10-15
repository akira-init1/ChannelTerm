---
name: cterm-serial-debug
description: Operate ChannelTerm (cterm) shared serial Sessions for MCU and embedded-Linux diagnosis. Use when a user wants to inspect boot logs, interact with a UART or serial console, run a command at a known idle Bash prompt, transfer files or directories through an embedded-Linux serial shell, or let a human and AI share one device Session. Do not use for JTAG, SWD, GDB, firmware flashing, or an unrelated local shell.
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
- Observe the response before choosing the next action. Do not invent device-specific commands.
- Obtain explicit approval before an operation that may reset a device, enter a bootloader, erase or flash storage, change persistent configuration, or otherwise interrupt the user's hardware workflow.
- Use leases and `terminal_write_leased` only for a deliberately managed multi-step operation. Renew and release an acquired lease; do not add lease machinery to ordinary reads or writes.

## Preserve Shared Ownership

- Assume other clients may be attached to the same Session. A complete write payload is serialized, but ordinary writers do not receive semantic shell ownership.
- Prefer leaving the Session open after diagnosis. `terminal_session_detach` only records a client departure on the Session event stream. The client must stop its own attachment I/O through its exit or detach action; calling the tool does not disconnect it. `terminal_close` closes the shared Session for every client and requires clear user intent.
- Before advising a CLI attachment to exit, check whether it created a temporary Host. Exiting that creating attachment also stops the Host and closes all of its Sessions, even if other clients remain attached. Keep it open while those Sessions are needed. Use a separately started persistent HTTP Host from the outset when Sessions must outlive CLI attachments.
- Do not stop a persistent HTTP Host unless the user asks to end it. The normal foreground-process exit is `Ctrl+C`; stopping it closes the Sessions owned by that Host.

## Transfer Files and Directories

Use the managed `channelterm file` workflow for uploads and downloads through an existing shared serial Session. Copying a firmware image is not flashing it; execution, installation, and flashing are separate operations requiring the user's intent.

### Check the execution environment

- The target must be at an idle POSIX-style shell with `mkdir`, `stty`, `dd` supporting `iflag=fullblock`, `wc`, `sha256sum`, and `sleep`. Directories also require `tar` and temporary archive/staging space. The CLI performs the transfer preflight; do not probe an unknown MCU, bootloader, or foreground program with shell commands.
- MCP tools do not accept client-side file payloads. To initiate a transfer, the agent needs permission and a tool to execute the local CLI, access to the local source or destination, and access to the intended HTTP Host. A path on the user's PC is not automatically available in the agent's execution environment, and `localhost` refers to that environment.
- If those capabilities are unavailable, give the user the exact CLI command to run where the file and Host are accessible, or direct them to `Ctrl+] f` in their attachment. Continue observing through MCP when available. Do not invent an upload tool or replace the managed transfer with shell-encoded payloads through `terminal_write` or `terminal_exec`.

### Start and observe the transfer

1. Resolve the intended Session and Host, direction, source, and destination from the request. Use an explicit `--session` to avoid selecting another device; add `--endpoint` for a non-default Host. The file command neither opens a serial port nor starts a Host. Keep the attachment that owns a temporary Host running.
2. When observing through MCP, capture the `next` event cursor from `terminal_session_events` before starting. Use this event cursor, not an output or activity cursor, to observe the transfer.
3. Run the appropriate CLI command in the environment that owns the local path. These examples use placeholder paths and Session references; substitute the actual values and quote paths for the local shell:

   ```sh
   channelterm file send ./app.bin /tmp/app.bin --session SER-1
   channelterm file receive /tmp/log.txt ./log.txt --session SER-1
   channelterm file send ./build/release /tmp/release --session SER-1
   ```

   The same send/receive commands recognize directories. An omitted send destination defaults to `/tmp/cterm/mcp-files/<local-basename>`; the human attachment menu defaults to `/tmp/cterm/user-files/<local-basename>`. Local paths belong to the CLI environment and board paths use POSIX syntax. The CLI manages the transfer lease and cleanup; do not acquire a competing lease manually.
4. For MCP observation, obtain the matching `transfer_id` from `FILE_TRANSFER_STARTED` or the active file-transfer lease. Call `terminal_wait_file_transfer` with that ID, the Session, and the captured event cursor. Match the intended transfer rather than accepting any transfer's completion. A wait timeout alone does not prove transfer failure; inspect the same transfer before waiting again or considering a retry.
5. Report `completed`, `cancelled`, or `failed` and confirm lease release before subsequent terminal writes. Raw-output `terminal_wait`, silence, or a reappearing prompt is not a completion signal. For success, use `resolved_path` for later operations: a name collision selects a free suffixed name rather than silently overwriting. Report the regular file's SHA-256 verification result; do not describe a directory archive digest as a stable directory-content hash. If only CLI evidence is available, use its exit status, verification output, and saved path, and identify that evidence explicitly.

### Cancellation and recovery

During an active attachment transfer, `Ctrl+C` opens a local cancellation confirmation; only `y` or `Y` confirms. Leave that interaction to the user and observe its result through `terminal_wait_file_transfer`. Do not inject a raw Ctrl+C into the payload stream or force-release the lease to simulate cancellation. Wait for the matching terminal result and cleanup before another operation.

After failure, report the reason and last confirmed byte count. Transfers do not resume automatically; resolve the cause before restarting. If the Session closed, re-establish the intended connection before retrying. If terminal echo or line handling remains abnormal after the transfer has ended, inspect `stty -a` at a confirmed idle board shell; restore `echo` for disabled echo, or use the documented `stty sane` recovery when broader TTY state was left unrestored. Do not send recovery commands while a transfer or another foreground program still owns the terminal.

For detailed behavior in a repository checkout, consult the [file-transfer guide](../../docs/getting-started/file-transfer.md), [CLI reference](../../docs/reference/cli.md#file), and [MCP transfer result contract](../../docs/reference/mcp-tools.md#terminal_wait_file_transfer). Installed skill bundles may omit repository docs; use the installed CLI's `file send --help` / `file receive --help` and available MCP schemas to check version-specific options and fields.

## Current Capability Boundary

ChannelTerm currently provides serial terminal I/O, retained output, shared Sessions, activity, discovery, command execution at Bash, and managed file transfer for suitable embedded Linux targets. It does not provide JTAG/SWD debugging, GDB breakpoints or stepping, register or memory inspection, manual RTS/DTR control, JSON serial macros, or general firmware flashing. The portable serial backend currently supports only `flow_control: none`.

## Report Evidence

Report the selected endpoint and known serial settings, the Session reference, actions taken, and the relevant device output. Distinguish automated or local-command evidence from real-hardware confirmation. If no physical device was exercised, say so explicitly.
