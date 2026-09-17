# Configuration Reference

ChannelTerm keeps three independent user-configuration responsibilities in one
`config.toml`: connection configuration, connection policy, and user
preferences. It separately stores automatically managed device identity state.

## Paths

The default directory is the platform directory returned by Go's `os.UserConfigDir`, followed by `channelterm`.

- User configuration: `config.toml`
- Managed device identity state: `state.json`
- Streamable HTTP bearer credential: `http-auth-token`

CLI and MCP serial opens use `LoadOrCreate`, which creates a minimal `config.toml` with owner-only file permissions where the platform honors them. `channelterm list` treats a missing configuration file as an empty profile list and does not create it.

`--config` and MCP `config_path` select an alternate TOML path for that operation. They do not move `state.json`.

`http-auth-token` is generated from 32 random bytes and stored with owner-only permissions where
the platform honors them. The first HTTP Host, built-in HTTP client, or HTTP selection in
`channelterm init --mcp` or `channelterm init --mcp-show` creates it when absent.
`CHANNELTERM_HTTP_AUTH_TOKEN` overrides this file for all these uses. Creation holds an adjacent
`http-auth-token.lock` file and installs a fully written,
synchronized temporary file, so concurrent first use cannot expose a partial credential. The stored
value must decode to exactly 32 bytes of unpadded Base64URL. The token is authentication material and
must not be committed or copied into logs. The HTTP selection in `init --mcp-show` intentionally
includes it in generated configurations, so that output must be handled as a secret.

## TOML schema

```toml
[serial]
default = "board"

[serial.profiles.board]
port = "COM8"
baud = 115200
data_bits = 8
parity = "none"
stop_bits = "1"
flow_control = "none"
wake = false

[connection]
default_policy = "ask"
```

`[serial]` and `[connection]` are the current persisted sections. The config
model also includes an empty `Preferences` boundary for future terminal display
and UI/TUI/GUI preferences. It has no fields or TOML section yet, and therefore
does not change existing files or serial behavior. When preference fields are
introduced, they will be added under `[preferences]` in this same file; no
separate `preferences.toml` is planned.

| Field | Meaning | Resolution default |
| --- | --- | --- |
| `serial.default` | Profile selected when no profile name is supplied. | none |
| `serial.profiles.<name>.port` | Operating-system serial endpoint. | none; required after resolution |
| `baud` | Positive baud rate. | 115200 |
| `data_bits` | `5`, `6`, `7`, or `8`. | 8 |
| `parity` | `none`, `odd`, `even`, `mark`, or `space`. | `none` |
| `stop_bits` | `1`, `1.5`, or `2`. | `1` |
| `flow_control` | `none`, `software`, or `hardware`. | `none`; only `none` is implemented |
| `wake` | Send one carriage return after a new connection. | false |
| `connection.default_policy` | Discovery response policy: `ask`, `auto`, or `deny`. | `ask` |

An unknown named profile and an invalid connection policy are errors. Invalid serial values are rejected before a physical open.

## Precedence and saving

Serial settings resolve in this order, from highest to lowest precedence:

```text
explicit CLI/MCP overrides
        |
selected named profile
        |
configured default profile
        |
built-in serial defaults
```

Only explicitly present flags or JSON properties override profile values. The port in a target-first `connect` or `attach SER-*` operation is always selected by that target.

`--save NAME` or MCP `save` writes the final resolved profile before opening the transport. It creates or replaces `serial.profiles.NAME`. If `serial.default` is empty, the saved name becomes the default. Existing configuration is otherwise read-only.

Writes use a synchronized temporary file in the same directory followed by replacement. Unix
targets provide atomic rename and directory synchronization. Windows receives a fully synchronized
replacement file but Go does not guarantee that the final rename is atomic and does not expose a
portable directory-sync operation. Profile saving also holds an adjacent `config.toml.lock` file
across the latest read, profile merge, and write, so concurrent ChannelTerm processes preserve
unrelated profile updates. Lock files contain no configuration or credential data.

Saving a serial profile does not create an empty `[preferences]` table. User
preferences never supply serial values and do not participate in profile
resolution or Transport opening.

## Connection policy

The MCP command resolves policy in this order:

```text
--connection-policy
        |
connection.default_policy
        |
ask
```

`ask` requires the consuming client to request user approval after discovery. `auto` permits the client to open when it already knows the required settings. `deny` tells the client to ignore a discovery-driven connection. None of these values opens a Session automatically or blocks an explicit user-requested open.

## `state.json`

`state.json` is a versioned, ChannelTerm-managed file. Version 1 contains durable device records with `device_id`, transport, matching evidence, last endpoint, and timestamps. It does not store Sessions, terminal output, profiles, labels, or authentication data.

The Device Registry persists an identity only when it has conservative evidence:

- `usb_serial`: transport + VID + PID + USB serial number.
- `usb_path`: transport + VID + PID + USB path, only when no USB serial is present.
- `runtime`: insufficient persistent evidence; the ID is process-local and not written.

State updates use a temporary file in the destination directory followed by rename. Corrupt JSON, malformed records, and unsupported versions are returned as errors without silently replacing the original file. Version 1 does not implement cross-process file locking; one Core process should own a given state file.
