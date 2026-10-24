# Application Module

`internal/core/app` is ChannelTerm's UI- and protocol-neutral use-case layer. Adapters receive
structured values and do not reproduce Session, configuration, or connection lifecycle rules.

`Application` is constructed with a required `*session.Manager`. A Device Registry is optional until
a device or decision use case is called. The caller owns both long-lived resources: constructing or
discarding an `Application` does not start or close the Registry or close the Manager. Application
retains the Manager directly for transport-neutral Session lookup, listing, and removal;
Transport-specific services use that same Manager to coordinate endpoint open/reuse and transfer
successfully connected Sessions into Manager ownership.

## Use cases

- `OpenSerial`: load or create configuration, resolve a profile, apply explicit overrides,
  optionally save through a locked read-modify-write transaction, connect or reuse a Session,
  optionally write one wake carriage return, and return Manager-owned metadata.
- `OpenSSH`: validate explicit SSH settings, connect or reuse a PTY shell Session, and return
  Manager-owned metadata without reading or writing connection profiles.
- `RunSSHFiles`: run SFTP management or SFTP/SCP copying on a dedicated connection without a
  Manager or shared Session. Own local file handles, recursive traversal, overwrite decisions,
  cancellation, progress callbacks, and structured results for adapters.
- `ListSessions`, `ReadSession`, `ReadSessionActivity`, `ReadSessionEvents`, `AttachSession`,
  `DetachSession`, `WriteSession`, `ResizeSession`, `ExecuteTerminalCommand`, `AcquireLease`, `RenewLease`,
  `ReleaseLease`, `LeaseStatus`, `BeginFileTransferCancel`, `ResolveFileTransferCancel`,
  `FileTransferCheckpoint`, and `CloseSession`: operate by opaque Session ID or short Session
  reference.
- `WithFileTransferShell`, `SendFile`, `ReceiveFile`, `SendFileWithCancellation`, and
  `ReceiveFileWithCancellation`: enter one target-side non-interactive shell per transfer, stream
  bounded chunks through a cursor-based Session view, coordinate the minimal Linux shell protocol,
  report acknowledged progress, and verify byte count and SHA-256 without owning local files or
  presentation. The shell scope keeps all internal chunk commands out of the interactive parent
  shell's history; nested probing and receive operations reuse the same scope. The
  `WithCancellation` forms accept an explicit safe-boundary cancellation probe for adapters that own
  local input state.
- `ListSerialPorts`: enumerate ports without opening them or changing Registry state.
- `ListSerialProfiles`: read and resolve named profiles; a missing file is an empty list and is not
  created.
- `ResolveSerialTarget`: verify that a native operating-system serial endpoint is currently present.
- `ListDevices`, `ReadDeviceEvents`: expose Registry snapshots and cursor streams.
- `ConnectionDecision`: combine exact device presence, exact active Session metadata, and policy
  without changing state.

## Terminal resizing

`ResizeSession(identifier, cols, rows)` resolves a managed Session and delegates to its existing
optional Channel capability. Lookup, lifecycle, unsupported-capability, and Transport errors
remain visible to the caller. It does not change Session lifecycle or write-lease semantics.
Adapters own size authority: currently only the foreground SSH CLI installs a local size watcher;
secondary attachments and MCP have no resize operation. The application API carries character
dimensions only, never an OS terminal descriptor, signal, or SSH-library object.

## Serial open invariants

`SerialService` uses the Manager's `GetOrCreate` operation to share one active Session per exact
`transport + endpoint` within that Manager. A Transport connection opens and transfers one Channel
to Session. Opening can block, so Manager coordination occurs without holding its registration lock.
Concurrent callers receive the first successful Session or the same opening error.

A Session candidate is connected before Manager registration. On construction, connection,
cancellation, or wake failure, the service closes the unregistered candidate. A reused Session keeps
the original profile, label, and Transport configuration.

Generated Session IDs use 16 random bytes encoded as hex. ID generation is retried a bounded number
of times if it collides with an existing registration.

## SSH open invariants

`OpenSSH` reuses the same Manager and candidate-cleanup helpers as serial opens. Its endpoint key
is the exact `user@host:port` (IPv6 is bracketed), paired with transport `ssh`. Different users or
ports create independent Sessions. Reuse preserves the first connection's credentials, label,
and PTY dimensions; it does not authenticate again or resize the shell. No DNS alias or hostname
case normalization is implied. Metadata contains no key, password, or signer.

