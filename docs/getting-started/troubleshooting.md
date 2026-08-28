# Troubleshooting

## No serial port was selected

`serial port is required` means neither the command, selected profile, nor default profile resolved a port. Supply `--port`, select a complete profile, or use a current `SER-*` target from `channelterm list`.

## The target reference is not present

`connect` and target-based `attach` resolve a local target against current serial enumeration. Run:

```powershell
channelterm list --kind device --transport serial --no-mcp
```

Use the exact current `REF`. A stale reference fails instead of opening a similarly named port.

## The port is missing, busy, or inaccessible

- A missing-device diagnostic usually means the endpoint was removed or misspelled.
- A busy diagnostic means another application or Session Host may own the port. If `list` shows an MCP Session, attach to its `SER-N` reference instead of opening a private connection.
- On Linux, a permission diagnostic commonly requires correcting device permissions or group membership and then starting a new login session.

ChannelTerm preserves the underlying driver error after adding its diagnostic, so retain the complete error text when reporting a failure.

## No prompt appears after connecting

ChannelTerm intentionally sends nothing on a normal connection. If the device is known to be at an idle interactive shell, reconnect with `--wake` or explicitly write a carriage return. Do not send wake input to an unknown bootloader or device state.

## Flow control is rejected

Although the public values are `none`, `software`, and `hardware`, the current serial backend can configure only `none` portably. Software or hardware flow control returns an unsupported error before opening the port.

## MCP will not start

- `--transport` accepts only `stdio` or `http`.
- `--path` must start with `/`.
- `--connection-policy` accepts only `ask`, `auto`, or `deny`.
- A listen failure can mean that the address is invalid or already in use.
- Invalid TOML, corrupt device state JSON, or an unsupported state version prevents startup; ChannelTerm does not silently replace those files.

## `list` reports MCP offline

An offline MCP source is informational: `list` still returns local devices and profiles. Start `channelterm mcp --transport http`, correct `--endpoint`, or use `--no-mcp` when only local information is needed.

## Output or events were dropped

A `dropped: true` result means the requested cursor is older than the bounded retention window. Continue from the returned `next` cursor, but do not assume the missing history can be reconstructed. ChannelTerm does not currently persist terminal logs.

## UTF-8 read fails

Terminal buffers retain arbitrary bytes. If `terminal_read` reports invalid UTF-8, repeat the read with `encoding: "hex"` or `encoding: "base64"` so no data is replaced.

For command-specific errors, see the [CLI reference](../reference/cli.md). For MCP validation and lifecycle errors, see [MCP tools](../reference/mcp-tools.md).
