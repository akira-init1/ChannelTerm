---
name: cterm-gdb-debug
description: Debug Linux user-space programs with GDB and ChannelTerm (cterm). Use for C/C++ breakpoints, stepping, stacks, variables, threads, or crashes on Linux boards and SSH servers, including gdbserver preflight, serial RSP bridging, and network debugging. Not for bare-metal, JTAG/SWD, KGDB, firmware flashing, or ordinary terminal diagnosis.
---

# ChannelTerm GDB Debug

Treat `cterm` as an alias for ChannelTerm. This workflow is model-neutral: the agent needs the relevant MCP tools and, for an external local GDB, a persistent process/terminal execution tool. Installing this skill supplies instructions, not binaries, credentials, an MCP connection, or an execution tool. Use the installed tool schemas and CLI help for exact arguments.

## Select the debugging path

Establish where the ChannelTerm Host, GDB, symbols, gdbserver, and target process run. Preserve the user's selected path when it meets the prerequisites.

| Situation | Path |
| --- | --- |
| Linux board reached through an existing serial Session | External serial RSP bridge; independently operate a target-compatible GDB. |
| Linux server reached through a ChannelTerm SSH shell, with GDB on that server | Run interactive GDB inside that shared shell. gdbserver is unnecessary for this path. |
| GDB on the workstation; program on an SSH-accessible Linux server or board | Native OpenSSH pipe to gdbserver, or a protected TCP connection. ChannelTerm can prepare/observe the server shell; the RSP connection is separate. |
| ChannelTerm Host, GDB, gdbserver, ELF, and Session shell in the same native Linux environment | Managed MCP `debug_start` / `debug_command` / `debug_events` / `debug_stop`. |

`debug_bridge_start` accepts only an open **serial** Session. It cannot bridge an SSH Session or connect to a remote TCP gdbserver. `debug_start` is not a general remote-SSH backend: another machine, container/PID namespace, or Windows/WSL split does not satisfy its local-environment requirement. Do not invent `debug_bridge_command` or `terminal_open_ssh` tools.

Reuse the intended Session from `terminal_list_sessions`; record its opaque ID, reference, transport, and owning MCP endpoint. Keep its owning Host alive. Use `cterm-serial-debug` or `cterm-ssh-debug` when available to establish a missing connection. Otherwise use installed CLI help; `channelterm ssh USER@HOST` opens an SSH shell and its own sharing Host, which the MCP client must reach. It cannot add an SSH Session to an existing Host. Known serial settings and the actual SSH host/user/authentication are prerequisites, not values to guess.

## Check the target and symbols before takeover

1. Observe the terminal first. Use `terminal_exec` only at an idle Bash prompt; use `terminal_write` and reads for another known shell or interactive GDB. Never send a shell wrapper into GDB or an RSP stream. Each `terminal_exec` uses a fresh child, so combine dependent directory/environment steps in one invocation.
2. On the **target**, check the OS, CPU, and executable tools. For a gdbserver path, run this shell preflight and inspect both the version output and exit status:

   ```sh
   uname -s
   uname -m
   if command -v gdbserver >/dev/null 2>&1; then
       gdbserver --version
   else
       printf '%s\n' 'gdbserver is not on PATH'
   fi
   ```

   A path alone is insufficient: execution can fail because of ABI, loader, library, or permission problems. Check a known SDK-specific absolute path if appropriate. If absent, inspect `/etc/os-release` and the board's package/SDK facilities. Use an authorized package installation or a compatible vendor/cross-built gdbserver, transfer it through the supported file workflow, then repeat the version check. Do not assume every board uses apt, copy the workstation's binary across architectures, or start a bridge before this succeeds. If installation is outside the task's authorization, report the missing prerequisite; remote native GDB remains an option if present.
