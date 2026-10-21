# Serial Terminal

## Enter U-Boot and export evidence

Use an existing shared serial Session and its owning MCP Host. Record the board's serial settings
and verify its actual countdown text, prompt (including spaces), interrupt key, and boot-start
markers. `terminal_enter_uboot` watches raw bytes inside the Host, avoiding model round trips in a
short countdown window. Its [contract](../reference/mcp-tools.md#terminal_enter_uboot) includes
board-dependent arguments and limits.

First read `terminal_session_events` and retain its `next` cursor to exclude old readiness events.
For manual reset, call with `mode: "wait_reset"`, observe a new `OPERATION_STATE` ready
event in another client/request, then reset the board. For an authorized software reboot from a
known idle Bash prompt, use `mode: "reboot"` with an explicit command such as `sync && reboot`.
Do not use reboot mode in U-Boot, a foreground program, or with an unsubmitted human command.

Only `status: "entered"` confirms the fresh prompt. `boot_started` means the observed window was
missed; timeout, dropped output or write failure is not success. Do not automatically reboot again.
Cancellation stops this operation without closing the shared Session; an already sent reboot can
still proceed. Normal U-Boot commands use raw terminal writes, not Bash-only `terminal_exec`.

Set `diagnostics: "capture"` only when retaining raw terminal contents is intended. Completion
events also contain the diagnostic ID, so another observer can recover it after a cancelled MCP
request whose response is no longer delivered. A returned
diagnostic ID can be downloaded on the caller's machine, including WSL:

```powershell
channelterm diagnostics --diagnostic-id DIAG-EXAMPLE --output boot-evidence.zip --endpoint http://127.0.0.1:37099/mcp
```

Replace the example ID with the actual returned value. To create a fresh retained-buffer snapshot:

```powershell
channelterm diagnostics --session SER-1 --output session-evidence.zip --include-raw --endpoint http://127.0.0.1:37099/mcp
```

Omit `--include-raw` for metadata only. Files are never overwritten, and the CLI verifies download
size and SHA-256. Download before the 15-minute expiry, earlier cache eviction, or Host exit.
The manifest describes truncation and missing evidence. Snapshot export has no receive timestamps;
operation capture times describe Host observation, not exact UART arrival. Review sensitive raw
contents before sharing. An error response must be checked through `isError` before parsing text;
use hex/base64 for ordinary reads during boot and preserve cursors across timeouts.

## Find a port

List currently detected serial targets without querying MCP:

```powershell
channelterm list --kind device --transport serial --no-mcp
```

Targets use the operating-system name shown by discovery: for example `COM8` on Windows,
`/dev/ttyUSB0` on Linux, or `/dev/cu.usbserial-110` on macOS.

## Open a direct terminal

The `serial` command opens a private connection owned by the current process:

```powershell
channelterm serial --port COM8 --baud 115200
```

The conventional defaults are 115200 baud, 8 data bits, no parity, one stop bit, and no flow
control. Confirm all settings against the actual device. A normal connection sends no bytes during
startup.

The `connect` command resolves a target printed by `list` and then opens the same kind of private
connection:

```powershell
channelterm connect COM8 --baud 115200
```

`TARGET` must be the first argument to `connect`, and `--port` cannot be combined with it.

## Interactive input

The CLI puts an interactive terminal into raw mode and restores it when the command exits.

On Windows Console hosts, navigation keys are sent as standard VT sequences: for example, Up is
`ESC [ A`, Down is `ESC [ B`, Right is `ESC [ C`, Left is `ESC [ D`, Home is `ESC [ H`, and End is
`ESC [ F`.

- `Ctrl+C` is sent to the remote terminal as byte `0x03`; it does not stop ChannelTerm.
- Press `Ctrl+]` to enter local escape mode; ChannelTerm displays the available escape commands
  locally.
- `Ctrl+] q` exits the local terminal client.
- `Ctrl+] ?` displays local escape help.
- `Ctrl+] ]` sends a literal `Ctrl+]` byte (`0x1d`) to the remote terminal.
- `Ctrl+] t` toggles local-only `[HH:MM:SS]` timestamps before recognized shell prompts. It is off
  by default and does not timestamp ordinary output or change Session data.
- `Ctrl+] f` is available only in a shared `attach`. Private `serial`, `connect`, and
  `attach --private` connections report that file transfer requires shared attach. See
  [file transfer](file-transfer.md) for the shared workflow.
- `Ctrl+] Esc` cancels local escape mode without sending a byte to the remote terminal, reports
  `[ChannelTerm] Escape cancelled` locally, and returns subsequent input to normal remote
  forwarding.

Use `--wake` only when a known, already-running shell is idle without a prompt. It sends exactly one
carriage return after a new connection succeeds. It is not enabled by default.

## Local highlighting

`serial`, `connect`, and `attach` enable semantic highlighting by default when stdout is a
color-capable terminal. Use `--highlight=false` when ChannelTerm must not add presentation colors.

- Plain terminal text receives non-bold semantic colors for errors, warnings, successful states,
  boot headings, field names, embedded interface/protocol identifiers, addresses, versions,
  timestamps, numbers, units, quoted values, structural delimiters, and recognized shell prompts.
- Local output automatically degrades from TrueColor to 256 colors, 16 colors, or no generated
  color. `NO_COLOR` disables generated colors and `CLICOLOR_FORCE=1` forces color when stdout is not
  detected as a terminal.
- Remote ANSI/VT always wins. Escape-containing output is forwarded raw, including sequences split
  across reads. Persistent SGR attributes and the alternate screen keep raw presentation active;
  semantic highlighting resumes only after the remote state is reset at a safe line boundary. This
  preserves full-screen programs such as `vim`, `htop`, and `menuconfig` without requiring reattach.
- Invalid UTF-8, standalone carriage-return updates, and other unsafe line controls pass through
  unchanged.

Highlighting is presentation-only. Session buffers and MCP reads keep the original bytes.

## Current serial constraints

`data-bits` accepts 5, 6, 7, or 8; parity accepts `none`, `odd`, `even`, `mark`, or `space`; stop
bits accept `1`, `1.5`, or `2`. The schema also accepts flow control values `none`, `software`, and
`hardware`, but the current cross-platform backend only implements `none`; the other values fail
before opening the port.

Physical serial connections do not support terminal resize operations. See the
[CLI reference](../reference/cli.md) and [Transport module](../modules/transport.md) for details.
