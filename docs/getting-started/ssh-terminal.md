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

PTY dimensions initially come from the owner's local output terminal, with an 80-by-24 fallback.
Use `--cols` and `--rows` to set them explicitly and `--term` to set the terminal type. Resizing an
attachment does not resize the remote PTY in this release. The default setup timeout is ten seconds;
`--timeout 30s` covers dialing, authentication, PTY, and shell requests only.

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
to exit locally. In the owning `ssh` terminal this closes the Host and all its Sessions; in another
`attach` terminal it detaches only that client. Remote shell exit/disconnection ends the Session,
causes an existing read-path diagnostic, and restores local terminal mode. Stopping the owner does
not leave a background SSH connection.

## Diagnostics and verification

If connection fails, record the exact error, `channelterm version`, SSH host and port, username,
key/trust-file paths (never their secret contents), and whether the error occurred before or after
the `SSH Session` status. Verify reachability and authentication with your existing SSH client.
An unknown or changed host key requires fingerprint verification before updating trust; it is not
fixed by disabling checks. An occupied sharing port is independent of the remote SSH port.

Automated tests use generated keys and loopback SSH servers, check rejected keys/PTY/shell requests,
cancel every setup stage, interrupt blocked I/O on close, and connect multiple Session clients.
A native Linux/WSL2 acceptance check additionally requires your actual SSH server and credentials:
open the first command, run `uname -a`, attach from a second terminal, verify matching output,
detach the second client, then exit the owner and confirm the Host listener has stopped.
