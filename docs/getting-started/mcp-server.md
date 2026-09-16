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

The Host requires `Authorization: Bearer <token>` on every HTTP request. On first use, ChannelTerm
creates an owner-readable token named `http-auth-token` in its platform user configuration
directory. Built-in commands such as `attach`, `list`, `events`, and `file` read it automatically.
For an external MCP client, configure the same header using the client's secret/header mechanism.
For a remote Host or an explicit secret manager, set `CHANNELTERM_HTTP_AUTH_TOKEN` to the same value
for both Host and client instead of copying the default local token file. Built-in clients refuse
cross-origin HTTP redirects so the credential cannot be forwarded to a different endpoint.

Customize the listener and endpoint path with:

```powershell
channelterm mcp --transport http --listen 127.0.0.1:12345 --path /terminal
```

The path must begin with `/`. `attach` automatically starts a host only for the exact default local endpoint. For a different endpoint, start the host explicitly.

An automatically started Host includes `X-ChannelTerm-Host-Lifetime: attachment` on every
authenticated response. Bundled attachments and file commands use that signal to warn every client,
including clients that join later, that the Host and its Sessions stop with the owning attachment.
A separately started Host omits the header and persists until its own process is stopped.

## Discovery and policy

At startup, the MCP process loads or creates `config.toml` and `state.json`, performs an initial device scan, and then scans periodically. The initial scan establishes a presence baseline without emitting `appeared` events. Discovery never opens a port or creates a Session.

The discovery decision policy is `ask`, `auto`, or `deny`. `--connection-policy` overrides the configuration value; an omitted value defaults to `ask`. The policy only tells an MCP client what to do after discovery. It does not itself prompt, connect, or block a user-requested open.

## Waiting for a file transfer

The MCP tool surface does not accept client-side file payloads. When an automation client also has
permission to run the local ChannelTerm CLI, it can use `channelterm file send LOCAL_PATH` without
a remote destination; ChannelTerm classifies that send under
`/tmp/cterm/mcp-files/<local-basename>`. It creates the hierarchy when absent and reuses it when
present. Human sends from the `Ctrl+] f` attachment menu instead default to
`/tmp/cterm/user-files/<local-basename>`.

An AI client must not use `terminal_wait` as its file-transfer completion signal. That tool waits
only for raw terminal bytes, and a user cancellation may restore the target TTY without producing
another prompt. Capture the `next` cursor from `terminal_session_events` before the transfer, obtain
`transfer_id` from its `FILE_TRANSFER_STARTED` event or active lease, then call
`terminal_wait_file_transfer` with both. It ignores other transfers and returns a structured
`completed`, `cancelled`, or `failed` state only after the matching file-transfer lease is released.
The returned `lease_released: true` means the next ordinary Session command will not race this
transfer's lease. A cancelled result includes the retained byte counts and `reason: user_cancelled`
in its event metadata.

## Network security

The default listener is loopback-only, and HTTP requests require a Bearer token. Authentication
does not provide per-tool authorization, TLS, user identity, revocation, or protection from a
malicious process running as the same operating-system user and able to read that user's token.
Binding a non-loopback address still prints a warning. Use TLS termination and separate network
access controls; do not expose the endpoint directly to an untrusted network.

On cancellation, the HTTP entry point stops accepting new requests, waits up to five seconds for
active handlers, and waits for that shutdown operation to finish before Manager cleanup begins.
Manager shutdown rejects new Session registration and waits for any already-started physical open
attempt to finish, closing its candidate rather than allowing it to register after shutdown.

See [MCP tools](../reference/mcp-tools.md) for the currently exposed tool names and schemas.
