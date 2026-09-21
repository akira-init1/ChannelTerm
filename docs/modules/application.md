# Application Module

`internal/core/app` is ChannelTerm's UI- and protocol-neutral use-case layer. Adapters receive
structured values and do not reproduce Session, configuration, or connection lifecycle rules.

`Application` is constructed with a required `*session.Manager`. A Device Registry is optional until
a device or decision use case is called. The caller owns both long-lived resources: constructing or
discarding an `Application` does not start or close the Registry or close the Manager.

## Use cases

- `OpenSerial`: load or create configuration, resolve a profile, apply explicit overrides,
  optionally save through a locked read-modify-write transaction, connect or reuse a Session,
  optionally write one wake carriage return, and return Manager-owned metadata.
- `ListSessions`, `ReadSession`, `ReadSessionActivity`, `ReadSessionEvents`, `AttachSession`,
  `DetachSession`, `WriteSession`, `ExecuteTerminalCommand`, `AcquireLease`, `RenewLease`,
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

Application additionally owns one exclusive lease per managed Session. Supported lease types are
`terminal`, `file-transfer`, and reserved `debug`. A normal write fails immediately with
`ErrSessionBusy` while a lease is active. `WriteSessionWithLease` requires a non-empty matching
opaque owner capability and rejects a missing, released, expired, or replacement lease with
`ErrLeaseNotOwned`.

Every lease has a 30-second Host-side TTL and an `expires_at` timestamp. `RenewLease` extends that
deadline only for the matching owner. Expiry atomically releases writer ownership, wakes pending
cancellation waits, and prevents a stale owner or timer from affecting a replacement lease. If
expiry occurs while a leased Channel write is still in flight, the Host closes and removes that
Session. The protocol-neutral Channel has no generic write-cancellation capability, so closing it is
the portable way to release the blocked write without allowing a replacement lease to race stale
bytes.

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

File transfer uses the same Session write and output-cursor semantics. It obtains a `file-transfer`
lease before its first protocol command, renews it every 10 seconds, and releases it on success,
failure, or cancellation. A send creates a missing remote destination hierarchy before payload
transmission and reuses an existing hierarchy. Destination collision selection remains the first
free `_N` sibling.

The interactive target shell receives one `CTERM_FT=1 sh -c ...` bootstrap command. On Bash, the
bootstrap removes its own current in-memory history entry without changing earlier user history.
Shells without Bash-compatible history deletion retain the bootstrap as one recognizable entry. The
bootstrap temporarily disables target-terminal input echo, and initialization clears the displayed
command after the child starts. The non-interactive child runs path probing, per-chunk commands,
verification, and cleanup without additional interactive history entries. The child is kept alive
every five seconds while no protocol command is active and has a 30-second remote idle watchdog. A
killed client therefore leaves no Bash entry, or at most the one fallback entry on another shell,
and the child exits after its keepalives stop. Normal completion and safe-boundary cancellation
explicitly stop the watchdog, restore terminal echo, and exit the child before the file-transfer
lease is released. The bootstrap emits its final confirmation from the interactive parent only after
the child exits; this keeps all earlier command echo inside the lease's suppressed presentation
cursor range.

The protocol reads or writes at most 8 KiB of payload per chunk, saves and restores the remote TTY
mode around each raw `dd` operation, and performs end-to-end SHA-256 verification for regular files
and directory tar streams. A target-side send block uses a 10-second serial-input idle timeout and
verifies the exact file-size increase after restoring the saved TTY. A killed transfer process
therefore cannot leave `dd` waiting indefinitely in raw mode or acknowledge a short block.

A cancelled or failed operation uses independent recovery contexts to pad or drain its active raw
block, consume a pending acknowledgement, and remove directory-transfer staging data. Each recovery
I/O operation has a 15-second deadline and does not inherit the cancelled operation context. If
recovery reaches that deadline, the bundled MCP attachment invokes `terminal_close` with a separate
five-second-bounded request. This releases an in-flight Host write and prevents an unusable protocol
state from retaining the Session or lease. Recovery timeout errors are joined with the original
failure. Independent readers continue to see raw output, including received file bytes.

`CloseSession` first resolves and snapshots Session metadata, removes the Session from Manager
ownership through `SerialService`, closes it, and returns the pre-close information for adapter
results.

## Boundary rules

Application owns orchestration, not process lifetime, stdin/stdout, flags, ANSI, MCP schemas, or
concrete UI behavior. New CLI or protocol adapters should call this layer instead of looking up
Session or Serial Transport objects directly.
