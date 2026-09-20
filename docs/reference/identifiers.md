# Identifiers and References

ChannelTerm exposes several identifiers with different ownership and lifetimes. They are not interchangeable.

| Value | Identifies | Created by | Lifetime | Example |
| --- | --- | --- | --- | --- |
| Native serial target | A currently discoverable endpoint on this computer | Operating system / serial discovery | Re-resolved against current discovery on each use | `COM8`, `/dev/ttyUSB0` |
| Shared Session reference | A Manager-owned live Session | Session Manager | One Session Host process; never reused by that Manager | `SER-1` |
| Opaque `session_id` | The canonical Manager registration key for a live Session | Application cryptographic random ID generator | One Session registration; lost when removed or the host exits | `0123456789abcdef0123456789abcdef` |
| `device_id` | A Device Registry identity record | Device State Store | Persistent only when reliable USB evidence is available; otherwise process-local | `dev_0123456789abcdef0123456789abcdef` |
| `transfer_id` | One file-transfer lease and its structured events | Session Host lease coordinator | One file-transfer lease within one Host process | `FT-123` |

## Native serial target

Serial targets use the operating-system endpoint returned by discovery without adding a ChannelTerm
prefix. Typical forms are `COM8` on Windows, `/dev/ttyUSB0` on Linux, and
`/dev/cu.usbserial-110` on macOS. Windows names are matched case-insensitively; Unix paths retain
case-sensitive filesystem semantics.

A native target asks ChannelTerm to discover and open an endpoint. It is not a Session. `connect`
and target-based `attach` check that the port is currently present before opening it. The former
generated forms such as `SER-COM8` and `SER-/DEV/TTYUSB0` are not device targets.

## Transport-extension boundary

`TARGET` is a transport-neutral CLI role, not a synonym for a serial port. The current release has
only one concrete Transport, so the only implemented target forms are native serial endpoints.
SSH and Telnet remain future directions; no SSH or Telnet target syntax is currently accepted.

Each future Transport must own an unambiguous target grammar and its discovery or resolution rules.
The CLI attach dispatcher can add another transport-specific target kind without changing the
meaning of existing values. In particular, short references of the form `<TRANSPORT>-<N>` remain
live Session references: `SER-1` is not a serial target, and a future `SSH-1` would identify a
Session rather than an SSH host. A new Transport must update `list`, command help, this document,
and focused classification tests in the same change; it must not silently reinterpret an existing
Session reference or native serial endpoint.

## Shared Session reference

The Manager allocates short references using the transport prefix and a monotonically increasing process-local counter. A serial Session normally receives `SER-1`, then `SER-2`, and so on. A reference is fixed at registration, is not reused during that Manager's lifetime, and disappears when the Session is removed or the host stops.

MCP tools that accept `session_id` resolve either the opaque ID or the short Session reference. CLI `attach` also accepts both. A Session label is display-only: it may be empty or duplicated and cannot be used as an identifier.

## Opaque `session_id`

Normal serial opens generate 16 cryptographically random bytes and encode them as 32 lowercase hexadecimal characters. Treat the value as opaque; its current representation is not a parsing contract. It is the canonical key returned in MCP results and the long CLI listing.

The ID identifies a Session, not a port or physical device. Reopening an endpoint after the old Session is removed creates a new ID.

## `device_id`

`terminal_list_devices` exposes `device_id` as public runtime behavior. It identifies the Device Registry's best match for a discovered adapter, not a live Session and not an endpoint name.

Persistent IDs use the `dev_` prefix plus a random 128-bit value and are recovered through state matching. The value is not a hash of VID, PID, serial number, path, or endpoint. A `runtime` identity also receives a random-looking `device_id`, but `persistent: false` means it is valid only for the current process.

`usb_path` evidence identifies a device class at one physical USB location; it is weaker than a device serial number and must not be described as proof of the same physical board after moving ports.

## `transfer_id`

The Session Host allocates a `transfer_id` when it acquires a `file-transfer` lease. The lease
acquire/release and every STARTED, PROGRESS, COMPLETED, FAILED, or CANCELLED event for that transfer
carry the same value. It is a correlation identifier, not the secret lease `owner` capability, and
is valid only for that Host process. Pass it together with the Session and an event cursor to
`terminal_wait_file_transfer`; do not use it as a Session ID or assume its current `FT-` formatting
is globally unique.

## Correct usage

```text
COM8 or /dev/ttyUSB0 -> resolve/open local serial target
SER-1                -> read/write/attach/close existing shared Session
session_id           -> canonical programmatic address of that Session
device_id            -> correlate Device Registry identity records
transfer_id -> correlate one file-transfer lease and outcome
```

Do not pass `device_id` to Session operations. Do not pass a native target such as `COM8` to a tool
expecting an existing Session. Do not infer a target or endpoint from an opaque Session or device
ID. `SER-1` and `SER-2` identify live shared Sessions, never physical serial ports.
