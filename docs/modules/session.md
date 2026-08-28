# Session Module

`internal/core/session` owns protocol-neutral terminal lifecycle, the single Transport reader, retained output, retained write activity, and Manager registration.

## Lifecycle

```text
new --> connecting --> open --> closing --> closed
          |              |
          +----error-----+--> failed
```

`Connect` may run only from `new`. It checks and passes the context to Transport connection. A cancelled or failed connection enters `failed` and closes both reader-facing buffers.

Only `open` permits read, write, activity read, and resize operations. `Close` wakes waiting readers, closes the Transport to unblock active I/O, waits for the reader goroutine, releases buffer memory, and enters `closed`. Sequential repeated closes return the first close result. The caller must not run `Connect` concurrently with `Close`.

An unexpected reader error enters `failed`. EOF is treated as an output end condition. Received bytes returned with an error are appended before the error is handled.

## Ownership and concurrency

A Session owns its Transport from successful construction until close. Exactly one reader goroutine calls `Transport.Read` and appends bytes to the receive buffer. Session never delegates Transport reads to consumers.

Writes use a separate mutex across the complete short-write retry loop. Concurrent callers therefore cannot interleave their payload bytes. `Close` does not wait for that write mutex; closing the Transport is responsible for releasing an in-progress blocking write and avoids a lock cycle.

This serialization protects byte-level write boundaries only. Session does not infer writer intent and does not provide writer ownership, an exclusive lease, a transaction, priority, arbitration, or shell-state coordination. Clients that need semantic coordination must currently arrange it outside Session.

## Activity

Every confirmed write records a `SessionEvent` with timestamp, actor (`user`, `agent`, or `system`), operation (`write`), and a copied payload. A partial write followed by an error records only the confirmed prefix; a zero-byte failure records nothing. Activity metadata is never forwarded to Transport.

Activity and remote output are independent cursor streams. Reading one cannot consume or modify the other.

## Manager

`Manager` owns registered Sessions and fixed `SessionMetadata` (`Transport`, `Endpoint`, `Label`, `Reference`). Labels may be empty or duplicate. References are allocated from a per-transport monotonically increasing counter and are not reused during Manager lifetime.

Lookup and removal accept the opaque Session ID or short reference. `Remove` transfers ownership without closing; its caller must close the returned Session. `Close` removes and attempts to close every current registration, joining cleanup errors after all Sessions have been processed.

`GetOrCreate` reserves an endpoint while one connection attempt is in progress. Within one Manager, it returns the existing active Session for the same exact `transport + endpoint` pair. `new`, `connecting`, `open`, and `closing` Sessions still own an endpoint. `failed` and `closed` registrations do not prevent a later attempt.
