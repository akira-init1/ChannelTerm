---
name: cterm-file-transfer
description: Upload or download files and directories with ChannelTerm (cterm), choosing a shared serial Session, SFTP, or legacy SCP. Use for copying programs, collecting logs, verifying transferred files, or handling transfer failures and cancellation. Not for firmware flashing, rsync synchronization, or GDB debugging.
---

# ChannelTerm File Transfer

Treat `cterm` as an alias for ChannelTerm. Choose an existing supported transfer path; do not implement a new payload protocol with terminal writes. Copying a program does not authorize executing, installing, or flashing it.

## Select the path

Honor the user's chosen protocol. Otherwise choose from the actual reachable services and target state:

| Available connection | Workflow | Ownership and result |
| --- | --- | --- |
| SSH server with SFTP subsystem | Prefer `channelterm sftp` | Dedicated file connection; CLI exit status and verification output |
| SSH server requiring legacy SCP, with POSIX shell and `scp` | `channelterm scp` | Dedicated file connection; CLI exit status and verification output |
| Existing shared serial Session at an idle Linux shell | `channelterm file send` / `receive` | Host-managed transfer lease; CLI result and optional MCP transfer events |
| Bare MCU, bootloader, or unknown foreground program | Establish its actual protocol first | Do not send Linux shell transfer commands |

SFTP/SCP do not reuse the terminal's SSH connection, create a Session, or publish Session file-transfer events. They do not require an open terminal. Do not call `terminal_wait_file_transfer` for these dedicated connections. Protocol selection is a workflow decision, not an automatic CLI fallback after failure.

## Resolve files and execution access

- Identify direction, source, destination, and the intended Session/Host or SSH account/address. A local path belongs to the machine running the CLI; a file on the user's PC is not automatically accessible to an agent elsewhere. `localhost` also refers to that execution environment.
- Initiating any of these transfers requires CLI execution access and access to the relevant local files. MCP has no client-file upload tool and no SFTP/SCP tools. If the agent cannot run the CLI in the right environment, give the user the exact command to run there. For serial transfers, `Ctrl+] f` in their attachment is another entry point; MCP can still observe the result.
- Check the installed subcommand's `--help` before relying on version-specific options. Quote local-shell arguments, use board/SSH paths in POSIX form, and do not put passwords or private-key contents in commands or reports.

## SFTP and legacy SCP

Use the real username, reachable address, port, and credential paths. The examples use placeholders:

```sh
channelterm sftp user@host.example put ./app.bin /tmp/app.bin --verify=sha256
channelterm sftp user@host.example get /tmp/log.txt ./log.txt --verify=sha256
channelterm sftp user@host.example put ./release /tmp/release --recursive --verify=sha256
channelterm scp user@host.example get /tmp/log.txt ./log.txt --verify=sha256
```

- Authentication follows ChannelTerm SSH rules: verify a first-use fingerprint through a trusted source; a changed/revoked key is an error, not permission to delete trust entries. Use `--port`, `--identity`, and `--known-hosts` for explicit settings. Password/passphrase/trust prompts require a real terminal; unattended use needs usable key authentication and established host trust. `ssh-agent`, `~/.ssh/config`, and keyboard-interactive authentication are not supported.
- Directories require `--recursive`. Parent directories must already exist; create missing directories only within the requested destination scope. SFTP `mkdir` creates one level. Remote paths have no glob or tilde expansion. An existing destination directory receives the source basename.
- Existing files fail by default, unlike serial transfers' suffix-based collision handling. Use `--overwrite` only when replacement is intended. For SFTP replacement, consider `--overwrite --safe-replace --verify=sha256`: it stages each file, verifies it, and then replaces it. Upload requires the server's POSIX rename extension. It is not whole-directory rollback and does not preserve previous file metadata. SCP does not support safe replacement.
- SHA-256 verification is opt-in; the default is `none`. SFTP verification needs no remote shell/hash utility, but uploads require reading the destination back. Verified SCP requires remote `sha256sum` and checks for it before copying. Do not claim content verification from the progress bar or SSH encryption alone.
- Observe the CLI until exit. Successful completion follows file close, requested verification, and replacement, not merely 100% payload progress. Report the saved path, exit status, and whether SHA-256 was requested and matched. A recursive transfer verifies files individually, not one stable directory digest.
- Ctrl+C cancels immediately without the serial attachment's confirmation menu. `--operation-timeout` bounds the whole operation, including prompts and verification; `--timeout` bounds setup only. Cancellation/failure can leave partial destinations or staging files. Inspect the reported paths before retrying, especially after a lost rename response. There is no resume or automatic reconnect. An independent SSH terminal remains open.

