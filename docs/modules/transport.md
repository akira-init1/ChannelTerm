# Channel and Transport Modules

## Channel contract

`internal/core/channel.Channel` is an established protocol-neutral bidirectional byte stream:

```go
type Channel interface {
    Read(p []byte) (int, error)
    Write(p []byte) (int, error)
    Close() error
    State() State
}
```

A Channel owns its live stream resource, not retained history. `Read` and `Write` follow Go
reader/writer contracts, caller buffers are not retained, and `Close` is repeatable. State reports
`open`, `closing`, `closed`, or `failed` as a point-in-time lifecycle snapshot.

`channel.Stream` adapts an already-open `io.ReadWriteCloser` to this contract. Optional capabilities
are separate interfaces; currently `channel.Resizer` expresses terminal dimensions without forcing
file, debug, JTAG, or remote raw streams to implement terminal behavior.

## Transport contract

`internal/core/transport.Transport` establishes a protocol-specific Channel:

```go
type Transport interface {
    Connect(ctx context.Context) (channel.Channel, error)
}
```

A Transport owns partially acquired resources during connection. `Connect` receives
cancellation/deadline context, releases partial resources on error, and transfers a successful live
resource to the returned Channel. Session then supplies one continuous Channel reader, serializes
complete write payloads at the byte boundary, and retains output.

## Serial implementation

`internal/core/transport/serial` uses `go.bug.st/serial`
to enumerate and open operating-system endpoints.

Validated configuration includes port, positive baud rate, 5-8 data bits, supported parity,
supported stop bits, and flow control. Empty optional values normalize to 8-N-1 with no flow
control. `software` enables native bidirectional XON/XOFF (DC1/DC3); `hardware` enables RTS/CTS.
The pinned [serial patch](../../third_party/serial/CHANNELTERM.md) applies Windows DCB and Unix
termios flags. Driver rejection is returned to the caller; framing and physical wiring must match
the peer. Software flow control consumes control bytes and is not transparent to raw binary data.

Serial `Connect` checks context before and immediately after the operating-system open. The
library's blocking open itself cannot be interrupted after it starts; if cancellation is detected
after success, ChannelTerm closes the acquired port before returning.

Each successful Serial `Connect` wraps the opened port in `channel.Stream` and transfers ownership.
Channel `Read` and `Write` delegate to the driver. Channel `Close` closes the driver resource
exactly once, causes later operations to fail immediately, and unblocks an active read through the
driver. A Serial Transport can open another independent Channel after the first closes, although
normal ChannelTerm Session workflows create a new Transport for a new lifecycle.

Unix writes use nonblocking syscalls and the existing close-signal pipe so Close can release
a flow-stopped writer without resuming transmission. The fd stays owned until active I/O has
left; partial writes report their committed prefix and are never replayed automatically.

Serial Channels do not implement `channel.Resizer` because physical serial ports have no terminal
dimensions. Transport connection itself never sends wake or line-ending bytes; Application sends an
explicitly enabled wake byte through Session after connection.

## Enumeration and metadata

`ListPorts` returns unchanged endpoint names plus best-effort metadata. Windows enriches through
SetupAPI; Linux reads sysfs; the current CGO-free macOS build reports endpoints without USB
metadata. Missing metadata does not remove a valid endpoint.

Open failures retain the driver error while adding a diagnostic category for missing device,
permission denial, busy port, or unknown failure. Windows access-denied errors are categorized as
likely busy; Linux/macOS permission errors include access guidance.

## SSH implementation

`internal/transport/ssh` keeps `golang.org/x/crypto/ssh` private to its implementation,
just as Serial hides its driver. Its exported `Config` contains ordinary values: username, host,
port, optional private-key and required `known_hosts` paths, connection timeout, PTY settings, and
context-aware password/unknown-host confirmation callbacks. Defaults are
port 22, a ten-second setup timeout, `xterm-256color`, and 80 columns by 24 rows.

`New` validates settings, snapshots selected private keys, and loads the trusted host-key
database without networking. A key or password provider is required. A missing trust file is allowed
only with a confirmation provider; malformed files and explicitly selected missing keys fail. The
Transport retains a credential/trust snapshot; changed files require a new Transport.

`PrivateKeyPath` precedes ordered `PrivateKeyPaths`; duplicate path strings are ignored. Unencrypted
keys are parsed at construction; encrypted bytes remain encrypted until authentication after host
verification. `KeyPassphrase` is cancellable, pauses the setup network budget, and allows three
incorrect-passphrase attempts. Returned buffers are cleared. OpenSSH keys expose a public portion,
so a connection-local lazy signer prompts only after server acceptance; legacy encrypted PEM keys
must be unlocked to discover that portion. RSA SHA-2 negotiation is preserved. Decrypted signers
are connection-local, never cached on Transport or persisted. Rejected candidates advance in order;
configuration/decryption errors abort instead of silently skipping an identity.
Candidates are prepared one at a time, so an unused legacy PEM key cannot prompt before an earlier
key succeeds. A local passphrase error cancels setup even when the server also offers password
authentication. Server rejection still permits the next candidate or password fallback. The backend
bounds the total authentication-method attempts to 64; the server may impose a smaller limit.

