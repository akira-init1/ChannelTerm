# Connection Policy Module

`internal/core/connectionpolicy` defines the default response to a discovered endpoint. It is a pure decision module: it does not scan, prompt, open, close, or write.

Policies are:

- `ask`: a client must obtain user approval before a discovery-driven open.
- `auto`: a client may open without another approval prompt when it already has valid connection settings.
- `deny`: a client should ignore the discovery event.

The safe default is `ask`. Configuration parsing is case-insensitive after trimming; unsupported non-empty values are errors rather than silent fallback.

Decision actions are `ask`, `connect`, `deny`, and `none`:

| Present | Active Session | Policy | Action |
| --- | --- | --- | --- |
| false | any | any | `none` |
| true | true | any | `none` |
| true | false | `ask` | `ask` |
| true | false | `auto` | `connect` |
| true | false | `deny` | `deny` |

Application considers `new`, `connecting`, `open`, and `closing` Sessions active for the exact transport and endpoint. `failed` and `closed` are excluded so a later attempt is possible.

Policy applies only to discovery-driven behavior. An explicit user request can still invoke `terminal_open_serial` under `deny`. Likewise, `auto` does not guess baud rate or other settings and does not cause Core to connect by itself; the consuming client remains responsible for any action.

MCP resolves the effective policy from the explicit `--connection-policy` override, then `connection.default_policy`, then `ask`.
