# Repository Map

This document is the sole canonical detailed source/package tree and ownership map. Summaries
elsewhere should link here instead of duplicating it.

The public repository currently has the following durable structure:

```text
ChannelTerm/
|-- .github/
|   `-- workflows/
|       |-- ci.yml                      Test, race, vet, vulnerability, and cross-build gates
|       `-- release.yml                 Tagged cross-build, packaging, checksums, and GitHub Release
|-- cmd/
|   `-- channelterm/
|       `-- main.go                 Process entry point and composition root
|-- gui/
|   `-- electron/                   Independent Electron frontend (detailed ownership below)
|-- internal/
|   |-- cli/
|   |   |-- command/                Commands, flags, local output, host/client wiring
|   |   |-- highlight/              Presentation-only ANSI highlighting
|   |   |-- interactive/            Ctrl+] local escape state machine
|   |   `-- terminalinput/          Raw console setup, cancellable prompts, restoration, and terminal-size observation
|   |-- init/
|   |   `-- mcp/                    MCP client discovery, rendering, and safe config installation
|   |-- install/                    Per-user executable install/uninstall, aliases, PATH, manifest
|   |-- core/
|   |   |-- app/                    Adapter-neutral application use cases
|   |   |-- channel/                Established stream contract and lifecycle
|   |   |-- config/                 Connection profiles and policy, user preferences, TOML/state persistence
|   |   |-- connectionpolicy/       Discovery-response policy decisions
|   |   |-- debugger/               GDB/MI, authenticated RSP endpoints/control keepalive, binary relay
|   |   |-- device/                 Discovery registry, events, identity state
|   |   |-- session/                Session, Manager, output and activity buffers
|   |   |-- tool/                   Protocol-neutral Tool and Registry
|   |   `-- transport/
|   |       |-- transport.go        Protocol-specific Channel establishment
|   |       `-- serial/             Serial implementation, discovery, metadata
|   |-- transport/
|   |   `-- ssh/                    SSH authentication, PTY shell Channels, dedicated SFTP/SCP clients
|   `-- mcp/
|       |-- server.go               MCP server and stdio/HTTP adaptation
|       `-- terminal/               Terminal tool names, schemas, and translation
|-- scripts/
|   |-- build.ps1                   Six-target build from PowerShell
|   |-- build.sh                    Six-target build from Linux/Bash
|   `-- release.sh                  Clean tagged release verification and checksums
|-- skills/
|   |-- cterm-file-transfer/         Serial/SFTP/SCP transfer workflows for AI clients
|   |-- cterm-gdb-debug/             Linux GDB workflows over serial, SSH, or TCP
|   |-- cterm-serial-debug/          Shared serial-debugging workflow for AI clients
|   `-- cterm-ssh-debug/             Shared SSH shell diagnosis workflow for AI clients
|-- docs/                           Public English technical documentation
|-- .gitattributes                  Cross-platform text line-ending policy
|-- .gitignore                      Local/build/generated-state ignore policy
|-- AGENTS.md                       Repository engineering rules
|-- CLAUDE.md                       Repository-specific assistant context
|-- README.md                       Public landing page
|-- LICENSE                         Apache License 2.0
|-- THIRD_PARTY_NOTICES              Required SSH dependency redistribution notice
|-- go.mod
`-- go.sum
```

Tests use `*_test.go` beside the packages they exercise. The repository does not expose packages
outside `internal/`; consumers use the executable's CLI and MCP boundaries.

The main ownership distinctions are deliberate:

- **Automation:** `.github/workflows` enforces hosted repository checks and publishes verified tag
  builds through GitHub Releases. `scripts` owns local build and release verification without
  publishing releases.
- **Agent workflows:** `skills` packages reusable instructions for selecting and sequencing the
  existing ChannelTerm CLI and MCP interfaces. Skills do not own product behavior or protocol
  schemas.
- **Adapter:** `cmd/channelterm`, `internal/cli`, and `internal/mcp` own process composition and
  external protocol or presentation concerns. `internal/init/mcp` owns local MCP-client
  configuration discovery and installation. `internal/install` owns per-user executable placement,
  short-command aliases, PATH changes, and the installation ownership manifest. These packages may
  depend inward; Core may not depend on them.
- **Application:** `internal/core/app` orchestrates adapter-neutral use cases and managed debug
  operations; it does not own the Host process lifetime or presentation.
  It also owns serial U-Boot entry orchestration and bounded diagnostic archive retention/download
  use cases. These reuse Session cursors and application leases without adding Transport ownership.
  `internal/core/debugger` owns GDB/MI, pinned TLS connection endpoints, and the RSP relay through a minimal
  cursor-read/leased-write service; it never opens a terminal Transport. Application separately owns the pure external bridge lifecycle and its managed debug lease; the CLI `rsp-client` adapts external GDB pipe I/O and answers control heartbeats on the same authenticated endpoint.
- **Core services:** `internal/core/channel`, `session`, `config`, `connectionpolicy`, `device`, and
  `tool` own their protocol-neutral responsibilities. Channel owns established stream I/O and
  lifecycle; Session owns sharing, retained bytes, activity, and write serialization above it.
  `config` keeps connection profiles, discovery policy, and user preferences separate despite
  sharing one TOML file.
- **Transport:** `internal/core/transport` defines protocol-specific Channel establishment.
  `internal/core/transport/serial` owns serial connection setup and discovery; its opened port is
  transferred to a Channel. `internal/transport/ssh` owns credential-file parsing, host-key
  verification/persistence, authentication, and PTY/shell setup, then transfers the stream to a Channel with
  optional resizing on the same shell. Dedicated file clients use the same authenticated setup but
  own independent SSH connections and SFTP subsystem/SCP exec channels. They do not become terminal
  Channels or Sessions. Application owns local file selection, copying, and destination containment.
  Its configuration exposes ordinary settings, paths, and context-aware adapter interaction callbacks;
  SSH-library types stay private to the
  implementation, outside the generic Channel, Session, Application, and CLI contracts.
  The concrete SSH package is outside `internal/core`; it implements the Core Transport contract
  and returns Core Channels. Application composes it using library-independent configuration,
  following the same opening responsibility as Serial without moving existing Serial code.

Update this map when a top-level directory or `internal` package is added, removed, moved, or
assigned a materially different stable responsibility.

## Electron source ownership

```text
gui/electron/
|-- AGENTS.md                       Electron-specific layering, security, performance, and review rules
|-- src/
|   |-- main/
|   |   |-- index.ts                Window lifecycle, static application scheme, validated IPC
|   |   |-- security.ts             Trusted caller and content-policy checks
|   |   |-- menu.ts                 Native menu rendering from bounded display hints
|   |   `-- host/                   Main-only credentials, MCP client, validation, safe errors
|   |-- preload/                   Narrow contextBridge methods; no business decisions
|   |-- locales/                   Per-locale PO sources, ignored compiled modules, process-neutral resource imports
|   |-- renderer/
|   |   |-- components/            React application shell and view state
|   |   |-- sessions/              Session selection and display metadata
|   |   |-- terminal/              xterm presentation, bounded input, output render pacing
|   |   |-- styles/                Desktop presentation
|   |   |-- host.ts                Renderer adapter for the typed Preload bridge
|   |   `-- main.tsx               React entry point
|   `-- shared/                    GUI bridge types, fixed channels, pure menu model and English message definitions
|-- scripts/                       Independent build/start/dev/notices and desktop test runners
|-- tests/
|   |-- host/                      Disposable Go composition using the real Host with fake I/O
|   `-- *.ts                       Host Client integration, security checks, fixture lifecycle
|-- package.json                   Independent npm dependency graph and commands
|-- package-lock.json              Exact resolved dependencies
|-- tsconfig.json                  Strict TypeScript checks
|-- vite.config.ts                 Renderer build and loopback development server
|-- lingui.config.ts               Official message extraction, PO format, locales and compilation policy
|-- index.html                     Renderer document
|-- THIRD_PARTY_NOTICES             GUI redistribution obligations and generated notice location
|-- .prettierignore                Excludes generated Lingui modules from formatting
`-- .gitignore                     Local dependencies, build output, compiled catalogs and test artifacts
```

Main's Host Client calls existing authenticated MCP tools; it does not import Go source, own a
Session, or manage leases. The Go fixture is test composition only, not a second production Host
or Core implementation. It opens no hardware and is not included in GUI production assets.
The `gui/` boundary can accommodate a future independent frontend; no Qt project is present.
See [GUI architecture](electron-gui.md) and [GUI development](../development/electron-gui.md).
