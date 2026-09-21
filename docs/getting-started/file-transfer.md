# File Transfer over a Serial Session

ChannelTerm can stream a regular file or a directory through an existing shared Session without an
AI client or a separately installed board-side transfer program. The board side remains an ordinary
interactive Linux shell.

## Preconditions

- The remote shell is idle and accepts POSIX-style shell commands.
- The board provides `mkdir`, `stty`, `dd` with `iflag=fullblock`, `wc`, `sha256sum`, and `sleep`.
  BusyBox or GNU userland may provide these commands. Directory transfer also requires `tar`;
  ChannelTerm checks `command -v tar` and does not install it.
- The serial settings already match the board. File transfer does not change baud rate, data bits,
  parity, stop bits, or flow control.
- Other Clients may keep reading with their independent Session cursors. ChannelTerm acquires a
  Host-side `file-transfer` lease that blocks its other writers during transfer; it cannot prevent
  unsolicited output or writers that bypass ChannelTerm.

First create or join a shared serial Session in one terminal:

```powershell
channelterm attach COM8 --baud 115200
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

Automation clients must use `terminal_wait_file_transfer`, not raw-output `terminal_wait`, to
observe a final transfer result. See [MCP Server](mcp-server.md#waiting-for-a-file-transfer) for the
workflow and [MCP tools](../reference/mcp-tools.md#terminal_wait_file_transfer) for the result
schema.

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
writes the file or directory. An existing hierarchy is reused. `send` and `receive` never silently
overwrite their target. If the requested remote or local name exists, ChannelTerm chooses the first
free sibling using `_1`, `_2`, and so on, preserving extensions including `backup.tar.gz` and
`.env`. `receive` writes and verifies a temporary local file first, then installs it only when the
chosen destination remains absent. Confirm both paths before running the command. A non-default Host
endpoint is a terminal-control boundary and must be trusted. The HTTP Host requires a Bearer token
but does not provide TLS or per-tool authorization. The feature does not change the loopback-only
default listener.

Each transfer uses one non-interactive child shell. Interactive Bash removes the bootstrap from its
current in-memory history without changing earlier entries. Other POSIX shells may retain one
recognizable bootstrap entry. Internal probing, chunking, verification, and cleanup commands run in
the child rather than becoming separate interactive-history entries.

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

Press `s` or `r` once; no Enter is required. Invalid keys remain local, and `Esc` closes only the
menu. Send prompts for a local path and defaults the board destination to
`/tmp/cterm/user-files/<local-basename>`. Receive prompts for a board path and defaults the local
destination to `./<remote-basename>` relative to the directory where `attach` started. Board paths
use POSIX syntax; local paths use the current platform's rules.

Normal attach `Ctrl+C` goes to the board. During a transfer, ordinary keys are discarded locally and
`Ctrl+C` instead opens `[ChannelTerm] Cancel file transfer? [y/N]:`. Only `y` or `Y` confirms; every
other answer resumes. The Host retains its lease while the question is pending, so another
ChannelTerm writer cannot enter the shell protocol.

A confirmed cancellation stops at a safe block boundary, restores the target TTY, removes temporary
transfer data, releases the lease, and reports the last confirmed byte count. If the transfer client
disappears, lease expiry releases an idle operation; an in-flight blocked write instead causes the
Host to close the Session. Reopen the endpoint before retrying after such a forced close.

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

For each chunk, the shell saves its TTY mode, enters raw/no-echo mode, transfers one bounded block,
restores the saved mode, and acknowledges the block with a per-transfer token. Progress advances
only after acknowledgement. Start, progress, completion, cancellation, and failure are published as
structured Session events rather than inserted into raw terminal output.

The CLI displays a 20-cell ASCII progress bar with confirmed percentage, byte counts, speed, and
ETA. A successful regular-file transfer then reports both SHA-256 values, verification status, and
the saved path. Cancellation and failure report the last confirmed byte count and reason instead.

Shared attachments suppress only ChannelTerm's internal transfer range while continuing to advance
their own output cursors. Ordinary board output and independent MCP readers remain raw and
unchanged; suppressed protocol data is not replayed after the transfer ends.

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
endpoint itself quiescent. Independent `attach`, `terminal_read`, and `terminal_wait` readers remain
valid, although a receive transfer's raw file bytes are visible in their raw Session output and may
not be suitable for terminal rendering.

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
counts tar-stream bytes. Both endpoints compute SHA-256 over those exact tar bytes before reporting
success, so corruption is rejected even when the damaged stream remains a valid tar archive.
Extraction and staging commit must also succeed. The digest verifies the transferred archive bytes;
it is not a canonical content hash that remains stable across separate tar creation runs.

The current implementation does not provide resume, compression, zip, incremental synchronization,
sparse-file preservation, permissions/ownership preservation, symlink or hard-link handling,
per-chunk checksums, coordination with writers that bypass the Host, or non-Linux shell support.
