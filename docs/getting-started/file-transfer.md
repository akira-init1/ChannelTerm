# File Transfer over a Serial Session

ChannelTerm can stream a regular file or a directory through an existing shared Session without an
AI client or a separately installed board-side transfer program. The board side remains an ordinary
interactive Linux shell.

## Preconditions

- The remote shell is idle and accepts POSIX-style shell commands.
- The board provides `mkdir`, `stty`, `dd` with `iflag=fullblock`, `wc`, and `sha256sum`. BusyBox or GNU
  userland may provide these commands. Directory transfer also requires `tar`; ChannelTerm checks
  `command -v tar` and does not install it.
- The serial settings already match the board. File transfer does not change baud rate, data bits,
  parity, stop bits, or flow control.
- Other Clients may keep reading with their independent Session cursors. ChannelTerm acquires a
  Host-side `file-transfer` lease that blocks its other writers during transfer; it cannot prevent
  unsolicited output or writers that bypass ChannelTerm.

First create or join a shared serial Session in one terminal:

```powershell
channelterm attach SER-COM8 --baud 115200
```

The default `attach` workflow starts the local loopback Session Host automatically when necessary.
There is no need to start `channelterm mcp` manually or configure an AI client.

In another terminal, send a file to the board:

```powershell
channelterm file send firmware.bin
```

With no remote destination, this command-oriented path stores the file as
`/tmp/cterm/mcp-files/firmware.bin`. ChannelTerm creates `/tmp/cterm/mcp-files/` when it is absent
and reuses the directory when it already exists. Supply a remote destination explicitly to save
somewhere else.

In a third terminal, observe the shared transfer state without rendering raw file bytes:

```powershell
channelterm events SER-1
```

An MCP client should capture the Session event cursor, obtain `transfer_id` from the transfer's
structured start or lease event, and call `terminal_wait_file_transfer` with both values for the
final `completed`, `cancelled`, or `failed` result. It must
not use `terminal_wait` as the transfer completion signal because that tool waits only for raw
terminal bytes and cancellation may not emit another shell prompt. Every completed result uses
three fixed path meanings: `source_path` is the source location, `requested_path` is the requested
destination, and `resolved_path` is the final saved destination. For send, use `resolved_path` for
later board commands; for receive, use it for later local operations. `renamed` reports whether
collision handling selected an `_N` sibling. The wait returns only after the matching
`file-transfer` lease is released, so an AI may immediately issue its next ordinary Session command.

Receive a file from the board:

```powershell
channelterm file receive /tmp/log.txt ./log.txt
```

The same commands automatically recognize directories. No separate directory command is needed:

```powershell
channelterm file send .\build\release /tmp/release
channelterm file receive /var/log/myapp ./myapp
```

When more than one shared Session is open, select one explicitly:

```powershell
channelterm file send firmware.bin --session SER-1
channelterm file receive /tmp/log.txt ./log.txt --session SER-1
```

`--endpoint` selects a non-default Session Host endpoint. The file command never starts a host or
opens a Serial Transport itself; it attaches to an already-open Session and leaves that Session open
afterward.

For every send, ChannelTerm creates a missing remote parent hierarchy with `mkdir -p` before it
writes the file or directory. An existing hierarchy is reused. `send` and `receive` never silently overwrite their target. If the requested remote or local name
exists, ChannelTerm chooses the first free sibling using `_1`, `_2`, and so on, preserving
extensions including `backup.tar.gz` and `.env`. `receive` writes and verifies a temporary local
file first, then installs it only when the chosen destination remains absent. Confirm both paths
before running the command. A non-default Host endpoint is a terminal-control boundary and must be
trusted; the current HTTP Host has no ChannelTerm authentication layer. The feature does not change
the loopback-only default listener.

## Attach shortcut

While attached, press `Ctrl+] f` to open a local file-transfer menu. It uses the same shared
Session, Application transfer implementation, 8 KiB streaming, SHA-256 verification, and Host-side
`file-transfer` lease as the `channelterm file` command.

```text
Ctrl+] f
File transfer:
  s  Send PC -> Board
  r  Receive Board -> PC
  Esc  Cancel
Select:
```