3. Check `gdb --version` **where GDB will execute**. Select the target SDK's GDB or a multiarch build that supports the target architecture/ABI. Server-side native GDB requires only GDB there; external paths require gdbserver on the target and GDB on the debugger machine.
4. Identify launch versus attach, the intended executable/PID, arguments, working directory, environment, and tracing permissions. Attaching or stopping a service affects its execution; respect the user's existing authorization and service constraints. Do not stop a service merely to obtain a PID, guess among matching processes, or weaken system-wide ptrace policy. Keep inspection expressions free of assignments and function calls unless changing program state is intended.
5. Use symbols from the exact deployed build, including matching shared libraries/sysroot. For an authorized debug build, retain symbols, for example `g++ -g -O0 -fno-omit-frame-pointer -o demo demo.cpp`; use the project's build system for a real project. A freshly rebuilt binary is not a symbol substitute for an older running service. Use build IDs or identical artifact hashes where applicable. Configure GDB `file`, `set sysroot`, and `set substitute-path` for the actual local symbol/library/source paths. Use `cterm-file-transfer` if installed, or `channelterm file`, SFTP/SCP help, for transfers; complete transfers before serial takeover.

## Run GDB in the shared SSH shell

Use this path when the target server has native GDB and shared terminal visibility is useful.

1. Reuse the SSH Session, or open `channelterm ssh user@host.example` in a persistent terminal with live stdin. Verify host trust and authentication; keep the owner alive. Connect MCP to its printed endpoint and list Sessions to confirm ownership.
2. At the remote idle shell, verify `command -v gdb` and `gdb --version`, then start `gdb -q --args /srv/debug/demo` using interactive terminal input. Substitute real paths/arguments. For an authorized existing process, use `gdb -q /path/to/matching/executable -p PID` instead; it stops the process on attachment.
3. Wait for `(gdb)`, send `set pagination off`, then use `break main` and `run` for a newly launched program. For an attached program, inspect its current stop and use `continue` when intended. Read the response after each command; do not call `terminal_exec` while GDB owns the PTY.
4. Inspect a confirmed stop with `bt`, `info threads`, `frame`, `info locals`, or `print EXPRESSION`; use `next`, `step`, and `finish` as needed. Send Ctrl+C through terminal input only when this Session is known to be running the intended GDB/inferior. Shared writes are serialized but do not coordinate competing debugger commands.
5. To preserve a live inferior, issue `detach` before `quit`; after normal inferior exit, `quit` is enough. Do not accept an unexpected kill confirmation just to close GDB. Confirm the shell prompt returned and preserve the shared SSH Session. Disconnecting the SSH owner closes its Host/Sessions and does not prove the target process survived.

This is ordinary interactive GDB carried by ChannelTerm's SSH terminal. It does not gain managed MI events, debug leases, or automatic `debug_bridge_report` history.

## Use workstation GDB over SSH or TCP

Use a persistent local GDB process and actual matching symbols. If the agent has MCP access but no local process execution, use remote interactive GDB above or hand the local commands to the operator; do not claim external GDB has started.

### Prefer a native SSH pipe

After target preflight, establish host trust and usable authentication for the **native `ssh` client**. ChannelTerm SSH settings do not automatically configure it. This example assumes trusted, noninteractive key/agent authentication; `BatchMode=yes` fails instead of prompting inside the RSP pipe:

```text
gdb -q /work/symbols/demo
(gdb) target remote | ssh -T -o BatchMode=yes user@host.example gdbserver --once stdio /srv/debug/demo
(gdb) break main
(gdb) continue
(gdb) bt
```

For an authorized existing process, replace the server portion with `gdbserver --once --attach stdio PID`, keeping the matching local ELF. Apply the actual SSH port (`ssh -p`), identity, working directory, and safely quoted program arguments. `-T` avoids PTY transformations; never substitute `channelterm ssh` for this command. Keep remote startup output off the RSP stdout stream. Programs needing interactive stdin require another I/O arrangement; stdio gdbserver does not provide a normal interactive application terminal.

