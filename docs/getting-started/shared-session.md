# Shared Sessions

An HTTP MCP process can act as a local Session Host. It owns the physical serial connection while CLI windows and MCP Clients use independent cursors over the same Session.

```text
Physical serial endpoint -> Serial Transport -> Channel -> Session -> Client / Attachment
```

## Create or join by target

```powershell
channelterm attach SER-COM8 --baud 115200
```

For the default endpoint, `attach` starts a background local HTTP host if one is not already reachable. It then asks the host to open the serial target. Within that host's Manager, one active Session is shared for an exact `transport + endpoint` pair, so another open for `serial + COM8` returns the existing Session instead of opening the port twice.

The command reports whether the shared Session was created or reused, then attaches the current terminal. Press `Ctrl+]` to enter local escape mode; ChannelTerm displays the available escape commands locally. `Ctrl+] t` toggles prompt timestamps for this CLI attachment only, so other CLI attachments and MCP readers continue to receive raw output. `Ctrl+] Esc` reports `[ChannelTerm] Escape cancelled` locally, cancels escape mode without sending a byte to the shared Session, and returns subsequent input to normal remote forwarding; `Ctrl+] q` detaches this CLI without closing the host-owned Session.

## Join an existing Session

List host Sessions and copy either the short reference or opaque ID:

```powershell
channelterm list --kind session --long
channelterm attach SER-1
channelterm attach 0123456789abcdef0123456789abcdef
```

Each reader owns its own output and activity cursors. One Client reading output does not consume it for another Client. Session writes are serialized as complete payloads, including retries after short Transport writes, so concurrent payload bytes do not interleave.

An existing shared Session can also carry a CLI-only [file transfer](file-transfer.md). A second terminal can run `channelterm file send` or `channelterm file receive` while an attachment continues observing with its own cursor. During that transfer, the Host holds a `file-transfer` lease: attach input and ordinary `terminal_write` calls fail immediately rather than interleaving with the shell protocol.

That guarantee does not coordinate the meaning of concurrent commands. Session has no writer ownership, exclusive lease, transaction, priority, arbitration, or shell-state coordination. Clients must currently avoid conflicting command sequences themselves.

After attachment, the CLI watches new activity from the current tail and renders non-empty Agent writes as local `AI` activity blocks. It does not replay older activity, and writes containing only carriage-return or line-feed bytes are not rendered as blocks. This local view does not add bytes to Session output or change another client's cursor.

## Observe shared state

Use a separate terminal to observe attachment, lease, and file-transfer state without consuming device output:

```powershell
channelterm events SER-1
```

The command emits JSON Lines from the same bounded Session Event Stream used by MCP `terminal_session_events`. Each observer owns its cursor. If an observer is slower than the retained window, it receives `dropped: true` and continues at the returned cursor; it never slows the serial reader or Session writers.

## Use a private connection

```powershell
channelterm attach SER-COM8 --private --baud 115200
```

`--no-mcp` is an alias for `--private`. The current CLI process owns this port and closes it when the client exits. Other CLI or MCP clients cannot join it. `--private` is valid only with a serial target reference, not with a Session reference.

## Lifecycle boundary

Detaching a Client does not close a shared Session. `terminal_close` removes and closes one host-owned Session. Stopping the Session Host closes all Sessions still owned by its Manager. Session references such as `SER-1` are valid only for that host process lifetime.

See [Identifiers](../reference/identifiers.md) before passing target references, Session references, or `session_id` values between commands.

## Future direction

Stronger multi-writer coordination may be added above the byte-serialization boundary in the future. It is not part of the current Session contract.
