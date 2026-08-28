# ChannelTerm

A shared terminal-session core for human and AI access.

ChannelTerm is a cross-platform terminal tool for hardware debugging. It provides serial terminal access, reusable shared sessions, and MCP tools that allow humans and AI agents to observe and operate the same terminal session.

The current implementation focuses on **serial communication**.

## Features

* Cross-platform serial terminal for Windows, Linux, and macOS
* Shared terminal sessions for multiple clients
* MCP server over stdio and Streamable HTTP
* Human and AI access to the same Session
* Independent read cursors for concurrent clients
* Agent write activity visible from an attached CLI
* Serial device discovery and deterministic target references
* TOML serial profiles
* Raw terminal input with local escape commands
* In-memory retained terminal output
* JSON output for script-oriented session and device listing
* Static cross-compilation for `amd64` and `arm64`

## Quick Start

ChannelTerm currently requires Go 1.25 or later.

Clone the canonical repository and run:

```bash
git clone https://github.com/akira-init1/ChannelTerm.git
cd ChannelTerm
go build ./cmd/channelterm
```

Check the CLI:

```bash
./channelterm --help
```

On Windows PowerShell:

```powershell
.\channelterm.exe --help
```

Subsequent examples assume `channelterm` is on `PATH`; otherwise use `./channelterm` on Linux/macOS or `.\channelterm.exe` in Windows PowerShell.

## Serial Terminal

List detected serial devices:

```bash
channelterm list --kind device --transport serial --no-mcp
```

Open a serial terminal:

```bash
channelterm serial --port /dev/ttyUSB0 --baud 115200
```

Windows example:

```powershell
channelterm serial --port COM8 --baud 115200
```

A normal serial connection does not send startup input.

Use `--wake` when you explicitly want ChannelTerm to send one carriage return after connecting:

```bash
channelterm serial --port /dev/ttyUSB0 --baud 115200 --wake
```

### Interactive controls

While attached to a terminal:

```text
Ctrl+C      Send 0x03 to the remote terminal
Ctrl+] q    Exit or detach the local CLI
Ctrl+] ?    Show local escape help
Ctrl+] ]    Send a literal Ctrl+] byte
```

`Ctrl+C` is treated as remote terminal input rather than a local ChannelTerm exit command.

## Serial Profiles

Save commonly used serial settings:

```bash
channelterm serial --port /dev/ttyUSB0 --baud 115200 --save board
```

Reuse them later:

```bash
channelterm serial --profile board
```

Profiles are stored in TOML configuration.

## Shared Sessions

ChannelTerm can keep the physical serial connection inside a local Session Host so that multiple clients can observe the same Session without reopening the port.

Start or attach to a shared serial Session:

```bash
channelterm attach SER-/DEV/TTYUSB0 --baud 115200
```

Windows example:

```powershell
channelterm attach SER-COM8 --baud 115200
```

List active Sessions:

```bash
channelterm list --kind session
```

Attach another terminal to an existing Session:

```bash
channelterm attach SER-1
```

Each reader has its own cursor, so reading terminal output from one client does not consume it for another.

```text
Serial Device
     │
     ▼
  Transport
     │
     ▼
   Session
   ├── CLI
   ├── CLI
   └── MCP / AI Agent
```

Leaving an attached CLI with `Ctrl+] q` detaches that client without closing the shared Session.

## MCP

ChannelTerm can expose terminal operations to MCP clients and AI agents.

Start the default stdio MCP server:

```bash
channelterm mcp
```

Start a local Streamable HTTP server:

```bash
channelterm mcp --transport http
```

Default endpoint:

```text
http://127.0.0.1:37099/mcp
```

The MCP server exposes terminal capabilities including:

```text
device discovery
connection decisions
open serial Session
list Sessions
read output
wait for output
read activity
write terminal data
close Session
```

Terminal writes can be attributed to `user`, `agent`, or `system`. Attached CLI clients can display new Agent write activity without modifying the actual terminal byte stream.

> [!WARNING]
> The current HTTP MCP mode does not provide authentication. Keep it bound to loopback unless you understand and control the surrounding network environment.

## Private Sessions

If you do not want to use the MCP Session Host, open a private connection:

```bash
channelterm attach SER-/DEV/TTYUSB0 --private
```

or use the direct serial command:

```bash
channelterm serial --port /dev/ttyUSB0
```

Private Sessions belong to the current ChannelTerm process and cannot be joined by other clients.

## Build

Run the standard checks:

```bash
go test ./...
go vet ./...
```

Build the current platform:

```bash
go build ./cmd/channelterm
```

The repository also includes build scripts for Windows, Linux, and macOS on `amd64` and `arm64`.

PowerShell:

```powershell
.\scripts\build.ps1
```

Linux / WSL:

```bash
./scripts/build.sh
```

Generated release artifacts are written to:

```text
dist/
```

Cross-compilation verifies that the source builds for a target platform. It does not replace testing against native terminals or physical serial hardware.

## Documentation

Detailed documentation is available in [`docs/`](docs/README.md).

Useful starting points:

* [Build from source](docs/getting-started/build.md)
* [Serial terminal](docs/getting-started/serial-terminal.md)
* [Serial profiles](docs/getting-started/serial-profiles.md)
* [Shared Sessions](docs/getting-started/shared-session.md)
* [MCP server](docs/getting-started/mcp-server.md)
* [CLI reference](docs/reference/cli.md)
* [MCP tools](docs/reference/mcp-tools.md)
* [Architecture](docs/architecture/overview.md)

For exact command-line flags and defaults, the running program's `--help` output is authoritative.

## Current Scope

ChannelTerm currently implements serial communication.

Other terminal transports are not part of the current implementation and should not be assumed to be supported.

## License

ChannelTerm is licensed under the [Apache License 2.0](LICENSE).
