# MCP Server

ChannelTerm exposes the same terminal Tool Registry over MCP stdio and stateless Streamable HTTP.

## Configure supported MCP clients

To add the default local stdio server to installed or already configured Codex, Claude Code, OpenCode, and Zoo Code clients, run:

```powershell
channelterm init --mcp
```

The command adds a `channelterm` entry that starts `channelterm mcp`. It preserves other entries, leaves an existing `channelterm` entry unchanged, and writes configuration atomically. To inspect every generated format without modifying files, use `channelterm init --mcp-show`; append `codex`, `claude`, `opencode`, or `zoo` to show just one client.

## Stdio

```powershell
channelterm mcp
```

`stdio` is the default transport. Standard output is reserved for MCP JSON-RPC, so the absence of startup text is normal. An MCP client should start the process and communicate over its standard streams. When the process ends, its Manager closes remaining Sessions.

## Streamable HTTP

```powershell
channelterm mcp --transport http
```

The default endpoint is `http://127.0.0.1:37099/mcp`. Readiness is confirmed by the startup line written to standard error:

```text
MCP Streamable HTTP listening on http://127.0.0.1:37099/mcp
```

Silence after that line is normal. Stop the server with the process cancellation signal, normally `Ctrl+C` from a processed console. HTTP shutdown has a five-second graceful timeout; process cleanup then closes Manager-owned Sessions and the Device Registry.

Customize the listener and endpoint path with:

```powershell
channelterm mcp --transport http --listen 127.0.0.1:12345 --path /terminal
```

The path must begin with `/`. `attach` automatically starts a host only for the exact default local endpoint. For a different endpoint, start the host explicitly.

## Discovery and policy

At startup, the MCP process loads or creates `config.toml` and `state.json`, performs an initial device scan, and then scans periodically. The initial scan establishes a presence baseline without emitting `appeared` events. Discovery never opens a port or creates a Session.

The discovery decision policy is `ask`, `auto`, or `deny`. `--connection-policy` overrides the configuration value; an omitted value defaults to `ask`. The policy only tells an MCP client what to do after discovery. It does not itself prompt, connect, or block a user-requested open.

## Network security

The default listener is loopback-only. Binding a non-loopback address prints a warning because the current HTTP server provides no ChannelTerm authentication or authorization layer. Any MCP client that can reach the endpoint can invoke write-capable terminal tools. Expose it only on a trusted, separately protected network.

See [MCP tools](../reference/mcp-tools.md) for all 13 currently exposed tool names and schemas.
