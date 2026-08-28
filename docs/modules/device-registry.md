# Device Registry Module

`internal/core/device` tracks local endpoint presence separately from terminal Sessions. Discovery never opens an endpoint or writes device data.

## Scanning and events

A `Scanner` returns runtime `Endpoint` values containing transport, endpoint text, and optional serial metadata. `Registry.Start` performs one required initial scan. That scan establishes a baseline: devices are listed as present but do not emit `appeared` events.

Later successful scans compare exact `transport + endpoint` keys:

- a new or reappearing endpoint emits `appeared`;
- a previously present missing endpoint emits `disappeared`;
- an unchanged endpoint emits no event.

A failed scan does not modify presence, preventing false disappearances during transient enumeration failures. Production scanning runs once per second. Events are retained in a fixed 1024-entry buffer with cursor/drop semantics.

`List` returns only present devices, sorted by transport and endpoint. Disappeared records remain internal so a reappearance can be recognized.

## Identity state

The optional `StateStore` assigns `device_id` values independently from endpoint presence. Persistent matching is conservative:

1. `usb_serial`: transport, VID, PID, and USB serial.
2. `usb_path`: transport, VID, PID, and USB path only when serial is absent.
3. `runtime`: insufficient or ambiguous evidence; process-local only.

Manufacturer and product are diagnostic snapshots, not identity evidence. Endpoint names and `last_endpoint` never participate in matching. Duplicate durable matches, or two simultaneous endpoints claiming one stored record, fall back to distinct runtime identities instead of choosing arbitrarily.

Persistent IDs are randomly allocated and state updates atomically replace version-1 JSON. If persistence fails, the in-memory change is rolled back and the scan fails rather than exposing an ID that cannot survive restart.

## Lifecycle

The composition root owns Registry start and close. Cancelling its context stops periodic scanning; `Close` is idempotent and wakes event readers. `Application` and MCP device tools only borrow the Registry.

Device presence, DeviceID persistence, Session ownership, and connection policy are separate concepts. A present device can have no Session, a Session can outlive a disappearance until its Transport reports failure, and a `device_id` cannot address Session tools.
