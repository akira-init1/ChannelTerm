# Architecture Overview

ChannelTerm is shared terminal-session infrastructure for human and AI access to terminal-like
transports. The current implementation is serial-first and is primarily focused on hardware,
embedded-system, and terminal debugging, while the Core remains transport-neutral.

```text
CLI Adapter -------------------+
                               |
MCP Adapter -> Tool Registry --+--> internal/core/app.Application
                                            |
                              +-------------+-------------+
                              |                           |
                              v                           v
                    Session / Manager              Device Registry
                         |         ^                      |
                         |         |               Connection Policy
                         v         |
                  Transport        |
                         |         |
                         v         |
                      Channel -----+
```

The Transport-to-Channel arrows show connection establishment and ownership transfer. Once open,
Session reads and writes the Channel directly; Transport is not an extra runtime I/O hop.

`cmd/channelterm` is the composition root. It creates process cancellation and passes standard
streams to `internal/cli/command`. The CLI owns argument syntax, status text, raw console mode,
escape commands, highlighting, HTTP host setup, and MCP client attachment.

`internal/install` is an adapter-side operating-system integration boundary. It installs the
currently running executable for one user, owns the `cterm` command alias and any PATH entry it
adds, and records that ownership in a platform-local manifest. It reuses `internal/core/config` to
initialize or purge known configuration files, but it does not define configuration semantics and
does not participate in terminal runtime data flow.

`internal/mcp` converts the protocol-neutral Tool Registry to MCP stdio or Streamable HTTP. MCP
transport disconnects do not own Session lifecycle. `internal/mcp/terminal` owns public tool names,
JSON-shaped schemas, encoding, and result translation; it calls `internal/core/app.Application` for
implemented use cases.

`internal/core/app.Application` is the adapter-neutral use-case boundary. It coordinates profile
resolution, serial and SSH Session open/reuse, Session reads/writes/close, serial and profile discovery,
Device Registry reads, and connection decisions. It returns structured Core values and never formats
CLI or MCP output. Application retains the shared Session Manager directly for transport-neutral
Session operations; `SerialService` is limited to serial configuration and open/reuse orchestration.

The core service boundary contains:

- `session`: lifecycle, Session Manager ownership, raw output retention, activity retention,
  structured event retention, and write serialization.
- `channel`: established protocol-neutral byte-stream I/O and lifecycle state.
- `debugger`: local GDB/MI process control, bounded protocol parsing, and a binary RSP relay.
- `device`: current discovery state, device identity state, and a bounded device-event stream.
- `connectionpolicy`: a pure discovery-response decision; it never connects.
- `tool`: a protocol-neutral callable contract and registry.
- `config`: connection profiles, discovery policy, user preferences, TOML persistence, and managed
  state paths. Preferences are adapter-local and never enter Session or Transport configuration.
- `transport`: protocol-specific Channel establishment.
- `transport/serial`: opens serial-backed Channels and enumerates local serial endpoints.

The concrete SSH implementation lives in `internal/transport/ssh`, outside `internal/core`.
It owns key/trust-file parsing, verified trust persistence, authentication, PTY interactive shell
Channels, and dedicated SFTP/SCP file clients. Application
composes it through plain configuration and the Core Transport contract; `golang.org/x/crypto/ssh`
types remain private to the implementation. Context-aware callbacks carry password input and explicit
unknown-host approval from adapters; Core never performs CLI or GUI interaction. Established I/O crosses only the existing Channel
interface, so Channel and Session are independent of the SSH backend.

Dependency direction is one way: Core packages do not import CLI or MCP packages. A Transport
establishes a protocol connection and transfers the live resource to a Channel. A Channel owns
protocol-neutral read/write/close and lifecycle state but no retained history. A Session is the only
continuous Channel reader and owns retained output. Presentation transformations never enter stored
output.

The shared access boundary is:

```text
Physical endpoint -> Transport -> Channel -> Session -> Client / Attachment
```

Multiple Clients can share a Session within one owning Manager. Readers keep independent output,
activity, and structured-event cursors. Session serializes complete write payloads so concurrent
bytes do not interleave, but the Session layer has no ownership, transaction, priority, arbitration,
or shell-state model. Application-level leases provide temporary exclusive writing for selected
operations such as terminal command execution and file transfer.

## Current implementation boundaries

Serial and SSH PTY shells are concrete Transports. SSH is opened by the CLI and shared through
the existing authenticated HTTP Host and Session tools; no SSH-specific MCP tool is added.
Stream Channel categories such as file and debug/JTAG are representable but not implemented. The
repository has no public Go SDK, GUI/TUI adapter, or persistent terminal log. Session lifecycle and
file-transfer state are available through the bounded Session Event Stream; it is not a Channel
multiplexer or a persistent event log.

## Future direction

The Channel boundary permits future debug/JTAG or remote-network streams without adding protocol
assumptions to Session. Arbitrary SSH exec, Telnet, OpenOCD, and a dedicated file Transport/Channel remain
architectural directions, not current features or commitments. The current CLI file transfer is an
application-layer Linux shell protocol carried over an existing serial Session; it is not another
Transport.

## Managed user-space debugging

Application owns a debug operation above an existing Session, including exclusive lease ownership,
gdbserver startup, heartbeat, cancellation, and verified shell restoration. The debugger service
owns GDB/MI and relays raw RSP through direct Session-service calls. The private Unix socket accepts
only the exact owned GDB PID and user. This managed MI mode uses no TCP listener or loopback MCP recursion.
The initial backend is native Linux only and requires all processes and the ELF in one environment.
See [Application](../modules/application.md#user-space-debug-operations).


## External RSP bridge

A separate Application operation leases an already-open serial Session and exposes a single
credential-authenticated TLS/TCP stream. `internal/core/debugger` supplies endpoint authentication
and the same byte relay used by managed MI. The CLI `rsp-client` adapts GDB's pipe to the authenticated
connection and answers a separate authenticated control heartbeat on the same endpoint; it never
starts or controls GDB. Application uses fresh liveness evidence for automatic
management renewal. MCP retains explicit lifecycle management. Target preparation and debugger control remain with the external operator. No additional Transport, Session reader,
Agent Runtime, or RSP implementation is introduced. Failure/recovery ownership remains in
[Application](../modules/application.md#external-serial-rsp-bridge).
