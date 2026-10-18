# GDB debugging with ChannelTerm

Choose a path by where the debugger and program run. These workflows cover Linux user-space
programs, including C/C++ applications on servers and boards; bare-metal and KGDB are outside scope.
The bundled [GDB Skill](../../skills/cterm-gdb-debug/SKILL.md) provides agent preflight, operation,
and recovery steps. Installation follows the [Agent Skill instructions](mcp-server.md#install-the-bundled-agent-skill).

| Path | Requirement and control |
| --- | --- |
| [Managed GDB/MI](#managed-native-linux-prerequisites) | Host, tools, ELF, and shell in one native Linux environment; MCP controls supported debugger actions. |
| [Interactive GDB in SSH](#gdb-in-the-shared-ssh-terminal) | GDB and program on the remote Linux server; ordinary shared terminal input/output. |
| [External GDB over SSH/TCP](#workstation-gdb-and-remote-gdbserver) | Workstation GDB and remote gdbserver; native SSH or a restricted network carries RSP independently of ChannelTerm. |
| [External serial bridge](#external-gdb-over-a-prepared-serial-session) | Prepared serial Session to a Linux board; MCP manages ownership and `rsp-client` connects external GDB. |

An SSH Session to another machine does not satisfy the managed backend's same-environment check.
`debug_bridge_start` accepts only serial Sessions, not SSH Sessions or arbitrary TCP targets.

## Managed native Linux prerequisites

The managed GDB/MI backend requires ChannelTerm, GDB, gdbserver, the executable ELF, and the Session's
Bash shell in **one native Linux environment**. It supports one inferior, multiple threads, and
all-stop execution. It does not implement KGDB, multi-inferior control, Windows/WSL environment
crossing, or a development-board backend. Local PTY integration is distinct from physical serial
validation. The separate [external RSP bridge](#external-gdb-over-a-prepared-serial-session) supports
an independently operated debugger across host environments.

Install GDB, gdbserver, Bash, util-linux `setsid`, and the usual Linux `stty`, `awk`, `cat`, `date`,
and `sleep` utilities. The host must support pidfds. Use an existing Session at a known idle Bash
prompt; for example, an SSH Session into the same Linux user/environment. A login on another machine
or in a different filesystem/PID namespace is rejected during takeover. The ELF must match the
host's native amd64 or arm64 architecture. Build your own program with symbols, for example:

```bash
gcc -g -O0 -fno-omit-frame-pointer -o /tmp/addition /tmp/addition.c
```

A small source suitable for the workflow is:

```c
int add(int a, int b) {
    int sum = a + b;
    return sum;
}
int main(void) {
    int answer = add(3, 4);
    return answer == 7 ? 0 : 1;
}
```

Use a trusted executable and debugger. Debug tools grant process execution and memory inspection
under the Host user's authority; an owner capability is an additional operation boundary, not a
replacement for MCP transport authentication. Keep the existing HTTP bearer token private.

## Start and inspect

Generate a random owner once (for example, `openssl rand -hex 24`) and keep it secret. The following
MCP argument objects use an obviously fake owner; replace it and the Session reference. Each
`request_id` identifies exactly one action. Reuse identical arguments when retrying a lost response.

Call `debug_start`:

```json
{"session_id":"SSH-1","owner":"EXAMPLE-ONLY-REPLACE-WITH-RANDOM-OWNER","request_id":"start-addition-1","elf":"/tmp/addition","args":[]}
```

Keep `status.debug_id`. Optional absolute `gdb` and `gdbserver` paths override Host PATH lookup.
The initial remote stop occurs before `main`; consume `debug_events` starting at `cursor: 0` before
continuing. Use each returned `next` as the next cursor:

```json
{"debug_id":"DBG-example","owner":"EXAMPLE-ONLY-REPLACE-WITH-RANDOM-OWNER","cursor":0,"timeout_ms":10000}
```

Call `debug_command` to insert a breakpoint, then continue:

```json
{"debug_id":"DBG-example","owner":"EXAMPLE-ONLY-REPLACE-WITH-RANDOM-OWNER","request_id":"break-add-1","action":"break_insert","argument":"add"}
```

```json
{"debug_id":"DBG-example","owner":"EXAMPLE-ONLY-REPLACE-WITH-RANDOM-OWNER","request_id":"continue-1","action":"continue"}
```

Execution commands return MI acceptance; wait for a subsequent `*stopped` event before inspecting
values. At `add`, use `arguments`, `variables`, `stack`, `threads`, `registers`, or `register_names`.
`evaluate` with `argument: "a + b"` returns `7`. `next` executes the assignment, after which
`evaluate` with `argument: "sum"` returns `7`. `step` enters a called function, `finish` returns from
the current frame, and `thread` / `frame` select a numeric context. Give every command a fresh
request ID. Function calls from expressions are disabled; expressions may still modify values.
See the [MCP reference](../reference/mcp-tools.md#debug-tools) for exact arguments and limits.

Poll events or issue `action: "renew"` at least every 30 seconds, including while the inferior is
running. A client heartbeat expires after 60 seconds; the Host then attempts bounded detach and
shell restoration. A tool timeout does not authorize resubmitting a mutation with a different ID.

## Detach, terminate, and recover

Call `debug_stop` with the same owner and debug ID. The default detaches and leaves the process
alive. `terminate: true` is allowed only for a process launched by this operation; it is rejected
for attached processes. Repeating a completed stop is safe. Normal inferior exit automatically
runs the same restoration path.

```json
{"debug_id":"DBG-example","owner":"EXAMPLE-ONLY-REPLACE-WITH-RANDOM-OWNER"}
```

Success (`status.state: "closed"`) requires protocol completion, restoration of the saved `stty`
settings, and execution of a fresh random-marker `printf` at the recovered shell. This probe has no
filesystem or application side effects. Do not send raw terminal bytes to interrupt debugging;
use `debug_command` with `action: "interrupt"`. Other ChannelTerm writers receive a busy error.
Bundled CLI attachments suppress the leased RSP output range while independent raw readers retain
all bytes and their own cursors.

If restoration cannot be proved, the tool returns an error and closes/removes the Session. A repeat
stop still reports failure. Ordinary writing is never automatically reopened over an unverified
shell. Reconnect explicitly and inspect the target. On connection loss, GDB failure, lease expiry,
or output-buffer loss, the Host closes the Session and kills only its pidfd-owned gdbserver;
Linux releases its ptrace ownership. This failure cleanup does not intentionally kill an attached
inferior. A shell watchdog also ends that server after Host death or 45 seconds without a Host
heartbeat. Debugging can change program state before any failure, so surviving does not imply that
the application is unchanged.

## Attach to an existing process

Pass `pid` and the matching absolute ELF path to `debug_start`, with no launch arguments. The target
must already be a separate, trusted process with no controlling terminal and no open character
devices except `/dev/null`, `/dev/zero`, `/dev/random`, or `/dev/urandom`. Every descriptor is checked,
not just standard I/O. Pipes and sockets are also rejected because their peers may forward data
to the Session. A foreground program on the shared PTY, a process retaining a serial-port
handle, an unreadable `/proc` entry, or a different ELF is rejected. The program must not reopen
terminal devices during debugging; this backend is not a sandbox for hostile inferiors.

Linux ptrace/Yama policy still applies. Arrange tracing permission for the dedicated target rather
than weakening system-wide policy. The integration fixture explicitly opts its own disposable
process into tracing. Existing-process stop always detaches; there is no default kill.

## Validation and diagnostics

For local managed-backend checks, follow the prerequisites, commands, and coverage in
[Local GDB integration tests](../development/building-and-testing.md#local-gdb-integration-tests).
Those tests use native Linux GDB/gdbserver and PTYs; they do not validate a physical board or
cross-environment managed debugging.

When a result differs, collect the failing tool error, `debug_id`, event cursor, lifecycle state,
GDB/gdbserver versions, Linux architecture, and the exact test command. Raw terminal history and MI
values may contain program data; review before sharing them. Temporary runtime files are private
and removed on cleanup. Physical-board acceptance belongs to the separate external bridge workflow below.


## Debug a Linux server over SSH or TCP

ChannelTerm can share a server's SSH shell while GDB runs there, or prepare the target for a separate
workstation debugger. Native OpenSSH pipe/forwarding examples below use the external `ssh` executable;
`channelterm ssh` provides an interactive PTY shell and has no arbitrary exec or forwarding interface.
External local GDB requires a persistent process execution tool in addition to an MCP connection.

### Check the target and build

Open or reuse the intended [SSH Session](ssh-terminal.md), record its owning endpoint, and keep its
foreground owner alive. At an idle target shell, check `uname -s`, `uname -m`, and the debugger needed
for the selected path. For gdbserver:

```sh
if command -v gdbserver >/dev/null 2>&1; then
    gdbserver --version
else
    printf '%s\n' 'gdbserver is not on PATH'
fi
```

Check the exit status as well as output: a present file may fail due to loader, ABI, library, or
permission errors. Check a known vendor SDK path when applicable. If missing, use the target's
authorized package-management or cross-toolchain workflow, then repeat the check. A Linux board
does not necessarily have apt or accept the workstation's executable. Native GDB on the server
does not require gdbserver; check `command -v gdb` and `gdb --version` for that path instead.

For a small authorized C++ debug build, a representative command is:

```sh
g++ -g -O0 -fno-omit-frame-pointer -o /srv/debug/demo /srv/debug/demo.cpp
```

Use the project's build system for a real application. Paths and code are supplied by the operator.
Keep the matching unstripped ELF/debug files, libraries, and source; an unrelated rebuild does not
match an already running service. For workstation debugging, choose target-compatible GDB and
configure its `file`, `set sysroot`, and source path mappings as needed. Transfer files through the
[file-transfer workflow](file-transfer.md). Identify the intended PID and tracing permissions before
attach; stopping a service affects its execution and must fit the requested debugging scope.

### GDB in the shared SSH terminal

At the server shell, launch:

```sh
gdb -q --args /srv/debug/demo
```

For an authorized existing process, use `gdb -q /path/to/matching/executable -p PID` instead.
Once GDB owns the PTY, use interactive terminal input (`terminal_write` plus reads), not
`terminal_exec`, which sends a Bash wrapper. A new-program example is:

```text
(gdb) set pagination off
(gdb) break main
(gdb) run
(gdb) bt
(gdb) info locals
(gdb) next
```

Observe each stop before inspecting values. An attached process is already running under the
debugger's control: inspect it and use `continue`, not a fresh `run`. To preserve a live inferior,
use `detach` before `quit`; after normal program exit, simply quit. Verify that the shell returns.
The shared Session shows normal GDB terminal output, with no managed MI event stream or debug lease.
Keep other writers from issuing conflicting interactive commands.

### Workstation GDB and remote gdbserver

Prefer an SSH stdio connection when available. Establish trust and authentication for native
OpenSSH independently of ChannelTerm; the example assumes usable noninteractive authentication.
`BatchMode=yes` makes authentication failure explicit instead of waiting for prompts inside the pipe.

```text
$ gdb -q /work/symbols/demo
(gdb) target remote | ssh -T -o BatchMode=yes user@host.example gdbserver --once stdio /srv/debug/demo
(gdb) break main
(gdb) continue
(gdb) bt
```

Replace host, paths, arguments, and SSH options with the actual settings. For attach, replace the
server invocation with `gdbserver --once --attach stdio PID`. Keep startup output off protocol stdout;
do not allocate a PTY. The stdio path also needs a separate I/O arrangement for applications requiring
interactive stdin. See [GNU's gdbserver documentation](https://sourceware.org/gdb/current/onlinedocs/gdb.html/Server.html).

For a TCP-only setup, first establish authorized firewall/network restrictions. GNU gdbserver has
no built-in authentication, and the host component of its TCP address can be ignored:
`127.0.0.1:2345` does not establish that the listener is private. Do not expose it publicly. On the
target, start the following in a persistent terminal and inspect the actual listener with `ss -ltnp`
or an equivalent tool:

```sh
gdbserver --once :2345 /srv/debug/demo
```

Substitute a free port; for attach, use `gdbserver --once --attach :2345 PID`. From the workstation,
either connect directly over the verified restricted network with `target remote TARGET_IP:2345`,
or keep this native OpenSSH tunnel running in a separate terminal:

```sh
ssh -N -T -o ExitOnForwardFailure=yes -L 127.0.0.1:12345:127.0.0.1:2345 user@host.example
```

Then connect GDB with `target remote 127.0.0.1:12345`. The local bind restricts the forwarding listener;
it does not restrict the server's gdbserver port. See the [OpenSSH forwarding contract](https://man.openbsd.org/ssh.1#L).
The SSH port, forwarding port, gdbserver port, MCP HTTP endpoint, and ChannelTerm TLS bridge are
separate connections. `rsp-client` is for ChannelTerm's authenticated serial bridge, not plain gdbserver TCP.

With `target remote`, gdbserver has already launched or attached to the process. Set breakpoints
and `continue`; do not issue native `run` or upload with `load`. Detach a surviving inferior before
quitting, verify server/target state, then stop only this operation's tunnel. After network loss,
inspect the target before retrying; a dropped socket is not proof of detach or process exit.
Diagnose connection refusal from server startup, listener, route, and forwarding errors without
restarting an unrelated service or weakening host trust. External GDB commands are not automatically
reported into the ChannelTerm SSH Session, and `debug_bridge_report` requires a real serial bridge.

These instructions compose existing terminal operations with external GDB/OpenSSH behavior; they
do not add a ChannelTerm SSH RSP backend. Validate actual server authentication, target permissions,
toolchain compatibility, and end-to-end behavior in the deployment environment.

## External GDB over a prepared serial Session

The external bridge runs on every supported desktop build. ChannelTerm keeps the serial port open
and relays bytes; the operator owns GDB and target preparation. No local ELF, GDB, gdbserver, WSL
launcher, target Python supervisor, or MI operation is required by the bridge. It does not interpret
breakpoints, attach to a target process, or implement KGDB control. Its lifecycle and management
contracts are in the [MCP reference](../reference/mcp-tools.md#external-rsp-bridge-tools).

The target scope is user-space debugging on **any Linux development board with a compatible
gdbserver** that can carry RSP over the prepared serial connection. The bridge imposes no board
vendor, model, or CPU architecture restriction; use GDB and symbols matching the target.
Bare-metal debugging is outside scope. Windows/WSL2 and the ARM GDB command below are examples,
not restrictions on supported Linux boards. Hardware validation remains separate from this scope.

### Windows Host and MCP connection

Keep one persistent Host running in a Windows PowerShell window:

```powershell
channelterm mcp --transport http --listen 127.0.0.1:37099
```

Wait for `MCP Streamable HTTP listening on http://127.0.0.1:37099/mcp`; silence afterward is normal.
Configure the MCP client using `channelterm init --mcp` (select HTTP), or its secret/header settings
as described in [MCP server setup](mcp-server.md#streamable-http). Do not print its Bearer token.
Use `terminal_list_sessions` to confirm that Codex reaches this Host. Stop with `Ctrl+C` only after
restoring/closing active bridges; shutting down the Host closes its Sessions.

The two connections serve different purposes:

```text
Codex -- authenticated MCP HTTP :37099 --> Windows ChannelTerm
                                             |
WSL2 GDB --> Linux rsp-client -- TLS/TCP --> RSP bridge
                              data + control  |
                                       Serial Session --> COM --> Linux board / gdbserver
```

MCP creates/manages the bridge. GDB's data and helper control connections use the bridge's actual
returned TCP port, never the MCP HTTP endpoint. The helper receives only the private `connection`
object, not the MCP Bearer token or management `owner`.

### Prepare and open

1. Open the serial Session on the Windows Host using the normal serial workflow. Record the actual
   port, baud, data bits, parity, stop bits, and flow-control settings. Do not guess board settings.
   Call `terminal_open_serial` through MCP. This is an **illustrative configuration**, valid only if
   every setting has been independently confirmed for your board:

   ```json
   {"port":"COM8","baud":115200,"data_bits":8,"parity":"none","stop_bits":"1","flow_control":"none","wake":false,"label":"linux-board"}
   ```

   Save the returned opaque `session_id`; the example `SER-1` below is only its convenience reference.
   Use `terminal_read` to inspect the prompt. Check `uname -m`, `command -v gdbserver`, and
   `gdbserver --version` through the terminal tools before takeover. The board must run a compatible
   Linux gdbserver; board model and CPU architecture are not restricted by the bridge. Confirm
   executable permissions, target ABI/libraries, ptrace permission, and an ELF with matching symbols
   in WSL. If gdbserver is missing, install/build it using the board's supported toolchain before
   starting the bridge.
2. If the target needs an ELF, first use the existing [file transfer](file-transfer.md) workflow at
   its idle shell: `channelterm file send ./addition /tmp/addition --session SER-1` (select the owning
   `--endpoint` when needed). Run it where that local path is readable and the Host is reachable.
   Wait for verified completion and use the returned destination path. Do not upload Base64 through
   `terminal_write`. The transfer handles payload suppression, TTY restoration, and hash verification;
   it must finish before the bridge takes its debug lease.
3. At the target shell, save the exact output of `stty -g` outside the target. Use `terminal_exec`
   followed by `terminal_read` at an idle Bash prompt, or `terminal_write`/`terminal_read` for another
   known shell. This saved value is required if the Session is to be restored later.
4. The user or an external agent prepares the target using those existing terminal tools. For a
   trusted Linux target, a representative shell command is:

   ```sh
   ct_saved=$(stty -g); stty raw -echo; gdbserver --once stdio /tmp/addition 2>/tmp/ct-gdbserver.log; stty "$ct_saved"
   ```

   Adapt paths and process arguments to the board. Verify that the server starts, that inferior I/O
   does not consume or corrupt its RSP stream, and that no other console producer writes to the same
   serial line. This command is supplied by the operator; the bridge never sends it automatically.
5. Call `debug_bridge_start` with a fresh secret owner and request ID:

   ```json
   {"session_id":"SER-1","owner":"EXAMPLE-ONLY-REPLACE-WITH-RANDOM-OWNER","request_id":"bridge-1","acknowledge_unverified_target":true}
   ```

Startup checks the serial Session and incomplete interactive input, then takes a managed `debug`
lease. Passive inspection cannot prove that a silent target speaks RSP. Without explicit risk
acknowledgement, startup fails with that explanation. Even after acknowledgement, status reports
`target_state: unverified`; ChannelTerm never promotes a guess to protocol verification. Output
starts at the creation cursor; old terminal history is not replayed.

Save the returned `connection` object, containing `address`, `fingerprint`, and secret `token`, as
`bridge.json` in WSL. Use `umask 077` when creating it and `chmod 600 bridge.json`; do not put it in
source control or paste it into public logs. Keep the separate management `owner` private too.
**ChannelTerm does not transfer bridge.json into WSL.** The MCP client/operator must save or copy
only that object, as UTF-8 JSON without a BOM. For example, after the MCP client has exported it to
an account-private Windows file with restrictive ACLs, copy it into WSL's Linux filesystem:

```bash
umask 077
mkdir -p "$HOME/.local/state/channelterm-rsp"
chmod 700 "$HOME/.local/state/channelterm-rsp"
install -m 600 /mnt/c/Users/EXAMPLE/private/bridge-connection.json "$HOME/.local/state/channelterm-rsp/bridge.json"
```

Replace the source path with the actual private export. Do not put the JSON in command arguments,
shell history, terminal transcripts, or a shared clipboard. Do not rely on `chmod` on a Windows
mount to provide Linux file privacy. Remove the private export/copy when the bridge ends.

Retain `status.bridge_id` for management calls. Exact retries of the same start request return the
same operation and descriptor. Current `rsp-client` automatically maintains the 60-second management
lease while connected, including long GDB commands, breakpoint waits, and paused targets with no RSP
traffic. It answers authenticated challenges over a separate TLS control socket on the same port;
no heartbeat reaches the board. `auto_keepalive: true` in status confirms recent liveness evidence.
Use matching Host/helper versions. Call `debug_bridge_renew` every 30 seconds before connecting,
while arranging recovery after disconnect, or with a legacy data-only helper. Status reads do not
renew. Finish debugging before the absolute 24-hour `max_expires_at`; neither automatic nor explicit
renewal extends that limit.

### Inspect bridge ports

From another terminal with access to the same HTTP Host and its existing authentication setup, run:

```powershell
channelterm gdb-list
channelterm gdb-list --json
```

The table associates each actual TCP port/address with its Session, serial target, state, and
bridge ID. Use `--endpoint URL` for a different Host; listing one Host cannot discover bridges
in another process or a stdio-only Host. Disconnected bridges remain listed until finalization,
so their presence does not mean GDB is still connected. Empty results are successful; a connection,
authentication, or unsupported-Host error is reported separately. Queries neither renew leases
nor affect target I/O. They time out after ten seconds and can be cancelled with `Ctrl+C`.

The list contains no private connection material. Continue using the saved descriptor with
`rsp-client`; the listed address is not an unauthenticated `target remote IP:PORT` endpoint.
See the [CLI contract](../reference/cli.md#gdb-list) for fields and snapshot semantics.

### Connect independent GDB from WSL2

Use a Linux ChannelTerm executable in WSL for the pipe client helper. The Windows Host does
not install it or launch GDB. Verify `channelterm rsp-client --help` and your GDB's `--version`
inside WSL. Choose the GDB from the target SDK (or a multiarch GDB supporting the target), with the
matching ELF, architecture/ABI, and target sysroot. The debugger's target architecture is separate
from the Linux helper executable's WSL host architecture.

Start the GDB matching your target yourself. This example uses a 32-bit ARM Linux toolchain;
replace its GDB executable for other targets. Paths, function names, and variables are examples:

```text
$ arm-linux-gnueabihf-gdb /work/build/addition
(gdb) set sysroot /work/sdk/target-sysroot
(gdb) target remote | channelterm rsp-client --connection /home/EXAMPLE/.local/state/channelterm-rsp/bridge.json
(gdb) break add
(gdb) continue
(gdb) step
(gdb) next
(gdb) finish
(gdb) print sum
(gdb) info registers
(gdb) bt
(gdb) detach
(gdb) quit
```

The executable name and source locations depend on the target toolchain. GDB launches the pipe
helper; the helper authenticates TLS/TCP and passes bytes without RSP commands or packet rewriting.
GDB supports this [pipe connection form](https://www.sourceware.org/gdb/current/onlinedocs/gdb.html/Connecting.html).
A plain `target remote HOST:PORT` cannot authenticate this endpoint; use the helper. No token or
TLS preface is forwarded to the serial Session. ACK and no-ack traffic are both forwarded unchanged.

Use the address returned by `debug_bridge_start`; it is separate from the MCP HTTP endpoint.
The default is loopback with a preferred port pool and dynamic fallback. The
[MCP port policy](../reference/mcp-tools.md#external-rsp-bridge-tools) defines the pool,
`strict_port_pool`, explicit addresses, and retention limits. An explicit fixed port never silently
changes; `:0` requests an OS-selected port. These choices do not establish a device throughput limit.

For **mirrored networking**, use IPv4 loopback for both sides; Windows 11 22H2 or later supports this
mode. For **NAT**, find the Windows host gateway and the WSL source address, then select that specific
Windows interface for the bridge. Microsoft's [WSL networking guidance](https://learn.microsoft.com/en-us/windows/wsl/networking)
describes these differences. Inspect in WSL:

```bash
ip -4 route show default
ip -4 route get WINDOWS_HOST_IP
```

Use the gateway from the first command in the second (replace `WINDOWS_HOST_IP`); its `src` is the
client IP to allow. Add `listen_address: "WINDOWS_HOST_IP:37100"` and
`allowed_peer: "WSL_SOURCE_IP"` to the start request, replacing both placeholders with numeric
addresses. A fixed port has no fallback. Alternatively use `WINDOWS_HOST_IP:0` and read the actual
port from the result; explicit `:0` bypasses the preferred pool and its strict policy. Restrict the
Windows firewall rule to this interface, actual TCP port, and client IP; both data and control use
that same port. ChannelTerm does not change firewall rules, WSL settings, or forwarding. Addresses
can change after restart. Wildcard binds are rejected; do not replace the pin/token to troubleshoot.

If Codex's MCP client also runs inside NAT-mode WSL, configure the **MCP** Host separately with
`--listen WINDOWS_HOST_IP:37099` and that client's authenticated HTTP URL on the trusted host network.
If Codex runs on Windows, MCP can remain on Windows loopback. Bridge `allowed_peer` does not filter
MCP HTTP; its Bearer credential belongs only in the MCP client's secret settings. Keep the two
credentials and listener configurations distinct.

TLS 1.3, certificate pinning, and token authentication apply even on loopback. The first authorized
data connection consumes the endpoint; other data connections and reconnects cannot take over.
A current helper also authenticates exactly one control connection with the same bridge credential. Invalid
credentials do not consume it. The MCP Host's authentication still protects creation/management;
MCP access does not automatically authenticate a TCP peer.

### Show Codex-reported commands and results

Keep controlling GDB with the external execution tool. After an actual command or observation,
call `debug_bridge_report` separately so attached users can follow the work. For example, after
executing `print sum`, report the command and the real response with increasing sequence numbers:

```json
{"bridge_id":"RSP-example","owner":"EXAMPLE-ONLY-REPLACE-WITH-RANDOM-OWNER","sequence":1,"kind":"command","text":"print sum"}
```

```json
{"bridge_id":"RSP-example","owner":"EXAMPLE-ONLY-REPLACE-WITH-RANDOM-OWNER","sequence":2,"kind":"result","text":"$1 = 7"}
```

The result above is illustrative; copy the response actually observed. `kind: "state"` can report
an observed stop location or running state. Never include connection credentials, raw RSP packets,
or unreviewed sensitive values. These entries are labelled **Codex report; not a complete GDB audit**.
They do not execute GDB commands or prove the target state. MCP report failure must not stop GDB;
continue debugging; keepalive and any required manual renewal are independent. Retry the latest report unchanged, or use
a higher sequence for the next observation; do not rerun the GDB command merely to resend a report.

Multiple `channelterm attach SER-1` clients see live reports in separate status blocks, including
while binary RSP output is hidden. A new attachment replays the last 20 retained reports, marked
as history. `channelterm events SER-1` or `terminal_session_events` exposes the complete retained
event text with independent cursors. The history is bounded and lasts only with the Session;
it is not a persistent or complete audit. Manual GDB commands are invisible unless explicitly
reported. This workflow does not monitor a GDB log file or add a debugger control process.

### Disconnect, recover, or close

Normal detach must be issued through GDB before it exits. TCP EOF alone cannot distinguish a detach
from an unexpected GDB/helper exit, and only changes the bridge to `disconnected`. Automatic renewal
stops. A dead or stalled control connection is detected by the thirty-second challenge deadline
(up to about forty seconds after the last response); the data socket is then terminated. Idle TCP
keepalive also probes the data path. Network failure, helper crash, and an unverified target all
retain exclusive ownership for the remaining management grace. The serial Session remains exclusively leased,
and ordinary terminal writes remain blocked. No detach, interrupt, shell command, or process signal
is generated by this transition. Use the independently controlled GDB to detach before disconnecting
when possible. Ending a program over a serial/PTY stream may leave gdbserver waiting for EOF; this
bridge cannot turn TCP EOF into serial EOF or remotely kill that server. If necessary, finish target
cleanup through a separate authorized management path. Do not confirm shell readiness while RSP is
still active.

Once the external client has disconnected and the target is independently known to be at its shell,
call `debug_bridge_restore`:

```json
{"bridge_id":"RSP-example","owner":"EXAMPLE-ONLY-REPLACE-WITH-RANDOM-OWNER","confirm_shell_ready":true,"tty_state":"REPLACE-WITH-EXACT-SAVED-STTY-G-VALUE"}
```

The restore operation applies the supplied TTY snapshot, checks its exact `stty -g` readback, and
requires a fresh split-token shell response. Echoed input is not proof. Only `restored` with
`target_state: shell_verified` releases the lease while preserving the Session. The snapshot must
come from this target before preparation; no guessed `stty sane` defaults are used. A missing shell,
mismatched TTY state, lost output, or ten-second recovery timeout closes the Session and reports
`failed`. This verifies shell/TTY recovery, not the inferior's state or lack of debugging side effects.

The automatic restore probe keeps itself out of interactive Bash history; see the
[history contract](../reference/mcp-tools.md#external-rsp-bridge-tools). Raw setup commands still
follow the target shell's history rules. Use `terminal_exec` for ordinary checks at an idle Bash
prompt, and prefer absolute debugger paths over changing the shared shell's `PATH`. Do not clear
user history to hide setup commands.

For an uncertain target, missing TTY snapshot, or abandoned operation, use `debug_bridge_close`.
It closes the endpoint and serial Session without sending protocol commands or claiming recovery.
Heartbeat expiry and serial I/O/data loss also fail closed. Reopening the port does not itself reset
the target. Confirm its state independently before reusing it.

### Failure and recovery decisions

| Observation | Bridge/lease consequence | Operator action |
| --- | --- | --- |
| Connected, paused or waiting for a breakpoint | Fresh control responses maintain both leases; RSP may stay silent | Continue GDB; observe `auto_keepalive` and the absolute deadline. |
| GDB reports successful `detach` | GDB normally disconnects; lease remains held | Confirm gdbserver returned to the shell, then call restore with the saved TTY state. |
| GDB unexpectedly exits or rsp-client is killed | Data EOF stops control and automatic renewal; target remains unverified | Inspect target through an independent authorized path; do not assume detach. |
| Network drops or a control socket remains open but stops answering | Challenge timeout terminates data; remaining management grace is bounded | Diagnose routing/firewall/helper state. Reconnect cannot take over the consumed bridge. |
| Management deadline, internal lease, or 24-hour limit expires | Session closes; ordinary writes cannot resume | Resolve target state independently before reopening a serial Session. |
| Restore returns `restored` / `shell_verified` | Fresh shell response and exact TTY readback passed; lease released | Resume terminal work in the preserved Session. |
| Restore fails, times out, or no shell can be confirmed | Failed restoration closes the Session; lack of confirmation refuses to probe | Keep target state unknown; use board-side recovery, then establish a new Session. |

TCP disconnect is **not evidence that gdbserver exited**. Do not force ordinary lease release,
send speculative shell input into RSP, or claim recovery because reopening COM succeeded. If an
independent board-management path cannot establish a shell, end host ownership explicitly:

```json
{"bridge_id":"RSP-example","owner":"EXAMPLE-ONLY-REPLACE-WITH-RANDOM-OWNER"}
```

Send that object to `debug_bridge_close`. It closes the bridge and serial Session; it does not kill
or detach the remote program. For recoverable disconnected operations that need more time, send
the same object to `debug_bridge_renew` before `expires_at`, while respecting `max_expires_at`.

### Evidence boundaries and diagnostics

For automated and opt-in suites, use the commands and prerequisites in
[External RSP bridge verification](../development/building-and-testing.md#external-rsp-bridge-verification).
That page distinguishes fake serial/TLS checks, Linux PTY byte simulation, and real native
GDB/gdbserver/helper lifecycle tests, including the optional long idle test. Linux tests running
inside WSL2 still use a **Linux Host and PTY**; they do not validate a Windows Host, COM driver,
NAT/mirrored networking boundary, or physical board.

For Windows + WSL2 + Linux-board acceptance, execute the Host/open/prepare/connect/recover steps
above with real values and record Windows/WSL versions and networking mode, the actual serial settings,
board/target architecture, GDB/gdbserver versions, matching ELF, and independently observed results
for breakpoints, `next`/`step`/`finish`, variables, registers, and backtrace. Leave a breakpoint paused
for at least 310 seconds without MCP renewal, then continue and detach. In separate fresh runs,
terminate GDB, terminate only its rsp-client, and interrupt the network; confirm automatic renewal
stops and no ordinary writes are accepted. Recreate the target/bridge between runs because a
consumed endpoint cannot reconnect. Also verify competing writes are refused,
unauthorized/second clients cannot connect, disconnect retains the lease, explicit close releases
the port, and only a verified restore resumes normal writes. Windows/WSL2/Linux-board results require
that environment and must be reported separately from Linux PTY simulation and cross-builds.
Collect `bridge_id`, lifecycle state/error, lease state, relevant target logs, and the exact test
commands if behavior differs. Never include the owner, connection token, or private descriptor.
