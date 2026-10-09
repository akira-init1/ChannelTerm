# SSH Terminal

SSH opens one interactive PTY shell and shares its ChannelTerm Session through the existing local
HTTP Host. The foreground `ssh` command owns that Host. This workflow does not add SSH exec,
SFTP, or a different file-transfer implementation.

## Prerequisites

Use a running SSH server on Linux, WSL2, or another SSH-capable host, its actual reachable address
and SSH port, a valid username, and an authorized unencrypted private key. ChannelTerm defaults to
`~/.ssh/id_ed25519` and falls back to `~/.ssh/id_rsa` if the first file is missing. It does not use
ssh-agent, encrypted keys, passwords, or `~/.ssh/config`.

Verify the server's host-key fingerprint through a trusted source and establish the corresponding
entry in `~/.ssh/known_hosts`, for example by connecting once with OpenSSH after verifying its
fingerprint. Never trust the result of a network scan alone. Unknown or changed keys are rejected.
Use `--known-hosts PATH` for a separate trust database; nondefault ports use OpenSSH's
`[host]:port` entry form. No host-key verification bypass is provided.

## Open and share a shell

With the default local sharing port free:

```bash
channelterm ssh user@192.168.1.50
```

Replace the example username and address with your server's actual values. For explicit settings:

```bash
channelterm ssh user@192.168.1.50 --port 2222 --identity /path/to/private-key --known-hosts /path/to/known_hosts
```

The command prints a Session reference (normally `SSH-1`) and the sharing endpoint, then displays
the server's shell output. At the remote prompt, enter:

```sh
uname -a
```

The output depends on the remote system. In a second terminal, join the same Session:

```bash
channelterm attach SSH-1
channelterm list --transport ssh
```

Run `list` before attaching or from a third terminal. Both clients see the same retained terminal
bytes and future output. Writes are serialized by the existing Session, but this does not
coordinate concurrent commands or remote shell state.

The foreground `ssh` terminal controls the remote PTY size. Initial dimensions come from its
output terminal, or its input terminal when output is redirected, with an 80-by-24 fallback when
size querying is unavailable. `--cols` and `--rows` override the initial dimensions; after the
owner's window changes, both dimensions follow the local terminal. Use `--term` for terminal type.
Secondary `attach` windows share output but never resize the remote PTY, even if they are larger
or smaller. There is no size-control handoff. With no local terminal, dimensions stay fixed.

