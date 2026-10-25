# Desktop Host API

The standalone HTTP Host mounts `/gui/v1/ssh`, `/gui/v1/ssh/confirm` and `/gui/v1/serial-profiles` beside its configured MCP endpoint.
This is a separate authenticated HTTP adapter, not an MCP tool. Existing MCP names,
schemas and stdio behavior are unchanged. `internal/guiapi` calls the same
Application connection/configuration use cases and Session Manager used by the Host's CLI/MCP adapters.

## Exposure and authentication

All methods require the existing Host Bearer token and run behind its existing
constant-time authentication middleware. They inherit the Host listener; no second
listener, token, permissions store or automatic Host startup is added. The Electron
client accepts numeric loopback HTTP endpoints and derives this fixed path in Main.
Renderer cannot supply the API URL or access the Bearer token. Redirects are refused.

Requests with an `Origin` header are rejected, including authenticated browser
requests; no CORS access is granted. Responses use `Cache-Control: no-store`. The
API grants an authenticated Host client the ability to initiate outbound SSH and
read selected identity/trust files on that Host, and persist an explicitly approved unknown SSH key. A remotely exposed HTTP Host still
requires the trusted-network precautions in the [Host workflow](../getting-started/mcp-server.md).

Older Hosts, stdio Hosts and foreground CLI `ssh` owners do not expose this route.
If an explicitly configured MCP path equals a desktop route, that path keeps its MCP
meaning and that desktop route is not mounted. The GUI probes availability and disables
SSH creation when unavailable; existing shared SSH Sessions can still be attached.

## Requests and responses

`GET /gui/v1/ssh` returns `200 {"available":true,"advanced":true,"trust_confirmation":true}` after authentication.
Older implementations may omit optional capabilities; clients must not send advanced settings or
request trust confirmation without the corresponding advertisement. Query strings
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
| `confirm_host_key` | Optional boolean; false/omission retains strict noninteractive verification. True opts into the confirmation stream below. |
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
Unknown keys are rejected unless the caller opts into the advertised confirmation flow and the
user approves the actual handshake identity. Changed and revoked keys are always rejected.
Older Hosts require installing the verified server key on the Host before connecting.
No passwords, passphrases or private-key contents enter Session metadata, logs or
responses. Mutable request/credential buffers are cleared on completion; GUI password
inputs clear after submission. Managed runtimes cannot guarantee erasure of all
temporary string copies. Nothing is written to GUI preferences or connection profiles.

Without confirmation streaming, successful creation or reuse returns:

```json
{"session_id":"opaque-session-id","reused":false}
```

The GUI refreshes the shared list and attaches using existing Session tools. Closing
the dialog after completion, detaching, disconnecting or exiting the GUI does not close
the shared Session. Host shutdown and existing Session close semantics remain authoritative.
Reuse of an open endpoint preserves its existing authentication and metadata, as defined
by `Application.OpenSSH`; it does not reauthenticate an already-running Session.

Non-streaming failures contain only a fixed code, such as `{"error":"host_key_rejected"}`:

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
15 seconds, including the Transport's ten-second default network setup budget. Confirmation-enabled
requests add a bounded 120-second interaction allowance. Main allows
another five seconds for the response, while disconnect still cancels immediately.
Request cancellation releases incomplete setup through Application/Transport. If a
response is lost after registration, the Session may already exist: refresh the shared
list before retrying. Neither Main nor the API automatically repeats creation or writes.

Serial scanning/creation continues through existing `terminal_list_serial_ports` and
`terminal_open_serial` MCP tools. The GUI sends explicit form settings, overriding resolved
profile fields. Wake defaults to false and requires selecting the wake checkbox on each form
or profile application. Selecting a port and pressing Connect is explicit user intent;
scanning, reading profiles and saving alone open nothing.

## SSH first-trust confirmation

The GUI requests `confirm_host_key:true` only when the Host advertises `trust_confirmation:true`.
After ordinary request validation, the response uses `application/x-ndjson`: at most one `trust`
event followed by one terminal `result` or `error` event. HTTP 200 may therefore carry a terminal
error; clients must inspect `event`. Validation/authentication errors before streaming keep their
normal HTTP statuses. Without opt-in, existing JSON responses and strict unknown-key rejection remain.

```json
{"event":"trust","challenge_id":"64-lowercase-hex-capability","address":"[example.invalid]:2222","algorithm":"ssh-ed25519","fingerprint":"SHA256:verified-server-fingerprint"}
{"event":"result","session_id":"opaque-session-id","reused":false}
```

