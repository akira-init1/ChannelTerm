# Session Module

`internal/core/session` owns shared Session lifecycle above a protocol-neutral Channel, the single
Channel reader, retained output, retained write activity, retained structured events, and Manager
registration.

Application operations can publish the generic `OPERATION_STATE` event with an operation ID,
kind, status, and output cursor. Session stores it without interpreting bootloader protocols.
U-Boot matching and diagnostic ZIP retention remain Application responsibilities. Output retains
raw bytes and independent cursors; no terminal escape filtering or receive timestamps are added.

## Lifecycle

```text
new --> connecting --> open --> closing --> closed
          |              |
          +----error-----+--> failed
```

`Connect` may run only from `new`. It checks and passes the context to Transport connection, then
accepts ownership of the returned Channel. A cancelled or failed connection, or a successful
Transport result with no Channel, enters `failed` and closes the output, activity, and event buffers.

Only `open` permits read, write, activity read, event read, and resize operations. `Close` wakes
waiting readers, closes the Channel to unblock active I/O, waits for the reader goroutine, releases
buffer memory, and enters `closed`. Sequential repeated closes return the first close result. The
caller must not run `Connect` concurrently with `Close`.

Application may deliberately remove and close a Manager-owned Session when an application lease
expires during an in-flight Channel write, a file-transfer lease expires even without an active
write, or bounded file-transfer recovery times out. Application prevents new writes before
asynchronous expiry closure. This uses the existing `Close` contract to interrupt an otherwise
non-cancellable Channel write; it does
not add context or deadline semantics to the protocol-neutral Channel interface. Closure stops
local access; it does not roll back delivered bytes or establish remote program/TTY recovery.

An unexpected reader error enters `failed`. EOF is treated as an output end condition. Received
bytes returned with an error are appended before the error is handled. A Manager-owned Session then
notifies its Manager, which removes that exact Session instance and closes it to release the Channel
and all retained buffers. The terminal state after cleanup is `closed`; waiting readers still
receive the original reader error recorded before cleanup.

## Ownership and concurrency

A Session keeps its Transport only for connection establishment and owns the returned Channel until
close. Exactly one reader goroutine calls `Channel.Read` and appends bytes to the receive buffer.
Session never delegates Channel reads to consumers.

Writes use a separate mutex across the complete short-write retry loop. Concurrent callers therefore
cannot interleave their payload bytes. `Close` does not wait for that write mutex; closing the
Channel is responsible for releasing an in-progress blocking write and avoids a lock cycle.

This serialization protects byte-level write boundaries only. Session does not infer writer intent
and does not provide writer ownership, an exclusive lease, a transaction, priority, arbitration, or
shell-state coordination. Clients that need semantic coordination must currently arrange it outside
Session.

## Activity

Every confirmed write records a `SessionEvent` with timestamp, actor (`user`, `agent`, or `system`),
operation (`write`), and a copied payload. A partial write followed by an error records only the
confirmed prefix; a zero-byte failure records nothing. Activity metadata is never forwarded to
Channel.