### TCP when required

GNU gdbserver has no built-in authentication; its `HOST:PORT` host component may be ignored. Writing `127.0.0.1:2345` does **not** prove a loopback-only listener. Prefer the SSH pipe unless an authorized restricted network/firewall setup already protects the target port. Do not expose it to a public network.

1. Select an available target port and arrange access restrictions before starting `gdbserver --once :2345 /srv/debug/demo` on the target. Use `--attach :2345 PID` instead of the executable for attach. Keep the foreground server process alive; use interactive terminal input, not a short `terminal_exec` timeout. Inspect startup diagnostics and actual listening addresses with `ss -ltnp` or the target's equivalent.
2. For an SSH tunnel, run native OpenSSH in another persistent local process:

   ```sh
   ssh -N -T -o ExitOnForwardFailure=yes -L 127.0.0.1:12345:127.0.0.1:2345 user@host.example
   ```

   Then use `(gdb) target remote 127.0.0.1:12345`. The tunnel protects this connection but does not restrict the gdbserver listener on the server. Alternatively, connect to `(gdb) target remote TARGET_IP:2345` only on the verified restricted network. These are ordinary RSP endpoints, not ChannelTerm TLS bridge addresses or MCP HTTP ports.
3. Wait for a stop after connecting, then use `break` and `continue`. `target remote` starts/attaches through gdbserver; do not use `run` or `load` as though it were a native launch. `extended-remote` is a separate GDB mode; use it only when the requested workflow needs it and the installed server supports it.
4. Detach a surviving inferior before quitting GDB, verify target state and server exit, and stop only the tunnel/server processes created for this operation. Connection refusal calls for checking startup, route, port, and forwarding diagnostics. A broken connection is not proof of detach or permission to blindly reattach/relaunch. Do not kill unrelated gdbserver processes or restart the application automatically.

These connections bypass ChannelTerm's serial bridge. Do not create a dummy bridge to report them. Summarize observations directly; commands run in the shared SSH terminal remain visible there, but external GDB commands are not automatically copied into it.

## Use the external serial RSP bridge

1. Finish target/gdbserver and symbol preflight at the intended serial shell. Save the exact pre-debug `stty -g` value outside the target. Prepare raw RSP using the known shell; a launch example is:

   ```sh
   ct_saved=$(stty -g); stty raw -echo; gdbserver --once stdio /tmp/demo 2>/tmp/ct-gdbserver.log; stty "$ct_saved"
   ```

   Adapt paths and protect diagnostic output as needed. Ensure inferior I/O and other console producers cannot corrupt RSP; attached processes retaining the serial TTY are unsuitable. Silence is not readiness evidence. After takeover, do not send shell commands or terminal Ctrl+C into the stream.
2. Generate a secret random management `owner` (32–128 characters) and unique `request_id`. Call `debug_bridge_start` with the actual `session_id`, those values, and `acknowledge_unverified_target: true` only after preparation. Exact retries reuse all arguments. Keep `status.bridge_id` and save only the returned `connection` object to a private JSON file readable by the GDB-side helper. Restrict permissions/ACLs; never print its token or the owner in chat, commands, reports, or source control.
3. Use the actual returned address; default ports rotate through 37100–37999 with dynamic fallback. MCP HTTP is separate. Loopback is relative to each machine/namespace; a non-loopback bridge needs a specific numeric interface and `allowed_peer`. Do not use wildcard binds or weaken TLS pinning to fix connectivity.
4. Verify `channelterm rsp-client --help` on the GDB machine, then connect from its target-compatible GDB:

   ```text
   (gdb) file /work/symbols/demo
   (gdb) target remote | channelterm rsp-client --connection /private/bridge.json
   (gdb) break main
   (gdb) continue
   ```

   A plain `target remote HOST:PORT` cannot authenticate this TLS endpoint. Only one data client can consume a bridge; reconnect cannot take it over.
