[English](README.md) | [Deutsch](README.de.md) | [Español](README.es.md) | [Français](README.fr.md) | [日本語](README.ja.md) | [한국어](README.ko.md) | [Русский](README.ru.md) | [简体中文](README.zh-CN.md) | [繁體中文](README.zh-TW.md)

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

## Quick Start: One Command, One Terminal

List native serial targets without requiring a running MCP server:

```bash
channelterm list --kind device --transport serial --no-mcp
```

Skip this discovery step when the native target, such as `COM50`, is already known.

If an AI client should join the Session, configure a supported client once before attaching:

```bash
channelterm init --mcp
```

Choose HTTP, or press Enter for the HTTP default. The configuration includes the local Bearer
credential. Reload the AI client if it does not pick up configuration changes immediately.

Then run the command for the current platform:

```bash
# Windows
channelterm attach COM8 --baud 115200 --label board

# Linux
channelterm attach /dev/ttyUSB0 --baud 115200 --label board

# macOS
channelterm attach /dev/cu.usbserial-110 --baud 115200 --label board
```

That single `attach` command opens the serial device and attaches the current terminal. When no
compatible Host is already running at the default endpoint, it also starts a temporary local HTTP
MCP Session Host automatically. Typical startup output is:

```text
Shared Session created: SER-1 (0123456789abcdef0123456789abcdef)
[ChannelTerm] Temporary Session Host started; it and all shared Sessions stop when this attachment exits. Run 'channelterm mcp --transport http' separately for a persistent Host.
```

No separate MCP-server terminal is required for this quick path. While the attachment remains open,
a configured HTTP MCP client can use `http://127.0.0.1:37099/mcp` and operate the same `SER-1`.
The temporary Host and every Session it owns stop when the creating attachment exits; use a
persistent Host only when Sessions must outlive that terminal.

`--label board` is an optional display name, not an identifier. Use `SER-1` or the opaque Session ID
when another client needs to refer to the Session.

### Common workflows

| Goal | Command |
| --- | --- |
| Open a shared serial Session and auto-start a temporary Host | `channelterm attach COM8 --baud 115200` |
| Configure a supported AI client for the shared HTTP Host | `channelterm init --mcp` |
| Keep the Host and Sessions alive independently | `channelterm mcp --transport http` |
| List or join an existing Session | `channelterm list --kind session` then `channelterm attach SER-1` |
| Open a private serial connection with no MCP sharing | `channelterm attach COM8 --private --baud 115200` |
| Watch structured Session activity | `channelterm events SER-1` |
| Open the guided file-transfer menu in the current shared attachment | Press `Ctrl+]`, then press `f` |
| Send or receive through a shared Session | `channelterm file send firmware.bin --session SER-1` or `channelterm file receive /tmp/log.txt ./log.txt --session SER-1` |

A persistent Host is a separate long-running process. After it reports
`MCP Streamable HTTP listening on http://127.0.0.1:37099/mcp`, other shells and configured AI clients
can create or join Sessions. Silence after the readiness line is normal; stop the Host with
`Ctrl+C` when all of its Sessions should close.

While attached:

```text
Ctrl+C      Send 0x03 to the remote terminal
Ctrl+] q    Leave this CLI; an attachment-owned temporary Host also stops
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

That shared serial Session can also transfer regular files and directories without an AI client or a separately installed board-side transfer agent. In an active shared `attach`, press `Ctrl+]` and then `f` to open the guided send/receive menu. Directories use standard tar and acknowledged, cancellation-friendly chunks; the same operations are available as commands:

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

The HTTP server requires a per-user Bearer token and defaults to a loopback-only listener. Built-in
CLI clients use the local token automatically. It can also [listen on a trusted
LAN](docs/getting-started/mcp-server.md#listen-on-a-lan); remote clients must use the Host's reachable
address and send the same token through the `Authorization` header. Do not expose the endpoint
without TLS and separate network access controls.

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
./channelterm install
```

`install` copies the running binary into the current user's standard program location, creates the
`channelterm` and `cterm` commands, adds that location to the user PATH only when necessary, and
initializes a missing minimal configuration. Open a new terminal when it reports a PATH change.
`channelterm uninstall` removes the installer-owned commands and PATH change while preserving user
data; `channelterm uninstall --purge` requires explicit confirmation before deleting configuration,
device state, and the local HTTP credential.

Default locations are summarized here; the installer prints the effective paths after every run:

| Platform | Commands | Installation record | Default configuration |
| --- | --- | --- | --- |
| Windows | `%LOCALAPPDATA%\Programs\ChannelTerm\bin` | `%LOCALAPPDATA%\ChannelTerm\install.json` | `%APPDATA%\channelterm\config.toml` |
| Linux | `~/.local/bin` | `$XDG_STATE_HOME/channelterm/install.json`, or `~/.local/state/channelterm/install.json` | `$XDG_CONFIG_HOME/channelterm/config.toml`, or `~/.config/channelterm/config.toml` |
| macOS | `~/.local/bin` | `~/Library/Application Support/channelterm/install.json` | `~/Library/Application Support/channelterm/config.toml` |

See [Build and install from source](docs/getting-started/build.md), the exact
[`install` / `uninstall` contract](docs/reference/cli.md#install-and-uninstall), and
[configuration locations](docs/reference/configuration.md). Repository checks and cross-build
scripts are covered by [Building and testing](docs/development/building-and-testing.md).

## Configuration

ChannelTerm stores its local files under the platform user-configuration directory:

| Platform | Default directory |
| --- | --- |
| Windows | `%AppData%\channelterm\` |
| Linux | `$XDG_CONFIG_HOME/channelterm/`, or `~/.config/channelterm/` when unset |
| macOS | `~/Library/Application Support/channelterm/` |

`config.toml` contains user-owned serial profiles and connection policy. `state.json` is
ChannelTerm-managed device identity state. `http-auth-token` is the secret used to authenticate MCP
HTTP clients; do not share or commit it.

```powershell
# Save and reuse a serial profile.
channelterm serial --port COM8 --baud 115200 --save board
channelterm serial --profile board

# Select a different config.toml for this operation.
channelterm serial --config ./channelterm.toml --profile board
```

Common options are `--profile`, `--save`, `--config`, `--port`, and `--baud`; the MCP Host also
accepts `--connection-policy ask|auto|deny`. `--config` changes only the selected `config.toml`, not
the default `state.json` or `http-auth-token`. See [Serial profiles](docs/getting-started/serial-profiles.md)
and the [configuration reference](docs/reference/configuration.md) for complete fields, precedence,
validation, and persistence behavior.

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