Retention bounds both event count and combined payload size. The oldest complete events are evicted
before either limit is exceeded; an oversized event is not retained, and older cursors report
`Dropped`. Capacities and cursor details are owned by the [activity buffer contract](buffer.md#activity-buffer).

Activity and remote output are independent cursor streams. Reading one cannot consume or modify the
other.

## Structured event stream

The Event Stream is a third, independent bounded cursor stream. It retains `Event` values with a
per-Session monotonic `id`, timestamp, Session ID, type, actor, and JSON-compatible metadata. It
does not contain terminal bytes or activity write payloads.

The current event types are `SESSION_CREATED`, `SESSION_ATTACHED`, `SESSION_DETACHED`,
`LEASE_ACQUIRED`, `LEASE_RELEASED`, `FILE_TRANSFER_STARTED`, `FILE_TRANSFER_PROGRESS`,
`FILE_TRANSFER_COMPLETED`, `FILE_TRANSFER_CANCELLED`, `FILE_TRANSFER_FAILED`,
`TERMINAL_COMMAND_STARTED`, `TERMINAL_COMMAND_OUTPUT_STARTED`, `TERMINAL_COMMAND_COMPLETED`, and
`TERMINAL_COMMAND_FAILED`, `DEBUG_STATE`, and `DEBUG_CLIENT_REPORT`. Managed MI transitions carry
only `debug_id` and `state`; external bridge transitions carry only `bridge_id` and `state`.
Neither includes credentials. `DEBUG_CLIENT_REPORT` carries explicitly selected external-debugger
text, not raw Session bytes: bridge ID, report sequence, kind, text (at most 4096 UTF-8 bytes),
`reported_by: codex`, and `complete_audit: false`. Application validates the report owner and
known capabilities; Session stores it as ordinary event metadata. Reporting never changes target
state or renews a lease. Reports share the bounded event history and are not a persistent audit.
One file transfer's lease acquire/release and status events all carry the
same process-local `transfer_id`. `file-transfer` and `debug` lease events also include an `output_cursor`
snapshot used only by attach presentation to suppress the corresponding internal raw-output range.

One isolated Agent command carries the same non-secret `command_id` across its command events.
Command metadata records the original display command, bootstrap/output cursor boundary, exit code,
or failure text. Internal shell wrapper bytes remain raw Session output and `system` write activity;
the event cursor metadata lets presentation suppress only that wrapper without changing Session
storage. Lease owner capabilities are never event metadata.

Event metadata for file progress includes confirmed byte counts, total size, percent, and
best-effort speed. A user-confirmed cancellation is published after lease release with last
confirmed progress, `reason: user_cancelled`, and `lease_released: true`. Failure metadata retains
the last confirmed progress, and successful regular-file completion includes the verified
`local_sha256` and `remote_sha256` values. File-transfer metadata never exposes a lease owner
capability.

`ReadEvents` and `ReadRecentEvents` give every observer an independent event cursor. The
[event buffer](buffer.md#structured-session-event-buffer) overwrites old events for slow observers
and reports `Dropped`.
Publishing does not send on observer channels, so it cannot block Channel reads, Session writes, or
another observer.

## Optional stream capabilities

Read, write, close, and lifecycle state form the complete base Channel contract. `Resize` remains on
Session for current terminal compatibility, but Session invokes it only when the established Channel
implements the optional `channel.Resizer` capability. File, debug/JTAG, and other non-terminal
Channels therefore do not need a meaningless resize method.

## Manager

`Manager` owns registered Sessions and fixed `SessionMetadata` (`Transport`, `Endpoint`, `Label`,
`Reference`). Labels may be empty or duplicate. References are allocated from a per-transport
monotonically increasing counter and are not reused during Manager lifetime.

Lookup and removal accept the opaque Session ID or short reference. `Remove` transfers ownership
without closing; its caller must close the returned Session. `Close` removes and attempts to close
every current registration, joining cleanup errors after all Sessions have been processed. Shutdown
is irreversible: it first rejects new registration, waits for every in-progress `GetOrCreate`
attempt, and closes any candidate that finishes after shutdown began. Repeated or concurrent closes
cannot acquire ownership of the same Session twice.

`GetOrCreate` reserves an endpoint while one connection attempt is in progress. Within one Manager,
it returns the existing active Session for the same exact `transport + endpoint` pair. `new`,
`connecting`, `open`, and `closing` Sessions still own an endpoint. `failed` and `closed`
registrations do not prevent a later attempt.

Manager observes terminal lifecycle completion from the default Session Core. When a registered Core
reaches a terminal state because its Channel reader returns an error or EOF, Manager atomically
removes that same instance and calls `Close`. This prevents failed Sessions from retaining the
Channel, receive buffer, activity buffer, event buffer, or a stale list entry. An instance check
prevents a delayed completion signal from removing a newer Session that reuses the same ID.

After a successful registration, Manager publishes `SESSION_CREATED` with display metadata.
Registration remains complete even if an observer is slow.

## Conservative input summary

`Core.HasPendingInput` is an optional consumer capability above the unchanged Session interface.
It summarizes confirmed non-system writes while holding the write mutex, independently of bounded
activity retention. CR, LF, Ctrl+C, and Ctrl+U clear pending input; other non-backspace bytes mark
it pending, and backspace cannot prove the line is empty. Internal system protocol bytes leave
this summary unchanged. It supports safe debug takeover; it does not establish remote shell state,
parse escape sequences, or change retained bytes. Debug lease coordination belongs to Application.
