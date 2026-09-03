# File Transfer over a Serial Session

ChannelTerm can stream a file through an existing shared Session without an AI client or a separately installed board-side transfer program. The board side remains an ordinary interactive Linux shell.

## Preconditions

- The remote shell is idle and accepts POSIX-style shell commands.
- The board provides `stty`, `dd` with `iflag=fullblock`, `wc`, and `sha256sum`. BusyBox or GNU userland may provide these commands.
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

When more than one shared Session is open, select one explicitly:

```powershell
channelterm file send firmware.bin /tmp/firmware.bin --session SER-1
channelterm file receive /tmp/log.txt ./log.txt --session SER-1
```

`--endpoint` selects a non-default Session Host endpoint. The file command never starts a host or opens a Serial Transport itself; it attaches to an already-open Session and leaves that Session open afterward.

`send` truncates the selected remote destination after the board-side capability check succeeds. `receive` writes and verifies a temporary local file first, then replaces an existing local destination. Confirm both paths before running the command. A non-default Host endpoint is a terminal-control boundary and must be trusted; the current HTTP Host has no ChannelTerm authentication layer. The feature does not change the loopback-only default listener.

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

For send, ChannelTerm hashes bytes as it reads the local file. After the final chunk, the board computes the stored size with `wc -c` and digest with `sha256sum`; both must match.

For receive, the board announces size and digest before streaming. ChannelTerm writes to a temporary file beside the requested destination, hashes the stream, and replaces the destination only after the digest matches. A failed receive removes the temporary file and retains an existing destination until verification has succeeded.

## Protocol and concurrency limits

The protocol is deliberately shell-oriented rather than a new board agent. Control markers are ASCII lines containing a random token; file payload bytes are raw and are never encoded or loaded as one complete in-memory value.

Session still guarantees byte serialization for each individual write. Above it, the Host holds one exclusive `file-transfer` lease for the complete CLI command. Other ChannelTerm writers fail immediately with a clear locked-session error, while the leased command uses its opaque owner capability for every write. The lease is released after success, failure, or cancellation. It cannot stop shell prompt hooks, unsolicited device output, or writers that bypass ChannelTerm, so keep the endpoint itself quiescent. Independent `attach`, `terminal_read`, and `terminal_wait` readers remain valid, although a receive transfer's raw file bytes are visible in their raw Session output and may not be suitable for terminal rendering.

If the command is interrupted while a send chunk is waiting for bytes, ChannelTerm makes a bounded best-effort attempt to pad that chunk so the shell can restore its saved TTY mode. If the Session or device disappears, reconnect locally and run `stty sane` on the board console if its terminal mode was not restored.

The first version does not provide resume, compression, directory recursion, sparse-file preservation, permissions/ownership preservation, symlink handling, per-chunk checksums, coordination with writers that bypass the Host, or non-Linux shell support.
