# Repository Map

This document is the sole canonical detailed source/package tree and ownership map. Summaries elsewhere should link here instead of duplicating it.

The public repository currently has the following durable structure:

```text
ChannelTerm/
|-- cmd/
|   `-- channelterm/
|       `-- main.go                 Process entry point and composition root
|-- internal/
|   |-- cli/
|   |   |-- command/                Commands, flags, local output, host/client wiring
|   |   |-- highlight/              Presentation-only ANSI highlighting
|   |   |-- interactive/            Ctrl+] local escape state machine
|   |   `-- terminalinput/          Raw console setup and restoration
|   |-- init/
|   |   `-- mcp/                    MCP client discovery, rendering, and safe config installation
|   |-- core/
|   |   |-- app/                    Adapter-neutral application use cases
|   |   |-- channel/                Established stream contract and lifecycle
|   |   |-- config/                 Connection profiles and policy, user preferences, TOML/state persistence
|   |   |-- connectionpolicy/       Discovery-response policy decisions
|   |   |-- device/                 Discovery registry, events, identity state
|   |   |-- session/                Session, Manager, output and activity buffers
|   |   |-- tool/                   Protocol-neutral Tool and Registry
|   |   `-- transport/
|   |       |-- transport.go        Protocol-specific Channel establishment
|   |       `-- serial/              Serial implementation, discovery, metadata
|   `-- mcp/
|       |-- server.go               MCP server and stdio/HTTP adaptation
|       `-- terminal/               Terminal tool names, schemas, and translation
|-- scripts/
|   |-- build.ps1                   Six-target build from PowerShell
|   `-- build.sh                    Six-target build from Linux/Bash
|-- docs/                           Public English technical documentation
|-- .gitattributes                  Cross-platform text line-ending policy
|-- .gitignore                      Local/build/generated-state ignore policy
|-- AGENTS.md                       Repository engineering rules
|-- CLAUDE.md                       Repository-specific assistant context
|-- README.md                       Public landing page
|-- LICENSE                         Apache License 2.0
|-- go.mod
`-- go.sum
```

Tests use `*_test.go` beside the packages they exercise. The repository does not expose packages outside `internal/`; consumers use the executable's CLI and MCP boundaries.

The main ownership distinctions are deliberate:

- **Adapter:** `cmd/channelterm`, `internal/cli`, and `internal/mcp` own process composition and external protocol or presentation concerns. `internal/init/mcp` owns local MCP-client configuration discovery and installation. These packages may depend inward; Core may not depend on them.
- **Application:** `internal/core/app` orchestrates adapter-neutral use cases without owning process lifetime or presentation.
- **Core services:** `internal/core/channel`, `session`, `config`, `connectionpolicy`, `device`, and `tool` own their protocol-neutral responsibilities. Channel owns established stream I/O and lifecycle; Session owns sharing, retained bytes, activity, and write serialization above it. `config` keeps connection profiles, discovery policy, and user preferences separate despite sharing one TOML file.
- **Transport:** `internal/core/transport` defines protocol-specific Channel establishment. `internal/core/transport/serial` owns serial connection setup and discovery; its opened port is transferred to a Channel.

Update this map when a top-level directory or `internal` package is added, removed, moved, or assigned a materially different stable responsibility.
