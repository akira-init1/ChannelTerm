# Architecture Overview

ChannelTerm is shared terminal-session infrastructure for human and AI access to terminal-like transports. The current implementation is serial-first and is primarily focused on hardware, embedded-system, and terminal debugging, while the Core remains transport-neutral.

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
                  Serial Transport |
                         |         |
                         v         |
                      Channel -----+
```

`cmd/channelterm` is the composition root. It creates process cancellation and passes standard streams to `internal/cli/command`. The CLI owns argument syntax, status text, raw console mode, escape commands, highlighting, HTTP host setup, and MCP client attachment.

`internal/mcp` converts the protocol-neutral Tool Registry to MCP stdio or Streamable HTTP. MCP transport disconnects do not own Session lifecycle. `internal/mcp/terminal` owns public tool names, JSON-shaped schemas, encoding, and result translation; it calls `internal/core/app.Application` for implemented use cases.

`internal/core/app.Application` is the adapter-neutral use-case boundary. It coordinates profile resolution, serial Session open/reuse, Session reads/writes/close, serial and profile discovery, Device Registry reads, and connection decisions. It returns structured Core values and never formats CLI or MCP output.

The core service boundary contains:

- `session`: lifecycle, Session Manager ownership, raw output retention, activity retention, and write serialization.
- `channel`: established protocol-neutral byte-stream I/O and lifecycle state.
- `device`: current discovery state, device identity state, and a bounded device-event stream.
- `connectionpolicy`: a pure discovery-response decision; it never connects.
- `tool`: a protocol-neutral callable contract and registry.
- `config`: connection profiles, discovery policy, user preferences, TOML persistence, and managed state paths. Preferences are adapter-local and never enter Session or Transport configuration.
- `transport`: protocol-specific Channel establishment.
- `transport/serial`: the current concrete Transport, which opens serial-backed Channels, plus serial enumeration.

Dependency direction is one way: Core packages do not import CLI or MCP packages. A Transport establishes a protocol connection and transfers the live resource to a Channel. A Channel owns protocol-neutral read/write/close and lifecycle state but no retained history. A Session is the only continuous Channel reader and owns retained output. Presentation transformations never enter stored output.

The shared access boundary is:

```text
Physical endpoint -> Transport -> Channel -> Session -> Client / Attachment
```

Multiple Clients can share a Session within one owning Manager. Readers keep independent output and activity cursors. Writes pass through Session and are serialized as complete payloads so bytes from concurrent calls do not interleave. This is an I/O boundary guarantee, not semantic coordination between writers: there is no ownership lease, transaction, priority, arbitration, or shell-state coordination.

## Current implementation boundaries

Serial is the only concrete Transport. Stream Channel categories such as file, debug/JTAG, and remote network are representable through the same Channel contract but are not implemented. The repository has no public Go SDK, GUI/TUI adapter, persistent terminal log, or Session lifecycle EventBus.

## Future direction

The Channel boundary permits future debug/JTAG or remote-network streams without adding protocol assumptions to Session. SSH, Telnet, OpenOCD, and a dedicated file Transport/Channel remain architectural directions, not current features or commitments. The current CLI file transfer is an application-layer Linux shell protocol carried over an existing serial Session; it is not another Transport.