Transport always verifies the server before password authentication. It asks the adapter to confirm
only an unknown key, passing a plain `HostKey` value containing address, algorithm, and SHA256
fingerprint. Changed/revoked keys never reach confirmation. An approved unknown key is rechecked
under a cancellable interprocess file lock and appended to the OpenSSH trust database before
authentication continues. Persistence errors abort setup. Verified identity is pinned to the live
connection for rekeying without prompts after setup. Adapters cannot replace key material or bypass
verification. The CLI owns presentation; future GUI adapters can supply the same callbacks.

Shell and dedicated file connections prefer raw host-key algorithms matching the loaded
`known_hosts` records before other raw algorithms. Matching reuses the verifier's hostname,
port, hashed-name, wildcard, and exclusion rules; RSA records also select RSA SHA-2 signatures.
Certificate algorithms retain priority only when a matching `@cert-authority` record exists;
otherwise all raw algorithms precede certificates, including for unknown hosts, so a server
offering both can reach explicit first-use confirmation of its raw key. Authority markers are cached
at construction, and a trusted CA's key type does not restrict the certified host's key type.
Certificate-only servers still require a trusted matching CA and valid certificate; they do not
enter raw-key confirmation. This ordering only guides negotiation: the selected key or certificate
still passes the original verifier, and changed or revoked keys cannot trigger a replacement prompt
or password authentication.

Authentication tries a configured key before an optional password provider. Password authentication
has at most three attempts. Callback-owned byte buffers transfer to Transport and are cleared after
conversion to the backend's string API; Go/backend string copies cannot be guaranteed erased.
Passwords are not persisted, logged, or added to Session metadata. Callbacks must honor their context
and return promptly after cancellation. No keyboard-interactive provider is installed.

Backend client, signer, SSH session, and host-key callback types never appear in exported
configuration or method signatures. `Connect` returns only the existing `channel.Channel` contract.
Channel and Session do not depend on the SSH library, so replacing the backend stays within the
SSH Transport implementation without changing their contracts.

`Connect` owns TCP dialing, SSH authentication, opening a session channel, requesting a PTY,
and requesting an interactive shell. The caller's context and configured timeout cover all these
stages, except that the network-time budget pauses during interaction callbacks. The caller's
cancellation/deadline is never paused. Callbacks run synchronously without detached prompt workers.
Cancellation closes the socket to interrupt operations that lack context parameters.
Every failure releases the socket and any output pipes. After a successful handoff, cancelling
the setup context does not close the established Channel.

The returned private SSH Channel embeds `channel.Stream`, which delegates `Read`, `Write`, and
`Close` to a private SSH stream. It also implements the existing optional `channel.Resizer`.
Reads merge stdout and stderr through an unbuffered pipe; there is no cross-stream ordering
promise, though ordinary PTY output uses stdout. Writes go to SSH stdin. Session still owns the
only continuous Channel reader and retained terminal history. Concurrent readers use Session
cursors, not SSH readers. Close releases the socket and pipes before joining output workers, so
remote flow control cannot prevent shutdown. The existing Channel wrapper makes Close repeatable.
Remote shell EOF or a connection failure reaches the existing Session lifecycle/reaping path.
FIN/EOF and TCP-reset tests verify that blocked readers end and Manager closes/unregisters the
Session. Reconnection creates a fresh Channel; the Transport neither reconnects automatically
nor replays writes. Setup timeout does not limit an established shell. Optional
`KeepAliveInterval` probes run after successful setup, on shell and dedicated file connections.
Zero disables probes. `KeepAliveCountMax` defaults to 3; one outstanding authenticated global
request has a response deadline of interval times count. Negative replies also prove liveness.
The first probe is sent after one interval; subsequent probes never overlap. Expiry records
`ErrKeepAliveTimeout` and closes the socket. Close joins the probe and supervisor workers.
Optional `WriteTimeout` bounds each shell Write/Resize, including SSH channel flow control;
zero disables it. Expiry records `ErrWriteTimeout` and closes the entire connection because a
partial write cannot be safely replayed. It does not impose a deadline on file payload operations;
those use their owning context. Timer callbacks are joined before a write returns.
Repeated-close tests also check for retained SSH workers after server and Channel cleanup.

`Resize(cols, rows)` accepts nonzero character dimensions up to 65535 and calls the existing
backend shell's `WindowChange(rows, cols)`. It never opens another SSH session, shell, or connection.
Requests use SSH `window-change` without a server acknowledgement, so success means local request
submission, not proof that the server applied the dimensions. A closed/closing Channel returns
`channel.ErrNotOpen`; concurrent closure can also produce an SSH I/O error. No lifecycle lock is
held during the network operation: closing the Channel releases a blocked Resize. A caller that
owns a resize worker must cancel its notifications, close the Channel, and then join the worker.
The CLI's owner-only control policy is described in the [SSH workflow](../getting-started/ssh-terminal.md).