## Transfer through a shared serial Session

### Preconditions and start

The target must be at an idle POSIX-style Linux shell with an empty input line and `mkdir`, `stty`, `dd`, `wc`, `sha256sum`, and `sleep`. The CLI supports minimal `dd` without `iflag=fullblock` and performs the preflight. If it rejects unsubmitted input, have the user clear the shell line before retrying. Directories also require `tar` and temporary archive/staging space. Do not probe unknown MCU or bootloader input with shell commands.

Reuse the intended Session. Obtain its opaque `session_id` or short reference and its owning HTTP endpoint; a port or label is not a Session ID. If no Session exists, establish it using the actual serial settings, optionally following `cterm-serial-debug` when installed. Never guess communication settings. `file` neither opens the serial port nor starts a Host. Keep the attachment that owns a temporary Host running.

When observing through MCP, capture `next` from `terminal_session_events` before starting. This is an event cursor, not an output/activity cursor. Run the CLI where the local path and Host are accessible:

```sh
channelterm file send ./app.bin /tmp/app.bin --session SER-1
channelterm file receive /tmp/log.txt ./log.txt --session SER-1
channelterm file send ./build/release /tmp/release --session SER-1
```

Substitute the actual Session and paths; append `--endpoint` for a non-default Host. Directories are detected without a recursive flag. Omitting the send destination uses `/tmp/cterm/mcp-files/<local-basename>`; the human attachment menu uses `/tmp/cterm/user-files/<local-basename>`. Missing remote parent directories are created. Name collisions select free suffixed names instead of silently overwriting.

The CLI manages the transfer lease, renewal, and cleanup. Do not acquire a competing lease manually. Keep other command input out of the transfer; readers may continue with independent cursors, but ordinary writes are blocked by the lease.

### Completion, cancellation, and recovery

- For MCP observation, obtain the matching `transfer_id` from `FILE_TRANSFER_STARTED` or the active file-transfer lease. Call `terminal_wait_file_transfer` with that ID, the Session, and the captured event cursor. Do not accept another transfer's result. A wait timeout is not proof of failure; inspect the same transfer before waiting again or considering a restart.
- Report `completed`, `cancelled`, or `failed`, and confirm lease release before subsequent terminal writes. Raw-output `terminal_wait`, silence, or a prompt is not a completion signal. Use the completed result's `resolved_path` for subsequent operations; record `renamed` and the regular file's `sha256`. Directory transfer verifies archive bytes, not a stable directory-content hash. With CLI-only evidence, use its exit status, verification output, and saved path and identify that evidence explicitly.
- During an attachment transfer, Ctrl+C opens a local cancellation confirmation; only `y` or `Y` confirms. Leave that interaction to the user and observe the final result. Do not inject raw Ctrl+C into payload bytes or force-release the lease. Wait for cleanup before another operation.
- On failure, report the reason and last confirmed byte count. Transfers do not resume; resolve the cause before restarting. If the Session closed, re-establish the intended connection. If echo or line handling remains abnormal after cleanup, inspect `stty -a` at a confirmed idle board shell; restore `echo` for disabled echo, or use documented `stty sane` recovery for broader unrestored TTY state. Never send recovery commands while the transfer or another foreground program owns the terminal.

## Evidence and references

Report the chosen protocol, endpoint/Session, direction, actual saved path, verification result, and any partial-file cleanup still needed. Separate local-command evidence from real-device or real-server verification. Never report a successful transfer solely from a prepared command.

In a source checkout, use the [file-transfer guide](../../docs/getting-started/file-transfer.md), [CLI reference](../../docs/reference/cli.md), and [MCP transfer result contract](../../docs/reference/mcp-tools.md#terminal_wait_file_transfer). Installed bundles may omit repository docs; these instructions are self-contained, and installed CLI help and MCP schemas determine version-specific capabilities.
