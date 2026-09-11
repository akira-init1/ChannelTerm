# File Transfer over a Serial Session

ChannelTerm can stream a regular file or a directory through an existing shared Session without an AI client or a separately installed board-side transfer program. The board side remains an ordinary interactive Linux shell.

## Preconditions

- The remote shell is idle and accepts POSIX-style shell commands.
- The board provides `stty`, `dd` with `iflag=fullblock`, `wc`, and `sha256sum`. BusyBox or GNU userland may provide these commands. Directory transfer also requires `tar`; ChannelTerm checks `command -v tar` and does not install it.
- The serial settings already match the board. File transfer does not change baud rate, data bits, parity, stop bits, or flow control.
- Other Clients may keep reading with their independent Session cursors. ChannelTerm acquires a Host-side `file-transfer` lease that blocks its other writers during transfer; it cannot prevent unsolicited output or writers that bypass ChannelTerm.

First create or join a shared serial Session in one terminal:

```powershell
channelterm attach SER-COM8 --baud 115200
```

The default `attach` workflow starts the local loopback Session Host automatically when necessary. There is no need to start `channelterm mcp` manually or configure an AI client.

In another terminal, send a file to the board:

```powershell
channelterm file send firmware.bin /tmp/firmware.bin
```

In a third terminal, observe the shared transfer state without rendering raw file bytes:

```powershell
channelterm events SER-1
```

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
channelterm file send firmware.bin /tmp/firmware.bin --session SER-1
channelterm file receive /tmp/log.txt ./log.txt --session SER-1
```

`--endpoint` selects a non-default Session Host endpoint. The file command never starts a host or opens a Serial Transport itself; it attaches to an already-open Session and leaves that Session open afterward.

`send` and `receive` never silently overwrite their target. If the requested remote or local name exists, ChannelTerm chooses the first free sibling using `_1`, `_2`, and so on, preserving extensions including `backup.tar.gz` and `.env`. `receive` writes and verifies a temporary local file first, then installs it only when the chosen destination remains absent. Confirm both paths before running the command. A non-default Host endpoint is a terminal-control boundary and must be trusted; the current HTTP Host has no ChannelTerm authentication layer. The feature does not change the loopback-only default listener.

## Attach shortcut

While attached, press `Ctrl+] f` to open a local file-transfer menu. It uses the same shared Session, Application transfer implementation, 32 KiB streaming, SHA-256 verification, and Host-side `file-transfer` lease as the `channelterm file` command.

```text
Ctrl+] f
File transfer:
  s  Send PC -> Board
  r  Receive Board -> PC
  Esc  Cancel
Select:
```

Press `s` or `r` once to choose immediately; the selected key is echoed after `Select:` and ChannelTerm then shows the path prompts. No Enter is required for the menu choice. Invalid menu keys are ignored locally. For send, enter a required local path and accept or edit the remote default `/tmp/<local-basename>`. Board paths are always POSIX paths. For receive, enter a required board path and accept or edit the local default `./<remote-basename>`, relative to the directory in which `attach` was started. A regular file keeps the file protocol; a directory uses tar automatically. Local paths use the current platform's path rules. `Esc` exits only the file menu. The attachment has one local input dispatcher: normal attach `Ctrl+C` goes to the board, while menu/path input remains local. During an active transfer, `Ctrl+C` shows `Cancelling after the active transfer block...` and requests cancellation without detaching or sending Ctrl+C to the board. A regular file completes and acknowledges its already-started 32 KiB block before cancellation so the board restores its TTY. A directory transfer also stops reporting payload progress at the next block boundary, then safely drains or pads the already-started raw tar stream before releasing the lease; that cleanup can take longer than regular-file cancellation. ChannelTerm reports cancellation once and returns to the attachment after cleanup.

## Transfer flow

```text
Local file
   |
   | bounded 32 KiB reads
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

For each chunk, the shell saves its current TTY mode, enters raw/no-echo mode, transfers exactly one bounded block with `dd`, restores the saved mode, and emits an acknowledgement containing a random per-transfer token. The raw interval is limited to one chunk rather than the whole file. ChannelTerm reports progress only after a chunk is acknowledged and publishes it as `FILE_TRANSFER_PROGRESS` with structured confirmed byte counts, percent, and best-effort speed. It also publishes start, completion, and failure events without mixing them into raw Session output.

The local `file send` and `file receive` displays share one 20-cell ASCII progress formatter. For a non-empty regular file, it immediately renders a 0% frame before the first payload (for receive, after the board has announced its size), then refreshes in place with confirmed percentage, human-readable transferred/total sizes, speed, and ETA when the speed is usable. The 0% frame has no synthetic speed or ETA. A completed transfer first renders a 100% bar and then starts the SHA-256 summary on a new line. Cancellation and failure likewise finish the progress line before their status text, so an error never runs into a partially refreshed bar.

During a transfer, shared `attach` clients use the structured file-transfer events to gate their local raw-terminal presentation. The attachment that started the transfer retains its detailed progress display; other attachments receive concise status updates. All attachments continue advancing their own output cursors while suppressing the protocol shell commands, markers, and payload bytes, so the suppressed data cannot appear after the transfer finishes. Session raw output and independent MCP readers are unchanged.

For send, ChannelTerm hashes bytes as it reads the local file. After the final chunk, the board computes the stored size with `wc -c` and digest with `sha256sum`; both must match.

For receive, the board announces size and digest before streaming. ChannelTerm writes to a temporary file beside the requested destination, hashes the stream, and replaces the destination only after the digest matches. A failed receive removes the temporary file and retains an existing destination until verification has succeeded.

## Protocol and concurrency limits

The protocol is deliberately shell-oriented rather than a new board agent. Control markers are ASCII lines containing a random token; file payload bytes are raw and are never encoded or loaded as one complete in-memory value.

Session still guarantees byte serialization for each individual write. Above it, the Host holds one exclusive `file-transfer` lease for the complete CLI command. Other ChannelTerm writers fail immediately with a clear locked-session error, while the leased command uses its opaque owner capability for every write. The lease is released after success, failure, or cancellation. It cannot stop shell prompt hooks, unsolicited device output, or writers that bypass ChannelTerm, so keep the endpoint itself quiescent. Independent `attach`, `terminal_read`, and `terminal_wait` readers remain valid, although a receive transfer's raw file bytes are visible in their raw Session output and may not be suitable for terminal rendering.

If the command is interrupted while a send chunk is waiting for bytes, ChannelTerm makes a bounded best-effort attempt to pad that chunk so the shell can restore its saved TTY mode. If the Session or device disappears, reconnect locally and run `stty sane` on the board console if its terminal mode was not restored.

Directory tar streams are never written as complete temporary archives. PC-to-board uses Go `archive/tar` directly into Serial and target-side `tar` directly into a hidden same-parent staging directory; board-to-PC reverses those roles. A staged directory is renamed only after the full stream and extraction succeed. Local extraction rejects absolute or parent-traversal paths and rejects symlinks, hard links, devices, FIFOs, sockets, and other special entries. Directory progress counts tar-stream bytes, not a synthetic directory hash; successful completion guarantees tar completed, extraction succeeded, and staging was committed, but does not claim a directory-level SHA-256.

The first version does not provide resume, compression, zip, incremental synchronization, sparse-file preservation, permissions/ownership preservation, symlink or hard-link handling, per-chunk checksums, coordination with writers that bypass the Host, or non-Linux shell support.
