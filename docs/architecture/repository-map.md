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
|   |-- core/
|   |   |-- app/                    Adapter-neutral application use cases
|   |   |-- config/                 TOML config, state paths, precedence, persistence
|   |   |-- connectionpolicy/       Discovery-response policy decisions
|   |   |-- device/                 Discovery registry, events, identity state
|   |   |-- session/                Session, Manager, output and activity buffers
|   |   |-- tool/                   Protocol-neutral Tool and Registry
|   |   `-- transport/
|   |       |-- transport.go        Protocol-neutral byte-stream interface
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

- **Adapter:** `cmd/channelterm`, `internal/cli`, and `internal/mcp` own process composition and external protocol or presentation concerns. They may depend inward; Core may not depend on them.
- **Application:** `internal/core/app` orchestrates adapter-neutral use cases without owning process lifetime or presentation.
- **Core services:** `internal/core/session`, `config`, `connectionpolicy`, `device`, and `tool` own their protocol-neutral responsibilities. Session owns retained bytes and lifecycle.
- **Transport:** `internal/core/transport` defines the live byte-stream contract. `internal/core/transport/serial` owns only the physical serial stream and discovery implementation.

Update this map when a top-level directory or `internal` package is added, removed, moved, or assigned a materially different stable responsibility.