Linux/macOS detect changes through `SIGWINCH`; Windows queries the visible console window every
250 ms. Resizing the owner from 36 rows by 100 columns to 43 by 132 should make remote `stty size`
print `43 132`. Vim and htop can redraw on the same running shell without reconnecting. Adjust
only the owner's window when checking this behavior; differently sized attachments may wrap or
clip the shared full-screen output. See the [CLI reference](../reference/cli.md#ssh) for precise
size validation and shutdown behavior.

The default setup timeout is ten seconds; `--timeout 30s` covers dialing, authentication, PTY,
and shell requests only.

## Host lifetime and other listeners

The default Host listens only on `127.0.0.1:37099/mcp` and requires the existing ChannelTerm Bearer
token. Clients with the token can control its Sessions through existing tools. Do not share it with
untrusted clients. SSH private keys stay in the opening process and never enter Session metadata.

If another Host already occupies the default address, SSH fails before opening a remote connection.
It does not add Sessions to that separate Host. Select another loopback port:

```bash
channelterm ssh user@192.168.1.50 --listen 127.0.0.1:37100
channelterm attach SSH-1 --endpoint http://127.0.0.1:37100/mcp
channelterm list --transport ssh --endpoint http://127.0.0.1:37100/mcp
```

Run these commands in separate terminals. Session references are local to a Host; different Hosts
can each have `SSH-1`. A ready, idle Host/shell can remain silent until input or remote output arrives;
the printed endpoint plus a successful `list` confirms readiness.

Ctrl+C cancels initial connection setup. Once attached, it is sent to the remote PTY. Use `Ctrl+] q`
to exit locally. Input EOF also closes the owning `ssh` command. In that terminal, exit closes the Host and all its Sessions; in another
`attach` terminal it detaches only that client. Remote shell exit/disconnection ends the Session,
causes an existing read-path diagnostic, and restores local terminal mode. Stopping the owner does
not leave a background SSH connection.

## Diagnostics and verification

If connection fails, record the exact error, `channelterm version`, SSH host and port, username,
key/trust-file paths (never their secret contents), and whether the error occurred before or after
the `SSH Session` status. Verify reachability and authentication with your existing SSH client.
An unknown or changed host key requires fingerprint verification before updating trust; it is not
fixed by disabling checks. An occupied sharing port is independent of the remote SSH port.

For a size mismatch, record the client OS/terminal, whether the resized window is the owning
`ssh` command or a secondary `attach`, any initial dimension flags, and both local and remote
`stty size` results on Linux/macOS. On Windows, record the visible console rows/columns instead.
Include the failing real-server test output when available; do not include private keys or tokens.

Automated tests use generated keys and loopback SSH servers, check rejected keys/PTY/shell requests,
cancel every setup stage, interrupt blocked I/O on close, and connect multiple Session clients.
They also cover server EOF, TCP reset, a silent network blackhole, explicit reconnection, blocked
output/event readers, owner input EOF, and worker cleanup after repeated connection lifetimes.

## Test this machine against itself

Linux and WSL2 can run both sides on `127.0.0.1`. With Go, Python 3, OpenSSH server/client tools,
Vim, and htop available, run as your regular user from the repository root:

```bash
bash scripts/test-ssh-local.sh
```

The script creates private temporary keys, a trust file derived from its own host public key,
and a dedicated loopback server on an available port. It does not modify your SSH configuration,
authorized keys, or system service. It runs the CLI in real Linux PTYs, uses a clean interactive
Bash shell, disables shell history, and removes the test server and temporary keys on exit.
Its test-only server disables directory-mode checking to allow credentials under a temporary
path; public-key authentication and ChannelTerm's host-key verification remain enabled.
See [building and testing](../development/building-and-testing.md#local-openssh-integration-tests)
for prerequisite overrides and the explicit test opt-in.

The automated real-server checks require matching initial and changed PTY dimensions, successive
and rapid owner changes, unchanged dimensions after secondary-window changes, Vim's own updated
`lines`/`columns` and edit/save, and htop redraw at the new height. They also exercise shell startup,
htop screen entry/exit, Ctrl+C, owner and observer termination, termios restoration, a server
connection process terminating, explicit reconnection, and a local relay dropping SSH packets.
They do not provide visual certification of every terminal emulator or interactive application.

## Current validation limits and recovery

- **Closure is delivered through the read path.** Pending output/event readers terminate when
  the owner or remote connection ends. There is no dedicated `SESSION_CLOSED` event or guarantee
  of a final structured notification before the Host stops. Secondary attachments report a read
  error and restore local terminal mode; normal remote shell exit currently uses that error path too.
- **Recovery is explicit.** A closed or reset connection is reaped by the existing Manager.
  Start `channelterm ssh` again to open a new shell and reattach to its printed reference.
  Commands, terminal state, and output cursors are not replayed into the new connection.
- **Silent network loss has no SSH liveness deadline.** `--timeout` bounds setup only. An established
  shell can wait until TCP reports failure or the owner exits. The relay test covers local escape
  while idle or after a small write; the Transport test separately verifies that Close releases a
  blocked socket write. These do not guarantee local escape remains responsive during a saturated
  write. On Linux/WSL2, terminate the owning process with SIGTERM if it cannot process local input.
- **Worker checks have a defined boundary.** Repeated-close tests inspect SSH, Session, Manager,
  and CLI worker stacks after caller-owned test input is closed. An arbitrary blocked `io.Reader`
  cannot be cancelled by context alone; it may retain its input pump until its owner releases it.
  Exiting the CLI process releases that process's remaining input worker.

Localhost testing verifies real OpenSSH and Linux PTY behavior, including on WSL2. It does not
verify Windows/macOS native console behavior, a LAN firewall, NAT, physical link loss, or another
SSH server implementation. Repeat the shared-shell workflow with the real remote address for
those deployment boundaries.
