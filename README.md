# ChannelTerm

ChannelTerm lets humans and AI share the same hardware terminal session.

AI does not replace your terminal. It joins it: the AI can read and operate the device while you remain attached, see what it writes, interact directly, and intervene when needed.

```text
              Physical serial device
                        |
                 Serial Transport
                  (one connection)
                        |
                        v
              ChannelTerm Session SER-1
                 /                 \
                /                   \
       Human terminal             AI / MCP client
    channelterm attach        read and write tools
                \                   /
                 `--- same Session -'
```

ChannelTerm currently implements serial communication on Windows, Linux, and macOS. It is not a built-in AI; it exposes real terminal Sessions to human CLI clients and external AI clients through MCP.

## A Shared Terminal in Practice

An attached CLI renders each new, non-empty Agent write as a local `AI` activity block. The lines after the block are still the device's raw terminal output, and the human can type at the same prompt:

```text
root@board:~#

──────── AI ────────
[18:52:31] >> echo agent-check
────────────────────
agent-check
root@board:~# echo human-still-here
human-still-here
root@board:~#
```

The exact command output and input echo depend on the connected device. The `AI` block is local presentation derived from Session activity; it is not inserted into the device byte stream or stored terminal output.

## Why ChannelTerm?

A traditional serial terminal gives the human the physical connection:

```text
Human ---- terminal ---- Serial device
```

An AI-only serial tool gives the AI the connection, but leaves the human outside the live terminal:

```text
AI ---- MCP / serial ---- Serial device

Human         (not attached)
```

ChannelTerm keeps one physical connection in a shared Session:

```text
                         +---- Human terminal
                         |
Serial device ---- Session
                         |
                         +---- AI / MCP client
```

The result is a hardware-debugging workflow where AI actions stay observable and the human remains present. Sharing is more than reopening the same port: every client sees retained raw output through an independent cursor, and all writes pass through the same Session.

## Quick Start

Build or install `channelterm`, then list the serial targets detected on the machine:

```bash
channelterm list --kind device --transport serial --no-mcp
```

Create or join a shared Session and attach the human terminal. Use the target printed by `list`:

```bash
# Linux example
channelterm attach SER-/DEV/TTYUSB0 --baud 115200

# Windows example
channelterm attach SER-COM8 --baud 115200
```

For the default local endpoint, `attach` starts the loopback Session Host when needed, asks it to open or reuse the target, and then joins the returned Session. From another shell, inspect its short reference:

```bash
channelterm list --kind session
```

To put an AI in that same Session, configure its MCP client to use the same Streamable HTTP endpoint:

```text
http://127.0.0.1:37099/mcp
```

The AI can use the MCP Session tools to list Sessions and read or write `SER-1`; see the [MCP workflow](docs/getting-started/mcp-server.md) and [tool reference](docs/reference/mcp-tools.md). The separate `channelterm init --mcp` convenience command installs a stdio MCP configuration; use the shared HTTP endpoint when a CLI and an AI must join the same host-owned Session.

While attached:

```text
Ctrl+C      Send 0x03 to the remote terminal
Ctrl+] q    Detach this CLI without closing the shared Session
Ctrl+] ?    Show local escape help
Ctrl+] ]    Send a literal Ctrl+] byte
Ctrl+] Esc  Cancel local escape mode
```

Press `Ctrl+]` to enter local escape mode. ChannelTerm displays the available escape commands locally.

The HTTP server has no ChannelTerm authentication or authorization layer. Its default listener is loopback-only; do not expose it to an untrusted network.

## What It Enables

- One host-owned serial connection shared by human CLI and MCP clients.
- Independent output and activity cursors, so one reader does not consume another reader's data.
- Visible Agent writes in attached terminals without modifying raw Session output.
- Direct human input, including a remote `Ctrl+C`, while the AI remains connected.
- Client detach without closing the Session used by other clients.
- Serial discovery, deterministic target references, TOML profiles, and private direct connections when sharing is not wanted.
- MCP over stdio or Streamable HTTP, with the shared CLI workflow using the HTTP Session Host.

## Example Workflows

### Embedded Linux debugging

Attach to a board once. An AI reads the retained boot log and runs a focused diagnostic while the engineer watches the same device output and responds at the live shell.

### AI operates, human observes

The AI sends a command with `terminal_write`. The attached CLI displays the new Agent write as an `AI` activity block, followed by the real bytes returned by the device.

### Human intervenes

The human can type into the shared Session or send `Ctrl+C` to the remote terminal, then continue observing or detach with `Ctrl+] q`. ChannelTerm serializes complete write payloads so their bytes do not interleave, but it does not provide writer ownership, command transactions, priority, or shell-state arbitration; clients must avoid conflicting command sequences.

## How It Works

```text
Human CLI ---------+
                   |
                   v
            Session Host / Manager
                   |
AI via MCP --------+--> Session --> Serial Transport <--> Device
                        |    |
                        |    `-- serialized writes
                        `------- retained raw output
```

The Session Host owns the physical port and the Session lifetime. A Session owns the single Transport reader, bounded in-memory output and activity retention, and write serialization. CLI and MCP adapters use the same Core behavior; presentation such as highlighting and the `AI` activity block stays outside stored terminal data.

Shared Session references such as `SER-1` are valid only within the owning host process. Stopping that host closes its Sessions; detaching one client does not.

## Installation

ChannelTerm currently requires the Go version declared in [`go.mod`](go.mod) (Go 1.25.0 at the time of writing). Build the native executable from the repository root:

```bash
git clone https://github.com/akira-init1/ChannelTerm.git
cd ChannelTerm
go build ./cmd/channelterm
```

See [Build from source](docs/getting-started/build.md) for the supported platforms and [Building and testing](docs/development/building-and-testing.md) for repository checks and cross-build scripts.

## Documentation

- [Documentation index](docs/README.md)
- [Serial terminal workflow](docs/getting-started/serial-terminal.md)
- [Shared Session workflow](docs/getting-started/shared-session.md)
- [MCP server workflow](docs/getting-started/mcp-server.md)
- [CLI reference](docs/reference/cli.md)
- [MCP tool reference](docs/reference/mcp-tools.md)
- [Identifiers and references](docs/reference/identifiers.md)
- [Architecture overview](docs/architecture/overview.md)

For exact command flags and defaults, the running program's `--help` output is authoritative.

## Roadmap

Serial is the only concrete Transport implemented today. The transport-neutral Core leaves room for future SSH or Telnet Transports and stronger multi-writer coordination, but these are directions rather than implemented features or release commitments.

ChannelTerm does not currently implement GDB, JTAG/OpenOCD, virtual serial devices, file transfer, a built-in AI, or multi-agent orchestration.

## License

ChannelTerm is licensed under the [Apache License 2.0](LICENSE).
