# Serial Terminal

## Enter U-Boot and export evidence

Use an existing shared serial Session and its owning MCP Host. Record the board's serial settings
and verify its actual countdown text, prompt (including spaces), interrupt key, and boot-start
markers. `terminal_enter_uboot` watches raw bytes inside the Host, avoiding model round trips in a
short countdown window. Its [contract](../reference/mcp-tools.md#terminal_enter_uboot) includes
board-dependent arguments and limits.

Prepare these settings before resetting the board. Read retained output with `terminal_read`
(hex/base64 if it is not valid UTF-8), or reuse settings verified for the same board and firmware.
The caller or AI client selects the arguments; the tool does not discover them or pause to ask
questions during execution. When evidence is missing, ask for the specific missing setting.
The MCP tool descriptions carry this guidance even without an installed skill. Reuse settings and
answers already established, and do not ask again for an action the user has explicitly requested.
If current evidence establishes that U-Boot is already idle, use `terminal_write` for authorized
commands instead of starting an entry operation. Historical prompts alone do not establish that
state, and an empty Enter is not a neutral probe: it may repeat a previous command.

These examples illustrate parameter selection, not universal board defaults:

| Observed output or verified configuration | Argument choice |
| --- | --- |
| `Hit any key to stop autoboot: 3` | A fixed countdown marker can omit the number; the explicit any-key instruction permits `interrupt_data_hex: "20"` (space). |
| `Autoboot in 3 seconds` | `countdown_pattern: "Autoboot in "` matches the fixed portion. This message alone does not identify the interrupt input; ask or inspect firmware configuration. |
| Firmware explicitly requires the non-secret string `stop` | `interrupt_data_hex: "73746f70"` sends exactly that string, once, with no appended Enter. |
| Firmware explicitly requires Esc | `interrupt_data_hex: "1b"` sends that key, not a space. |

Patterns are literal, not regular expressions or wildcards. Choose distinctive markers and preserve
the full prompt's spaces. A generic U-Boot banner is not evidence that kernel boot has started.
Do not cycle through keys, infer them from a board model, or treat a timeout as proof of a wrong key.
Password handling has no dedicated credential-protection workflow; raw evidence and tool arguments
must not be treated as a secret store.

The entry flow requires a fresh countdown followed by a prompt. An already-idle U-Boot, a silent
countdown, and firmware that disables interruption are not covered by that flow. Learning settings
from a first boot may miss its window; prepare a later reset only within the user's authorized scope.

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

## Character encoding, description, and hexadecimal view

Serial bytes have no inherent character set. Select the encoding used by the device's shell:

```powershell
channelterm attach COM8 --baud 115200 --encoding gbk --description "Debug console"
channelterm attach SER-1 --encoding gb18030
channelterm attach SER-1 --view hex
```

`serial`, `connect`, and `attach` accept `--encoding utf-8|gbk|gb18030` (default `utf-8`),
`--view text|hex` (default `text`), and `--description`. These are local invocation settings,
not serial electrical parameters or shared Session metadata. `--save` does not persist them.
The description is a single line, up to 512 UTF-8 bytes, without control characters.

Text mode decodes received GBK/GB18030 bytes for the local UTF-8 console and encodes typed
UTF-8 input for the device. Split characters are buffered across reads. Unrepresentable or
invalid input is rejected without replacement or automatic replay. The default UTF-8 path
preserves the existing raw console behavior. Invalid device sequences in legacy encodings
display replacement characters; after an output gap, partial decoding state is discarded.

Hex mode is read-only and shows offsets, hexadecimal bytes, and printable ASCII; escape
sequences are shown as data. Offsets count bytes observed by this attachment and reset after
an output gap. Rows may end at read boundaries. Local escape commands, including detach, remain
available. Return to `--view text` to type into the terminal. File transfers, Host buffers,
MCP results, and other clients retain their original bytes regardless of these settings.

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

Unknown prompts and received key echo also appear without waiting for Enter. See the
[CLI reference](../reference/cli.md#serial) for recognized forms and the bounded buffering fallback.
Remote-controlled password suppression is preserved; ChannelTerm does not add local input echo.

## Current serial constraints

`data-bits` accepts 5, 6, 7, or 8; parity accepts `none`, `odd`, `even`, `mark`, or `space`; stop
bits accept `1`, `1.5`, or `2`. The schema also accepts flow control values `none`, `software`, and
`hardware`, but the current cross-platform backend only implements `none`; the other values fail
before opening the port.

Physical serial connections do not support terminal resize operations. See the
[CLI reference](../reference/cli.md) and [Transport module](../modules/transport.md) for details.
