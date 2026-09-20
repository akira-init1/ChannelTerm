# Serial Terminal

## Find a port

List currently detected serial targets without querying MCP:

```powershell
channelterm list --kind device --transport serial --no-mcp
```

A Windows port such as `COM8` is shown with the local target reference `SER-COM8`. Unix endpoints retain the operating-system path; for example `/dev/ttyUSB0` is shown as `SER-/DEV/TTYUSB0`.

## Open a direct terminal

The `serial` command opens a private connection owned by the current process:

```powershell
channelterm serial --port COM8 --baud 115200
```

The conventional defaults are 115200 baud, 8 data bits, no parity, one stop bit, and no flow control. Confirm all settings against the actual device. A normal connection sends no bytes during startup.

The `connect` command resolves a target printed by `list` and then opens the same kind of private connection:

```powershell
channelterm connect SER-COM8 --baud 115200
```

`TARGET_REF` must be the first argument to `connect`, and `--port` cannot be combined with it.

## Interactive input

The CLI puts an interactive terminal into raw mode and restores it when the command exits.

On Windows Console hosts, navigation keys are sent as standard VT sequences: for example, Up is `ESC [ A`, Down is `ESC [ B`, Right is `ESC [ C`, Left is `ESC [ D`, Home is `ESC [ H`, and End is `ESC [ F`.

- `Ctrl+C` is sent to the remote terminal as byte `0x03`; it does not stop ChannelTerm.
- Press `Ctrl+]` to enter local escape mode; ChannelTerm displays the available escape commands locally.
- `Ctrl+] q` exits the local terminal client.
- `Ctrl+] ?` displays local escape help.
- `Ctrl+] ]` sends a literal `Ctrl+]` byte (`0x1d`) to the remote terminal.
- `Ctrl+] t` toggles local-only `[HH:MM:SS]` timestamps before recognized shell prompts. It is off by default and does not timestamp ordinary output or change Session data.
- `Ctrl+] f` opens the local file-transfer menu. Send defaults to `/tmp/cterm/user-files/<local-basename>` on the board and creates that hierarchy when needed; receive defaults to `./<remote-basename>` on the PC. See [file transfer](file-transfer.md) for overwrite and cancellation behavior.
- `Ctrl+] Esc` cancels local escape mode without sending a byte to the remote terminal, reports `[ChannelTerm] Escape cancelled` locally, and returns subsequent input to normal remote forwarding.

Use `--wake` only when a known, already-running shell is idle without a prompt. It sends exactly one carriage return after a new connection succeeds. It is not enabled by default.

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

`data-bits` accepts 5, 6, 7, or 8; parity accepts `none`, `odd`, `even`, `mark`, or `space`; stop bits accept `1`, `1.5`, or `2`. The schema also accepts flow control values `none`, `software`, and `hardware`, but the current cross-platform backend only implements `none`; the other values fail before opening the port.

Physical serial connections do not support terminal resize operations. See the [CLI reference](../reference/cli.md) and [Transport module](../modules/transport.md) for details.