Press `s` or `r` once to choose immediately; the selected key is echoed after `Select:` and
ChannelTerm then shows the path prompts. No Enter is required for the menu choice. Invalid menu keys
are ignored locally. For send, enter a required local path and accept or edit the remote default
`/tmp/cterm/user-files/<local-basename>`. The missing hierarchy is created automatically and an
existing one is reused. Board paths are always POSIX paths. For receive, enter a required board
path and accept or edit the local default `./<remote-basename>`, relative to the directory in which
`attach` was started. A regular file keeps the file protocol; a directory uses tar automatically.
Local paths use the current platform's path rules. `Esc` exits only the file menu. Each attachment
has one local input dispatcher: normal attach `Ctrl+C` goes to the board, while menu/path input
remains local. At transfer start it reports that terminal input is locked. During a shortcut transfer,
ordinary keys (including Enter) are discarded locally before `terminal_write` and show one
`Input ignored during file transfer. Ctrl+C to cancel.` hint; they are never sent to the board.
While either that shortcut or a separate `channelterm file` process owns the Session's
`file-transfer` lease, `Ctrl+C` in any bundled attachment instead opens
`[ChannelTerm] Cancel file transfer? [y/N]:` locally without printing `^C`, detaching, or sending Ctrl+C to the
board. The transfer pauses at its next safe 8 KiB block boundary while the
answer is pending. An already active block may still finish and update the retained confirmed byte
count, but the attachment closes its redraw gate before displaying the prompt, so that progress
event is not redrawn over the confirmation prompt. Only `y` or `Y` confirms; printable answers are
echoed after the prompt's colon. `n`, `N`, Enter, and every other character print
`[ChannelTerm] File transfer resumed` and continue. During the prompt the Host retains its
`file-transfer` lease, so independent AI/MCP writes cannot enter the shell protocol. After a
confirmed cancellation, the board restores its TTY, ChannelTerm removes temporary transfer data,
releases the lease, publishes `FILE_TRANSFER_CANCELLED` with `reason: user_cancelled`, reports the last confirmed byte count once, and
returns to the attachment. Directory transfers use the same bounded blocks instead of one full-size
raw tar interval.

The Host lease has a 30-second TTL, and the bundled transfer client renews it every 10 seconds. If
the client is killed or loses its MCP connection long enough to miss renewal, the Host expires the
lease, publishes a `lease_expired` failure and release event, and allows another writer or transfer
to acquire the Session. A stale owner cannot use leased writes after expiry. If a leased Channel
write is still in flight at expiry, the Host instead closes and removes the Session so that the
blocked write cannot prevent cleanup; reopen the device Session before retrying. A cancellation
confirmation waiting on the missing owner returns as expired instead of remaining blocked.

During PC-to-board payload input, the target-side raw TTY has a 10-second idle timeout. After that
timeout, the shell restores the saved TTY mode and rejects the short block rather than waiting
indefinitely.

After `y` or `Y`, ChannelTerm also cancels an outstanding wait for a protocol marker such as
`PICK`, `INIT`, `READY`, `ACK`, or `FINAL`. This prevents a missing board response from trapping the
attachment in file-transfer mode. If raw payload transfer has already started, its bounded cleanup
continues independently while it tries to pad or drain the bounded block, consume its
acknowledgement, restore the target TTY, and remove directory-transfer staging data. Each recovery
write, read, acknowledgement wait, and cleanup command has a 15-second deadline. If the target
shell or protocol stops responding, the bundled attachment closes the Host Session with a separate
five-second-bounded request so an in-flight Host write cannot retain the lease. The reported failure
includes the recovery timeout, and the Session must be reopened before retrying. Target TTY
restoration and temporary-file removal are not confirmed and should be checked manually.

## Transfer flow

```text
Local file
   |
   | bounded 8 KiB reads
   v
CLI file command
   |
   | existing Session Write / independent output cursor
   v
Shared Session -> Channel -> Serial Transport -> Linux TTY
                                                |
                                      stty raw/no-echo
                                                |
                               dd reads or writes one chunk
                                                |
                                      restore saved TTY mode
                                                |
                                  wc -c + sha256sum
                                                |
                                                v
                                       verified remote file
```

For each chunk, the shell saves its current TTY mode, enters raw/no-echo mode, transfers exactly one
bounded block with `dd`, restores the saved mode, and emits an acknowledgement containing a random
per-transfer token. The raw interval is limited to one chunk rather than the whole file. ChannelTerm
reports progress only after a chunk is acknowledged and publishes it as `FILE_TRANSFER_PROGRESS`
with structured confirmed byte counts, percent, and best-effort speed. It also publishes start,
completion, cancellation, and failure events without mixing them into raw Session output.

