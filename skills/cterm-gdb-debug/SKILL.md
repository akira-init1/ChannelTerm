---
name: cterm-gdb-debug
description: Debug Linux user-space programs with GDB and ChannelTerm (cterm). Use for C/C++ breakpoints, stepping, stacks, variables, threads, or crashes on Linux boards and SSH servers, including gdbserver preflight, serial RSP bridging, and network debugging. Not for bare-metal, JTAG/SWD, KGDB, firmware flashing, or ordinary terminal diagnosis.
---

# ChannelTerm GDB Debug

Treat `cterm` as an alias for ChannelTerm. Use available MCP schemas and CLI help for exact arguments. External local GDB also needs persistent process execution; with MCP alone, use remote interactive GDB or provide commands to the operator.

## Select the debugging path

Locate the Host, GDB, symbols, and target. Preserve the user's selected path when its prerequisites hold.

| Situation | Path |
| --- | --- |
| Linux board through serial | External serial RSP bridge with independently operated GDB. |
| GDB installed on an SSH server | Interactive GDB in the shared SSH shell; no gdbserver needed. |
| Workstation GDB, remote Linux program | Native SSH pipe to gdbserver, or protected TCP; RSP travels separately from ChannelTerm. |
| Host, tools, ELF, and shell in one native Linux environment | Managed MCP GDB/MI. |

`debug_bridge_start` accepts only an open **serial** Session. `debug_start` requires the same Linux filesystem/PID environment; another machine or Windows/WSL split does not qualify.

Reuse the intended Session from `terminal_list_sessions`; retain its opaque ID, transport, and owning endpoint. Keep its Host alive. For setup, use `cterm-serial-debug` / `cterm-ssh-debug` when installed or CLI help. `channelterm ssh USER@HOST` creates its own sharing Host; connect MCP to its printed endpoint. No `terminal_open_ssh` tool exists. Obtain actual serial settings or SSH credentials without guessing.

## Check the target and symbols before takeover

1. Observe before writing. Reserve `terminal_exec` for idle Bash; each call uses a fresh child. Use `terminal_write` and reads for interactive GDB or another known shell. Never inject shell commands into RSP.
   Prefer absolute tool paths over changing the shared shell's `PATH` just for debugging. Raw setup commands follow shell history rules; `terminal_exec` is not a global history filter and cannot persist environment changes. Do not clear user history to hide setup. The bridge's automatic restore probe handles its own Bash history entry.
2. Check tools on the **target**, including successful execution rather than presence alone:

   ```sh
   uname -s
   uname -m
   if command -v gdbserver >/dev/null 2>&1; then
       gdbserver --version
   else
       printf '%s\n' 'gdbserver is not on PATH'
   fi
   ```

   Inspect output and exit status for loader, ABI, library, or permission errors. If missing, check the SDK path or target OS/package facilities; install a compatible gdbserver within existing authorization, then recheck. Do not assume apt or copy a workstation binary across architectures. For server-side native debugging, check `gdb --version` instead of gdbserver.
3. Verify GDB where it will run, target architecture/ABI support, and symbols/libraries from the exact deployed build. Configure `file`, `set sysroot`, and source mappings as needed. Use `cterm-file-transfer` when installed or supported CLI file commands; finish transfers before serial takeover.
4. Confirm launch versus attach, executable/PID, arguments, working directory, environment, and tracing permission. Respect service interruption constraints; do not weaken system-wide ptrace policy. Inspection expressions should avoid assignments/function calls unless mutation is intended.

## Run GDB in the shared SSH shell

1. At the remote shell, start `gdb -q --args /srv/debug/demo`, or `gdb -q /path/to/matching/executable -p PID` for authorized attach. Substitute actual paths/arguments.
2. Wait for `(gdb)`, disable pagination, then set breakpoints and `run` for launch or inspect/`continue` for attach. Use terminal writes and read each response; inspect only after a confirmed stop. Other writers must not issue conflicting commands.
3. To preserve a live inferior, `detach` before `quit`; after normal exit, just quit. Confirm the shell returned and keep the Session owner alive. This path uses ordinary terminal output, without managed MI events or debug leases.

## Use workstation GDB over SSH or TCP

Prefer a native SSH pipe. Establish host trust and noninteractive authentication for native `ssh` separately from ChannelTerm:

```text
gdb -q /work/symbols/demo
(gdb) target remote | ssh -T -o BatchMode=yes user@host.example gdbserver --once stdio /srv/debug/demo
```

For attach, replace the server invocation with `gdbserver --once --attach stdio PID`. Adapt SSH options, working directory, and quoted arguments. Keep `-T`, startup output off protocol stdout, and credentials private. Do not substitute `channelterm ssh`; it provides a PTY shell. Applications needing interactive stdin require separate I/O.