Each Connect can establish an independent Channel. Sharing and reuse instead happen in
`Application.OpenSSH`, keyed by exact username, host, and port. This release does not implement
ssh-agent, ssh_config, keyboard-interactive authentication, jump
hosts, or arbitrary SSH exec.

`StageError` identifies identity, DNS, connection, host-key, authentication, handshake, shell,
subsystem, transfer, or liveness failure while unwrapping the original cause. Backend-specific
authentication rejection detection stays inside Transport. Watchdog failures do not masquerade as
normal EOF in the Session read path. No credentials are added to diagnostics.

### Dedicated file clients

`OpenFiles(ctx, "sftp"|"scp")` reuses the same private authenticated setup as `Connect`, then returns
an owned `Files` client instead of a terminal Channel. SFTP requests the `sftp` subsystem with no PTY.
SCP opens a non-PTY exec channel for a fixed, quoted `scp -t` or `scp -f` invocation on a POSIX server.
It is the legacy SCP wire protocol, not a wrapper around the local OpenSSH executable. Neither path
requires a local `ssh`, `scp`, or `sftp` executable; the client build remains `CGO_ENABLED=0` portable.

Unlike a terminal Channel, the file client's opening context owns its complete lifetime. Cancelling
it closes the dedicated SSH connection to interrupt subsystem negotiation, reads, writes, or waits.
`Close` is idempotent and may run concurrently with I/O; other operations are caller-serialized.
SFTP protocol types remain private. The exported surface uses file metadata, streams, filesystem
sources, and validated transfer entries. Application owns actual local destination creation.

SFTP attribute decoding rejects an extended-attribute count that cannot fit in the received packet
before allocating its array. A trusted SSH host key authenticates the peer; it does not make malformed
file-protocol metadata safe. Loopback wire tests cover this allocation boundary independently of
dependency vulnerability-database coverage.
Remote file sizes that overflow the signed size representation are rejected at metadata conversion,
before Application opens a download destination; malformed metadata cannot trigger local truncation.

SCP control records are limited to 4096 bytes; malformed mode/size, traversal or unexpected root names,
duplicate paths, unsupported records, excessive depth, and truncated payloads abort the connection.
Remote paths are single-quoted, with no glob or tilde expansion. A no-overwrite upload adds a remote
existence check; the legacy protocol cannot make that check atomic against concurrent remote writers.
Prefer SFTP for stronger exclusive file-creation behavior. A server's normal channel EOF after the
last download acknowledgement is accepted only when framing completed and the SSH exit status succeeds.
SCP upload progress starts at zero for each regular file, including empty files, then reports bytes
accepted by the stream. A callback error aborts the connection. The final SCP acknowledgement still
precedes file completion; progress is not a disk-durability or success notification. An optional
upload verification callback receives the payload SHA-256 after acknowledgement and must succeed
before the file is counted. No payload hashing is required when the callback is nil.
`DownloadSCP` returns the number of regular files whose receive callback and final acknowledgements
succeeded, followed by its optional verification callback. A sender error, missing acknowledgement,
failed verification, or failed local acknowledgement does not count
that file. Earlier completed files remain counted if a later entry or remote command exit fails.

SCP verification uses additional non-PTY exec channels on the same dedicated connection.
`RequireSHA256` checks `command -v sha256sum` before copying. `SCPUploadDestination` resolves the
existing-directory rule with a fixed quoted `test -d` command, preserving literal dot components.
`SHA256` executes only `sha256sum < quoted-path`, validates a 64-hex-digit stdin digest, and fails
on command errors or malformed output. Stdout and stderr retention is bounded to 4096 bytes;
oversized stdout fails, and diagnostics are quoted. These methods accept no arbitrary command,
and the connection context interrupts their setup, reads, and exit waits. They are unavailable on
SFTP clients: SFTP verification uses ordinary file reads and needs no exec permission. Application
owns comparisons and replacement policy. Concurrent filesystem changes remain outside the contract.

SFTP rename replacement requires the server's POSIX rename extension. There is no destructive
delete-then-rename fallback. `Files.OpenResume` opens an existing regular file read/write without
truncation, or creates a new file exclusively, and returns the opened handle's length. Application
owns prefix comparison, offsets, progress and final verification; Transport owns no resume jobs.
Concurrent remote writers are not excluded by this handle. SFTP create-without-overwrite uses
exclusive creation. Filesystem
preflight checks do not constitute locking against unrelated local or remote writers.
`RequirePOSIXRename` checks the advertised extension before Application stages a safe upload.

## Current implementation boundary

Serial and SSH PTY shells are the concrete Transports. There is no dedicated file-transfer
Transport, debug/JTAG Transport, local PTY Transport, Android implementation, or iOS implementation.

Future Transport directions and the distinction between current and planned behavior are recorded in
the [Architecture Overview](../architecture/overview.md). The `file send/receive` workflow remains an
Application-layer protocol over an existing Session. Dedicated `sftp/scp` operations use the SSH
backend's file client; neither workflow adds a generic file Transport or terminal Channel.