`OpenSSHRequest.Config` carries library-independent settings, credential file paths, and optional
context-aware password/unknown-host confirmation callbacks supplied by the adapter. Application
asks the SSH Transport constructor to validate and load them; it does not construct or receive
third-party SSH signers, clients, sessions, or callbacks. Generic Session I/O still sees only the
existing Channel contract.

A candidate connects before registration. Failed or cancelled candidates are closed, and failed
opens do not reserve the endpoint. Successful SSH Sessions use opaque random IDs and `SSH-N`
references from the existing Manager. There are no changes to Session or generic read/write/close
ownership. See the [SSH workflow](../getting-started/ssh-terminal.md) for CLI Host lifetime.

## Dedicated SSH file operations

`RunSSHFiles` validates requests and preflights upload trees before connecting. Only regular files
and directories are uploaded; source symlinks and special files fail. Directory copies require
`Recursive`. An existing destination directory receives the source basename. Missing destination
parents are not implicitly created. `Overwrite` is false by default; replacing a same-type regular
file or merging a directory tree requires explicit opt-in. This is not transactional: cancellation,
disk-full errors, or disconnects may leave partial files and directories, including a truncated
previous file when overwrite was selected. No automatic retry/reconnect or rollback is performed.

`SafeReplace` is an opt-in alternative for SFTP put/get with `Overwrite`. Application owns a
random, exclusively created sibling temporary file and only replaces the destination after a
complete copy and successful close. Local downloads also sync the temporary file. Uploads require
the advertised POSIX rename extension before any staging; local replacement follows the OS's
`os.Root.Rename` semantics. No delete-first fallback is used. Cleanup is attempted on failure;
an unavailable remote connection can leave a staging file whose path is included in the error.
This is per-file staging, not a whole-tree transaction or rollback. Replacement creates a new
file and does not preserve the previous inode's permissions, ownership, ACLs, or hard links.
`Files` counts completed replacements; `Bytes` can include an incomplete staged copy.

`Verify` accepts `none` (also the empty default) or `sha256`, with verification restricted to put/get.
Application hashes the SFTP source stream, then reopens the closed destination and hashes exactly
the expected size with cancellation checks. Upload verification reads the remote destination back;
download verification rereads the local destination. A safe replacement verifies the temporary file
before rename. SCP preflights the remote hash utility before copying: Transport hashes uploaded
payloads and invokes Application after each file's final acknowledgement; Application compares that
digest with the remote destination hash. For downloads, Application compares the remote source hash
with a local reread after protocol acknowledgement. No shared Session or terminal exec is involved.

`Verification` receives `SSHFileVerification` with the file name and empty hashes before checksum
work, then matching `SourceSHA256`/`DestinationSHA256` values on success. A callback error aborts;
the match event precedes a safe rename and does not confirm replacement. `VerifiedFiles` counts
successfully completed files whose digests matched, and remains zero when verification is disabled.
`Files` excludes a file whose verification failed; earlier verified files remain counted on a later
failure. `Bytes` remains payload-only and excludes checksum readback traffic. Read/size errors,
missing tools, malformed remote hashes, and mismatches fail without fallback. Checksums do not
provide disk durability, namespace locking, or snapshots against concurrent mutation.

Remote child paths preserve the caller's literal parent path. In particular, the application does
not lexically collapse `symlink/..` when appending an upload basename or descending into an SFTP
directory. Only the server can resolve its filesystem's symlinks correctly.

Downloads use `os.Root` to confine remote-selected descendants within the caller-selected local
destination parent. Remote names cannot contain separators, drive/stream syntax, control characters,
Windows device aliases, or trailing spaces/dots. Duplicate names (case-insensitive for portability),
more than 100,000 entries, or depth beyond 128 fail. Existing local symlinks and type changes are
rejected. SFTP does not follow remote source symlinks; legacy SCP cannot identify server-side symlink
expansion, but its received entries are still confined and validated. Local creation modes are
0600 for files and 0700 for directories, subject to OS semantics; metadata preservation is not promised.