5. Current matching `rsp-client` versions automatically renew while connected, including idle breakpoint waits; inspect `auto_keepalive`. Independently call `debug_bridge_renew` every 30 seconds before connecting, after disconnect while arranging recovery, or with a legacy helper. Status and reports do not renew. Respect the 60-second grace and absolute 24-hour limit.
6. Optionally call `debug_bridge_report` with `bridge_id`, `owner`, increasing `sequence`, `kind` (`command`, `result`, `state`), and actual observed `text` (up to 4096 UTF-8 bytes). Omit secrets, raw RSP, and sensitive values. Report failures must not interrupt GDB or renewal; retry only the latest identical report, never reexecute a GDB command to resend it. Current output labels these as **Codex reports** even for another calling model; this is not verified model identity or a complete audit.
7. Issue GDB `detach` before `quit` when preserving a live program. Disconnection retains the lease and leaves shell state unknown. Only after independently confirming the target shell has returned, call `debug_bridge_restore` with the saved exact `tty_state` and `confirm_shell_ready: true`. Require `restored` / `shell_verified` before ordinary terminal writes. An exit can leave serial gdbserver awaiting EOF; use a separate authorized management path when needed.
8. If recovery cannot be established or the snapshot is missing, call `debug_bridge_close` to end bridge and shared serial Session ownership. It does not detach or kill the target. Failed/timed-out restore and lease expiry also close the Session. Never force-release the debug lease, guess `stty sane`, or use speculative shell probes while RSP may still own the line. Remove private connection files after cleanup.

## Use managed native Linux MCP debugging

Use this path only when the Host and shell share the same Linux filesystem/PID environment and native amd64/arm64 ELF. Verify GDB, gdbserver, Bash, `setsid`, and required shell utilities there. The backend supports one inferior, multiple threads, and all-stop execution; attach rejects controlling TTYs, pipes, sockets, and unsafe character devices. A network server with open sockets therefore needs an external path for attach.

1. Call `debug_start` with `session_id`, secret random `owner`, unique `request_id`, absolute local `elf`, and launch `args` or attach `pid`. Do not prepare your own gdbserver or raw TTY for this managed mode.
2. Keep `status.debug_id`. Consume `debug_events` from `cursor: 0`, retaining each returned `next`. Use supported `debug_command` actions such as `break_insert`, `continue`, `next`, `stack`, and `evaluate`; discover exact fields from the schema. Give each new command a unique request ID and retry uncertain responses with identical arguments/ID.
3. Execution acceptance is not a stop: wait for a stopped event before inspecting. Poll events or call `debug_command` with `action: "renew"` at least every 30 seconds, also during long runs. Debug and terminal cursors are independent. `evaluate` can mutate values even though function calls are disabled.
4. Interrupt through `debug_command` with `action: "interrupt"`. End through `debug_stop`; the default detaches, while `terminate: true` is only for an authorized launched inferior. Require successful shell restoration before resuming terminal work. Recovery failure closes the Session; reconnect and inspect rather than replaying uncertain actions.

## Report and reference

Report the selected path, actual machines/architecture, verified tool versions, Session/Host when used, executable/symbol match, commands and observed stops/results, and final process/connection state. Separate prepared instructions, local checks, and real-board/server evidence. On failure, include the failing stage, relevant error and nonsecret identifiers/cursors; do not invent target recovery.

In a source checkout, see the [GDB workflow](../../docs/getting-started/gdb-debugging.md), [MCP contracts](../../docs/reference/mcp-tools.md#debug-tools), and [SSH workflow](../../docs/getting-started/ssh-terminal.md). Installed skill bundles may omit these docs; the procedures above remain usable with installed help and schemas. For external connections, consult the [GNU gdbserver manual](https://sourceware.org/gdb/current/onlinedocs/gdb.html/Server.html) and [OpenSSH manual](https://man.openbsd.org/ssh.1) for version-specific behavior.
