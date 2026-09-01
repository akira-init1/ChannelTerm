# Runtime Data Flow

## Serial output

```text
OS serial driver
      |
      v
Serial Channel.Read
      |
      v
Session reader goroutine
      |
      v
16 MiB receive Ring Buffer
      |
      v
Application.ReadSession
      |
      +--> CLI cursor --> optional prompt timestamping and ANSI highlighting --> stdout
      |
      `--> MCP cursor --> utf8 / hex / base64 result
```

One Session goroutine is the only continuous reader of a Channel. Consumers copy bounded chunks by independent absolute cursor. Slow consumers cannot block the Channel reader; they receive `dropped: true` if overwritten output passes their cursor.

## Terminal input

```text
CLI raw input                 MCP JSON input
      |                            |
Ctrl+] local controller       decode complete payload
      |                            |
      +------------+---------------+
                   |
                   v
          Application.WriteSession
                   |
                   v
       Session serialized Write loop
                   |
          +--------+---------+
          |                  |
          v                  v
  Activity Buffer      Channel.Write
                             |
                             v
                       serial device
```

`Ctrl+C` is ordinary remote data in the CLI raw-input path. `Ctrl+]` commands remain local. Prompt timestamps are per-CLI presentation state and are inserted only before recognized shell prompts after Session reads, so they never enter the Ring Buffer or MCP cursor path. Session serializes each complete write, including short-write retries, so concurrent payload bytes do not interleave. This does not coordinate writer intent: Session provides no writer ownership, exclusive lease, transaction, priority, arbitration, or shell-state coordination. Activity records actor and confirmed bytes but actor metadata is not sent to the device.

## Serial open and reuse

```text
CLI flags or MCP input
          |
          v
Application.OpenSerial
          |
          +--> load/create TOML --> resolve profile --> apply explicit overrides
          |                                      |
          |                                      `--> optional save
          v
Session Manager.GetOrCreate(transport, endpoint)
          |
          +--> active Session exists --> return it with reused=true
          |
          `--> create Serial Transport --> open Serial Channel --> Session reader
                                      --> optional wake --> Manager registration
```

Concurrent opens for the same exact endpoint wait on one in-progress open. Failed and closed Sessions do not permanently reserve the endpoint.

## Device discovery

```text
periodic Serial.ListPorts
          |
          v
Device Registry scan comparison
          |
          +--> current Device snapshot
          +--> bounded appeared/disappeared events
          `--> conservative state.json identity matching
                            |
                            v
               Application.ConnectionDecision
                            |
                            v
                    ask / connect / deny / none
```

The first successful scan is a baseline and emits no appearance events. A transient later scan failure preserves the last known presence state. Discovery and policy evaluation never open a Transport; a client remains responsible for an approved explicit open.

## Shared HTTP attachment

```text
Physical serial endpoint
          |
          v
   Serial Transport
          |
          v
    Serial Channel
          |
          v
 host-owned Session
          |
          +--> CLI Attachment through MCP
          `--> MCP Client
```

The host Manager shares one active Session for an exact `transport + endpoint` pair within that host process. Each Client or Attachment maintains independent output and activity cursors. Closing one MCP connection releases only that Client; `terminal_close` or Session Host shutdown closes the shared Channel and its underlying serial resource.

## CLI file transfer

```text
local file <--> bounded CLI chunks
                     |
                     v
          existing Session read/write
                     |
                     v
                  Channel
                     |
                     v
               Serial Transport
                     |
                     v
        Linux shell: stty + dd
                     |
                     v
             wc -c + sha256sum
```

The file-transfer use case is layered above Session and does not access Serial Transport directly. A CLI attachment uses the existing terminal tools only as a byte-oriented client of the host-owned Session; no file-specific MCP schema exists. Payloads are bounded, raw chunks. Other readers retain independent cursors, while other writers must avoid the shell during a transfer because Session has no transfer-wide lease.
