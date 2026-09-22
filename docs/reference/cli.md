# CLI Reference

This reference explains behavior and constraints rather than duplicating the complete generated
help. Run the following against the binary you are using whenever exact flags or defaults matter:

```powershell
channelterm --help
channelterm attach --help
channelterm events --help
channelterm file --help
channelterm init --help
channelterm install --help
channelterm list --help
channelterm mcp --help
channelterm serial --help
channelterm uninstall --help
```

The current parser also implements `channelterm connect --help`, although `connect` is not listed in
the current top-level help text.

## Top-level command

Syntax:

```text
channelterm [options] [command]
```

With no arguments, ChannelTerm prints top-level usage and exits successfully. `help`, `--help`, and
`-h` do the same. `version`, `--version`, and `-v` print the same five-line build identity:

```text
channelterm VERSION
commit:   COMMIT
built:    TIMESTAMP
go:       GO_VERSION
platform: GOOS/GOARCH
```

Ordinary direct Go builds use version `devel` and report `unknown` for commit and build time. The
repository build scripts inject the current 12-character Git commit (with `-dirty` when applicable)
and a UTC RFC 3339 build timestamp. Tagged release artifacts use the injected release version. Go
version and target platform come from the compiled binary. An unknown command returns an error and
the process exits with status 1.

The top-level help currently lists `attach`, `events`, `file`, `init`, `install`, `list`, `mcp`,
`serial`, `uninstall`, `help`, and `version`.

## `install` and `uninstall`

Purpose: install, update, or remove ChannelTerm for the current user without requiring an
administrator account.

```text
channelterm install [--no-path] [--adopt] [--allow-downgrade]
channelterm uninstall [--purge [--yes]]
```

`install` copies the currently running executable; it does not download a release. Linux and macOS
install one executable at `~/.local/bin/channelterm` and a relative symbolic link
`~/.local/bin/cterm -> channelterm`. Windows installs synchronized executable copies under
`%LOCALAPPDATA%\Programs\ChannelTerm\bin`. The command initializes the default `config.toml` through
the same config package used by runtime commands. A missing file receives the minimal template, an
existing valid file remains unchanged, and malformed configuration stops installation before the
program files are changed.

The installer adds only the command directory to the current-user PATH, and only when it is absent.
Linux and macOS write a visibly delimited block to the active shell's user startup file; fish uses a
dedicated `conf.d/channelterm.fish`. Windows updates the current-user `Path` value and broadcasts an
environment-change notification. Existing processes retain their old environment, so a new terminal
may be required. `--no-path` skips PATH management. Uninstall removes a PATH entry or shell block
only when the installation manifest proves ChannelTerm added it and the recorded block is unchanged.

Before updating, the installer verifies its recorded binary SHA-256. Valid three-component semantic
versions are compared: a downgrade requires `--allow-downgrade`; development, custom, and prerelease
version strings are treated as unordered and may be reinstalled. A file that exists without an
installation manifest is never silently overwritten. `--adopt` accepts an existing regular command
only when its SHA-256 matches the running binary, or an existing short-command link only when it
already resolves to the standard installed command. Directories and unrelated files are rejected.

The ownership manifest is stored at:

| Platform | Installation manifest |
| --- | --- |
| Windows | `%LOCALAPPDATA%\ChannelTerm\install.json` |
| Linux | `$XDG_STATE_HOME/channelterm/install.json`, or `~/.local/state/channelterm/install.json` |
| macOS | `~/Library/Application Support/channelterm/install.json` |

The manifest records build provenance, expected command paths, the installed SHA-256, alias kind,
and only the PATH change made by ChannelTerm. Paths loaded from it must still match the platform's
standard layout before any deletion occurs.

Ordinary `uninstall` removes the two commands, the installer-owned PATH change, and `install.json`;
it preserves `config.toml`, `state.json`, and `http-auth-token`. `--purge` additionally removes those
known default files and their lock files after the user answers `y` or `Y` to the `[y/N]` prompt.
`n`, an empty answer, and all other input cancel the purge. `--yes` supplies confirmation for
automation and is invalid without `--purge`. Purge does not recursively remove
unknown files and never edits Codex, Claude Code, OpenCode, or Zoo Code MCP configuration. Windows
uses a temporary helper to remove whichever installed `.exe` is running after the parent exits.

