# GDB debugging through MCP

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

Run the opt-in local suite from the repository with GDB/gdbserver/GCC on PATH:

```bash
CHANNELTERM_TEST_GDB=1 go test -race ./internal/mcp -run '^TestGDBLocal' -count=1 -v
```

These tests use real GDB/MI, gdbserver, Linux PTYs, and the MCP SDK. They cover the addition workflow,
two-thread inspection, normal exit, safe attach/detach, automatic lease renewal, disconnect survival,
authorization and retry semantics, native
ELF mismatch, unsafe attach rejection, and a missing shell that must fail closed. Ordinary Go tests
separately exercise simulated binary fragmentation, empty reads, invalid UTF-8, dropped buffers,
final ACK tracking, lease conflict/renewal/expiry, input tracking, and MI cancellation.

When a result differs, collect the failing tool error, `debug_id`, event cursor, lifecycle state,
GDB/gdbserver versions, Linux architecture, and the exact test command. Raw terminal history and MI
values may contain program data; review before sharing them. Temporary runtime files are private
and removed on cleanup. Physical serial settings and development boards have not been validated by
these tests; their backend is outside this first release's scope.


## External GDB over a prepared serial Session

The external bridge runs on every supported desktop build. ChannelTerm keeps the serial port open
and relays bytes; the operator owns GDB and target preparation. No local ELF, GDB, gdbserver, WSL
launcher, target Python supervisor, or MI operation is required by the bridge. It does not interpret
breakpoints, attach to a target process, or implement KGDB control. Its lifecycle and management
contracts are in the [MCP reference](../reference/mcp-tools.md#external-rsp-bridge-tools).

### Prepare and open

1. Open the serial Session on the Windows Host using the normal serial workflow. Record the actual
   port, baud, data bits, parity, stop bits, and flow-control settings. Do not guess board settings.
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
Retain `status.bridge_id` for management calls. Exact retries of the same start request return the
same operation and descriptor. Current `rsp-client` automatically maintains the 60-second management
lease while connected, including long GDB commands, breakpoint waits, and paused targets with no RSP
traffic. It answers authenticated challenges over a separate TLS control socket on the same port;
no heartbeat reaches the board. `auto_keepalive: true` in status confirms recent liveness evidence.
Use matching Host/helper versions. Call `debug_bridge_renew` every 30 seconds before connecting,
while arranging recovery after disconnect, or with a legacy data-only helper. Status reads do not
renew. Finish debugging before the absolute 24-hour `max_expires_at`; neither automatic nor explicit
renewal extends that limit.

### Connect independent GDB from WSL2

Use a Linux ChannelTerm executable in WSL for the pipe client helper. The Windows Host does
not install it or launch GDB. Start the ARM-capable GDB yourself with matching symbols and sysroot:

```text
$ arm-linux-gnueabihf-gdb /work/build/addition
(gdb) target remote | channelterm rsp-client --connection /work/private/bridge.json
(gdb) break add
(gdb) continue
(gdb) next
(gdb) print sum
(gdb) bt
(gdb) detach
```

The executable name and source locations depend on the target toolchain. GDB launches the pipe
helper; the helper authenticates TLS/TCP and passes bytes without RSP commands or packet rewriting.
GDB supports this [pipe connection form](https://www.sourceware.org/gdb/current/onlinedocs/gdb.html/Connecting.html).
A plain `target remote HOST:PORT` cannot authenticate this endpoint; use the helper. No token or
TLS preface is forwarded to the serial Session. ACK and no-ack traffic are both forwarded unchanged.

The bridge defaults to `127.0.0.1`, rotating through unallocated ports **37100–37999** before OS
dynamic fallback. Each Host continues from its previous allocation and wraps at the end of the pool.
Set `strict_port_pool: true` on `debug_bridge_start` to fail when this pool is exhausted. An explicit
`listen_address` overrides the pool policy; fixed ports never silently change, and `:0` requests an
OS dynamic port. Always use the returned address. MCP HTTP remains on its separate default **37099**.
The pool size is not a device limit or a concurrency benchmark. With WSL mirrored networking, WSL can reach
Windows services using IPv4 loopback. With NAT networking, use the Windows host address visible
from WSL instead. See Microsoft's [WSL networking guidance](https://learn.microsoft.com/en-us/windows/wsl/networking).
For NAT, explicitly provide `listen_address` with that particular Windows interface IP and port
zero, plus `allowed_peer` with the actual WSL client IP. Wildcard addresses are rejected. Scope any
Windows firewall rule to that interface, TCP port, and client IP; ChannelTerm does not change
firewall rules, WSL configuration, or port forwarding. WSL addresses can change after restart.
Do not change the descriptor's certificate fingerprint or token when choosing a reachable address.

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

For an uncertain target, missing TTY snapshot, or abandoned operation, use `debug_bridge_close`.
It closes the endpoint and serial Session without sending protocol commands or claiming recovery.
Heartbeat expiry and serial I/O/data loss also fail closed. Reopening the port does not itself reset
the target. Confirm its state independently before reusing it.

### Evidence boundaries and diagnostics

Ordinary tests cover TLS authentication, peer filtering, single-client admission, byte transparency,
lease exclusion/expiry, blocked-write closure, MCP management, and success/failure of recovery.
`TestRSPBridgePTYBinaryAndShellRestoration` uses an actual Linux PTY with `cat` as a byte simulator;
it verifies TTY readback and shell probing, not GDB commands or board behavior. The separate
`TestGDBExternalPTY` opt-in test uses real, independently launched native GDB/gdbserver and the
`rsp-client` helper to check breakpoint/step/variable/backtrace/detach behavior over a Linux PTY,
then publishes the actual commands and results through the independent report interface:

```bash
CHANNELTERM_TEST_GDB=1 go test -race ./internal/mcp -run '^TestGDBExternalPTY$' -count=1 -v
```

It requires the same local test tools and ptrace permission as the managed suite. Neither native
PTY test establishes Windows/WSL2/ARM hardware acceptance.

For real acceptance, record Windows/WSL versions and networking mode, the actual serial settings,
board/target architecture, GDB/gdbserver versions, matching ELF, and independently observed results
for breakpoints, stepping, variables, and backtrace. Also verify competing writes are refused,
unauthorized/second clients cannot connect, disconnect retains the lease, explicit close releases
the port, and only a verified restore resumes normal writes. Windows/WSL2/ARM board results require
that environment and must be reported separately from Linux PTY simulation and cross-builds.
Collect `bridge_id`, lifecycle state/error, lease state, relevant target logs, and the exact test
commands if behavior differs. Never include the owner, connection token, or private descriptor.
