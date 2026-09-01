# Channel, Transport, and Serial Modules

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

A Channel owns its live stream resource, not retained history. `Read` and `Write` follow Go reader/writer contracts, caller buffers are not retained, and `Close` is repeatable. State reports `open`, `closing`, `closed`, or `failed` as a point-in-time lifecycle snapshot.

`channel.Stream` adapts an already-open `io.ReadWriteCloser` to this contract. Optional capabilities are separate interfaces; currently `channel.Resizer` expresses terminal dimensions without forcing file, debug, JTAG, or remote raw streams to implement terminal behavior.

## Transport contract

`internal/core/transport.Transport` establishes a protocol-specific Channel:

```go
type Transport interface {
    Connect(ctx context.Context) (channel.Channel, error)
}
```

A Transport owns partially acquired resources during connection. `Connect` receives cancellation/deadline context, releases partial resources on error, and transfers a successful live resource to the returned Channel. Session then supplies one continuous Channel reader, serializes complete write payloads at the byte boundary, and retains output.

## Serial implementation

`internal/core/transport/serial` is the only current concrete Transport. It uses `go.bug.st/serial` to enumerate and open operating-system endpoints.

Validated configuration includes port, positive baud rate, 5-8 data bits, supported parity, supported stop bits, and flow control. Empty optional values normalize to 8-N-1 with no flow control. Although `software` and `hardware` are stable accepted names, the current backend cannot configure portable XON/XOFF or RTS/CTS and returns `ErrFlowControlUnsupported` for both.

Serial `Connect` checks context before and immediately after the operating-system open. The library's blocking open itself cannot be interrupted after it starts; if cancellation is detected after success, ChannelTerm closes the acquired port before returning.

Each successful Serial `Connect` wraps the opened port in `channel.Stream` and transfers ownership. Channel `Read` and `Write` delegate to the driver. Channel `Close` closes the driver resource exactly once, causes later operations to fail immediately, and unblocks an active read through the driver. A Serial Transport can open another independent Channel after the first closes, although normal ChannelTerm Session workflows create a new Transport for a new lifecycle.

Serial Channels do not implement `channel.Resizer` because physical serial ports have no terminal dimensions. Transport connection itself never sends wake or line-ending bytes; Application sends an explicitly enabled wake byte through Session after connection.

## Enumeration and metadata

`ListPorts` returns unchanged endpoint names plus best-effort metadata. Windows enriches through SetupAPI; Linux reads sysfs; the current CGO-free macOS build reports endpoints without USB metadata. Missing metadata does not remove a valid endpoint.

Open failures retain the driver error while adding a diagnostic category for missing device, permission denial, busy port, or unknown failure. Windows access-denied errors are categorized as likely busy; Linux/macOS permission errors include access guidance.

## Current implementation boundary

Serial is the only concrete Transport and serial-backed Channel in the current repository. There is no dedicated file-transfer Transport, debug/JTAG Transport, remote-network Transport, PTY Transport, Android implementation, or iOS implementation.

## Future direction

The base Channel interface can carry future debug/JTAG and remote-network streams while Session continues to provide the same lifecycle, buffering, sharing, and write serialization. Those stream types, SSH/Telnet Transports, OpenOCD integration, and a dedicated file Transport/Channel are not currently implemented or committed to a release. The current CLI file transfer stays above Session and drives native Linux shell commands over the existing serial byte stream; it does not change Channel or Transport.
