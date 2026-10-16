# Buffers and Cursors

Session maintains three bounded, independent buffers: raw Channel output, write activity, and
structured Session events. Device Registry maintains a separate stream for discovery events.

## Output receive buffer

- Default capacity: 16 MiB (`DefaultReceiveBufferSize`).
- Default initial AI/MCP recent-read limit: 64 KiB (`DefaultAIReadLimit`).
- Storage: fixed-capacity byte ring allocated once for the Session lifetime.
- Overflow: overwrite the oldest bytes; an append larger than capacity retains only its newest
  capacity bytes.

`OutputCursor` is an absolute next-byte position, not an index into the current ring. `ReadOutput`
waits for bytes at a cursor and returns at most the requested limit. If the cursor is older than
retained data, the read begins at the oldest retained byte and sets `Dropped`.

`ReadRecent` returns a bounded copy of the newest bytes without waiting. Returned chunks never alias
the mutable backing buffer. A non-positive read limit is an error.

## Activity buffer

- Default capacity: 1024 `SessionEvent` values.
- Default payload capacity: 16 MiB across all retained events.
- Overflow: evict the oldest complete events before either the event or payload limit is exceeded,
  without blocking `Session.Write`. A single payload larger than 16 MiB is not retained.
- Payload ownership: event data is copied on append and copied again for readers.

`ActivityCursor` is independent from `OutputCursor`. Recent activity returns a continuation cursor
at the current tail, including when the event list is empty. A read whose requested events were
evicted, including an oversized event that was never retained, advances to the oldest available
cursor and sets `Dropped`.

## Structured Session event buffer

- Default capacity: 1024 `Event` values (`DefaultEventBufferCapacity`).
- Overflow: overwrite the oldest complete event without blocking Session I/O or other observers.
- Ownership: events and supported metadata values are copied for storage and for readers.

`EventCursor` is independent from output and activity cursors. `ReadEvents` waits for events at
the supplied cursor; a cursor older than retained history advances to the oldest event with
`Dropped`. `ReadRecentEvents` returns the newest retained events and a continuation cursor at the
tail, including for an empty live buffer. Both require a positive event limit.

This stream carries lifecycle metadata rather than terminal output or write payloads. See
[Session events](session.md#structured-event-stream) for event types and publication semantics.

## Device event buffer

Device Registry retains 1024 appearance/disappearance events. It uses its own `device.Cursor` and
the same high-level `Next`/`Dropped` continuation pattern.

## Concurrency and close

Each buffer replaces a notification channel whenever data or lifecycle state changes. Readers
inspect state under a short mutex and wait without holding it. Slow or blocked consumers therefore
do not apply backpressure to Channel reads, Session writes, or device scans.

Closing a buffer wakes current waiters and lets them read retained data before returning the close
error. Session shutdown subsequently releases all three backing stores; it does not wait for every
consumer to drain history. These buffers are retention windows, not persistent logs; overwritten or
released history cannot be recovered by that consumer.