For TCP, restrict access **before** starting `gdbserver --once :2345 /srv/debug/demo` in a persistent target terminal. For attach, use `gdbserver --once --attach :2345 PID`. Verify the actual listener with `ss -ltnp` or equivalent: gdbserver has no authentication, and `127.0.0.1:2345` does not guarantee loopback binding. Never expose it publicly. Connect through a protected network with `target remote TARGET_IP:2345`, or keep this native SSH tunnel running separately:

```sh
ssh -N -T -o ExitOnForwardFailure=yes -L 127.0.0.1:12345:127.0.0.1:2345 user@host.example
```

Then use `target remote 127.0.0.1:12345`. The tunnel does not restrict the target listener. Substitute available ports; none is an MCP or ChannelTerm TLS endpoint.

After a confirmed stop, set breakpoints and `continue`; `target remote` does not use native `run` or `load`. Detach surviving inferiors before quitting, verify target/server state, then stop only owned helpers. After failure, inspect diagnostics and target state before retrying. These external paths have no ChannelTerm bridge reports.

## Use the external serial RSP bridge

1. Finish preflight and save the exact pre-debug `stty -g` value outside the target. Prepare the serial shell for RSP; adapt this launch example:

   ```sh
   ct_saved=$(stty -g); stty raw -echo; gdbserver --once stdio /tmp/demo 2>/tmp/ct-gdbserver.log; stty "$ct_saved"
   ```

   Isolate inferior I/O and other console output from RSP; attached processes retaining this TTY are unsuitable. Silence is not readiness. Do not send terminal Ctrl+C or shell input during takeover.
2. Call `debug_bridge_start` with `session_id`, a secret random `owner`, unique `request_id`, and `acknowledge_unverified_target: true` after preparation. Exact retries reuse all arguments. Retain `status.bridge_id`; save only `connection` in a private JSON file with restrictive permissions/ACLs. Never disclose tokens or owner capabilities.
3. Use the returned address. Non-loopback binds require a specific interface and `allowed_peer`; preserve authentication and TLS pinning. Verify the GDB-side helper's help, load matching symbols, then connect:

   ```text
   (gdb) file /work/symbols/demo
   (gdb) target remote | channelterm rsp-client --connection /private/bridge.json
   ```

   Plain `target remote HOST:PORT` cannot authenticate this endpoint; reconnect cannot take over a consumed bridge.
4. Current `rsp-client` maintains leases while connected, including idle waits; check `auto_keepalive`. Otherwise call `debug_bridge_renew` every 30 seconds, including before connection and during disconnected recovery. Status/reports do not renew. Respect the 60-second grace and 24-hour maximum.
5. Optionally use `debug_bridge_report` for actual commands/results/states, with increasing sequences and redacted text; omit raw RSP and secrets. Report failures must not interrupt GDB or renewal; retry only the latest identical report, never rerun GDB commands for reporting. Current labels say **Codex report**, regardless of calling model; this is not a complete audit.
6. Detach through GDB before quitting when preserving the inferior. Disconnection retains the lease and does not prove shell recovery. Once the shell is independently confirmed, call `debug_bridge_restore` with the saved `tty_state` and `confirm_shell_ready: true`; require `restored` / `shell_verified`. Serial gdbserver may still await EOF; use separate authorized management if needed.
7. If recovery is uncertain or the snapshot missing, `debug_bridge_close` closes bridge and shared Session without detaching/killing the target. Restore failure or expiry also closes the Session. Never force-release the lease or guess `stty sane`. Remove private connection files afterward.

## Use managed native Linux MCP debugging

Verify native amd64/arm64 ELF, GDB, gdbserver, Bash, `setsid`, and required utilities in the Host environment. This backend is single-inferior, multithreaded, all-stop. Attach rejects controlling TTYs, pipes, sockets, and unsafe character devices; services with sockets need an external path.

1. Call `debug_start` with `session_id`, secret random `owner`, unique `request_id`, absolute `elf`, and launch `args` or attach `pid`. Let the backend prepare gdbserver/TTY.
2. Retain `status.debug_id`. Consume `debug_events` from cursor zero, then each returned `next`; debug and terminal cursors differ. Use schema-supported `debug_command` actions with unique request IDs; uncertain retries keep identical arguments/ID.
3. Wait for a stopped event before inspection. Poll events or issue `action: "renew"` every 30 seconds, including long runs. `evaluate` can modify values despite disabled function calls.
4. Interrupt with `action: "interrupt"`, not terminal bytes. End with `debug_stop`; default detach preserves the inferior, while `terminate: true` requires an authorized launched process. Resume terminal work only after successful restoration; failure closes the Session.

## Report and reference

Report the selected path, target/tool versions, symbol match, actual observations, and final process/Session state. Distinguish prepared commands, local checks, and real-target evidence; redact secrets and identify uncertain recovery.

Full examples: [GDB workflow](../../docs/getting-started/gdb-debugging.md). Exact interfaces: [MCP contracts](../../docs/reference/mcp-tools.md#debug-tools) and [SSH workflow](../../docs/getting-started/ssh-terminal.md). Installed bundles may omit repository docs; use the essential steps above with installed help/schemas.
