---
name: cterm-ssh-debug
description: Open, share, and diagnose ChannelTerm (cterm) SSH terminal Sessions. Use for remote Linux or embedded-Linux shell commands, logs, interactive programs, SSH authentication/connectivity problems, and human/AI shared SSH access. Not for serial-port setup, GDB/KGDB debugging, SSH tunneling, or bulk file transfer.
---

# ChannelTerm SSH Debug

Treat `cterm` as an alias for ChannelTerm. This skill handles terminal diagnosis through an SSH PTY shell. It does not add debugger protocols or arbitrary SSH exec support. For uploads/downloads, use `cterm-file-transfer` when installed; dedicated SFTP/SCP connections are independent of the shared shell.

For C/C++ source-level debugging, hand off the selected Session, Host, and target to `cterm-gdb-debug` when installed, or the [GDB guide](../../docs/getting-started/gdb-debugging.md#debug-a-linux-server-over-ssh-or-tcp). That workflow distinguishes interactive remote GDB from external native-SSH/TCP gdbserver connections. ChannelTerm's serial RSP bridge cannot take over an SSH Session.

## Reuse or establish the Session

1. If connected to the intended ChannelTerm MCP Host, call `terminal_list_sessions` and reuse the intended open SSH Session. Check its transport and target rather than guessing from its position in the list. Record its opaque ID, short reference such as `SSH-1`, and Host endpoint; references are local to each Host. A username/hostname or label is not a Session ID.
2. If a new connection is needed, obtain the actual username, reachable host, SSH port, and intended authentication method from the task or existing configuration. Do not substitute a guessed device or credentials. ChannelTerm has no `terminal_open_ssh` MCP tool: a new SSH Session is opened by the local `channelterm ssh` command.
3. Verify local CLI access and that the execution environment can reach the SSH server. Password, encrypted-key passphrase, and first-use host-trust prompts require a real terminal; unattended use requires usable key authentication and established trust. If the agent lacks that environment, give the user the command to run, then use their shared Session through the configured MCP connection.
4. Start the owner in a persistent terminal with live stdin. Do not pipe an empty input stream or run it in a short-lived command runner that will close stdin after startup: EOF closes the owning Host. Check `channelterm ssh --help` for installed-version options. Substitute actual values in these examples:

   ```sh
   channelterm ssh user@host.example
   channelterm ssh user@host.example --port 2222 --identity /path/to/private-key --known-hosts /path/to/known_hosts
   ```

5. The foreground command creates its own loopback HTTP Host. It cannot add an SSH Session to a separately running Host. If the default sharing address is occupied, leave the existing Host running and use another available loopback port:

   ```sh
   channelterm ssh user@host.example --listen 127.0.0.1:37100
   ```

   In another terminal, use the printed reference and endpoint:

   ```sh
   channelterm list --transport ssh --endpoint http://127.0.0.1:37100/mcp
   channelterm attach SSH-1 --endpoint http://127.0.0.1:37100/mcp
   ```

   The remote SSH port (`--port`) and local sharing port (`--listen`) are different. A configured MCP client must reach this Host, not an unrelated default Host. `localhost` refers to the client's execution environment. Keep the Host's Bearer credential private; starting a Host does not configure an agent's MCP connection automatically.
6. Confirm readiness from the printed Session/endpoint and a successful Session listing. An idle shell may be silent; do not send probe commands just because no new bytes arrive.

## Authenticate without weakening trust

- On first use, verify the displayed fingerprint through a trusted source before accepting it. Unknown-host confirmation records a public key in `known_hosts`, not a password. Changed or revoked keys are rejected; do not delete trust records or bypass verification to make a connection succeed.
- ChannelTerm tries existing `~/.ssh/id_ed25519` and `~/.ssh/id_rsa` keys by default. Repeated `--identity` options select an explicit candidate list. Invalid/missing selected keys fail; a rejected valid key may fall back to a password if the server offers it and the local input supports prompts. Passwords and passphrases are neither echoed nor saved.
- It does not use `ssh-agent`, `~/.ssh/config`, or keyboard-interactive authentication. Translate known address/port/identity settings into supported explicit options; do not assume aliases, jump hosts, or agent-held keys will work. Do not put secrets in arguments, terminal commands, or reports.

## Observe and operate the shell

- Use `terminal_read` without a cursor for a recent snapshot, then retain `next` and call `terminal_wait` for subsequent output. `dropped: true` means earlier data was lost; do not invent it. Activity cursors from `terminal_read_activity` / `terminal_wait_activity` are separate from output cursors.
- Determine whether the Session is at an idle shell or an interactive/foreground program before writing. At a known idle Bash prompt, use `terminal_exec` for one non-interactive command and its structured result. This operates through the existing shell, not an SSH exec channel. Each call uses a fresh child: combine dependent steps in one command; do not assume `cd` or exported variables persist between calls.
- Use `terminal_write` for interactive programs, keys, or other shell input; observe output before choosing another action. Do not send a `terminal_exec` wrapper into Vim, a password prompt, an application REPL, or a non-Bash shell. A running interactive program needs its own documented exit sequence.
- Other clients may share the Session. Complete writes are serialized, but ordinary commands do not acquire semantic shell ownership. Reuse the known Session state and avoid concurrent command sequences that assume exclusive input.
- Limit diagnosis to the user's scope. Reboots, persistent configuration changes, service restarts, and destructive commands need clear authorization; a request to inspect logs does not authorize them. Do not perform an unrequested mutation merely to test connectivity.
- The owning terminal controls PTY resizing; secondary attachments do not. For display problems, compare owner dimensions and remote `stty size` at an idle POSIX shell. Initial `--cols`/`--rows` do not create a resizing role for another attachment.

## Lifetime, interruption, and recovery

- Ctrl+C cancels connection setup; after attachment it goes to the remote PTY. Use `Ctrl+] q` to exit the local attachment. In the owner this stops its Host and closes its Sessions; in a secondary attachment it detaches only that client. Keep the owner running while other clients need the Session.
- `terminal_session_detach` records a departure event but does not stop client I/O or release the connection. `terminal_close` closes the shared Session for everyone. Do not use either as a substitute for understanding ownership, and do not close a shared Session merely because diagnosis finished.
- Remote shell exit, disconnect, or owner input EOF ends the owning command. Reads can fail without a dedicated `SESSION_CLOSED` event. There is no automatic reconnect or replay of commands/cursors. After loss, establish a new Session explicitly and inspect state before retrying a command whose effect is uncertain.
- `--timeout` bounds connection setup only. For a task needing liveness limits, consider `--keepalive-interval 10s --keepalive-count 3` and `--write-timeout 15s`, choosing values for the actual network. These are opt-in; expiration can close the Session. A heartbeat reply alone does not prove the shell is consuming input. The owner's `Ctrl+] q` remains available during blocked writes.
- On errors, distinguish remote DNS/TCP/authentication/PTY setup from a busy local sharing address. Host-key errors require trust investigation; missing or invalid keys require correcting the selected path/format. Never respond by disabling verification or replacing another Host. Record the exact diagnostic and the stage where it failed before retrying.

## Evidence and references

Report the real host/port, Session reference and owning endpoint, actions and observed results, and whether the owner remains running. Include CLI version and relevant errors for failures, without passwords, tokens, or key contents. Distinguish prepared commands, local checks, and real-server verification. No GDB breakpoints or source-level debugging are provided by this workflow.

In a source checkout, consult the [SSH terminal guide](../../docs/getting-started/ssh-terminal.md), [CLI contract](../../docs/reference/cli.md#ssh), and [MCP tools](../../docs/reference/mcp-tools.md). Installed bundles may omit repository docs; use installed CLI help and available MCP schemas for version-specific details. If the transfer skill is absent, dedicated `sftp --help` / `scp --help` and the [file-transfer guide](../../docs/getting-started/file-transfer.md) describe the existing file operations.