The fingerprint above is abbreviated; real fingerprints use the Transport's complete SHA-256 value.
A failed stream ends with `{"event":"error","error":"host_key_rejected"}` or another existing safe
setup code. Main validates identities and caps each stream at 16 KiB, with an 8 KiB pending frame.
It owns the random Host capability and gives Renderer a separate window-local, single-use prompt ID.
Renderer cannot submit a key, fingerprint, trust-file path or decision for another window's setup.

The user independently verifies the displayed address, algorithm and fingerprint, then chooses
**Trust and connect** or **Reject connection**. Reject initially receives keyboard focus. The
confirmation expires after 120 seconds; window close, disconnect or request cancellation cancels
pending setup. Main sends one authenticated `POST /gui/v1/ssh/confirm` with
`{"challenge_id":"...","accept":true}` (or false). That endpoint has the same Origin/token policy,
a 1 KiB body limit, five-second body deadline and two concurrent decision slots. Missing/consumed/
expired capabilities return `409 confirmation_expired`; a valid decision returns `{"accepted":true}`.
No list or recovery endpoint exposes pending capabilities, and decisions/connection attempts are not replayed.

Approval resumes the same paused Transport handshake. Transport rechecks the exact key under its
existing `known_hosts` file lock before appending it, preserving comments and records. A changed or
revoked record installed while the dialog is open rejects approval. No new connection is substituted
between preview and approval. Reused Sessions retain their existing authentication and do not prompt.
Trust persistence happens before later authentication/setup; an explicitly approved key may remain
saved even if those later steps fail or the response is lost. Re-read trust/Session state before retrying.

This dialog saves to the **Host-side** `known_hosts_file` chosen for that connection (or its normal
Host default). Dedicated local file transfers still use their separately selected **local companion**
trust file and strict verification; approving a Host connection does not authorize a different local
file, path or key. No trust-file path or credential contents are returned in prompt IPC.

## Serial profiles

`GET /gui/v1/serial-profiles` returns `{"default":"board","profiles":[{"name":"board","profile":{...}}]}`.
The profile object contains `port`, `baud`, `dataBits`, `parity`, `stopBits`, `flowControl`, and
`wake`, resolved through the same Core defaults as CLI profiles. Missing configuration returns
an empty list without creating a file. The endpoint never accepts or returns configuration paths,
unrelated sections, credentials, terminal settings or Session identifiers.

`POST` accepts one of:

```json
{"action":"save","name":"board","replace":false,"profile":{"port":"COM8","baud":115200,"dataBits":8,"parity":"none","stopBits":"1","flowControl":"none","wake":false}}
{"action":"default","name":"board"}
```

Save persists the displayed connection settings without opening hardware. Existing names require
`replace:true`; otherwise the result is `409 profile_exists`. Saving the first profile sets the
default, matching CLI saving. The default action selects an existing name without modifying its
values; missing names return `404 profile_not_found`. Success is `200 {"saved":true}`. Delete,
rename and clearing the default are not exposed. Existing CLI/MCP save behavior is unchanged.

Names are nonblank, trimmed, at most 128 UTF-8 bytes, without control characters. Ports are nonblank
and at most 512 bytes without control characters; baud is 1?4000000. Other serial fields use the
existing configuration enums/ranges. GET rejects invalid or unsupported entries and more than 256
profiles with `422 configuration`, rather than silently omitting them. Edit such configurations
through the CLI/file workflow before retrying. GUI response consumption is bounded to 512 KiB.

The same Bearer/Origin policy as SSH applies. A trusted authenticated client may now read and write
serial profiles in the Host user's configuration. Unknown fields, query strings and trailing JSON
are rejected. POST bodies are limited to 8 KiB, with a five-second read deadline. At most two profile
operations run concurrently. The five-second operation context cancels lock waits and is checked
before saving; filesystem persistence already in progress may finish after cancellation. Main waits
at most eight seconds and disconnect cancels the request. Refresh after an uncertain result; no
mutation is replayed. Configuration errors use fixed safe codes and never disclose local paths.

Persistence uses Core's existing locked read/merge/replace transaction, preserving other profiles
and modeled configuration sections. See [configuration](configuration.md#precedence-and-saving)
for OS atomicity limits. No second GUI profile store, new TOML fields, listener or MCP tool is added.
