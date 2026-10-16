# ChannelTerm Documentation

This navigation index links every public document. Technical facts are owned by the linked pages;
for command-line flags and defaults, the running program's `--help` output remains authoritative.

## Project Entry Points

- [English README](../README.md) — canonical public landing page
- [Third-party redistribution notices](../THIRD_PARTY_NOTICES)
- [简体中文 README](../README.zh-CN.md)
- [繁體中文 README](../README.zh-TW.md)
- [日本語 README](../README.ja.md)
- [한국어 README](../README.ko.md)
- [Français README](../README.fr.md)
- [Deutsch README](../README.de.md)
- [Español README](../README.es.md)
- [Русский README](../README.ru.md)

## Getting Started

- [Build and install from source](getting-started/build.md)
- [Use the serial terminal](getting-started/serial-terminal.md)
- [Use and share an SSH shell](getting-started/ssh-terminal.md)
- [Create and use serial profiles](getting-started/serial-profiles.md)
- [Share a Session](getting-started/shared-session.md)
- [Transfer files over a shared Session or SFTP/SCP](getting-started/file-transfer.md)
- [Use managed Linux GDB/MI or an external serial RSP bridge](getting-started/gdb-debugging.md)
- [Run the MCP server](getting-started/mcp-server.md)
- [Troubleshoot common failures](getting-started/troubleshooting.md)

## Agent Skills

- [ChannelTerm Serial Debug Skill](../skills/cterm-serial-debug/SKILL.md) — shared MCU and embedded-Linux
  serial-debugging workflow for ChannelTerm/cterm
- [ChannelTerm File Transfer Skill](../skills/cterm-file-transfer/SKILL.md) — protocol selection,
  upload/download, verification, and recovery over shared serial Sessions, SFTP, or SCP
- [ChannelTerm SSH Debug Skill](../skills/cterm-ssh-debug/SKILL.md) — SSH authentication, shared
  shell diagnosis, Host ownership, and connection recovery

## Reference

- [CLI reference](reference/cli.md)
- [MCP tools](reference/mcp-tools.md)
- [Configuration](reference/configuration.md)
- [Identifiers and references](reference/identifiers.md)

## Architecture

- [Architecture overview](architecture/overview.md)
- [Repository map](architecture/repository-map.md)
- [Runtime data flow](architecture/data-flow.md)

## Modules

- [Application](modules/application.md)
- [Session](modules/session.md)
- [Channel and Transports (serial and SSH)](modules/transport.md)
- [Buffers and cursors](modules/buffer.md)
- [Device Registry](modules/device-registry.md)
- [Connection Policy](modules/connection-policy.md)
- [Tool Registry](modules/tool-registry.md)

## Development

- [Development setup](development/setup.md)
- [Building and testing](development/building-and-testing.md)
- [Code style](development/code-style.md)
