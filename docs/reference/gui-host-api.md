# Desktop Host Connection API

The standalone HTTP Host mounts `/gui/v1/ssh` beside its configured MCP endpoint.
This is a separate authenticated HTTP adapter, not an MCP tool. Existing MCP names,
schemas and stdio behavior are unchanged. `internal/guiapi` calls the same
`Application.OpenSSH` and Session Manager used by the Host's MCP tools.

## Exposure and authentication

Both methods require the existing Host Bearer token and run behind its existing
constant-time authentication middleware. They inherit the Host listener; no second
listener, token, permissions store or automatic Host startup is added. The Electron
client accepts numeric loopback HTTP endpoints and derives this fixed path in Main.
Renderer cannot supply the API URL or access the Bearer token. Redirects are refused.

Requests with an `Origin` header are rejected, including authenticated browser
requests; no CORS access is granted. Responses use `Cache-Control: no-store`. The
API grants an authenticated Host client the ability to initiate outbound SSH and
read selected identity/trust files on that Host. A remotely exposed HTTP Host still
requires the trusted-network precautions in the [Host workflow](../getting-started/mcp-server.md).

Older Hosts, stdio Hosts and foreground CLI `ssh` owners do not expose this route.
If an explicitly configured MCP path equals `/gui/v1/ssh`, that path keeps its MCP
meaning and the desktop API is not mounted. The GUI probes availability and disables
SSH creation when unavailable; existing shared SSH Sessions can still be attached.

## Requests and responses

`GET /gui/v1/ssh` returns `200 {"available":true,"advanced":true}` after authentication.
Older implementations may omit `advanced`; clients must not send advanced settings to them. Query strings
are not accepted. `POST` requires `Content-Type: application/json` and one JSON object;
unknown fields, trailing JSON, malformed or oversized bodies are rejected.

| Field | Meaning and bounds |
| --- | --- |
| `host` | Required hostname or unbracketed IP, maximum 253 UTF-8 bytes; no embedded port. |
| `user` | Required SSH user, maximum 128 bytes. Transport validates user/host grammar. |
| `port` | Required integer from 1 to 65535. The GUI defaults to 22. |
| `label` | Optional display-only label, maximum 128 bytes; no control characters. |
| `identity_files` | Optional ordered list of at most 8 Host-side paths, each at most 4096 bytes. |
| `known_hosts_file` | Optional Host-side trust-file path, at most 4096 bytes; blank uses the Host user's `.ssh/known_hosts`. |
| `password` | Optional password, at most 4096 bytes. Omission differs from an empty password. |
| `key_passphrase` | Optional encrypted-key passphrase, at most 4096 bytes. |
| `advanced` | Optional object described below; omission preserves existing Transport defaults. |

The `advanced` object requires every field below except `keepalive_seconds` and `write_timeout_seconds`, which may be omitted for zero. Durations are integer seconds; fractional,
negative and out-of-range values are rejected before Application setup:

| Field | Bounds; default when `advanced` is omitted |
| --- | --- |
| `timeout_seconds` | 1–120; 10-second network setup budget. |
| `keepalive_seconds` | 0–3600; 0 disables authenticated keepalive probes. |
| `keepalive_count` | 1–10; 3 intervals bound the keepalive response wait. |
| `write_timeout_seconds` | 0–3600; 0 disables the Transport write timeout. Expiry closes the SSH connection; input is never replayed. |
| `term` | 1–64 ASCII letters/digits or `_ . + -`; `xterm-256color`. |
| `columns`, `rows` | Integers 1–1000; 80 columns and 24 rows. Initial PTY size only. |

These values configure the existing SSH Transport and are not persisted. Reusing an existing
Session preserves its original setup; it does not change timeouts, keepalive or PTY dimensions.
No attachment gains automatic remote resize authority from these fields.

Without explicit identities or a password, existing Host-side `id_ed25519` and
`id_rsa` files are tried in that order. Private-key and password authentication,
including encrypted keys, use the existing SSH Transport. Supplying identities and
a password permits Transport's key-first/password-fallback behavior. A supplied
password is attempted once; a passphrase is attempted once per selected identity.
No SSH agent, keyboard-interactive authentication, arbitrary exec, file transfer,
profile persistence, or host-key bypass is exposed.

Known, unchanged keys must pass the existing Transport's `known_hosts` verification.
Unknown, changed and revoked keys are rejected. This API never approves or writes
trust records: verify and install the server key on the Host before connecting.
No passwords, passphrases or private-key contents enter Session metadata, logs or
responses. Mutable request/credential buffers are cleared on completion; GUI password
inputs clear after submission. Managed runtimes cannot guarantee erasure of all
temporary string copies. Nothing is written to GUI preferences or connection profiles.

Successful creation or reuse returns:

```json
{"session_id":"opaque-session-id","reused":false}
```

The GUI refreshes the shared list and attaches using existing Session tools. Closing
the dialog after completion, detaching, disconnecting or exiting the GUI does not close
the shared Session. Host shutdown and existing Session close semantics remain authoritative.
Reuse of an open endpoint preserves its existing authentication and metadata, as defined
by `Application.OpenSSH`; it does not reauthenticate an already-running Session.

Failures contain only a fixed code, such as `{"error":"host_key_rejected"}`:

| HTTP status | Codes / meaning |
| --- | --- |
| 401 | Existing Host authentication rejection. |
| 403 | `origin_forbidden`. |
| 404 / 405 / 415 | `not_found`, `method_not_allowed`, `invalid_content_type`. |
| 400 | `invalid_input` or `configuration`. |
| 429 | `busy`: at most two concurrent SSH setup requests per Host handler. |
| 422 | `host_key_rejected`, `authentication_failed`, `connection_failed`, or `cancelled`. |

The body is limited to 32 KiB with a five-second read deadline. Each request has a
setup deadline of `timeout_seconds + 5` seconds measured from request handling, defaulting to
15 seconds, including the Transport's ten-second default network setup budget. Main allows
another five seconds for the response, while disconnect still cancels immediately.
Request cancellation releases incomplete setup through Application/Transport. If a
response is lost after registration, the Session may already exist: refresh the shared
list before retrying. Neither Main nor the API automatically repeats creation or writes.

Serial scanning/creation continues through existing `terminal_list_serial_ports` and
`terminal_open_serial` MCP tools. The GUI sends explicit serial settings with `wake:false`,
never a startup command or profile-save request. Selecting a port and pressing Connect is
explicit user intent; scanning alone opens nothing.
