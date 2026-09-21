[English](README.md) | [简体中文](README.zh-CN.md) | [繁體中文](README.zh-TW.md) | [日本語](README.ja.md) | [한국어](README.ko.md)

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

## Quick Start: Share One Serial Session

For a Session that survives individual CLI attachments, keep a dedicated Host running.

Terminal 1 — start the persistent local Host:

```bash
channelterm mcp --transport http
```

Readiness is confirmed by `MCP Streamable HTTP listening on http://127.0.0.1:37099/mcp`.
Silence after that line is normal.

Terminal 2 — list native serial targets, then open and attach to one:

```bash
channelterm list --kind device --transport serial --no-mcp

# Windows
channelterm attach COM8 --baud 115200 --label board

# Linux
channelterm attach /dev/ttyUSB0 --baud 115200 --label board

# macOS
channelterm attach /dev/cu.usbserial-110 --baud 115200 --label board
```

Use only the command for the current platform. `--label board` is an optional display name; it is
not an identifier and cannot replace a Session reference.

Terminal 3 — another human CLI lists and joins the existing Session:

```bash
channelterm list --kind session
channelterm attach SER-1
```

Every attachment receives the same raw device output through an independent cursor. Press
`Ctrl+] q` to detach one CLI without closing the shared Session. Stop Terminal 1 with `Ctrl+C` only
when the Host and all of its Sessions should close.

For a quick single-terminal workflow, `channelterm attach COM8` (or a native `/dev/...` target)
can start a temporary Host automatically. That Host and its Sessions stop when the creating
attachment exits, so the dedicated Host above is the recommended shared workflow.

While attached:

```text
Ctrl+C      Send 0x03 to the remote terminal
Ctrl+] q    Detach this CLI without closing the shared Session
Ctrl+] ?    Show local escape help
Ctrl+] ]    Send a literal Ctrl+] byte
Ctrl+] t    Toggle local shell-prompt timestamps
Ctrl+] f    Open the local file send/receive menu
Ctrl+] Esc  Cancel local escape mode
```

To add a supported AI client to the same Host, run `channelterm init --mcp`, select HTTP, and let
the client list or operate `SER-1`. See [MCP Server](docs/getting-started/mcp-server.md) for the
authentication and client workflow.

## A Shared Terminal in Practice

An attached CLI renders an Agent command as a local `AI` activity block. `terminal_exec` keeps that
command out of an idle Bash prompt's history; the lines after the block are still the device's raw
terminal output, and the human can type again when the short command lease is released:

```bash
root@board:~#

──────── AI ────────
[18:52:31] >> echo agent-check
────────────────────
agent-check
root@board:~# echo human-still-here
human-still-here
root@board:~#
```

The exact command output depends on the connected device. The `AI` block is local presentation
derived from structured Session events; it is not inserted into the device byte stream or stored
terminal output. Raw keys and interactive programs still use `terminal_write` and retain the
device's normal history behavior.

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

## AI, Files, and Local Controls

That shared serial Session can also transfer regular files and directories without an AI client or a separately installed board-side transfer agent. Directories use standard tar and acknowledged, cancellation-friendly chunks:

```bash
channelterm file send firmware.bin --session SER-1
channelterm file receive /tmp/log.txt ./log.txt --session SER-1
channelterm file send ./build/release /tmp/release --session SER-1
channelterm file receive /var/log/myapp ./myapp --session SER-1
```

When the command omits its remote destination, it saves under `/tmp/cterm/mcp-files/`; the interactive shortcut uses `/tmp/cterm/user-files/`. ChannelTerm creates a missing destination hierarchy and reuses it on later transfers. The Linux shell uses native `mkdir`, `stty`, `dd`, `wc`, `sha256sum`, and `sleep`, plus `tar` for directories; ChannelTerm streams bounded chunks, reports progress, and verifies end-to-end SHA-256 for file bytes and directory tar streams. Directory transfers use temporary target-side archives so cancellation stops after the active chunk instead of draining the remaining directory. A temporary file-transfer lease blocks other ChannelTerm writers until the command finishes. While that lease is active, `Ctrl+C` in any bundled attachment opens a default-No local cancellation confirmation; otherwise it keeps its normal remote-terminal meaning. See the [file-transfer workflow](docs/getting-started/file-transfer.md) for prerequisites and limits.

