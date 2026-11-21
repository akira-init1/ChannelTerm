# Shared Sessions

An HTTP MCP process can act as a local Session Host. It owns the underlying connection while
CLI windows and MCP Clients use independent cursors over the same Session. When `attach` starts the
default local Host automatically, that Host is stopped when the initiating `attach` process exits. A
manually started Host remains available until it is stopped explicitly.

```text
Endpoint -> Serial or SSH Transport -> Channel -> Session -> Client / Attachment
```

## Create or join by target

```powershell
channelterm attach COM8 --baud 115200
```

For the default endpoint, `attach` starts a background local HTTP host if one is not already
reachable. It then asks the host to open the serial target. Within that host's Manager, one active
Session is shared for an exact `transport + endpoint` pair, so another open for `serial + COM8`
returns the existing Session instead of opening the port twice.

The command reports whether the shared Session was created or reused, then attaches the current
terminal. Press `Ctrl+]` to enter local escape mode; ChannelTerm displays the available escape
commands locally. `Ctrl+] t` toggles prompt timestamps for this CLI attachment only, so other CLI
attachments and MCP readers continue to receive raw output. `Ctrl+] Esc` reports
`[ChannelTerm] Escape cancelled` locally, cancels escape mode without sending a byte to the shared
Session, and returns subsequent input to normal remote forwarding. `Ctrl+] q` leaves this CLI;
whether the Host survives depends on ownership, as described in [Lifecycle boundary](#lifecycle-boundary).

Each CLI attachment applies semantic colors to safe plain text by default when its own stdout is a
color-capable terminal. Use `--highlight=false` to disable generated colors. Remote ANSI/VT causes
only that attachment to use byte-transparent output while its control or persistent display state is
active; highlighting resumes after a safe reset boundary. Session history and every other client
continue to receive the same raw bytes.

Interactive attachments also show a [local status bar](../reference/cli.md#local-status-bar) with
observed controller, operation and input permission. `Ctrl+] i` still performs explicit takeover;
the display itself does not request control. Use `--status-bar=false` to disable it. Complex terminal
controls, long lines, complex Unicode or resizing remove the footer for the rest of the attachment;
Session output and other clients' MCP reads remain unchanged.

## Join an existing Session

List host Sessions and copy either the short reference or opaque ID:

```powershell
channelterm list --kind session --long
channelterm attach SER-1
channelterm attach 0123456789abcdef0123456789abcdef
```

Each reader owns its own output and activity cursors. One Client reading output does not consume it
for another Client. Session writes are serialized as complete payloads, including retries after
short Transport writes, so concurrent payload bytes do not interleave.

An existing shared Session can also carry a managed [file transfer](file-transfer.md). Use
`Ctrl+] f` in an attachment for a guided local flow, or run `channelterm file send` /
`channelterm file receive` from another terminal. While a `file-transfer` lease is active, `Ctrl+C`
in any bundled attachment opens a default-No local cancellation confirmation instead of calling
`terminal_write`; normal terminal `Ctrl+C` still sends `0x03`. While confirmation is pending, the
transfer owner pauses at a safe block boundary and the Host retains its lease. Independent MCP
`terminal_write` calls therefore fail immediately rather than entering the prompt or interleaving
with the shell protocol.

The file-transfer lease and all of its status events share one `transfer_id`. MCP clients waiting on
that ID receive the terminal result only after the matching lease release, so the next ordinary
write does not race cleanup.

Session's byte-serialization guarantee does not coordinate the meaning of concurrent commands. The
Session layer has no writer ownership, transaction, priority, arbitration, or shell-state model.
Application control scopes coordinate authenticated multi-call operations above Session and share
the existing lease gate. All writers need current control metadata; AI raw work requires an explicit
scope. Human takeover revokes old writes before recovery and keeps AI paused until explicit allowance.
Unknown recovery closes the captured Session. See the [control contract](../reference/mcp-tools.md#session-control)
and [migration steps](http-authentication-migration.md#default-controlled-sessions).

After attachment, the CLI watches new activity from the current tail and renders non-empty Agent
writes as local `AI` activity blocks. It does not replay older activity, and writes containing only
carriage-return or line-feed bytes are not rendered as blocks. This local view does not add bytes to
Session output or change another client's cursor. Large raw Agent writes use a bounded preview
instead of printing the entire payload; see the [CLI contract](../reference/cli.md#attach).
File uploads should use the existing [file transfer](file-transfer.md) workflow, which renders
progress and verification rather than file bytes. Manual shell uploads can still echo board output.

## Observe shared state

Use a separate terminal to observe attachment, lease, and file-transfer state without consuming
device output:

```powershell
channelterm events SER-1
```

The command emits JSON Lines from the same bounded Session Event Stream used by MCP
`terminal_session_events`. Each observer owns its cursor. If an observer is slower than the retained
window, it receives `dropped: true` and continues at the returned cursor; it never slows the serial
reader or Session writers.

## Use a private connection

```powershell
channelterm attach COM8 --private --baud 115200
```

`--no-mcp` is an alias for `--private`. The current CLI process owns this port and closes it when
the client exits. Other CLI or MCP clients cannot join it. `--private` is valid only with a serial
target, not with a Session reference.

## Lifecycle boundary

An ordinary attachment disconnects without closing its shared Session. Host owners have a different
lifetime boundary:

| Process that exits | Effect |
| --- | --- |
| Secondary `attach`, or an attachment to a separately running Host | Only that attachment leaves; the Host and Sessions remain. |
| `attach` that created the temporary Host | The temporary Host and all of its Sessions stop. |
| Foreground `ssh` owner | Its sharing Host and all of its Sessions stop. |
| Separately started `mcp --transport http` Host | All Sessions owned by that Host close. |

`terminal_close` removes and closes one host-owned Session. Session references such as `SER-1`
are valid only for that Host process lifetime.

If the underlying serial Channel ends or its reader fails, the host automatically removes and closes
the affected Session. Its short reference and opaque ID stop resolving, retained buffers are
released, and a later open can create a new Session for the reconnected device. Clients waiting on
the failed Session receive the original stream error and should list Sessions again before
reconnecting.

Expired file-transfer and debug leases close and remove the original Session even when no write
is in flight. They never reopen it for another writer. A terminal lease retains its existing expiry
policy: an in-flight leased write causes closure; otherwise writer ownership is released. Closure
does not establish remote program exit or TTY recovery. Verify device state through an appropriate
independent recovery procedure before reconnecting; reopening alone is not proof of safety. See
[Application Module](../modules/application.md) for exact lease timing and lifecycle behavior.

See [Identifiers](../reference/identifiers.md) before passing serial targets, Session references, or
`session_id` values between commands.

## Electron GUI attachment

For a desktop view, the [Electron client](../development/electron-gui.md#connect-to-an-existing-host)
can attach to an existing Session on the same Host. It shows raw output and ordinary input with
Host-authoritative lease checks. Detaching or closing that GUI leaves the Host and other clients
running. Its serial file picker and progress panel reuse the [managed file workflow](file-transfer.md#electron-file-picker-and-progress), including safe cancellation. A GUI-owned file operation must finish or be cancelled before disconnect/exit. Debug observation and explicit Session takeover are available; general debugger commands remain outside this GUI scope.

## Shared SSH shell

`channelterm ssh user@host.example` opens an SSH PTY shell and hosts it as `SSH-1` by default.
Another terminal can run `channelterm attach SSH-1` and observe the same retained output using
an independent cursor. `channelterm list --transport ssh` lists the active Session.
The foreground `ssh` command owns this Host: keep it open while other attachments use the shell.
Exiting a secondary attachment does not close the shell; exiting the owner stops its entire Host.

Use `channelterm ssh user@another-host.example --shared` to open a shell on the running
Host alongside its serial and SSH Sessions. Use `channelterm list` to see their references.
Follow the [SSH terminal workflow](ssh-terminal.md) for authentication, alternate endpoints,
and safe shutdown.

## Exclusive user-space debugging

A [local Linux GDB operation](gdb-debugging.md) temporarily owns a `debug` lease. Other readers keep
independent raw cursors, while ordinary writers receive a busy error. Bundled attachments suppress
the leased binary protocol output and remain attached. Use the debug tools for interrupt/detach;
normal Ctrl+C is not debugger control. The original terminal mode and a random-marker shell probe
must succeed before ordinary input resumes. Expired debug leases and unverified recovery close the
Session instead of reopening writing. `DEBUG_STATE` events identify lifecycle transitions.


For an independently operated GDB, use the
[external serial RSP bridge](gdb-debugging.md#external-gdb-over-a-prepared-serial-session).
It shares the same exclusive write lease and attachment output suppression, but does not run MI or
start a target server. A current helper maintains the leases through a separate authenticated control
connection, even while RSP is idle. External client disconnection stops automatic renewal but retains the lease
during the remaining management grace (see the bridge workflow for renewal and lifetime bounds).
Only a separately confirmed shell/TTY restoration allows ordinary writers again; explicit bridge close also closes the Session.

Codex can separately publish actual external commands, results, and observed states with
`debug_bridge_report`. All attach clients see labelled report blocks; a new client can see recent
retained reports. They are caller reports, not a complete GDB audit or shell output. Reporting
failure does not stop debugging. See the [reporting workflow](gdb-debugging.md#show-codex-reported-commands-and-results).

## Interrupt an operation and take control

In an attached CLI, press `Ctrl+] i`; in Electron, use **Interrupt AI / Take control**. Check the
Session and operation shown in the confirmation. The Host first revokes the old control version,
then requests the supported worker's cancellation/recovery. Keep input paused while stopping or
unconfirmed. Known commands, managed serial transfers and managed debug reuse their recovery
paths; unknown external work may require closure of that original Session. Recovery does not
prove that background remote work stopped.

After confirmed recovery, only the taking-over instance can write and AI stays paused. Explicitly
choose **Allow AI operations** or `Ctrl+] a` when new AI work is appropriate. No old operation or
input restarts. Use **Query original result** or `Ctrl+] r` to inspect an uncertain takeover rather
than repeating it. Disconnecting does not undo the pause. Another authorized user instance can
explicitly take control or allow new work using the current version. See the
[CLI details](../reference/cli.md#human-interruption-from-an-attachment) and
[desktop workflow](../reference/gui-menus.md#human-interruption-and-control).

Independent SSH file tasks use their own transfer cancel action. They do not own a shared PTY;
cancelling them does not send terminal Ctrl+C, close a Session or prevent later independent tasks.

## Unused Sessions and Host lifetime

New Electron-launched Hosts release a Session after 30 seconds with no registered use or protected
work; the MCP listener stays available. Existing manual Hosts keep their own policy. Closing a tab
or window still only detaches that view. Another client's live monitor, a background tab, active
operation, lease, debugger, recovery or human AI pause retains the relevant Session. A standalone
list poll does not. Unknown external debugger state is retained conservatively.

Check `host_idle_status` for effective policy and retention reasons; GUI displays its effective
source/defaults and grace periods. Use [Host configuration](../reference/configuration.md#host-idle-policy)
to keep resources (`0s`) or explicitly enable fully idle exit. Policy changes require an explicit
Host restart after finishing shared work. A released serial endpoint may be opened by another
program; closing an SSH Session may end its shell. Reconnect explicitly to create a new Session.
Reconnection never resends input. A stopped Host needs an external launch; HTTP requests cannot wake it.