Progress counts per-file bytes accepted by the stream, not durable remote storage. `Files`/`Bytes`
results count copied files and payload bytes; failures return an error and may include partial counts.
For SCP downloads, Transport supplies the completed-file count only after local copying and the
final protocol acknowledgements and any requested verification succeed. Payload bytes from a failed
file can still be included.
Every regular file first reports `(name, 0, size)`, including empty files, before payload copying.
A callback error aborts the operation. These callbacks do not indicate that close, acknowledgement,
or safe replacement succeeded; the operation result supplies authoritative completed-file counts.
The file client is always closed. No Session events, cursors, MCP schemas, or writer leases change.
SSH file-operation errors carry a `StageError` identifying transfer failure; connection and
authentication errors retain their Transport stage. Wrapping preserves `errors.Is` cancellation,
deadline, and filesystem identities. No secret input is included in these diagnostics.
See [file transfer](../getting-started/file-transfer.md#sftp-and-scp-over-ssh) for user-visible limits.

## Read and write behavior

`WriteSession` and `WriteSessionWithLease` reject a payload larger than 1 MiB before Session lookup
or lease acquisition. This application-layer limit applies to decoded bytes regardless of the
calling adapter or transport. MCP additionally rejects input whose decoded size cannot fit before
allocating the decoded copy.

A nil read cursor returns recent retained data. A supplied cursor waits through the Session.
Application does not encode terminal bytes, add presentation, or merge output with activity.

`WriteSession` validates the actor and retries short writes while honoring context cancellation
between retries. Session still provides lower-level byte-boundary write serialization and activity
recording.

## Isolated terminal commands

The shared pending-input check recognizes complete, bounded `ESC [ row ; col R` cursor-position
replies, including replies split across write activity events. Such replies do not add pending
input or clear existing human input. Other escapes, malformed/incomplete replies, and overwritten
input history remain conservative until an explicit line reset. Recognition is syntactic, not
proof of who produced a reply; terminal bytes and stored activity are never filtered.

`ExecuteTerminalCommand` is the command-aware alternative to a raw Agent write. It accepts one
printable command line, acquires and renews a `terminal` lease, rejects a Session whose retained
write activity shows unsubmitted interactive input, and runs the command in an isolated
non-interactive child shell. The interactive parent must be Bash: it deletes only the current
bootstrap history entry, disables input echo, and emits cursor markers around the child. The child
therefore cannot change the human shell's directory or environment. Application publishes the
original command and bootstrap/output boundary as structured terminal-command events; it writes the
internal wrapper as `system`, not `agent`, activity.

Cancellation sends Ctrl+C through the still-owned lease and waits up to five seconds for the wrapper
to restore terminal echo and report its exit. If that recovery cannot be confirmed, Application
closes the Session rather than release a lease over an unknown terminal state. A completed command
returns its exit code plus raw-output start and end cursors. Interactive programs, multiline input,
background-job lifecycle, and arbitrary raw keys remain `WriteSession` concerns, not
terminal-command semantics.

## Writer leases and transfer cancellation

### U-Boot entry and diagnostic evidence

`EnterUBoot` is a serial-only application operation using the existing terminal lease. It validates
bounded literal patterns and interrupt bytes before acquiring ownership, arms a fresh raw-output
cursor, and publishes `OPERATION_STATE` readiness. Reboot mode requires a caller-verified idle Bash
prompt. Its separate history-cleaned bootstrap keeps echo unchanged and does not wait for a Bash
end marker that a reboot may prevent. It does not nest `ExecuteTerminalCommand` or its recovery.

The observer handles arbitrary bytes and cross-chunk markers, drains retained backlog before
acting, sends the interrupt once, and requires a fresh exact prompt line after the countdown.
Boot-start evidence takes precedence; overwritten output stops the operation. Cancellation and
timeout stop further writes and release only this operation's lease, without recovery keys or
closing a healthy shared Session. Already submitted device writes cannot be undone. These are
Host observations, not guarantees about hardware scheduling or remote execution.

`ExportDiagnostics`, `DiagnosticInfo`, and `ReadDiagnostic` own immutable ZIP evidence in bounded
Application memory. No filesystem or additional network listener is introduced. Construction is
serialized; at most four archives of at most 16 MiB each are retained for 15 minutes, with oldest
eviction and expired-memory reclamation on store access. Host exit discards archives. Raw evidence
requires explicit opt-in; operation capture retains at most 2 MiB and 4096 observation segments.
Snapshot export reads at most 2 MiB of recent output. Each archive includes at most 256 recent
activity/events, with at most 256 KiB of write payload. Gaps, truncation, unavailable buffers and
independent sampling are explicit. Capture times are application observation times, not UART
arrival times. See the [MCP contracts](../reference/mcp-tools.md#terminal_export_diagnostics).

### Existing lease contract

Application owns one exclusive lease per managed Session. Supported lease types are
`terminal`, `file-transfer`, and `debug`. A normal write fails immediately with
`ErrSessionBusy` while a lease is active. `WriteSessionWithLease` requires a non-empty matching
opaque owner capability and rejects a missing, released, expired, or replacement lease with
`ErrLeaseNotOwned`.

File-transfer acquisition checks retained input activity after claiming writer ownership. An
unsubmitted command or recalled history line returns `ErrTerminalCommandInputPending` with recovery
guidance, releases the unpublished lease, and sends no shell bytes. A caller must clear that line
before retrying; Application never executes or discards the pending command automatically.

Every lease has a 30-second Host-side TTL and an `expires_at` timestamp. `RenewLease` extends that
deadline only for the matching owner. Expiry wakes pending cancellation waits and rejects stale
leased writes. File-transfer expiry atomically marks the Session unavailable for writes and new
leases before asynchronously closing and removing its original registration. This applies even
while waiting for READY, ACK, or receive data with no write in flight. A release arriving after the
deadline follows expiry, not normal unlock. Delayed file-transfer closure uses the original
Manager reference, and exclusion cleanup matches its transfer ID rather than a replacement lease.
Debug expiry also closes the Session; terminal expiry retains its existing policy of closing only
when a leased write is still in flight. Channel has no generic write cancellation, so Close releases
blocked I/O. Already delivered bytes cannot be rolled back. Local closure does not prove that a
device-side program exited, its TTY recovered, or reconnecting is safe.

Acquiring a `file-transfer` lease allocates a non-secret process-local `transfer_id`. Lease
acquire/release and every transfer event carry it, while the owner capability is never published. An
expired file-transfer lease publishes `FILE_TRANSFER_FAILED` with `reason: lease_expired`, followed
by `LEASE_RELEASED` with `state: expired`. `ReportFileTransferEvent` accepts status only from that
lease owner with the matching transfer ID and rejects a completed event without the required source,
requested, resolved, and renamed fields.

Each `file-transfer` lease also owns cancellation state. `BeginFileTransferCancel` changes `running`
to `confirming` only for that lease. `FileTransferCheckpoint` blocks its owner at a safe boundary
until `ResolveFileTransferCancel` resumes, cancels, or the lease expires. A confirmed cancellation
normally returns after the owner releases the lease and publishes `FILE_TRANSFER_CANCELLED`; an
abandoned owner instead returns the expired resolution at the TTL.

File receive invokes its local progress callback with zero bytes after remote metadata is known and
before the first payload. This callback is presentation-facing and does not alter the file-transfer
protocol. A cancellation request is observed at chunk boundaries, whether supplied by the optional
Session capability or the explicit regular-file `WithCancellation` probe. An already-started raw
block can therefore complete, be acknowledged, and restore the remote TTY before the file-transfer
lease is released.

Directory transfer checks the same optional Session capability at setup and block boundaries;
adapters can also return cancellation from its progress callback. Directory tar data is staged in a
temporary target-side archive and uses the same bounded raw blocks. Cancellation removes the partial
archive after the active block instead of transmitting or draining the untransferred remainder. Both
endpoints compute SHA-256 over the exact tar byte stream; extraction or local installation is
reported successful only when byte count and digest match.

`AttachSession`, `DetachSession`, and authorized `ReportFileTransferEvent` calls publish the
corresponding state changes on the independent event stream. Readers, cursors, raw output, Channel,
and Session internals are unaffected. See [Session](session.md).

Terminal command execution publishes `TERMINAL_COMMAND_STARTED`, `TERMINAL_COMMAND_OUTPUT_STARTED`,
`TERMINAL_COMMAND_COMPLETED`, or `TERMINAL_COMMAND_FAILED`. Lease events still delimit exclusive
writer ownership. The command event contains the display command and output cursor but never the
lease owner capability.

## Shared Session file transfer

File transfer uses the same Session write and output-cursor semantics. It obtains a `file-transfer`
lease before its first protocol command, renews it every 10 seconds, and releases it after confirmed
shell recovery on success, failure, or cancellation. Bundled clients continue renewing during
cancellation cleanup until the operation returns, then stop the heartbeat. Unconfirmed recovery or
renewal failure requests closure instead of releasing the lease for reuse; if that request fails,
Host-side expiry remains the fallback. A send creates a missing
remote destination hierarchy before payload transmission and reuses an existing hierarchy.
Destination collision selection remains the first
free `_N` sibling.

The interactive target shell receives one `CTERM_FT=<token> sh -c ...` bootstrap command. On Bash, the
bootstrap removes its own current in-memory history entry only when its fresh token matches that
entry. Ignored commands and disabled Bash history leave earlier user history unchanged.
Shells without Bash-compatible history deletion retain the bootstrap as one recognizable entry. The
bootstrap temporarily disables target-terminal input echo, and initialization clears the displayed
command after the child starts. The non-interactive child runs path probing, per-chunk commands,
verification, and cleanup without additional interactive history entries. The child is kept alive
every five seconds while no protocol command is active and has a 30-second remote idle watchdog. A
killed client therefore leaves no Bash entry, or at most the one fallback entry on another shell.
The watchdog operates only at idle shell boundaries; it is not evidence of recovery when the Host
lease expires. Normal completion and safe-boundary cancellation
explicitly stop the watchdog, restore terminal echo, and exit the child before the file-transfer
lease is released. The bootstrap emits its final confirmation from the interactive parent only after
the child exits; this keeps all earlier command echo inside the lease's suppressed presentation
cursor range.

Startup has a separate 15-second deadline for the echo-disabled and child-ready markers. Failure
uses the existing bounded recovery path. This deadline does not limit the later payload transfer.

The protocol reads or writes at most 8 KiB of payload per chunk, saves and restores the remote TTY
mode around each raw `dd` operation, and performs end-to-end SHA-256 verification for regular files
and directory tar streams. A target-side send block uses a 10-second serial-input idle timeout and
verifies the exact file-size increase after restoring the saved TTY. This bounds the intended
raw-input wait and prevents acknowledging a short block; Host closure does not verify its outcome.
Send initialization probes `dd iflag=fullblock`; a minimal `dd` instead reads one-byte blocks with
`count` equal to the announced chunk size. The timeout, size check, and final hash apply to both paths.

A cancelled or failed operation uses independent recovery contexts to pad or drain its active raw
block and consume the post-restoration acknowledgement before sending further shell commands.
Recovery also removes directory-transfer staging data. Each recovery
I/O operation has a 15-second deadline and does not inherit the cancelled operation context.
Unconfirmed recovery, including overwritten output, invalid recovery acknowledgements, or an
unconfirmed child-shell exit, invokes `terminal_close` with a separate five-second-bounded request.
`ErrFileTransferRecoveryUnconfirmed` preserves the underlying error and prevents deferred release
even if closure fails. Further shell cleanup writes stop after this failure. A peer read/write
failure marker emitted after the raw block can still proceed to outer shell-exit confirmation;
ordinary transfer errors with confirmed recovery retain the connection. Independent readers see
raw output while the Session remains open, including received file bytes. Closing discards retained
buffers, so delivery of final events to every observer is not guaranteed.

The CLI transfer adapter lets an in-flight write return its acknowledged byte count after cancellation,
with a 15-second response grace. Abandoning an MCP response immediately could mistake delivered bytes
for an unwritten block and duplicate them during padding. If the grace expires, the adapter closes
the Session before attempting further recovery because the remote byte count is unknown.

## Session close

`CloseSession` first resolves and snapshots Session metadata, removes the Session directly from
Manager ownership, closes it, and returns the pre-close information for adapter results. This
lifecycle path is independent of the Transport service that originally opened the Session.

## Boundary rules

Application owns orchestration and managed debug subprocesses, not Host process lifetime, host stdin/stdout, flags, ANSI, MCP schemas, or
concrete UI behavior. New CLI or protocol adapters should call this layer instead of looking up
Session or Serial Transport objects directly.

## User-space debug operations

`StartDebug`, `DebugCommand`, `ReadDebugEvents`, and `StopDebug` coordinate one native Linux inferior
through `internal/core/debugger`. The backend checks ELF architecture and existing-process I/O
isolation before acquiring the terminal. A persistent optional Session input summary rejects
unsubmitted human/agent input even after activity-history overwrite. Alternate Session
implementations without that summary must have complete retained activity. This summary is not a
terminal emulator: the caller must supply a known idle Bash prompt.

Start claims a fresh internal debug lease capability, separate from the client owner. It saves
`stty -g`, verifies the same local filesystem, and switches the PTY to raw/no-echo mode. A private
wrapper launches gdbserver without a controlling terminal; a second wrapper redirects new-inferior
I/O to `/dev/null`. GDB runs with init files, auto-loading, debuginfod, and expression function calls
disabled. Non-stop mode is off and fork-following stays on the parent with detach-on-fork enabled.
There is no raw MI or console passthrough. The relay uses the existing Session reader's independent
cursor and the Application leased-write gate. It fails on dropped bytes instead of resynchronizing.

An internal 30-second lease is renewed every 10 seconds. Debug lease expiry always closes/removes
the Session, even without an active write. Public lease release cannot release a managed operation.
Client events/commands maintain a separate 60-second heartbeat; expiry attempts detach rather than
killing an attached process. Setup and MI commands are capped at 30 seconds; recovery has an
independent 10-second context. An uncertain command timeout kills GDB and fails the operation closed.
An authoritative MI error preserves the operation for another command. A shell watchdog checks Host
process identity and an atomically replaced heartbeat file, ending only its original server after
Host death or 45 seconds without renewal. PID/start-time checks and pidfds prevent cleanup from
signalling a reused server PID.

GDB closes RSP before its MI exit record can arrive; the coordinator allows up to two seconds for
that record, without inferring success from EOF. In ACK mode, the relay observes checksum-valid
packet framing without rewriting bytes. For normal exit it confirms the final W/X acknowledgement;
for explicit termination it confirms the last reply acknowledgement. Only then may Application end
a gdbserver waiting for stdio EOF, which PTYs cannot transmit. Detach uses GDB's target-detach
handshake. All paths still require the shell footer, restoration of the exact saved TTY settings,
and a fresh split-token `printf` probe before releasing the lease. A failure closes the Session;
no timeout, silent period, or socket closure is accepted as proof of restoration.

Operation IDs and exact start requests remain retained for Host-lifetime retry deduplication, with
at most 128 operations. Commands retain at most 4096 requests / approximately 16 MiB per operation;
reaching the limit rejects new commands but still permits stop. MI lines are capped at 1 MiB and
nesting at 64 levels. Events retain at most 512 records / 4 MiB and return at most 128 per read;
a stale cursor is an error. These debug cursors are distinct from Session output/activity/events.
Lifecycle state is `starting`, `stopped`, `running`, `exited`, `recovering`, `closed`, or `failed`.
Closed/failed states cannot be overwritten by delayed MI notifications.

The private runtime directory is mode 0700 and the socket mode 0600; `SO_PEERCRED` checks the exact
GDB PID and Host user. Neither debug events nor Session events contain the client owner or internal
lease capability. The ordinary MCP Host authentication remains in force. The operation is not a
sandbox for malicious programs, debugger executables, or same-user processes. See the
[workflow](../getting-started/gdb-debugging.md) for prerequisites, safe attach, and validation limits.


## External serial RSP bridge

`StartRSPBridge`, `RSPBridgeStatus`, `RenewRSPBridge`, `CloseRSPBridge`, and `RestoreRSPBridge`
coordinate a separate bridge over an already prepared serial Session. The Application never launches
or controls GDB, gdbserver, or a target supervisor in this mode. Its coordinator is independent of the
managed MI operation. Input-pending checks are shared; both use the existing debug lease gate.
The Session keeps its sole Channel reader, raw history, cursors, and write serialization.

`ListRSPBridges(ctx)` provides a separate non-secret inventory for Host observers. It copies run
references under the coordinator mutex, reads each status under its status mutex, and joins
Manager metadata without holding the coordinator lock. It includes unfinished runs, including
disconnected/recovering bridges, until their terminal state is published. Results are sorted by
actual port, address, and bridge ID. Concurrent finalization can remove Session metadata or
supersede a returned snapshot. No lease, target I/O, or managed GDB operation is touched;
cancellation ends collection only. Adapters enforce Host access policy without requiring a
per-bridge owner for this restricted summary. The public fields and authorization boundary are
owned by the [MCP reference](../reference/mcp-tools.md#external-rsp-bridge-tools).

The bridge coordinator owns a `debugger.EndpointAllocator`. One mutex protects its rotating cursor,
real TCP binds, and logical address reservations. Allocation continues through 37100..37999 across
requests, skipping active addresses; an active-pool count avoids scanning a fully reserved pool.
This avoids repeatedly traversing the same occupied prefix during concurrent creation. Releases
remove reservations without rewinding the cursor. Explicit and OS-selected binds within the pool
also count as active pool addresses. The allocator closes the listener and removes its reservation
under the same mutex, preventing dynamic allocation from selecting an address still reserved during
finalization. See the [port policy](../reference/mcp-tools.md#external-rsp-bridge-tools)
for strict exhaustion, dynamic fallback, and explicit addresses. `debugger.Endpoint` owns bounded TLS
authentication and transfers one accepted socket to the relay. Its listener remains bound, rejecting
later data clients until finalization; current helpers may authenticate one control socket. Logical
reservations and OS binds are distinct; neither client authentication nor disconnect releases either.
Final close/failure or verified restoration does. Application
adapts the Session cursor/leased write to the existing `debugger.Relay`; authentication bytes never
reach the Session. Creation does not send target bytes. Pending input, incomplete input history,
non-serial Sessions, and conflicting leases are rejected. Silent-target readiness remains explicitly
unverified and requires an operator acknowledgement; no RSP probe/parser is added to Session.

The operation retains a secret management capability separately from its internal lease owner and
TCP token. Status/events omit all three. Start retries are scoped to owner/request ID and fingerprint
all arguments; retention is bounded to 128 operations. The management TTL is 60 seconds; status reads
do not extend it. Each one-second monitor pass checks the existing 30-second debug lease and renews
with at most twenty seconds remaining, including during the post-disconnect management grace.
Automatic management renewal requires both a valid internal lease and recent authenticated control
challenge/response evidence. Explicit renewal also renews the internal lease first; a stale lease
fails closed instead of leaving the management layer apparently healthy. All renewal is capped at
24 hours from start; closed/expired operations cannot be resurrected.

A current helper authenticates data using `ChannelTerm-RSP/2`, then one separate control TLS socket
using `ChannelTerm-HBT/1` on the same listener, with the existing token, pin, and peer checks. The
control loop uses random 16-byte challenges, a ten-second interval and a thirty-second response
bound, independent of RSP traffic. It closes both sockets on control failure and stops on data EOF.
The helper bounds data reads after control EOF while allowing final RSP bytes to drain, because
independent TCP streams can deliver EOF in either order. Legacy `ChannelTerm-RSP/1` data connections
retain manual management renewal. Session and the managed MI relay never see this control protocol.
The exact public lifetime rules are owned by the [MCP reference](../reference/mcp-tools.md#external-rsp-bridge-tools).

The worker alone transitions listening -> connected -> disconnected. Management mutations serialize
on an operation mutex; state reads use a separate short-held mutex. Finalization cancels the
relay, closes the Session to interrupt an unbounded Channel write, then joins the relay and closes
the endpoint/reservation before publishing its final state. No worker publishes a state after that join. Concurrent relay EOF must
not hide a lost-buffer error from the other direction.

Disconnected is not restored: ordinary writers remain excluded. Explicit restoration requires an
externally returned shell and its pre-debug TTY snapshot. It joins the disconnected worker, applies
and reads back the caller's exact `stty -g` value, and uses the existing split-token command-marker
reader for a fresh proof. The independent ten-second timeout closes the Session even if a probe write
blocks. In Bash, the probe deletes its own history entry only after matching its fresh split token
in the last entry; ignored probes or disabled history must not delete a prior user command. Other
shells keep their normal history behavior. Only successful verification releases the lease while preserving the Session. Unknown target
state, lost serial bytes, expiry, explicit close, or failed recovery closes the Session without
claiming target cleanup. There are no ACK-mode assumptions, injected detach commands, or remote PID
signals in this lifecycle. See the [MCP contracts](../reference/mcp-tools.md#external-rsp-bridge-tools)
and [external workflow](../getting-started/gdb-debugging.md#external-gdb-over-a-prepared-serial-session).

`ReportRSPBridge` is an independent observation path. It authenticates the same management owner,
validates kind/UTF-8/size and rejects known bridge capabilities in the text, then publishes
`DEBUG_CLIENT_REPORT` through Session. The operation mutex serializes publication and the latest
sequence/digest retry cache with shutdown. Increasing sequences may skip values; exact latest
retries are deduplicated, older/conflicting reports fail. Only a live, nonexpired bridge accepts
new reports. Context cancellation and all report errors affect reporting only. No bytes, GDB
commands, target-state assertions, heartbeat renewal, or cleanup are generated by reporting.
The existing bounded event buffer owns history; observers cannot backpressure the relay. Caller
selection/redaction and incomplete-audit labelling are part of the public reporting contract.
