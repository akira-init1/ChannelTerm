# SSH Terminal

SSH opens one interactive PTY shell and shares its ChannelTerm Session through the existing local
HTTP Host. The foreground `ssh` command owns that Host. For dedicated SFTP or legacy SCP operations,
use the [file-transfer workflow](file-transfer.md#sftp-and-scp-over-ssh); those connections do not share
the terminal Session or insert protocol bytes into its history. Arbitrary SSH exec is not exposed.

## Prerequisites

Use a running SSH server on Linux, WSL2, or another SSH-capable host, its actual reachable address
and SSH port, a valid username, and either a login password or an authorized private
key. ChannelTerm tries existing `~/.ssh/id_ed25519` and `~/.ssh/id_rsa` keys in that order.
When neither exists, it can prompt for a password. If a key is rejected and the server offers
password authentication, it also prompts. Passwords are not echoed or saved. Existing invalid or
malformed keys still produce an error. Encrypted keys prompt for a passphrase after host verification;
it is hidden, never saved, and allows up to three attempts. Repeat `--identity PATH` (or `-i PATH`)
to supply an ordered list instead of default discovery. A missing or invalid selected key fails;
server rejection of a valid key proceeds to the next candidate. It does not use ssh-agent, keyboard-interactive
authentication, or `~/.ssh/config`.

On first connection, verify the displayed server fingerprint through a trusted source, then enter
`yes` to save its public key in `~/.ssh/known_hosts`. This is server identity, not a saved password
or generated client private key. Windows uses the user's `.ssh` directory just like Linux/macOS.
Existing OpenSSH trust records work without another prompt. Changed or revoked keys are rejected;
they are never automatically replaced. Never trust the result of a network scan alone.
Use `--known-hosts PATH` for a separate trust database; nondefault ports use OpenSSH's
`[host]:port` entry form. No host-key verification bypass is provided.
If a server offers both a raw key and a certificate, first-use confirmation can use its raw key
when no matching CA is trusted. A certificate-only server requires a trusted CA in `known_hosts`;
see the [host-trust rules](../reference/cli.md#ssh).

Prompts require a real terminal on stdin. Piped/scripted connections require an authorized key
and an already-trusted server. Cancel a prompt with Ctrl+C, Ctrl+D, or Ctrl+Z. The original console
mode is restored on success and failure. Authentication allows up to three password attempts,
subject to the server's own limits. Connection profiles and remembered passwords are not added.

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
and shell network requests only. Time answering trust/password prompts does not consume this
budget. Cancellation remains active while waiting for input.

For connections where network loss must be detected promptly, enable explicit limits:

```bash
channelterm ssh user@192.168.1.50 --keepalive-interval 10s --keepalive-count 3 --write-timeout 15s
```

An unanswered probe closes the connection after its 30-second response window; the first probe
starts ten seconds after setup. A shell write or resize taking more than 15 seconds also closes
the connection, including when the remote process stops consuming input. Both features are off
by default. No input is replayed, and reconnection still requires a new command.

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
- **Silent network loss needs explicit limits.** `--timeout` bounds setup only. Without keepalive,
  a shell can wait until TCP reports failure or the owner exits. The owner processes local escapes
  independently of its ordered remote write worker: `Ctrl+] q` closes the owned connection and joins
  that worker even when a write is blocked, without requiring `--write-timeout`. Exit discards queued,
  unsent input; it is never replayed. The queue holds at most 256 input batches (up to 4 KiB each);
  overflow reports an error and closes the connection. Use SFTP for bulk data rather than huge pastes.
  Heartbeat replies alone do not mean the remote shell is consuming input. Ordinary secondary
  attachments retain their existing synchronous write behavior and cannot close the owner's Session.
- **Worker checks have a defined boundary.** Repeated-close tests inspect SSH, Session, Manager,
  and CLI worker stacks after caller-owned test input is closed. An arbitrary blocked `io.Reader`
  cannot be cancelled by context alone; it may retain its input pump until its owner releases it.
  Exiting the CLI process releases that process's remaining input worker.

Localhost testing verifies real OpenSSH and Linux PTY behavior, including on WSL2. It does not
verify Windows/macOS native console behavior, a LAN firewall, NAT, physical link loss, or another
SSH server implementation. Repeat the shared-shell workflow with the real remote address for
those deployment boundaries.
