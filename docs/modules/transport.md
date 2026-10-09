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
control. Although `software` and `hardware` are stable accepted names, the current backend cannot
configure portable XON/XOFF or RTS/CTS and returns `ErrFlowControlUnsupported` for both.

Serial `Connect` checks context before and immediately after the operating-system open. The
library's blocking open itself cannot be interrupted after it starts; if cancellation is detected
after success, ChannelTerm closes the acquired port before returning.

Each successful Serial `Connect` wraps the opened port in `channel.Stream` and transfers ownership.
Channel `Read` and `Write` delegate to the driver. Channel `Close` closes the driver resource
exactly once, causes later operations to fail immediately, and unblocks an active read through the
driver. A Serial Transport can open another independent Channel after the first closes, although
normal ChannelTerm Session workflows create a new Transport for a new lifecycle.

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
port, private-key and `known_hosts` paths, connection timeout, and PTY settings. Defaults are
port 22, a ten-second setup timeout, `xterm-256color`, and 80 columns by 24 rows.

`New` validates settings, reads and parses the unencrypted private key, and loads the trusted
host-key database without opening a network connection. Missing or malformed files fail before
connection. The Transport retains a private snapshot; changed files require a new Transport.
The CLI resolves default paths but does not parse SSH keys or construct SSH-library objects.
Host verification is mandatory; callers cannot inject a callback that disables it.

Backend client, signer, SSH session, and host-key callback types never appear in exported
configuration or method signatures. `Connect` returns only the existing `channel.Channel` contract.
Channel and Session do not depend on the SSH library, so replacing the backend stays within the
SSH Transport implementation without changing their contracts.

`Connect` owns TCP dialing, SSH authentication, opening a session channel, requesting a PTY,
and requesting an interactive shell. The caller's context and configured timeout cover all these
stages. Cancellation closes the socket to interrupt operations that lack context parameters.
Every failure releases the socket and any output pipes. After a successful handoff, cancelling
the setup context does not close the established Channel.

The returned `channel.Stream` delegates `Read`, `Write`, and `Close` to a private SSH stream.
Reads merge stdout and stderr through an unbuffered pipe; there is no cross-stream ordering
promise, though ordinary PTY output uses stdout. Writes go to SSH stdin. Session still owns the
only continuous Channel reader and retained terminal history. Concurrent readers use Session
cursors, not SSH readers. Close releases the socket and pipes before joining output workers, so
remote flow control cannot prevent shutdown. The existing Channel wrapper makes Close repeatable.
Remote shell EOF or a connection failure reaches the existing Session lifecycle/reaping path.
FIN/EOF and TCP-reset tests verify that blocked readers end and Manager closes/unregisters the
Session. Reconnection creates a fresh Channel; the Transport neither reconnects automatically
nor replays writes. A silent packet blackhole does not impose a new post-connect deadline:
Close still releases blocked I/O, while setup timeout does not limit an established shell.
Repeated-close tests also check for retained SSH workers after server and Channel cleanup.

Each Connect can establish an independent Channel. Sharing and reuse instead happen in
`Application.OpenSSH`, keyed by exact username, host, and port. This release does not implement
Channel resizing, password authentication, encrypted private keys, ssh-agent, ssh_config, jump
hosts, SSH exec, or SFTP.

## Current implementation boundary

Serial and SSH PTY shells are the concrete Transports. There is no dedicated file-transfer
Transport, debug/JTAG Transport, local PTY Transport, Android implementation, or iOS implementation.

Future Transport directions and the distinction between current and planned behavior are recorded in
the [Architecture Overview](../architecture/overview.md). The current CLI file transfer remains an
Application-layer protocol over an existing serial Session; it is not another Transport.
