# User-space GDB through MCP

This first backend requires ChannelTerm, GDB, gdbserver, the executable ELF, and the Session's
Bash shell in **one native Linux environment**. It supports one inferior, multiple threads, and
all-stop execution. It does not implement KGDB, multi-inferior control, Windows/WSL environment
crossing, or a development-board backend. Local PTY integration is distinct from physical serial
validation.

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
