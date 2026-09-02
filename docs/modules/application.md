# Application Module

`internal/core/app` is ChannelTerm's UI- and protocol-neutral use-case layer. Adapters receive structured values and do not reproduce Session, configuration, or connection lifecycle rules.

`Application` is constructed with a required `*session.Manager`. A Device Registry is optional until a device or decision use case is called. The caller owns both long-lived resources: constructing or discarding an `Application` does not start/close the Registry or close the Manager.

## Use cases

- `OpenSerial`: load or create configuration, resolve a profile, apply explicit overrides, optionally save, connect or reuse a Session, optionally write one wake carriage return, and return Manager-owned metadata.
- `ListSessions`, `ReadSession`, `ReadSessionActivity`, `WriteSession`, `AcquireLease`, `ReleaseLease`, `LeaseStatus`, `CloseSession`: operate by opaque Session ID or short Session reference.
- `SendFile`, `ReceiveFile`: stream bounded chunks through a cursor-based Session view, coordinate the minimal Linux shell protocol, report acknowledged progress, and verify byte count and SHA-256 without owning local files or presentation.
- `ListSerialPorts`: enumerate ports without opening them or changing Registry state.
- `ListSerialProfiles`: read and resolve named profiles; a missing file is an empty list and is not created.
- `ResolveSerialTarget`: resolve a currently present `SER-*` target to the operating-system endpoint.
- `ListDevices`, `ReadDeviceEvents`: expose Registry snapshots and cursor streams.
- `ConnectionDecision`: combine exact device presence, exact active Session metadata, and policy without changing state.

## Serial open invariants

`SerialService` uses the Manager's `GetOrCreate` operation to share one active Session per exact `transport + endpoint` within that Manager. A Transport connection opens and transfers one Channel to Session. Opening can block, so Manager coordination occurs without holding its registration lock. Concurrent callers receive the first successful Session or the same opening error.

A Session candidate is connected before Manager registration. On construction, connection, cancellation, or wake failure, the service closes the unregistered candidate. A reused Session keeps the original profile, label, and Transport configuration.

Generated Session IDs use 16 random bytes encoded as hex. ID generation is retried a bounded number of times if it collides with an existing registration.

## Read and write behavior

A nil read cursor returns recent retained data. A supplied cursor waits through the Session. Application does not encode terminal bytes, add presentation, or merge output with activity.

`WriteSession` validates the actor and retries short writes while honoring context cancellation between retries. Session still provides lower-level byte-boundary write serialization and activity recording. `Application` additionally owns one exclusive lease per managed Session: `terminal`, `file-transfer`, and reserved `debug` types. A normal write fails immediately with `ErrSessionBusy` while a lease is active; `WriteSessionWithLease` requires the matching opaque owner capability. Acquiring a lease is atomic with a currently executing Application write, but unrelated Sessions remain independent. Readers, cursors, raw output, Channel, and Session internals are unaffected. See [Session](session.md).

File transfer uses the same Session write and output-cursor semantics. It obtains a `file-transfer` lease before its first protocol command and releases it on success, failure, or cancellation. It reads or writes at most 32 KiB of payload per chunk, saves and restores the remote TTY mode around each raw `dd` operation, and performs end-to-end SHA-256 verification. Independent readers continue to see raw output, including received file bytes.

`CloseSession` first resolves and snapshots Session metadata, removes the Session from Manager ownership through `SerialService`, closes it, and returns the pre-close information for adapter results.

## Boundary rules

Application owns orchestration, not process lifetime, stdin/stdout, flags, ANSI, MCP schemas, or concrete UI behavior. New CLI or protocol adapters should call this layer instead of looking up Session or Serial Transport objects directly.
