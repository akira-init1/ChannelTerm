# Buffers and Cursors

Session maintains two bounded, independent buffers: raw Channel output and write activity. Device Registry maintains a third bounded stream for discovery events.

## Output receive buffer

- Default capacity: 16 MiB (`DefaultReceiveBufferSize`).
- Default initial AI/MCP recent-read limit: 64 KiB (`DefaultAIReadLimit`).
- Storage: fixed-capacity byte ring allocated once for the Session lifetime.
- Overflow: overwrite the oldest bytes; an append larger than capacity retains only its newest capacity bytes.

`OutputCursor` is an absolute next-byte position, not an index into the current ring. `ReadOutput` waits for bytes at a cursor and returns at most the requested limit. If the cursor is older than retained data, the read begins at the oldest retained byte and sets `Dropped`.

`ReadRecent` returns a bounded copy of the newest bytes without waiting. Returned chunks never alias the mutable backing buffer. A non-positive read limit is an error.

## Activity buffer

- Default capacity: 1024 `SessionEvent` values.
- Overflow: overwrite the oldest event without blocking `Session.Write`.
- Payload ownership: event data is copied on append and copied again for readers.

`ActivityCursor` is independent from `OutputCursor`. Recent activity returns a continuation cursor at the current tail, including when the event list is empty.

## Device event buffer

Device Registry retains 1024 appearance/disappearance events. It uses its own `device.Cursor` and the same high-level `Next`/`Dropped` continuation pattern.

## Concurrency and close

Each buffer replaces a notification channel whenever data or lifecycle state changes. Readers inspect state under a short mutex and wait without holding it. Slow or blocked consumers therefore do not apply backpressure to Channel reads, Session writes, or device scans.

Closing wakes current waiters after retained data has been consumed. Session close then releases its output and activity backing storage. These buffers are retention windows, not persistent logs; cursor overflow is permanent data loss for that consumer.
