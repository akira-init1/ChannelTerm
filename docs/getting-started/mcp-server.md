# MCP Server

ChannelTerm exposes the same terminal Tool Registry over MCP stdio and stateless Streamable HTTP.

## Configure supported MCP clients

To add ChannelTerm to installed or already configured Codex, Claude Code, OpenCode, and Zoo Code
clients, run:

```powershell
channelterm init --mcp
```

The command prompts for HTTP or stdio and defaults to HTTP when you press Enter. HTTP installs the
shared endpoint `http://127.0.0.1:37099/mcp` with its Bearer credential; stdio installs an entry
that starts `channelterm mcp`. The command preserves other entries, leaves an existing `channelterm`
entry unchanged, and writes configuration atomically.

To print every supported client's configuration without modifying client files, run:

```powershell
channelterm init --mcp-show
```

This command presents the same HTTP-or-stdio prompt and also defaults to HTTP. The HTTP examples
contain the actual per-user Bearer credential, so they can be pasted directly into Codex, Claude
Code, OpenCode, or Zoo Code. Do not share, log, or commit HTTP output. Selecting HTTP creates
`http-auth-token` on first use when necessary. Append `codex`, `claude`, `opencode`, or `zoo` to
display only one client's selected configuration.

## Stdio

```powershell
channelterm mcp
```

`stdio` is the default transport. Standard output is reserved for MCP JSON-RPC, so the absence of
startup text is normal. An MCP client should start the process and communicate over its standard
streams. When the process ends, its Manager closes remaining Sessions.

## Streamable HTTP

```powershell
channelterm mcp --transport http
```

The default endpoint is `http://127.0.0.1:37099/mcp`. Readiness is confirmed by the startup line
written to standard error:

```text
MCP Streamable HTTP listening on http://127.0.0.1:37099/mcp
```

Silence after that line is normal. Stop the server with the process cancellation signal, normally
`Ctrl+C` from a processed console. HTTP shutdown has a five-second graceful timeout; process cleanup
then closes Manager-owned Sessions and the Device Registry.

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

The path must begin with `/`. `attach` automatically starts a host only for the exact default local
endpoint. For a different endpoint, start the host explicitly.

### Listen on a LAN

The HTTP Host can accept clients from a trusted LAN. Prefer binding the Host's specific LAN address
instead of every interface. For a machine whose LAN address is `192.168.1.20`, run:

```powershell
channelterm mcp --transport http --listen 192.168.1.20:37099
```

Configure clients with `http://192.168.1.20:37099/mcp`. To listen on every IPv4 interface, use
`--listen 0.0.0.0:37099`; `0.0.0.0` is only a bind address, so clients must still use the Host's
reachable LAN address. Allow the selected TCP port through the Host firewall only for trusted source
networks, and provide the same Bearer token to remote clients through their secret or header
configuration. ChannelTerm does not provide TLS, so use TLS termination and separate network access
controls before traffic crosses an untrusted network.

An automatically started Host includes `X-ChannelTerm-Host-Lifetime: attachment` on every
authenticated response. Bundled attachments and file commands use that signal to warn every client,
including clients that join later, that the Host and its Sessions stop with the owning attachment. A
separately started Host omits the header and persists until its own process is stopped.

## Execute Bash commands without shell history

At a known idle Bash prompt, an AI should use `terminal_exec` rather than `terminal_write` for an
ordinary non-interactive command:

```json
{"session_id":"SER-1","command":"uname -a","timeout_ms":30000}
```

ChannelTerm runs the command in an isolated non-interactive Bash child, automatically owns and
renews a temporary writer lease, and returns the exit code plus the exact raw-output cursor range.
The parent Bash removes ChannelTerm's bootstrap from its current history, so neither the command nor
the bootstrap appears under the Up arrow. A bundled `attach` client displays the original command
once in its local `AI` block and continues showing the real device output. Human input is ignored
locally while the command lease is active and resumes after release.

Use one compound command when steps share a directory or environment, for example
`cd /tmp && sha256sum app.bin`. Each call uses a new child, so `cd` and `export` do not persist into
the human shell or the next call. The tool rejects a partial line already entered by a human or AI,
but no generic serial protocol can prove that an arbitrary device is at a shell prompt. Do not call
it while a foreground process owns the terminal. Use `terminal_write` for raw keys, interactive
programs, password prompts, and non-shell devices; those raw bytes retain the target's normal
history behavior.

The default command timeout is 30 seconds. Timeout or MCP cancellation sends Ctrl+C and waits for
the wrapper to restore terminal echo. A failed recovery closes the Session rather than allowing new
writes into an unknown TTY state. Commands are retained in structured events for the observable `AI`
block, so never place passwords or tokens in the command text.

## Discovery and policy

At startup, the MCP process loads or creates `config.toml` and `state.json`, performs an initial
device scan, and then scans periodically. The initial scan establishes a presence baseline without
emitting `appeared` events. Discovery never opens a port or creates a Session.

The discovery decision policy is `ask`, `auto`, or `deny`. `--connection-policy` overrides the
configuration value; an omitted value defaults to `ask`. The policy only tells an MCP client what to
do after discovery. It does not itself prompt, connect, or block a user-requested open.

## Waiting for a file transfer

The MCP tool surface does not accept client-side file payloads. When an automation client also has
permission to run the local ChannelTerm CLI, it can use `channelterm file send LOCAL_PATH` without a
remote destination; ChannelTerm classifies that send under `/tmp/cterm/mcp-files/<local-basename>`.
It creates the hierarchy when absent and reuses it when present. Human sends from the `Ctrl+] f`
attachment menu instead default to `/tmp/cterm/user-files/<local-basename>`.

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

The default listener is loopback-only, and HTTP requests require a Bearer token. Authentication does
not provide per-tool authorization, TLS, user identity, revocation, or protection from a malicious
process running as the same operating-system user and able to read that user's token. The same
Bearer credential authorizes `terminal_exec`; history isolation is not a security boundary and does
not reduce the command's authority on the connected device. Command text is retained in Session
events for attached observers, so credentials and other secrets must use an appropriate interactive
or device-specific input path instead of command arguments. Binding a non-loopback address still
prints a warning. Use TLS termination and separate network access controls; do not expose the
endpoint directly to an untrusted network.

On cancellation, the HTTP entry point stops accepting new requests, waits up to five seconds for
active handlers, and waits for that shutdown operation to finish before Manager cleanup begins.
Manager shutdown rejects new Session registration and waits for any already-started physical open
attempt to finish, closing its candidate rather than allowing it to register after shutdown.

See [MCP tools](../reference/mcp-tools.md) for the currently exposed tool names and schemas.
