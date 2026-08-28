# Identifiers and References

ChannelTerm exposes several identifiers with different ownership and lifetimes. They are not interchangeable.

| Value | Identifies | Created by | Lifetime | Example |
| --- | --- | --- | --- | --- |
| Local target reference | A currently discoverable endpoint on this computer | CLI `list` formatting | Re-resolved against current discovery on each use | `SER-COM8` |
| Shared Session reference | A Manager-owned live Session | Session Manager | One Session Host process; never reused by that Manager | `SER-1` |
| Opaque `session_id` | The canonical Manager registration key for a live Session | Application cryptographic random ID generator | One Session registration; lost when removed or the host exits | `0123456789abcdef0123456789abcdef` |
| `device_id` | A Device Registry identity record | Device State Store | Persistent only when reliable USB evidence is available; otherwise process-local | `dev_0123456789abcdef0123456789abcdef` |

## Local target reference

For serial, the CLI constructs `SER-` plus the uppercase endpoint text. `COM8` becomes `SER-COM8`; `/dev/ttyUSB0` becomes `SER-/DEV/TTYUSB0`.

A target reference asks ChannelTerm to discover and open an endpoint. It is not a Session. `connect` and target-based `attach` check that the referenced port is currently present before opening it.

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

## Correct usage

```text
SER-COM8  -> resolve/open local serial target
SER-1     -> read/write/attach/close existing shared Session
session_id-> canonical programmatic address of that Session
device_id -> correlate Device Registry identity records
```

Do not pass `device_id` to Session operations. Do not pass `SER-COM8` to a tool expecting an existing Session. Do not infer a target reference or endpoint from an opaque Session or device ID.