The local `file send` and `file receive` displays share one 20-cell ASCII progress formatter. For a
non-empty regular file, it immediately renders a 0% frame before the first payload (for receive,
after the board has announced its size), then refreshes in place with confirmed percentage,
human-readable transferred/total sizes, speed, and ETA. Every frame has the field order
`[progress bar] percentage transferred / total speed ETA`, uses `#` and `-` for the fixed-width bar,
and uses binary B/KiB/MiB/GiB units. Before a meaningful rate is available it reports `0 B/s` and
`ETA --`; a known completed total reports `ETA 0s`. A completed transfer first renders a 100% bar
and then starts a common send/receive summary containing `Local SHA-256`, `Remote SHA-256`, `Verify`,
and `Saved` on new lines. Cancellation and failure likewise finish the progress line before their
status text, so an error never runs into a partially refreshed bar.

During a transfer, shared `attach` clients use the Host's `file-transfer` lease lifecycle and its
raw-output cursor boundaries to gate local presentation. This includes preflight work such as
remote-path detection and cleanup, not only payload streaming. The attachment that started the
transfer retains its detailed progress display. Other attachments render acknowledged bytes, total
bytes, percentage, speed, and ETA with the same 20-cell format on one line that is refreshed in
place. Success leaves the 100% frame visible, clears the next status row, starts the timestamped
completion block using the attachment's local timezone, and prints both full SHA-256 values with
`Verify: MATCH` and the saved path for regular files. Cancellation and failure instead report the
last confirmed byte count and either the
cancellation reason or error. Attach ends an unterminated visible board line before it renders one
of these status blocks, without adding an extra line when the board already ended its output;
neither operation changes raw Session data. All attachments
continue advancing their own output cursors while suppressing only the semantic internal-operation
range: shell commands, markers, and payload bytes cannot appear later from retained history, while
ordinary board output (including text that happens to contain `@CTERM:` ) remains raw. Normal Agent
writes retain their local `AI` activity blocks after completion, failure, or cancellation. Session
raw output and independent MCP readers are unchanged.

For a successful regular file started from the `Ctrl+] f` menu, the local output uses that same
order: 100% progress, timestamped `File transfer completed`, and then the four verification fields.
There is no blank line between the completion status and `Local SHA-256`.

For send, ChannelTerm hashes bytes as it reads the local file. After the final chunk, the board
computes the stored size with `wc -c` and digest with `sha256sum`; both must match.

For receive, the board announces size and digest before streaming. ChannelTerm writes to a temporary
file beside the requested destination, hashes the stream, and replaces the destination only after
the digest matches. A failed receive removes the temporary file and retains an existing destination
until verification has succeeded.

## Protocol and concurrency limits

The protocol is deliberately shell-oriented rather than a new board agent. Control markers are ASCII
lines containing a random token; file payload bytes are raw and are never encoded or loaded as one
complete in-memory value.

Session still guarantees byte serialization for each individual write. Above it, the Host holds one
exclusive `file-transfer` lease for the complete CLI command. Other ChannelTerm writers fail
immediately with a clear locked-session error, while the leased command uses its opaque owner
capability for every write. The lease is released after success, failure, or cancellation. It cannot
stop shell prompt hooks, unsolicited device output, or writers that bypass ChannelTerm, so keep the
endpoint itself quiescent. Independent `attach`, `terminal_read`, and `terminal_wait` readers
remain valid, although a receive transfer's raw file bytes are visible in their raw Session output
and may not be suitable for terminal rendering.

If the command is interrupted while a send chunk is waiting for bytes, ChannelTerm pads only that
announced 8 KiB-or-smaller interval and waits for the shell to restore its saved TTY mode before
releasing the lease. Directory transfers use the same acknowledged block boundary, then remove their
temporary target archive instead of draining the untransferred remainder. If the Session or device
disappears, reconnect locally and run `stty sane` on the board console if its terminal mode was not
restored.

Directory tar streams use temporary target-side archives to make every raw-terminal interval
bounded. PC-to-board creates a hidden archive beside the destination, then extracts into a hidden
staging directory. Board-to-PC creates its archive under target `/tmp` and extracts received blocks
into a hidden local staging directory. The target needs enough temporary free space for the archive
and, during a send extraction, the staged directory. A staged directory is renamed only after the
full stream and extraction succeed. Local extraction rejects absolute or parent-traversal paths and
rejects symlinks, hard links, devices, FIFOs, sockets, and other special entries. Directory progress
counts tar-stream bytes, not a synthetic directory hash; successful completion guarantees tar
completed, extraction succeeded, and staging was committed, but does not claim a directory-level
SHA-256.

The first version does not provide resume, compression, zip, incremental synchronization,
sparse-file preservation, permissions/ownership preservation, symlink or hard-link handling,
per-chunk checksums, coordination with writers that bypass the Host, or non-Linux shell support.
