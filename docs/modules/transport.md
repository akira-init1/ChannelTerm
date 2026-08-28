# Transport and Serial Modules

## Transport contract

`internal/core/transport.Transport` is a protocol-neutral bidirectional terminal byte stream:

```go
type Transport interface {
    Connect(ctx context.Context) error
    Read(p []byte) (int, error)
    Write(p []byte) (int, error)
    Resize(cols, rows uint16) error
    Close() error
}
```

A Transport establishes and owns live protocol resources. It does not own terminal history. Session supplies one continuous reader, serializes complete write payloads at the byte boundary, and retains output. Implementations must not retain caller buffers and must make `Close` safe to call repeatedly.

`Connect` receives cancellation/deadline context and must release partially acquired resources. `Read` and `Write` follow Go reader/writer contracts. `Resize` reports a protocol error when a transport has no terminal-size capability.

## Serial implementation

`internal/core/transport/serial` is the only current concrete Transport. It uses `go.bug.st/serial` to enumerate and open operating-system endpoints.

Validated configuration includes port, positive baud rate, 5-8 data bits, supported parity, supported stop bits, and flow control. Empty optional values normalize to 8-N-1 with no flow control. Although `software` and `hardware` are stable accepted names, the current backend cannot configure portable XON/XOFF or RTS/CTS and returns `ErrFlowControlUnsupported` for both.

Serial `Connect` checks context before and immediately after the operating-system open. The library's blocking open itself cannot be interrupted after it starts; if cancellation is detected after success, ChannelTerm closes the acquired port before returning.

`Read` and `Write` snapshot the current port and delegate to the driver. `Close` clears the shared port reference before closing the driver resource, causing later operations to fail immediately and unblocking an active read through the driver. A successfully closed Transport object can be connected again, although normal ChannelTerm Session workflows create a new object for a new lifecycle.

Serial has no terminal dimensions, so `Resize` always returns an unsupported error. Transport connection itself never sends wake or line-ending bytes; Application sends an explicitly enabled wake byte through Session after connection.

## Enumeration and metadata

`ListPorts` returns unchanged endpoint names plus best-effort metadata. Windows enriches through SetupAPI; Linux reads sysfs; the current CGO-free macOS build reports endpoints without USB metadata. Missing metadata does not remove a valid endpoint.

Open failures retain the driver error while adding a diagnostic category for missing device, permission denial, busy port, or unknown failure. Windows access-denied errors are categorized as likely busy; Linux/macOS permission errors include access guidance.

## Current implementation boundary

Serial is the only concrete Transport in the current repository. There is no PTY, Android, or iOS Transport implementation.

## Future direction

The protocol-neutral interface leaves room for future SSH or Telnet Transports. Neither is currently implemented or committed to a release.