To put an AI in that same Session, configure its MCP client to use the same Streamable HTTP endpoint:

```text
http://127.0.0.1:37099/mcp
```

The AI can use the MCP Session tools to list and read `SER-1`, run an idle Bash command with
`terminal_exec`, or send explicitly raw terminal bytes with `terminal_write`; see the
[MCP workflow](docs/getting-started/mcp-server.md) and
[tool reference](docs/reference/mcp-tools.md). Run `channelterm init --mcp` to install client
configuration or `channelterm init --mcp-show` to print it; both prompt for HTTP or stdio and
default to HTTP. Printed HTTP configuration contains the local Bearer credential and must not be
shared or committed.

Press `Ctrl+]` to enter local escape mode. ChannelTerm displays the available escape commands locally. `Ctrl+] f` starts a guided file/directory send/receive flow on the current attachment, with `/tmp/cterm/user-files/<basename>` and `./<basename>` defaults and non-overwriting `_1`, `_2` selection. `Ctrl+] t` is off by default and prepends local `[HH:MM:SS]` timestamps only to conservatively recognized shell prompts; it does not change shared Session or MCP output.

The HTTP server requires a per-user Bearer token and defaults to a loopback-only listener. Built-in CLI clients use the local token automatically. Remote MCP clients must send the same token through the `Authorization` header; do not expose the endpoint without TLS and separate network access controls.

## What It Enables

- One host-owned serial connection shared by human CLI and MCP clients.
- Independent output and activity cursors, so one reader does not consume another reader's data.
- Visible Agent writes in attached terminals without modifying raw Session output.
- Direct human input, including a remote `Ctrl+C`, while the AI remains connected.
- Client detach without closing the Session used by other clients.
- Serial discovery with native Windows, Linux, and macOS target names, TOML profiles, and private direct connections when sharing is not wanted.
- MCP over stdio or Streamable HTTP, with the shared CLI workflow using the HTTP Session Host.
- Bounded CLI file/directory send/receive over an existing serial Session, with SHA-256 verification and safe staged directory extraction.

## Example Workflows

### Embedded Linux debugging

Attach to a board once. An AI reads the retained boot log and runs a focused diagnostic while the engineer watches the same device output and responds at the live shell.

### AI operates, human observes

At an idle Bash prompt, the AI sends a non-interactive command with `terminal_exec`. The attached
CLI displays the command once as an `AI` activity block, followed by the real bytes returned by the
device, while the command and internal bootstrap stay out of Bash history.

### Human intervenes

The human can type into the shared Session or send `Ctrl+C` to the remote terminal, then continue
observing or detach with `Ctrl+] q`. A short `terminal_exec` lease temporarily locks human input;
ordinary raw writes retain byte-level serialization without shell-state arbitration, so clients
must still avoid conflicting raw command sequences.

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

The Session Host owns the physical port and the Session lifetime. A Serial Transport opens a protocol-neutral Channel; Session owns the single Channel reader, bounded in-memory output and activity retention, and write serialization. CLI and MCP adapters use the same Core behavior; presentation such as highlighting and the `AI` activity block stays outside stored terminal data.

Shared Session references such as `SER-1` are valid only within the owning host process. Stopping that host closes its Sessions; detaching one client does not.

## Installation

ChannelTerm requires Go 1.25 or newer. Use a currently supported patched Go toolchain, then build the native executable from the repository root:

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
- [File-transfer workflow](docs/getting-started/file-transfer.md)
- [MCP server workflow](docs/getting-started/mcp-server.md)
- [CLI reference](docs/reference/cli.md)
- [MCP tool reference](docs/reference/mcp-tools.md)
- [Identifiers and references](docs/reference/identifiers.md)
- [Architecture overview](docs/architecture/overview.md)

For exact command flags and defaults, the running program's `--help` output is authoritative.

## Roadmap

Serial is the only concrete Transport implemented today. The transport-neutral Core leaves room for future SSH or Telnet Transports and stronger multi-writer coordination, but these are directions rather than implemented features or release commitments.

ChannelTerm does not currently implement GDB, JTAG/OpenOCD, virtual serial devices, a built-in AI, or multi-agent orchestration. File transfer is intentionally a small Linux-shell protocol rather than a general transfer agent.

## License

ChannelTerm is licensed under the [Apache License 2.0](LICENSE).