Important errors include an unowned or modified destination, a modified installer-owned binary or
alias, a newer installed release without `--allow-downgrade`, malformed existing configuration, an
unsafe manifest path, inability to modify PATH, and an absent or malformed installation manifest
during uninstall.

## `init`

Purpose: discover supported local MCP clients and install a ChannelTerm MCP server entry, or print
the exact client-specific configuration for a selected transport.

```text
channelterm init --mcp
channelterm init --mcp-show [codex|claude|opencode|zoo]
channelterm init --mcp-help
```

Both commands prompt for a transport:

```text
Select MCP transport:
  1) HTTP (default) - shared Host for multiple clients
  2) stdio - local child process for each MCP client
Choice [1]:
```

Pressing Enter, entering `1`, or entering `http` selects HTTP. Entering `2` or `stdio` selects
stdio. A closed empty standard input also uses HTTP, allowing a non-interactive invocation to use
the documented default.

`--mcp-help`, `--help`, and `-h` display short descriptions of both transports and link to the
[official MCP transport specification](https://modelcontextprotocol.io/specification/latest/basic/transports).

`--mcp` detects Codex, Claude Code, OpenCode, and Zoo Code from their standard configuration
locations or installed client executable where applicable. For every detected client, it adds a
server named `channelterm` using the selected transport. HTTP uses the authenticated shared endpoint
`http://127.0.0.1:37099/mcp`; stdio starts `channelterm mcp`. Existing `channelterm` entries are
retained unchanged, so rerunning the command is idempotent. No supported client detected is an
error.

`--mcp-show` prints configurations only for the selected transport, generated by the same client
adapters used by installation, with a separate visible heading for each client. HTTP output contains
the actual per-user `Authorization: Bearer <token>` credential; OpenCode HTTP examples also disable
its OAuth fallback. Treat HTTP output as a secret: do not share, log, or commit it. The command does
not change client configuration files, but selecting HTTP loads or creates `http-auth-token` so
every printed example is immediately usable. Selecting stdio does not access the token. With no
client name the command prints all four formats; a client name prints only that format. The
recognized names are `codex`, `claude`, `opencode`, and `zoo`.

Configuration writes parse the existing client configuration first, preserve unrelated servers and
unknown fields, and replace the file atomically after writing a synchronized temporary file. A
malformed configuration fails without changing the original file. A stdio configuration starts a
child process and does not expose a network listener. An HTTP configuration joins the default Host
started by `attach`, or a persistent Host started with `channelterm mcp --transport http`.

Important errors include combining `--mcp` and `--mcp-show`, omitting both, unknown client names, an
invalid transport selection, malformed client configuration, HTTP credential load or creation
failure, and a filesystem write failure.

## `serial`

Purpose: open a private, process-owned serial Session and bridge local terminal input and output.

```text
channelterm serial [--profile NAME] [--port PORT] [options]
```

| Option | Default | Accepted values and effect |
| --- | --- | --- |
| `--port` | none | Operating-system serial endpoint. The resolved configuration must contain one. |
| `--baud` | 115200 | Positive integer baud rate. An omitted flag does not override a profile value. |
| `--data-bits` | 8 | `5`, `6`, `7`, or `8`. |
| `--parity` | `none` | `none`, `odd`, `even`, `mark`, or `space`. |
| `--stop-bits` | `1` | `1`, `1.5`, or `2`. |
| `--flow-control` | `none` | Values: `none`, `software`, `hardware`; the backend supports only `none`. |
| `--wake` | false | Sends one carriage return after a newly opened Session connects. |
| `--highlight` | true | Style plain text; use `--highlight=false` to disable it. |
| `--profile` | none | Named profile from the selected TOML file. |
| `--config` | default path | Alternate TOML configuration path. Missing files are created for an opening workflow. |
| `--save` | none | Saves resolved settings under a non-empty profile name before opening. |
| `--help`, `-h` | false | Print command help without opening a port. |

Side effects and lifecycle:

- The command loads or creates configuration, may save a profile, opens the physical port,
  configures local raw mode when stdin is a terminal, and closes the Session on exit. On Windows
  Console hosts, raw input enables virtual-terminal input and normalizes a queued Esc key record to
  byte `0x1b`, so navigation keys are passed through as standard VT sequences and bare Esc reaches
  the local controller; the original console mode is restored when the command exits.
- It sends no startup input unless `--wake` resolves to true.
- `Ctrl+C` is remote input. Use `Ctrl+] q` for a local exit.
- Pressing `Ctrl+]` enters local escape mode and displays the available escape commands locally;
  press `Esc` to cancel that local mode without sending a byte. Cancellation reports
  `[ChannelTerm] Escape cancelled` locally and returns the next input to normal remote forwarding.
  `Ctrl+] t` toggles this CLI instance's prompt timestamps, which are off by default. The timestamp
  uses local `[HH:MM:SS]` format and appears only before conservatively recognized shell prompts,
  not ordinary output lines. This feedback is not Session output.
- Received Session bytes remain raw. Highlighting is enabled by default only for a color-capable
  terminal output and prompt timestamps are applied only while writing this CLI's output.
- The highlighter assigns non-bold semantic colors to status words, boot headings, field names,
  embedded interface/protocol identifiers, addresses, versions, timestamps, numbers, units, quoted
  values, structural `()`, `[]`, and `{}` delimiters, and recognized shell-prompt components. It
  selects TrueColor, 256-color, or 16-color output from local terminal capabilities. `NO_COLOR`
  disables generated colors, `CLICOLOR_FORCE` can force color for a non-terminal destination, and
  ordinary redirected output remains unstyled.
- Remote ANSI/VT takes precedence. Escape-containing output is byte-transparent, including sequences
  split across reads. Persistent SGR attributes and the alternate screen keep the attachment in raw
  presentation; semantic highlighting resumes only after remote state is reset and a safe line
  boundary is reached. Full-screen programs such as `vim` and `htop` are therefore not mixed with
  semantic styling.

Examples:

```powershell
channelterm serial --port COM8
channelterm serial --port COM8 --baud 921600 --parity even --stop-bits 2
channelterm serial --profile board --baud 115200 --highlight=false
channelterm serial --port COM8 --save board
```

Important errors include a missing port, an unknown profile, empty `--save`, unsupported or invalid
serial values, a busy/missing/inaccessible port, an invalid Boolean highlight value, unexpected
positional arguments, raw-console setup failure, and I/O or close errors. Saving happens before the
physical open, so a saved profile may remain after an open failure.

## `connect`

Purpose: resolve a current native serial target from discovery and open it as a private Session.

```text
channelterm connect TARGET [serial options]
```

`TARGET` must be first and must exactly name a present operating-system serial endpoint, such as
`COM8`, `/dev/ttyUSB0`, or `/dev/cu.usbserial-110`. Windows port matching is case-insensitive; Unix
device paths are case-sensitive. `--port` is rejected because the target already selects the
endpoint. All remaining serial options use `serial` semantics, including profile inheritance,
`--save`, `--wake`, highlighting, and local ownership.

```powershell
channelterm connect COM8 --baud 115200
```

Errors include a missing or flag-first target, a target that is no longer present, a forbidden
`--port` override, and the same configuration/open errors as `serial`. The former `SER-COM8` and
`SER-/DEV/TTYUSB0` device-target forms are not accepted; `SER-N` remains a Session reference.

## `attach`

Purpose: attach the local terminal either to a host-managed shared Session or, with an explicit
privacy flag, to a local private serial target.

```text
channelterm attach TARGET_OR_SESSION [options]
```

`TARGET` is reserved as the transport-neutral destination position. Only native serial targets are
implemented today. Future SSH or Telnet support may add its own explicitly documented target form;
the current CLI does not accept SSH or Telnet destinations. See
[Identifiers and References](identifiers.md#transport-extension-boundary).

The normal target-first forms are:

- `attach COM8`: ensure the default local Session Host is running, create or reuse its Session for
  `COM8`, then attach.
- `attach /dev/ttyUSB0`: the equivalent native-target form on Linux.
- `attach SER-1`: attach to an existing Session by short reference.
- `attach <session_id>`: attach by opaque ID.
- `attach COM8 --private`: open a private local connection without MCP.

| Option | Default | Accepted values and effect |
| --- | --- | --- |
| `--endpoint` | `http://127.0.0.1:37099/mcp` | Complete Streamable HTTP MCP endpoint. |
| `--private`, `--no-mcp` | false | Open a serial target locally; valid only for a native serial target. |
| `--highlight` | true | Same local highlighting as `serial`; set false for raw presentation. |
| `--profile`, `--config`, `--save` | as for `serial` | Used only with a serial target. |
| `--baud`, `--data-bits`, `--parity` | as for `serial` | Used only with a serial target. |
| `--stop-bits`, `--flow-control`, `--wake` | as for `serial` | Used only with a serial target. |
| `--label` | empty | Display-only label for a newly created shared Session; not an identifier. |
| `--help`, `-h` | false | Print command help without connecting. |

For compatibility, flags may precede an existing Session reference, but the documented
human-oriented syntax places the target or Session first. Serial options with a Session reference
are rejected. `--private` with a Session reference is rejected.

When opening a target against the default local endpoint, `attach` may start
`channelterm mcp --transport http` in the background and wait up to five seconds for readiness. That
automatically started Host belongs to this `attach` process and is stopped, with its Sessions, when
the process exits. It marks authenticated HTTP responses as attachment-owned, so every bundled
attachment or file client that joins it prints the same lifecycle notice. Start
`channelterm mcp --transport http` separately before attaching when the Host must survive the first
attachment. An already-running or manually started Host is never stopped by `attach`. It will not
auto-start a custom or remote endpoint. Opening a target through MCP requires a local loopback
endpoint. A reused Session retains its original metadata and connection settings; new attach flags
do not reconfigure it. Supplying `--save` still runs the open/save workflow rather than bypassing it
for an already listed Session.

The initial attach read returns recent retained output, then uses private cursors to wait for later
output and activity. New non-empty Agent writes are shown as local `AI` activity blocks; prior
activity and CR/LF-only writes are not rendered. A `terminal_exec` event renders its original
command in the same `AI` block format while the internal `system` wrapper is hidden by exact output
cursor boundaries. Command output remains raw and visible. During the command's `terminal` lease,
ordinary attachment keys are discarded locally and produce
`[ChannelTerm] Input ignored while an AI command is running. Cancel it from the MCP client.` at most
once; input resumes when the lease is released. For a `file-transfer` lease, attach suppresses only
the Host-recorded raw-output cursor range of ChannelTerm's internal preflight, protocol, payload,
and cleanup work; it neither edits Session raw bytes nor filters marker-shaped board text. The same
rule applies to retained output from a transfer that finished before the attachment joined.
Completion, failure, and cancellation release that presentation range, so later Agent writes keep
their `AI` activity blocks and later ordinary terminal output renders normally. File-transfer
started, completed, failed, and cancelled status lines convert Host event times to the CLI's local
timezone and use `[HH:MM:SS] [ChannelTerm]` prefixes; the in-place progress frame has no timestamp.
Before a local status block, attach adds one line ending only when the visible terminal is mid-line,
so an unterminated board prompt cannot join the status text and already-terminated board output does
not accumulate blank lines. This affects only local presentation, never Session raw output. On
Windows Console hosts, raw input enables virtual-terminal input and normalizes queued Esc and Ctrl+C
Console key events to bytes `0x1b` and `0x03`; the original console mode is restored on exit. All
local input is dispatched by mode: in normal attach, `Ctrl+C` is sent to the board; in the file menu
and path prompts it remains local; and during an active shortcut transfer it opens a local
cancellation confirmation rather than writing to the Session. Entering that transfer displays
`[ChannelTerm] Terminal input locked. Ctrl+C to cancel.`; ordinary keys, including Enter, are
discarded before `terminal_write` and produce
`[ChannelTerm] Input ignored during file transfer. Ctrl+C to cancel.` at most once per transfer.
Completion, failure, or cancellation resets that local hint state, so normal input resumes and a
later transfer can show it again. Pressing `Ctrl+]` enters local escape mode and displays the
available escape commands locally; press `Esc` to cancel that local mode without sending a byte.
Cancellation reports `[ChannelTerm] Escape cancelled` locally and returns the next input to normal
remote forwarding. Local status output resets the prompt presentation boundary, so later prompts
remain eligible for timestamps and highlighting. `Ctrl+] t` toggles local-only prompt timestamps for
this attachment, off by default; it does not change the shared Session or MCP output. `Ctrl+] f`
opens a local file-transfer menu with a `Select:` prompt. Press `s` or `r` once to choose send or
receive immediately; the key is echoed locally after `Select:` and no Enter is needed. Invalid keys
and menu `Esc` remain local. Send defaults to POSIX `/tmp/cterm/user-files/<local-basename>` and
receive defaults to local `./<remote-basename>`. During an active shortcut transfer, `Ctrl+C` starts
a new line and prints the untimestamped prompt `[ChannelTerm] Cancel file transfer? [y/N]:`. The
worker pauses at its next safe 8 KiB block boundary while the prompt is pending, and the
`file-transfer` lease remains held so ordinary AI/MCP `terminal_write` calls stay blocked. The
attachment closes its progress redraw gate before printing the prompt, so a concurrent acknowledged
block cannot overwrite the question. Only `y` or `Y` confirms cancellation. Printable answers are
echoed after the prompt's colon even though the terminal is in raw mode. `n`, `N`, Enter, and every
other character print `[ChannelTerm] File transfer resumed` and let the worker continue. After
confirmation, the target restores its TTY, temporary data is removed, the lease is released, and
attach prints one timestamped cancellation result with the last confirmed byte counts and
`Reason: cancelled by user`. The published `FILE_TRANSFER_FAILED` metadata reports
`error: user_cancelled` and `reason: user_cancelled`. Residual console input from the confirming
Ctrl+C is ignored; normal attachment input resumes after the transfer ends. This feedback is not
Session output. `Ctrl+] q` closes only the MCP client connection. It does not call `terminal_close`.

Examples:

```powershell
channelterm attach COM8 --baud 115200 --label board
channelterm attach SER-1
channelterm attach COM8 --private --highlight=false
channelterm attach SER-1 --endpoint http://127.0.0.1:12345/terminal
```

Important errors include a missing Session reference, an offline custom host, a Session not found or
already closed, an invalid target, forbidden target/Session option combinations, host startup
timeout, MCP tool failure, serial configuration failure, and terminal I/O failure.

All Streamable HTTP commands require a Bearer token. ChannelTerm creates and reads the default
per-user token automatically. Set `CHANNELTERM_HTTP_AUTH_TOKEN` for a Host or built-in CLI client
that must use an explicitly supplied token, including a remote endpoint. The value must not contain
whitespace. External clients must send `Authorization: Bearer <token>` themselves. Built-in clients
refuse cross-origin redirects rather than forwarding the credential to another origin.

## `events`

Purpose: stream structured state events from an existing shared Session without attaching a terminal
or reading terminal output.

```text
channelterm events SESSION [--endpoint URL] [--max-events N]
```

The command writes one JSON event per line to standard output. It first returns retained events,
then waits for later events with a private event cursor until interrupted. `--endpoint` defaults to
`http://127.0.0.1:37099/mcp`; `--max-events` defaults to 128 and must be positive. An overflow
marker line with `dropped: true` means the observer fell behind the bounded event retention window;
continue from its `next` cursor.

```powershell
channelterm events SER-1
```

Events carry Session and file-transfer state only. They never include terminal bytes and do not
change `attach`, `terminal_read`, or another observer's cursor.

## `file`

Purpose: stream one regular file or directory through an existing shared Session. Files retain
end-to-end SHA-256 verification; directories use a standard tar stream and staged extraction.

```text
channelterm file send LOCAL_PATH [REMOTE_PATH] [--session SESSION] [--endpoint URL]
channelterm file receive REMOTE_PATH LOCAL_PATH [--session SESSION] [--endpoint URL]
```

`--session` accepts a short reference such as `SER-1` or an opaque Session ID. It may be omitted
only when the selected Host has exactly one open Session. No open Session is an error; multiple open
Sessions require an explicit selection. `--endpoint` defaults to `http://127.0.0.1:37099/mcp`. When
`file send` omits `REMOTE_PATH`, its command-oriented default is
`/tmp/cterm/mcp-files/<local-basename>`. An explicit remote path remains authoritative.

The command requires no AI client. It joins an already-open host-owned Session, acquires a temporary
`file-transfer` lease, and leaves the Session open afterward. It does not open a Serial Transport or
start a Host itself. The default `attach` workflow starts the local Host when needed.

Both directions use acknowledged 8 KiB chunks and display confirmed progress on one refreshed line.
The 20-cell ASCII progress bar includes percentage, transferred and total sizes, speed, and ETA. A
successful regular-file transfer finishes with `Local SHA-256`, `Remote SHA-256`, `Verify`, and
`Saved`; cancellation and failure instead finish the progress line and report the last confirmed
byte count.

While the transfer lease is active, other ChannelTerm writers fail immediately. `Ctrl+C` in a
bundled attachment opens a default-No confirmation; only `y` or `Y` cancels. Cancellation waits for
a safe block boundary, restores the target TTY, removes temporary transfer data, and releases the
lease before publishing its final result. If bounded recovery cannot restore a usable protocol
state, ChannelTerm closes the Host Session and the endpoint must be reopened. Exact recovery timing
and failure behavior are documented in the
[file-transfer workflow](../getting-started/file-transfer.md#protocol-and-concurrency-limits).

The remote shell must provide `mkdir`, `stty`, `dd` with `iflag=fullblock`, `wc`, `sha256sum`, and
`sleep`; directory transfer additionally requires `tar`. A transfer uses one non-interactive child
shell. Bash removes the bootstrap from its current in-memory history; another POSIX shell may retain
one recognizable bootstrap entry.

File-transfer events use the same path fields in both directions: `source_path` is the source
location, `requested_path` is the user- or Agent-requested destination before collision handling,
and `resolved_path` is the final destination actually saved. For send, the source is local and the
destination is on the board; for receive, the source is on the board and the destination is local. A
completed transfer includes all three fields plus boolean `renamed`; consumers must use
`resolved_path` for later operations on the saved file. The former `local_path` and `remote_path`
fields are removed without compatibility aliases. Regular-file completion also includes `sha256`;
directory completion omits a directory digest.

Regular files use byte-count and SHA-256 verification. Directories use staged tar archives; local
extraction rejects absolute or parent-traversal paths, links, devices, FIFOs, sockets, and other
special entries. ChannelTerm commits a staged destination only after complete transfer and
successful extraction. Directory completion represents verified tar-stream transfer, not a stable
directory content hash.

Shared attachments observe structured progress while suppressing local rendering of ChannelTerm's
internal commands, markers, and payload bytes. They continue advancing independent raw-output
cursors, and ordinary Session output remains unchanged. Rendering resumes from the current cursor
after completion, failure, or cancellation without replaying the suppressed protocol range.

Before a send, ChannelTerm creates a missing remote parent hierarchy with `mkdir -p`; an existing
hierarchy is reused. Failure to create it stops before payload transmission. Send and receive do not
silently overwrite. If a requested remote or local destination exists, the first free `_1`, `_2`,
and later sibling is selected, preserving simple and compound extensions and dotfiles. A receive
refuses to install if another process creates that selected local path during transfer. These are
deliberate write-capable operations; confirm the paths and use only a trusted Session Host. The file
command does not change the Host's loopback listener default or add authentication.

The shell must remain idle with respect to non-ChannelTerm output, but other ChannelTerm writers are
blocked by the Host lease during transfer and receive `Session SER-1 is locked by file-transfer`
-style errors. Independent Session readers continue to work. A receive exposes raw file bytes to
those readers because Session output remains unmodified.

An attached CLI remains connected for reads during a shortcut transfer. Ordinary keyboard input is
discarded locally until the lease is released; independent MCP writers receive the normal busy
error.

Examples:

```powershell
channelterm file send firmware.bin /tmp/firmware.bin
channelterm file send firmware.bin --session SER-1
channelterm file send firmware.bin /tmp/firmware.bin --session SER-1
channelterm file receive /tmp/log.txt ./log.txt --session SER-1
```

Important errors include an offline Host, no or ambiguous open Session, an existing lease,
missing/non-regular local source, invalid remote path, missing or incompatible board commands,
unavailable remote `tar`, unsupported symlinks/special entries, unsafe tar paths, remote
open/read/write failure, Session cursor overwrite, local I/O failure, size mismatch, SHA-256
mismatch, and cancellation. See the focused
[file-transfer workflow](../getting-started/file-transfer.md) for protocol and recovery constraints.

## `list`

Purpose: combine local serial discovery, saved profiles, and an optional MCP Session snapshot
without opening a port or changing configuration.

```text
channelterm list [--transport NAME] [--kind KIND] [options]
```

| Option | Default | Accepted values and effect |
| --- | --- | --- |
| `--transport` | all | Comma-separated names; unsupported names match no local source. |
| `--kind` | all | Comma-separated `device`, `profile`, or `session`; unknown kinds are errors. |
| `--endpoint` | `http://127.0.0.1:37099/mcp` | MCP HTTP source for active Sessions. |
| `--config` | default path | Existing TOML file used for profile rows. It is never created by `list`. |
| `--json` | false | Emit one indented JSON result object with all available fields. |
| `--long`, `-l` | false | Add opaque Session IDs to the text table. It does not change JSON. |
| `--no-mcp` | false | Skip the MCP query and report MCP state as `disabled`. |
| `--help`, `-h` | false | Print command help. |

If MCP is unreachable, the command reports it as `offline` but still returns local devices and
profiles. An active Host lease is displayed in the Session row's occupancy as
`locked by file-transfer` (or its lease type). Local target/profile rows and a Session from a local
loopback host are merged when they describe the same endpoint. A remote host's Session is not merged
with a similarly named local port.

The compact table shows `TARGET`, `KIND`, `STATE`, `OCCUPANCY`, `SESSION`, and `LABEL`. `TARGET` is
directly usable with `connect` or target-based `attach`; `SESSION` contains a short reference such
as `SER-1` when one exists. Long output also shows transport, opaque Session ID, and source.

The JSON object has `mcp` and `items` fields. Each item reports `kind`, `transport`, `target`,
`state`, `occupancy`, `source`, and optional reference, Session, and label fields. For device and
profile rows, `reference` is now the same native endpoint as `target`; it is no longer a generated
`SER-<port>` value. Treat JSON fields as the script-oriented output; the compact table is
presentation-oriented.

Examples:

```powershell
channelterm list
channelterm list --kind device --transport serial --no-mcp
channelterm list --kind session --long
channelterm list --json
```

Important errors include an unknown kind, unexpected positional argument, serial enumeration
failure, invalid configuration, and local source failure. MCP unavailability by itself is not a
command failure.

## `mcp`

Purpose: start a long-running MCP server and own its Session Manager and Device Registry for the
process lifetime.

```text
channelterm mcp [--transport stdio|http] [--listen ADDRESS] [--path PATH] [--connection-policy ask|auto|deny]
```

| Option | Default | Accepted values and effect |
| --- | --- | --- |
| `--transport` | `stdio` | `stdio` or `http`. |
| `--listen` | `127.0.0.1:37099` | TCP listen address used only in HTTP mode. |
| `--path` | `/mcp` | HTTP handler path; must start with `/`. |
| `--connection-policy` | configuration, then `ask` | Explicit `ask`, `auto`, or `deny` override. |

Startup loads or creates `config.toml` and `state.json`, starts serial discovery, registers tools,
and then serves until cancellation or client/process termination. Stdio writes no status text to
stdout. HTTP writes its effective endpoint to stderr and warns when the bound address is not
loopback. HTTP requires the configured Bearer token. An attachment-owned auto-started Host also
returns `X-ChannelTerm-Host-Lifetime: attachment` on authenticated responses; a separately started
Host omits that header.

```powershell
channelterm mcp
channelterm mcp --transport http
channelterm mcp --transport http --listen 127.0.0.1:12345 --path /terminal
```

Important errors include an unsupported transport, path without a leading slash, invalid connection
policy, invalid configuration or state, initial discovery failure, listen failure, and protocol
serving failure. Normal client EOF and process cancellation are normal shutdowns.
