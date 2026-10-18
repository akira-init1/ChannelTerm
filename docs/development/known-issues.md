# Known Issues

Unresolved reports are recorded here with their evidence limits and the information needed to
resume investigation. An entry does not establish a root cause or promise a fix.

## Serial GDB bridge restore timeout after detach

Status: open; investigation paused. The root cause is unconfirmed, and a controlled reproduction
of the reported failure is not yet available.

### Reported behavior

A report against build `906111408233` used an external GDB 10.2 client, a Windows ChannelTerm Host,
and a serial-connected ARMv7 Linux board running gdbserver 10.2 with `--once stdio`. GDB reported a
successful detach, and the bridge entered `disconnected`. A subsequent `debug_bridge_restore`
returned:

```text
shell/TTY restoration could not be verified; Session closed: context deadline exceeded
```

The attached terminal then received EOF for the old Session. Reopening the serial endpoint created
a new usable Session. This does not establish that the old restoration succeeded.

The error and the implementation support restoration timeout as the immediate reason the old
Session closed. See the [bridge recovery contract](../reference/mcp-tools.md#external-rsp-bridge-tools)
and [recovery workflow](../getting-started/gdb-debugging.md#disconnect-recover-or-close).

### Evidence limits

- GDB reported detach, and gdbserver stderr recorded detaching from the test process. Whether
  gdbserver had exited when the restore probe was sent was not established.
- A shell prompt was present in retained output, but fresh output cursors were not available to
  establish readiness at the moment of restoration.
- Raw serial output after the restore probe was not preserved. Command execution, TTY readback,
  and the presence or absence of the verification marker remain unknown.
- Neither lease expiry nor spontaneous serial disconnection was established as the cause.
- An isolated local Bash/PTY check of the original restore command produced the expected marker.
  It did not exercise the Windows Host, physical serial transport, ARM target, or GDB lifecycle.
- A later hardware reproduction was deferred because the independent SSH management connection
  was unreachable. That networking failure does not explain the earlier serial restore timeout.

### Resume investigation

1. Establish an independently verified management/recovery path before another serial takeover.
2. Record the running Host and helper versions, actual serial settings, original TTY snapshot,
   and GDB/gdbserver versions. Normalize event times without relying on the board's wall clock.
3. Capture GDB/RSP output, helper stderr and exit status, and the test gdbserver's process state
   around detach. Distinguish debugger detachment from server exit and shell readiness.
4. Preserve serial output continuously from a cursor recorded before detach through restoration,
   including raw bytes, cursor progression, and dropped-output indicators. Do not wait for Session
   closure to begin collecting it. Redact authentication and management capabilities.
5. Correlate the restore invocation and result with probe-write completion, actual TTY readback,
   marker output, and Session closure. Keep diagnostic data outside this public document.

Do not extend the timeout or remove failure-triggered closure without evidence that identifies the
failure. The separate Bash-history cleanup does not resolve this report. Close this entry only
after identifying the cause and verifying the relevant recovery behavior; local PTY success alone
does not establish real-board recovery.
