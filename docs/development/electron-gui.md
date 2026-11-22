# Electron GUI Development

The [Electron prototype](../architecture/electron-gui.md) automatically connects to the default
local Host, starting its companion when needed, then creates or attaches to shared Sessions. Source responsibilities and directories are in the
[repository map](../architecture/repository-map.md#electron-source-ownership); this guide owns
installation, execution, checks, and troubleshooting.

## Dependencies and initialization

Use Node.js 22.12 or newer (a supported LTS release), npm, and a native desktop session. Dependencies
are pinned by the independent `gui/electron/package-lock.json`. Electron, React, TypeScript, Vite,
xterm.js, Lingui Core, and the MCP TypeScript SDK are local to that project. No nested repository or global npm
package is required. Go 1.26+ is needed to build the native companion and isolated integration fixture.
The JavaScript GUI build remains independent of Go.

From the repository root:

```powershell
Set-Location gui/electron
npm.cmd ci
npm.cmd run dev
```

On Linux/macOS use `cd gui/electron` and `npm` in place of `npm.cmd`. The `.cmd` form avoids Windows
PowerShell script-execution policy errors without changing system policy. Electron may download its
platform runtime on first launch; dependency and runtime downloads require network access.

`dev` compiles PO catalogs, builds Main/Preload once, starts Vite on `127.0.0.1:5173`, and opens the native desktop window.
Renderer changes use Vite refresh. Restart `dev` after Main, Preload, shared bridge, or build-script
changes; a renderer reload disconnects its Host client. Closing Electron also stops this development
server, while the shared Host remains running. Port 5173 is strict: a conflict fails startup rather than loading an unrelated server.

Build and launch the production assets independently:

```powershell
npm.cmd run build
npm.cmd start
```

`build` strictly compiles PO catalogs, type-checks TypeScript, bundles Main and sandbox-compatible Preload with esbuild, builds
the React/xterm renderer with Vite, and collects runtime license texts into `dist/`. `start` loads
those assets through the local application scheme. This produces runnable application assets,
not an installer. For the default endpoint, `start` and `dev` check the companion's build record
against native platform/architecture, Go source/module/embedded-resource contents, and executable
bytes. `host:build` records these inputs beside `dist/host/channelterm[.exe]`. A matching build
needs no Go invocation; a missing, changed or unrecorded build requires preparation.
Only refusal of the default TCP listener permits automatic rebuilding. An occupied listener or
uncertain probe preserves existing processes; a stale companion is not launched if that listener
later disappears. Preparation failure still allows connecting to an existing compatible Host,
but blocks launching an unverified companion. This build record is not an authentication mechanism.
To update an active companion, finish shared Session work, stop that Host, and run
`npm.cmd run host:build`, then restart `start`/`dev`. No process is killed by preparation, and no
credential is deleted or changed by the GUI. Custom endpoints skip companion Host preparation.
Node/npm are development requirements; the local desktop package includes
its own Electron runtime and the platform-matched Host under `resources/host/`. See the
[release boundaries](../architecture/electron-gui.md#multiple-frontends-and-release-boundaries).

### Old Host or missing credentials after an upgrade

A GUI rebuild does not upgrade an already-running Host. If only the retired `http-auth-token`
exists, the GUI displays a localized legacy-configuration diagnostic rather than repeatedly
suggesting an unspecified credential failure. This is a migration hint, not proof of the listener's
identity or version. Finish all shared Session work before stopping its Host; stopping it affects
every client. Update/rebuild the Host, restart the GUI and let the new Host initialize/migrate its
credentials. Retrying cannot update a running executable. Keep old files until migration has been
verified; do not put the retired token in `CHANNELTERM_HTTP_AUTH_TOKEN`.

Missing identity, missing local management credential, invalid files and unreadable files have
separate structured diagnostics. Check that GUI and Host use the same OS account and
`CHANNELTERM_HOST_CONFIG_DIR`. A valid explicit independent credential remains authoritative;
the GUI never replaces it or falls back to the shared token. After correcting configuration, Retry
rereads credentials. See [migration and rollback boundaries](../getting-started/http-authentication-migration.md).

The native production/development suites test legacy-only configuration against an occupied isolated
Host, localized errors, repeated safe retry, unchanged Sessions and successful connection after
credential initialization. This regression runs even when the user's default port 37099 is occupied;
only the separate default-port launch test is skipped in that case. Build-preparation tests cover
dirty inputs, modified binaries, absent build records, compiler failure and uncertain listener probes.

## Multiple windows and terminal tabs

Host Client presence integration tests use isolated Go Hosts to check distinct window instances,
multiple attachment uses, parent delegation, write provenance, malformed registration rejection,
and disconnect without Session closure. The ordinary Host Client tests also retain structured
busy/timeout error checks: tool discovery must not replace Main's result validation with an SDK
success schema applied to error results. Native desktop suites exercise window cleanup and the
existing workspace restore path. Instance inventory is currently a protocol capability; no new
renderer presence or takeover UI is implied.

Use **Window > New Window** to open another GUI window on the same configured Host. A connected
source creates a connected destination without starting another Host. **New Tab** (or the strip's
plus button) opens an empty tab with the Session chooser. Select an open Session or use
**Session > New Connection...**. Selecting a Session fills an empty tab; otherwise it activates
an existing tab for that Session or adds a new one. Start from an empty tab to attach the same
Session twice. **Next/Previous Tab** and **Next/Previous Window** wrap within their respective lists.

Right-click a terminal tab and choose **Move Tab to New Window**. This also works for background
tabs without changing which source tab is selected. The source stays until the destination has
attached and rendered recent retained output. Failure preserves the source, without reopening the
device or replaying input. The destination shows the retained-output limitation: at most 64 KiB,
with no guarantee of full screen/parser state or earlier history. **Close Tab** releases one GUI
attachment; **Close Window** releases that window's clients; **Disconnect from Host** affects only
that window; **Exit GUI** closes the app. None closes a shared Host Session. Owned file work blocks
the owning window's close/disconnect and app exit until finished or explicitly cancelled.

Limits are 16 windows (including pending moves), 32 tabs per window and one pending move per source
window. At 32 attached tabs, close one before changing presentation settings, which safely attaches
a replacement first. Local tab reordering, two-pane splitting and bounded workspace recovery follow
the [window contracts](../reference/gui-menus.md#window-action-contracts). Lossless xterm
migration is not implemented. See [window ownership](../architecture/electron-gui.md#windows-and-tab-handoff).

To release a shared connection explicitly, use **Session > Close shared Session...** and check
the displayed full target ID before confirming. This affects every client and can interrupt active
work, including a leased Session; use the operation's own cancellation flow when recovery is needed.
Cancel/Escape leaves the connection open. The native suite verifies cancellation, stale IPC rejection,
confirmed closure and another client's observation using the isolated Host. Unit coverage also
checks foreign/detached handles, concurrent close/write rejection and the actual leased-close
contract. These tests do not prove physical serial control-line or remote-process cleanup effects.

**Copy for AI** in the status bar copies full target metadata in the window's language. The desktop
suite verifies all nine languages through the real Main clipboard path, restores the prior clipboard,
and checks visibility at 760 px. Unit tests reject foreign, stale and closed handles, detach during
the metadata query, oversized fields, control/newline injection and credential-bearing Host URLs.
No AI service or real device is used by these checks.

Multiline paste checks use the isolated echo Session: cancel/Escape sends nothing, native paste
uses the same preview as shortcuts, repeated confirmation submits once, and bracketed paste retains
its markers and newline conversion. Acquiring a Lease while previewing invalidates the draft even
after release; detaching discards it. The production Host still validates encoding and write authority.
These desktop checks send only test strings to fake I/O, not shell commands to a real device.

## File client build and verification

File operations use another build of the same CLI at `dist/files/channelterm[.exe]`. Main invokes
only its fixed file subcommands with argument arrays and no shell. This separate path lets the
file client be updated while Windows keeps the persistent Host executable locked. After changing
the Go file client, run `npm.cmd run files:build`; it does not stop or replace the running Host.
`start`/`dev` prepare it only when missing, including with a custom loopback Host endpoint. A
packaged application must include the platform-matched executable under `resources/files/` as well
as `resources/host/`. File features require the new client; the MCP Host protocol is unchanged.

The native suite checks external transfer progress and binary-output suppression against the real
isolated Host, plus file-picker navigation/target binding using explicitly test-only IPC snapshots.
Screenshots: `test-results/file-transfer-progress.png` and `file-receive-browser.png`. These snapshots
do not prove a physical board's shell/tools or native file-dialog selection. Hardware acceptance
must separately round-trip a disposable binary and directory, compare SHA-256, verify cwd isolation,
and test cancellation without replay. See the [transfer workflow](../getting-started/file-transfer.md#electron-file-picker-and-progress).

The Node test suite also builds the real Go file CLI and runs Main's dedicated SSH adapter against
a disposable password-authenticated loopback SFTP server. It cancels an in-progress upload,
resumes the partial file, downloads and compares bytes, and confirms the terminal Session remains
usable. Request/endpoint validation, busy exclusion and secret-free progress are covered. Advanced-option tests cover default compatibility, malformed/conflicting
intent, exact overwrite approval and stale targets, final receipt order/counts, checksum failures,
partial statistics, and a stalled-child cleanup deadline. Real loopback SFTP checks existing-target
refusal, verified upload/download replacement and an operation deadline; Core tests cover checksum
mismatch preservation and temporary-file cleanup. This
does not prove external OpenSSH/SCP interoperability, physical network interruption, or native
file-dialog behavior; those remain separate acceptance checks.

The native desktop suite additionally exercises the SSH file form and progress panel through
Renderer, IPC and the real CLI: cancel an upload, manually resume, and download identical bytes.
It substitutes only the native picker result with a disposable local path; the production Main
selection/endpoint checks still run. Screenshots are `test-results/ssh-file-dialog.png` and
`ssh-file-progress.png`. The SCP option disables resume; SCP server interoperability is covered
separately from this SFTP fixture.
The same desktop scenario lists the uploaded file through **Browse SFTP**, inspects its size,
checks a missing-path error, retries with Enter, selects the path without starting a transfer,
and then downloads it. `test-results/ssh-browser.png` captures this view. The Node suite covers
listing limits, malformed records, failed child exit, credential isolation, cancellation before
launch/during I/O, stale targets and root-directory properties using disposable fixtures.
The management scenario uses only this fixture's in-memory paths: cancel the native confirmation,
create directories, reject nonempty removal, rename, remove empty directories and delete the
already-verified fixture payload. `test-results/ssh-management.png` records the result. Native
confirmation is substituted to inspect its exact target and default-cancel policy; Main/IPC/CLI
and SFTP execution remain real. Node tests additionally cover finite intent validation, stale
registration, invalid receipts/exit status, cancellation and no retry; Go tests cover typed safe
error codes and cancellation after acknowledgement. These fixtures do not prove real server
permissions or hardware behavior.
The same native scenario checks advanced options in all nine languages at a compact window size,
SCP/resume conflicts, invalid deadlines, default-cancel overwrite, exact native paths, and real
verified SFTP replacement. Screenshots are `ssh-advanced-options.png`, `ssh-advanced-results.png`
and `ssh-advanced-compact.png` under `test-results/`. Native picker/confirmation responses are test
seams; the production renderer, trusted IPC, CLI and loopback SFTP execution remain in use.
During an owned transfer, native tests also repeat window-close and app-quit requests, verify one
themed notice and no native message box, dismiss with Escape/Continue, and then explicitly cancel
the transfer. `test-results/file-close-notice.png` captures the notice.

## Shared default highlight definitions

Edit `internal/cli/highlight/defaults.json` from the repository root for built-in palette,
category, priority or pattern changes. Version 1 records each pattern, optional `i` flag,
selected capture group, category and numeric priority. Higher priorities own overlaps; the
CLI preserves uncovered lower-priority fragments. Palette entries retain either a hex color
or an ANSI foreground code, including the delimiter's exact TrueColor conversion.

Go embeds this file; `src/shared/default-highlights.ts` imports the same JSON without a Go
runtime or a second generated copy. The terminal view consumes these definitions for local
semantic styling.
Do not modify Session bytes to apply these definitions.

Maintain `internal/cli/highlight/testdata/defaults-cases.json` alongside rule changes, then run
`go test ./internal/cli/highlight` from the root and `npm.cmd run check` here. Both engines
verify every rule and prompt against the same corpus, including capture groups, unrelated
text, Unicode around tokens and prompt-prefix rejection. Use the common regex syntax already
present: explicit ASCII classes and separate flags, not Go POSIX classes or inline flags.
The corpus does not establish general equivalence of Go and JavaScript Unicode case folding;
renderers also need their own byte/UTF-16/cell mapping and stream-control tests.

**Settings > Preferences... > Terminal style** defaults to **Default (CLI colors)**.
**Plain (white on black)** suppresses semantic and remote colors for retained and new content,
while keeping terminal controls, selection and parser state. The preview and **Restore defaults**
use the same palette. Existing font-only preferences migrate without losing font choices.
HEX stays a byte display and does not receive semantic ANSI sequences.

See [line metadata verification](#line-metadata-verification) for receipt timestamps and line numbers.

The native suite verifies semantic colors, 16/256/TrueColor and inverse suppression, OSC background
changes, selection visibility, CR overwrite, alternate-screen entry/exit and restoration of retained
colors when switching back. An independent Host observer confirms raw output is unchanged and
style changes send no device bytes. Screenshots are `terminal-semantic-style.png` and
`terminal-monochrome-style.png` under `test-results/`. These use fake I/O, not a physical terminal.

## Menu translations (Lingui 5.9.0)

Core, CLI, configuration helpers, and the PO formatter are pinned to **5.9.0**. Use the local
commands below; newer Lingui documentation may describe `check` commands unavailable in this
version. The [menu reference](../reference/gui-menus.md#translation-and-keyboard-behavior) owns
language-selection behavior. The repository map owns source locations.

`src/shared/menu-messages.ts` defines stable IDs and English defaults with Lingui's official
[annotated `MessageDescriptor` form](https://lingui.dev/ref/macro), verified against the pinned
5.9 extractor:

```typescript
/* i18n */ { id: "help.quick", message: "Quick help" }
```

Keep the annotation and unquoted `id`/`message` property names, as shown; the 5.9 extractor reads
these descriptors directly. Menu code continues to reference stable IDs. No runtime macro or
Vite-only Babel transform is necessary, and source descriptors are not imported into either
runtime bundle. Translate messages in `src/locales/<locale>/messages.po` for each of the
[nine supported locales](../reference/gui-menus.md#translation-and-keyboard-behavior); each PO
uses the existing explicit ID as `msgid`. English defaults are filled
for new source entries. Do not edit compiled arrays or keep translations in TypeScript logic.

From `gui/electron`, after adding or changing a message:

```powershell
npm.cmd run i18n:extract
# Edit the PO translations, then check and compile:
npm.cmd run i18n:check
npm.cmd test
```

Extraction scans `src`, merges new messages into each configured catalog, preserves existing
translations, and removes obsolete entries with `--clean`. Review PO changes. To deliberately
replace existing English translations after changing English defaults, run
`npm.cmd run i18n:extract -- --overwrite`; it does not overwrite target-language translations.
The extraction regression test uses disposable copies to prove that a new message and an additional
locale preserve old translations. No translation service or account is used.

`i18n:check` runs the official `lingui compile --strict`; it rejects missing target translations
and invalid message syntax. Extract first: Lingui 5's compile command validates catalogs, not
source/catalog synchronization. Menu tests additionally require every defined/menu ID to exist
in the compiled catalogs. `fallbackLocales: false` intentionally prevents explicit fallback
configuration from masking missing translations in strict mode.

`npm.cmd run i18n:compile` is the non-strict development command. In Lingui 5, missing target
translations fall back to `sourceLocale: "en"` during compilation; an entirely unknown ID falls
back to the ID. This permits a local preview but does not pass the production gate. Tests cover
both the strict failure and the English fallback, including an ICU placeholder.

The official compiler writes ignored `src/locales/*/messages.ts`; these files are also excluded
from Prettier. `src/locales/catalogs.ts` only imports/registers these generated resources. Main
and Renderer each create their own Lingui instance from their bundled copies. Preload does not
load catalogs or translation tooling.

`pretypecheck` and `pretest` strictly compile catalogs so those commands work on a fresh checkout.
The process build helper also compiles before esbuild starts: production is strict, while `dev`
and `test:desktop:dev` allow English development fallbacks. Vite consumes the same generated
TypeScript through ordinary imports; its config does not affect Main. `start` uses existing
production assets. Restart `npm.cmd run dev` after changing PO files or definitions so both
processes load the same revision; running compilation alone may refresh Renderer but cannot
replace the already-bundled Main catalog.

To add a language, extend `locales` in `src/shared/menus.ts`, add its generated-module import
and registration in `src/locales/catalogs.ts`, and define one `language.<locale>` picker label.
The Lingui config derives its locale list from the shared list. Extract, translate the new PO
catalog and picker label, check, and extend language-switch tests. Existing message definitions
need no additional array element or per-message language structure change.

## Connect to an existing Host

An updated standalone HTTP Host or foreground CLI `ssh` Host supports GUI SSH creation.
Older Hosts may support only attachment. After rebuilding the Go executable, restart the intended Host before reconnecting
the GUI. Host shutdown closes its Sessions, so finish work in those Sessions first.

Opening the GUI automatically connects to `http://127.0.0.1:37099/mcp`. If that TCP listener
is absent, Main starts the native companion with fixed loopback HTTP arguments, waits for
readiness, and authenticates. No terminal command or existing Session is needed. An existing
listener is reused; wrong credentials, incompatible services, probe timeouts and occupied ports
never start a replacement or select another port. Failures expose **Retry connection**.

By default the Host survives GUI exit and manual disconnect so other clients can continue
using shared Sessions. New desktop companions independently release unused Sessions after a
30-second grace; explicit configuration can retain them or enable full idle exit. Existing Host
policy is unchanged by GUI connections. See [Host idle policy](../reference/configuration.md#host-idle-policy). Manual disconnect stays disconnected until explicitly reconnected.
To stop an automatically started Host, finish shared work and stop its `channelterm` process
using the OS process manager; stopping it closes all of its Sessions. There is no GUI Host-stop
action. See the [Host workflow](../getting-started/mcp-server.md) for manual ownership.
Custom loopback endpoints remain manual and never start a companion.

The [GUI configuration reference](../reference/configuration.md#electron-client-settings) owns
endpoint and credential precedence. For an existing Host on another loopback port, set the
GUI-only endpoint override before starting Electron:

```powershell
$env:CHANNELTERM_GUI_ENDPOINT = 'http://127.0.0.1:37100/mcp'
npm.cmd start
```

Linux/macOS equivalent: `CHANNELTERM_GUI_ENDPOINT=http://127.0.0.1:37100/mcp npm start`.
The Host information dialog can submit a bounded numeric loopback endpoint to Main when no environment
override is set. Main validates and saves it, then releases only this window's client resources.
Use **Connect to Host...** afterward; no input or connection setup is replayed. No Host configuration
file, listener, credential or idle policy is changed. See the
[settings contract](../reference/configuration.md#electron-client-settings) for persistence and windows.

The dialog shows the Desktop application version, Electron runtime version, connected MCP server
version, and bundled CLI package version. Missing or invalid metadata is **Unknown**. The bundled CLI
version requires a matching `package-manifest.json` SHA-256 for the fixed file companion; source builds
without that manifest remain unknown. This is local metadata consistency, not signature verification
or proof of the running Host's binary. SSH, remote size control and managed-transfer capabilities come
from Main's existing authenticated probes; they do not confer device permission. Disconnect clears
live version/capability validity. Connection failures retain the existing localized recovery diagnostics.

Main reads the independent local management credential selected by `http-host.json`. Match the
Host's `CHANNELTERM_HOST_CONFIG_DIR` when it uses a non-default authentication directory. Supply
an independent registered client token through `CHANNELTERM_HTTP_AUTH_TOKEN` using your normal
secret-handling procedure;
never put it in React code, a `VITE_` variable, source control, or URL parameters.

**Session > New Connection...** opens the unified form after Host connection. Choose Serial or
SSH inside its Protocol selector; SSH is disabled without the Host's desktop API capability:

- Serial: scan the Host's available ports or enter a port manually, set baud/framing/flow control,
  and press Connect. Scanning is read-only. The default form is 115200, 8N1, no flow control;
  wake is off unless explicitly selected. Expand **Serial profiles** to apply/save Host CLI
  settings or select a default; profile application resets wake to off. Successful setup refreshes the shared list and attaches the new/reused Session.
  **Scan ports** also exposes read-only device discovery and the selected device's Host decision;
  see the [menu workflow](../reference/gui-menus.md). Discovery never opens a port automatically.
- SSH: enter host, port and user, then choose password or private-key authentication. Key paths
  and `known_hosts` paths refer to files on the Host. Blank key paths use existing default
  identities; encrypted keys accept a passphrase. Blank trust path uses the Host user's
  `.ssh/known_hosts`. Updated Hosts display an unknown-key confirmation bound to the current
  handshake; independently verify its address, algorithm and fingerprint before trusting. Changed
  and revoked keys always fail. Older Hosts require prior installation of a verified key. Password inputs clear on submission and credentials are never saved as preferences.
  On a Host advertising advanced SSH support, expand **Advanced SSH settings** for connection
  timeout, keepalive interval/response limit, write timeout, terminal type and initial PTY dimensions.
  Defaults preserve existing setup. Zero disables keepalive or write timeout; write timeout expiry
  closes the connection without replay. These fields affect new connections, not reused Sessions
  or later window resizes. Older Hosts retain the original form without these fields.

Creation failures keep the existing attachment. An uncertain result is not automatically retried;
refresh Sessions before making another explicit attempt. See the [desktop Host API](../reference/gui-host-api.md)
for the exact security, capability and lifetime boundaries.

1. Wait for automatic local connection (or select **Connect to Host** for a custom endpoint), then a Session in the sidebar or
   **Session > New Connection...** to create one. The initial terminal screen
   contains a recent output snapshot, not an unlimited log.
2. Type in the terminal. Input goes through `terminal_write` with actor `user`; Ctrl+C is ordinary
   terminal input when input is enabled. No command delimiter is added by the adapter.
3. A Host lease makes the view read-only. A rejected write discards queued input; **Resume input**
   acknowledges recovery when the Host no longer reports a lease. It does not replay old input.
4. **Close tab (detach)** (the current attachment's close icon), **Disconnect Host**, or closing the window leaves the shared Session running.
   To close the shared connection for every client, use the separately confirmed
   **Session > Close shared Session...** action described in the menu reference.

The sidebar's Host panel shows status and its configured endpoint. The activity-rail terminal
button or **View** menu hides/reopens the sidebar without detaching. **Help** shows a local
usage summary. **Settings > Language** switches menu/help text among the nine supported languages
for the current window. Menus close with Escape or an outside click. See the
[menu reference](../reference/gui-menus.md) for actions, targeting, and unimplemented planning boundaries.
Search filters Session references, labels,
endpoints, and transport names; Serial/SSH/Other group headers expand or collapse their rows.
An empty connected Host shows
**No Sessions available**; use **Session > New Connection...** or open one with the CLI and use **Refresh**. On connection failure,
review the safe error notice, check the configured Host, and select **Retry connection**.
The bottom status bar distinguishes attached input, paused input, and lease read-only state.
See [workbench layout and display states](../architecture/electron-gui.md#workbench-layout-and-display-states)
for sizing and extension boundaries.

## Checks and isolated Host

Debug-send checks compare the exact bytes echoed by the isolated real Host, including NUL, non-UTF-8
bytes and line endings. Native confirmation is substituted to inspect/cancel the precise payload;
stale/detached views, Lease/control rejection, uncertain delivery and malformed/oversized/encoding
failures are covered without hardware. Snippet tests use disposable files and check consent, bounds,
restart without execution and failure preservation. Nine locales and a compact native window are
checked separately from real-device acceptance.


Client-setup checks use a disposable real Host registration domain and separate client home/config
directories. They verify native confirmation/cancel, existing-entry preservation, read-only registration,
revoked authentication/repair, secret/path isolation, nine locales and compact layout. Native dialogs
are substituted to inspect their exact sanitized preview. When installed, a fresh Codex process checks
configuration parsing and OpenCode checks an actual MCP connection; the former alone is not connection
evidence. Missing executables are reported separately. No Agent prompt is sent and no user configuration
or registration is modified. Go tests cover malformed/changed files, injected write failure,
cancellation and recovery at publication boundaries.

Continuous-recording tests check exact raw bytes, split UTF-8/control escaping, combined data/index
limits, rotation, buffer gaps, canceled selection, disk-full injection and stop during a slow/failing
write. The real fake-I/O Host checks independent cursors, transfer-range capture and background idle
protection. Native desktop checks substitute only destination/trash dialogs, record after tab close,
verify normal stop/exit, and abruptly exit their own disposable app to inspect unverified-tail markers.
They preserve an independent observer and use all nine locales plus a compact window. This is not a
physical disk-full, power-cut, real device or OS trash-interoperability test.

Host settings checks use an isolated GUI profile, a fake-I/O Host and an independent observer.
They cover local endpoint validation, atomic save failure, environment precedence, restart persistence,
unreachable/incompatible/authentication failures, stale attachments, other-window survival, nine
languages and a compact native window. No personal client configuration, credential, device or Host
process is changed. Package version/hash tests use disposable files; real package/platform acceptance
remains separate.

Run from `gui/electron`:

```powershell
npm.cmd run check
# Include both production and development native desktop checks:
npm.cmd run check:desktop
```

`check` stops on the first failure and runs formatting, TypeScript (including its strict
`i18n:check` prerequisite), unit/integration tests and the production build. `check:desktop`
adds both native desktop modes, also stopping on failure. Neither command installs dependencies,
packages installers, signs, publishes or contacts a real device. Run `npm.cmd ci` first.
There is no dedicated semantic lint command; Prettier checks formatting only.

The `GUI` jobs in `.github/workflows/ci.yml` run `npm ci` and `check` on Linux, Windows and
macOS with Node.js 24 and Go. Windows additionally runs both native desktop modes. They use
the existing read-only repository permission and no signing or publishing secrets. Linux/macOS
desktop sessions, arm64 native execution and real hardware remain separate acceptance work;
successful builds or fixture tests do not establish those results. A workflow change is not
evidence that hosted CI has run: record the actual job results after an authorized push.

`test` builds a fixed-path disposable Go fixture under ignored `test-results/` with `CGO_ENABLED=0`.
The fixture uses the real MCP adapter, one authoritative Application, Manager, Session, and Channel
with an in-memory echo endpoint. It listens on an ephemeral loopback port, uses a generated token,
and takes test controls over private stdin. No physical serial port, credentials, external SSH
server, or user Host is accessed. EOF on fixture stdin stops it and cleans up its own Session.
Desktop test windows use a fresh profile under ignored `test-results/desktop-profile-*` for
both Electron userData and sessionData, so shortcut persistence checks cannot overwrite the
user's GUI preferences. A test-only Node preload sets these paths before application startup;
no test profile control or additional IPC capability ships in the application.

Startup tests cover existing-listener reuse, custom-endpoint exclusion, cancellation, launch
failure, known process exit, and suppression of duplicate launches after uncertain timeouts.
A separate native check uses a temporary app and a detached no-hardware fixture on port 37099;
it verifies automatic startup, reuse without an executable, authentication failure, explicit
disconnect, and shared lifetime after GUI exit. If that port is already occupied, this check
is reported as skipped and does not touch the existing listener. A private stop file and a
two-minute deadline clean up only the test fixture.

The Node tests cover authenticated list/attach/read/write, timeout and cancellation, duplicate detach
and disconnect, stale attachment rejection, lease contention and recovery, buffer overwrite, Session
close, Host disconnect, credential redaction, redirect refusal, IPC source/argument checks, and
independent client survival. They do not prove hardware or remote-network behavior.
Terminal presentation tests cover common encoding aliases and byte fixtures shared with CLI tests,
split GBK/GB18030 characters (including supplementary Unicode), Big5/HKSCS, raw UTF-8 compatibility,
hex escaping/offsets, invalid local settings, and lossy/stale/read-only write rejection in Main.
Native connection checks type Chinese through the real bridge, verify the GBK bytes through an
independent Host client, inspect decoded output, and switch hex/text views without Session closure.
The dark settings dialog and hex display are captured in `test-results/terminal-settings.png`
and `terminal-hex.png`. These checks use fake serial I/O, not a physical port.
Native desktop tests switch through all nine catalogs and verify renderer/native labels and
exclusive language checkmarks, hover flyouts, edge placement and keyboard navigation.
Connection tests use in-memory serial I/O and a temporary loopback SSH server with disposable
credentials to verify creation, authentication failure, automatic attachment and shared lifetime. Translation tests run the pinned CLI against disposable PO copies: new-message and new-locale
extraction, translation preservation, strict rejection, and English fallback. Notice-collection
tests distinguish absent optional platform packages from missing required packages/licenses.

Advanced SSH checks cover bounded input, legacy Host capability fallback without sending unsupported
fields, Transport configuration/deadline mapping, and native creation with explicit initial PTY and
liveness settings. Existing cancellation, trust rejection and shared-lifetime tests remain applicable.

`test:desktop` builds the production bundle and uses Playwright's Electron driver to launch a real
visible native window against that fixture. It checks isolation settings, missing Node globals,
keyboard round trips, displayed output, lease input blocking, overwrite notices, reconnect,
detach/reattach, and window close while another client continues to use the Host. Workbench checks
resize the native window to 760 × 500, 1180 × 780, and 1500 × 900, verifying visible controls,
terminal fitting, and no document overflow. They cover disconnected, connected-empty, selection,
attached, and failure/retry views, plus long labels, closed entries, and a scrolling 40-Session list.
They also check transport grouping, case-insensitive search, no-match feedback, group collapse
across refresh, and continued attachment/input after hiding and reopening navigation.
Compact-layout checks verify the 32 px terminal header, 24 px status bar, two-line Session rows,
and absence of redundant input/target elements. They drag the sidebar divider, exercise keyboard
limits and composition guards, shrink the window, cancel pointer capture, and double-click to
reset width while retaining the attachment and fitted terminal. Screenshots are saved as
`test-results/workbench-compact-layout.png` and `workbench-compact-resized.png`.
Panel checks toggle all three regions, resize the right/bottom dividers, fit both sidebars at
minimum window size, switch bottom tabs with the keyboard, and verify unchanged attachment
and zero terminal bytes from layout shortcuts. Settings checks cover conflicting/reserved
combinations, custom dispatch, saved local bindings, storage failures, menu labels, reset,
and editor focus. Fresh app processes check saved bindings and malformed-storage fallback.
Screenshots: `workbench-panels.png`, `workbench-panels-compact.png`, and `workbench-shortcuts.png`.
Unit checks cover platform defaults, Option/physical keys, IME/AltGr rejection, malformed or
duplicate stored bindings, and bounded native menu hints.
Terminal-search checks exercise literal and Chinese matches, case sensitivity, previous/next,
empty results, composition guards, monochrome decorations, narrow-window visibility, select/copy,
the 1000-match cap, and detach cleanup without device bytes. The search addon is pinned to
`@xterm/addon-search` 0.16.0 alongside xterm 6.0.0 from the same upstream commit. It is MIT-licensed
JavaScript with no runtime dependencies or native modules; the production notice collector includes
its license. Its proposed public buffer/decorations APIs are enabled only for the renderer terminal,
without new IPC or system permissions. Native execution on other OS/architecture pairs remains
separate validation. `test-results/terminal-search-compact.png` records the compact search bar.

Terminal-control checks cover historical cursor/device queries without unsolicited replies,
fragmented live cursor queries, real terminal selection/copy/paste, stale/oversized clipboard
IPC, lease-blocked paste with no replay, shortcut search, font settings and Ctrl+wheel without
application zoom or device writes. `test-results/terminal-display-settings.png` captures the
display dialog. These use fake serial I/O and the native clipboard, restoring its previous text.
They do not establish physical serial-device or other native-platform behavior.

`uboot.test.ts` covers bounded board settings, partial results, fresh authenticated readiness,
event gaps, old-Host capability rejection, owned cancellation and waits beyond the ordinary HTTP
timeout. `uboot-checks.mjs` exercises the native panel through production IPC with the real isolated
Host and fake serial I/O: default-Cancel previews, exact interrupt bytes, diagnostic ZIP saving,
explicit stop, window exit, retained shared Sessions and nine-locale compact layouts. Native
confirmations and destination pickers are substituted in that test; it does not reset hardware or
establish a physical board's countdown timing. A real-board run still requires verified serial
settings, board markers, interrupt bytes and explicit reset/reboot authorization.

Menu checks cover native/renderer language and checkmark synchronization, sidebar actions,
keyboard opening, Escape/focus restoration, outside-click dismissal, and disabled offline/empty/
closed actions. They reject stale attachment callbacks and missing non-current context targets,
verify local clear leaves Host history intact and sends no bytes, and exit the GUI with an
independent observer still attached. Ctrl+C/Ctrl+L/Ctrl+W, Alt and F1 pass through xterm.
Chromium CDP composition/commit checks cover Chinese input without duplicate bytes; real OS IME
candidate windows still need manual native testing. No OS-global shortcut is registered; the
renderer handles only assigned layout and terminal clipboard chords under the documented focus policy.
Empty/large-list and missing-context-target checks replace list IPC snapshots in disposable test processes;
connection, terminal I/O, leases, and lifecycle checks use the real isolated Go Host. A fresh Electron
process restores production IPC for subsequent recovery checks. No test-only controls ship in the app.
Screenshots are saved under ignored `test-results/desktop.png` and `test-results/workbench-*.png`;
`workbench-menu.png` and `workbench-menu-ja.png` capture menu surfaces;
layout failures also report element bounds and save `workbench-layout-failure.png`.
The window scenario checks multiple Sessions and duplicate attachments to one Session, independent
input/cursors, cross-window attachment rejection, tab/window navigation, hidden-tab output loss,
background-tab moves, and shared Session survival after tab/window closure. A separate disposable
process destroys a provisional destination at acknowledgement to verify rollback, then closes a
source before commitment to verify provisional-window cleanup. It also checks inherited and independent
language preferences, all nine window/context labels and restore notices, and shared Lease status.
Production IPC is used for the successful handoff. `test-results/window-moved-tab.png` records the restore notice.
The test closes its own windows and fixture.
This requires an interactive desktop; Linux CI needs its own display setup.
`test:desktop:dev` runs the same desktop scenario with Vite and its nonce-protected refresh
preamble, checking the development entry point as well as the production bundle.

The Go gates remain independent; from the repository root run the applicable commands in
[Building and testing](building-and-testing.md). The fixture's Go composition source is included
in normal Go package compilation without invoking npm. No Node tool is needed to build the CLI.

Serial-profile checks use a disposable Host configuration directory shared by the normal Application
resolver and desktop API. Unit/integration checks cover name conflicts, explicit replacement, default
selection, concurrent saves, lock cancellation, malformed IPC/HTTP input and safe errors. Native checks
apply custom baud/settings, preserve wake opt-in and verify that saving alone creates no Session.
They never open physical serial devices or use the developer's configuration.

SSH trust checks use temporary loopback keys/files and exercise approval, rejection, disconnect,
expired/replayed/foreign decisions and changed/revoked records, including changes during confirmation.
The delayed-approval regression ensures the five-second HTTP body deadline does not interrupt the
two-minute prompt. Native tests verify initial Reject focus, refusal without persistence, and approval
with the same shared Host Session lifecycle. No developer SSH trust file is accessed.

### Device discovery checks

The isolated Host composes the real Registry and identity store with a controllable fake scanner.
`tests/devices.test.ts` checks scan failure/recovery, disappearance, port moves, conflicting USB
identities, Host occupancy, typed serial access errors, coalesced reads, stale selection and
disconnect cancellation. An open counter proves discovery and decision reads do not create Sessions.
`scripts/device-checks.mjs` exercises the same changes through native Renderer/Preload/Main, checks
reselection and manual entry, and checks all nine languages in a compact window. It runs as part of
both desktop suites and leaves the existing shared Session alive.

For hardware acceptance, separately record port/framing/flow settings, actual USB identity evidence,
appearance/disappearance and access-error behavior. Do not infer permission or external occupancy
from enumeration, or treat the fake scanner/cross-builds as native hardware evidence. Unplug/reset
actions require a known safe device and operator authorization.

### Session activity checks

Source regressions cover missing identities despite `actor: user`, public field projection,
secret exclusion, distinct current/original initiators, malformed/oversized summaries, snapshot
copy isolation, bounded histories after event loss, multi-window online/ended instances and local
SSH task visibility. Disconnect/history retention clears liveness without erasing provenance.
The native activity scenario checks the separate initiator column and unknown-source label.
Source translations use the same official extraction/strict compilation workflow as menus.

Activity tests cover independent cursors, input/metadata redaction, UTC normalization, malformed
responses without cursor advancement, timeout, the 200-row bound, loss notices, overlapping reads,
foreign/stale attachments and detach during a read. The native scenario observes Agent input,
Lease acquisition/release and a test-only client debug report through production IPC, verifies
output remains readable by another client, and checks hide/reopen and three-tab keyboard navigation.
`test-results/session-activity.png` captures the compact panel. The fixture uses private stdin
controls with in-memory I/O; a synthesized report tests labeling, not an actual GDB audit.

### Diagnostic ZIP checks

Download tests cover exact chunk offsets, size/hash consistency, bounded Base64, checksum failure,
cancellation, exclusive destination creation, overlapping requests and partial-file cleanup. Real
isolated Host tests inspect ZIP entries to distinguish metadata-only and explicitly raw snapshots,
verify byte-identical existing-ID downloads and reject evicted IDs. Native tests use production
Renderer/IPC/Host paths while substituting only the destination picker; they verify default opt-out,
raw opt-in, existing-ID download, unavailable archives and picker cancellation. The resulting dialog
is captured in `test-results/diagnostic-export.png`. Destination files are disposable test artifacts.
These checks do not establish physical device evidence or other native-platform filesystem behavior.

## Troubleshooting and limits

Directory preference tests use temporary TOML files and verify CLI/GUI defaults, validation,
explicit overrides and preservation of other config sections. File-selection tests reject invalid,
duplicate, stale and oversized drops before launch. Native `file-checks.mjs` uses Chromium OS-backed
drops through the real Preload/Main selection path, with serial browser/payload responses replaced
only in the disposable test process. It checks direct files, one question per batch, TAR default,
remembering/resetting the shared preference and stopping on cancellation/failure. Screenshot:
`test-results/file-drop-directory.png`. Desktop processes use an isolated user configuration root;
these tests do not read or modify personal transfer preferences. Native tests on other OS/architecture
pairs and physical serial transfers remain separate validation.

| Symptom | Check / recovery |
| --- | --- |
| Companion missing / startup failed | For source builds run `npm.cmd run host:build`, verify local configuration and port 37099, then retry. Main never searches PATH. |
| Token unavailable | Check the Host account and `CHANNELTERM_HOST_CONFIG_DIR`, or supply an independent registered token to Main. The retired shared token is rejected; Electron creates no credentials. |
| Authentication rejected | Verify the configured endpoint and Main environment match the intended Host. Do not disable authentication or change CORS/Origin rules. |
| No Sessions | Use Session > New Connection..., or open a Session through an existing CLI/MCP workflow and refresh. SSH creation requires the updated standalone Host API. |
| Lease busy | Continue reading; wait for the owning operation. GUI neither steals nor releases leases. |
| Output overwritten | Earlier bytes are unavailable. xterm resets and continues from retained output; the warning remains while attached. Detaching removes that view's notice. |
| Session closed | Refresh and select an open Session. Reattachment cannot reopen a closed device. |
| Host disconnected / uncertain write | Check Host ownership/liveness, reconnect explicitly, and inspect remote state before retyping. Input is never automatically retried. |
| Full-screen application size differs | In the intended SSH view, explicitly select **Use this view for remote size**. Other views fit locally. Older Hosts have no resize route; update the Host. |
| Window will not launch | Check Electron download completion and native display availability. Scripts clear inherited `ELECTRON_RUN_AS_NODE` for the child process. |
| Race tests fail before compilation | Install the supported C compiler and enable CGO for race testing; ordinary production Go builds remain CGO-free. |

Targets are Windows/Linux/macOS, x64/arm64. Native Windows x64 desktop testing is separate from
cross-compilation; each other native OS/architecture still needs runtime validation. Real serial
settings and hardware are not involved in the fixture. Local unsigned packaging is described below;
signing/notarization, update delivery, and desktop release automation remain unimplemented.

### Remote SSH size checks

`tests/resize.test.ts` exercises two real Host Clients against the disposable loopback SSH server:
creation versus reuse, actual `window-change` dimensions, explicit takeover, stale owner rejection,
release/detach, foreign handles, malformed dimensions and serial-only local fitting. Additional
checks cover late takeover cleanup and a bounded geometry queue. Go tests cover concurrent submission,
permissions, registration reuse and the fixed HTTP boundary. Native connection checks resize the
Electron window, observe changed wire dimensions, exercise takeover/release, and capture
`test-results/ssh-remote-size.png`. These fixtures do not prove behavior of external SSH servers or
full-screen programs on every native OS.

### Unified debug overview checks

The disposable Host fixture explicitly registers a TCP-over-SSH observation through Application;
it does not launch GDB or scan ports. `tests/debug.test.ts` checks the real MCP snapshot, independent
port roles, provenance, unchanged Session state, safe IPC copying and malformed responses.
`debug-checks.mjs` exercises native sidebar navigation, report labels, endpoint details and
locating a Session without creating an attachment. Nine-language strict compilation includes all
Debug labels. `test-results/debug-overview.png` is local ignored evidence. Core/CLI tests cover
MI transitions, report expiry, identity/capability checks, legacy JSON and no-renewal semantics.
External GDB process integration and physical target acceptance must be recorded separately;
fixture evidence does not establish external executor report accuracy.

### Connection history checks

`connection-history.test.ts` covers credential/path exclusion on write/read, serial wake reset,
malformed storage, bounded eviction, favorites and successful-use accounting. Native connection
checks reopen a saved serial record and verify prefilled parameters without connection or wake.

### Workspace recovery checks

`tab-layout.test.ts` verifies stable local reordering, focus position and split collapse without
attachment replacement. `split-checks.mjs`, included in the native desktop suite, exercises real
tab dragging and scoped keyboard reordering, both split directions, independent same-Session
cursors, input routing and Lease rejection, nine locales at narrow width, window resizing,
workspace restart, pane close/move and failed-attach recovery. Loopback SSH checks prove observers
cannot seize remote size on layout changes and explicit takeover affects only the requested view.
The failure case replaces only the disposable test process's attach IPC handler; successful
paths use production IPC and the real Host with fake serial I/O and loopback SSH. Screenshots are
`test-results/split-views.png` and `test-results/split-compact.png`. Synthetic composition events
check key-dispatch guards only; these tests do not establish native IME or physical-device behavior.

`highlight-rules.test.ts` covers safe literal limits, independent template copies and saved-record
identity migration/reuse without endpoint-based rule inheritance. The streaming tests cover all
UTF-8 splits, overlap precedence, match overflow and rule changes during remote ANSI state.
`connection-highlight-checks.mjs` exercises the native editor, ordering, enabling, deletion,
clear/import, template replacement/deletion isolation, search coexistence, exact Host bytes,
storage failure/retry, nine locales, restart, saved-preset reuse, splitting and window handoff.
Its serial I/O is disposable and simulated; `test-results/connection-highlights-compact.png`
records narrow-window rendering. Failed storage is injected only in the isolated test renderer.

`workspace.test.ts` validates bounded persisted preferences and tab order/settings, rejects malformed
or foreign-Host snapshots, strips runtime/input/output fields and matches full live Session identity.
`workspace-checks.mjs`, included in the native desktop suite, restarts Electron with a disposable
profile while the isolated Host survives. It verifies language/layout/defaults, ordered surviving
and closed targets, no Session recreation, no replay of a pending DOM draft or historical terminal
queries, and usable views after storage failure. The test writes no personal configuration and
opens no physical device. The screenshot is ignored local evidence at `test-results/workspace-restored.png`.

Control regressions must cover old-client rejection, stale queued input, per-window human ownership,
private metadata filtering and generation changes invalidating paste. Use isolated Host fixtures;
failed or unknown recovery must never advertise writable input. Protocol behavior is owned by
[Session control](../reference/mcp-tools.md#session-control).

`control.test.ts` covers strict IPC projections, immutable takeover/result keys, duplicate requests,
per-window ownership, reconnect queries and stale allowance. `control-checks.mjs` runs through the
production bridge in the native desktop suite: confirmation races, Host-authoritative input,
explicit allowance, result retention across disconnect and compact layout. Existing SSH file
tests cancel a real loopback SFTP child without closing its terminal Session. All use disposable
profiles and fake/loopback devices, not personal credentials or physical hardware.

### Workbench localization checks

`workbench-i18n.test.ts` checks every public recovery code, malformed/unknown failures, safe SSH
reasons, plural session counts and renderer message references. Errors stored before a locale
change are translated again without inspecting their diagnostic text. The native
`workbench-i18n-checks.mjs` scenario switches all nine catalogs with offline views and a retained
output-loss notice, checking accessibility/status text and byte-display preservation. Existing
native scenarios cover Lease blocking, disconnects, input composition and compact layout.
These checks use isolated Host fixtures; they do not establish native input-method or device
acceptance on other operating systems.

## Local desktop packages

From `gui/electron`, after `npm ci` and the required checks:

```powershell
npm.cmd run package
# Only unpacked application files, without an installer/archive:
npm.cmd run package -- --dir
# Build the other supported CPU architecture on the same OS:
npm.cmd run package -- --arch=arm64
```

Packaging requires the native OS toolchain, Node/npm and Go. End users need neither Node nor Go.
The command uses pinned electron-builder 26.18.0 and the pinned Electron runtime; runtime/tool
downloads require network access. It accepts only `--dir` and `--arch=x64|arm64`, builds for the
current OS, and writes a new `release/<go-os>-<go-arch>/build-*/artifacts/` directory each time.
Existing developer companions and previous package directories are never overwritten. Do not
remove a package directory while its Host is still serving shared Sessions.

| Build OS | Configured formats | CPU targets |
| --- | --- | --- |
| Windows | ZIP directory archive, per-user NSIS installer | x64, arm64 |
| Linux | tar.gz directory archive, deb | x64, arm64 |
| macOS | app ZIP, DMG | x64, arm64 |

Windows x64 package execution and NSIS installation/removal have native automated coverage.
The other package formats/architectures require builds and acceptance on those native systems;
configuration and Go cross-builds are not evidence that those desktop packages run. No signed,
notarized or publicly published desktop release is implied. Do not bypass OS protection prompts.

Every package carries the Electron runtime, bundled JS, matching `resources/host/channelterm`
and `resources/files/channelterm` executables (`.exe` on Windows), application/runtime licenses,
and Go dependency notices. macOS resources are inside the app's `Contents/Resources` directory.
Both Go companions are copies of one `CGO_ENABLED=0` build. The GUI package version is injected
into the CLI together with the source commit (marked `-dirty` when appropriate) and UTC build time.
`resources/package-manifest.json` records these values, the runtime version and companion SHA-256
hashes; `SHA256SUMS` covers the generated distribution artifacts. Hashes identify bytes; they are
not signatures. JS is bundled into `app.asar`; native helpers remain executable outside it.
The package allowlist excludes source, node_modules, developer profiles and test fixtures.

Extract a ZIP/tar.gz to a stable writable location, or copy a macOS app out of its mounted DMG,
then launch `channelterm-desktop` (`.exe` on Windows). Do not run a persistent Host from a temporary
self-extracting archive. “Portable” refers to the application directory: preferences, credentials,
and workspace state still use the existing user directories. Packaging preserves the existing
`@channelterm/electron` profile identity. It does not store credentials in the installation or
change PATH, the CLI installer, Host authentication, or the default loopback listener.

The Windows installer is per-user, permits a destination choice, creates a Start menu shortcut,
and does not launch the GUI automatically. Its process check replaces electron-builder's default
termination behavior. Installation, upgrade and removal stop with exit code 2 when any
`channelterm-desktop.exe` or `channelterm.exe` is running, or process inspection fails. This is
deliberately conservative, including CLI processes from other directories. Finish shared work,
stop the relevant Host explicitly, then retry. Uninstall removes its application/shortcut and
registration, preserving user configuration. Removing an extracted directory likewise requires
first finishing work and stopping the Host; closing the GUI alone does not stop it.

Run native package acceptance against an unpacked application directory (the `.app` on macOS):

```powershell
npm.cmd run test:package -- release/windows-amd64/build-EXAMPLE/artifacts/win-unpacked
./scripts/test-installer.ps1 -Installer release/windows-amd64/build-EXAMPLE/artifacts/ChannelTerm-Desktop-0.1.0-windows-amd64.exe
```

`test:package` copies the actual package outside the checkout to a path containing spaces and
non-ASCII characters, uses disposable profiles and an empty PATH, validates helper hashes/native
version output, and tests production IPC, shared Session survival, serial setup and loopback SSH
trust/file transfer/cancellation. Native pickers use disposable test destinations. It contacts no
physical device. The Windows installer check requires PowerShell 7 and refuses to run if any
ChannelTerm installation/process already exists. It installs into an owned test directory, starts
only an isolated no-device Host, checks upgrade/uninstall refusal without termination, then removes
the installation and checks configuration retention. Failures preserve evidence for diagnosis.
These checks do not replace a fresh OS image, native IME, physical serial or real SSH acceptance.

Packaging is separate from `npm run build`, CLI builds and tagged CLI releases. There is no
desktop publish/sign/update step. The builder is a build-only MIT dependency; its tool graph is
excluded from the runtime. Audit reports include existing and build-tool advisories; review them
for the fixed local build inputs rather than running an unrelated automatic dependency upgrade.

`idle-checks.mjs` uses real Application policy with a one-second fixture grace and private sweep triggers: another client retains the Session after GUI exit, final detach permits removal, and MCP remains reachable. Core fake-clock/concurrency tests additionally cover presence expiry with active work, leases/recovery/pause, reused IDs, close outside admission locks, and explicit Host-exit fencing. No physical device or native non-Windows claim is implied.

### Upgrade, mixed builds and rollback

Keep the GUI and both companions from one complete native package. Record its manifest version,
commit, OS/architecture and hashes together with the running Host version: different local builds
can share a version label, and replacing GUI files does not replace an already-running Host.
The following matrix defines the checks and their limits; it does not certify arbitrary pairs of
historical releases.

| Combination or operation | Current behavior and acceptance boundary |
| --- | --- |
| Matching GUI, Host and CLI package | Native package acceptance checks helper versions/hashes and the runtime. Windows x64 coverage does not establish the other five native targets. |
| Newer GUI with an older Host | Connection depends on the Host's actual protocol and capabilities. Optional features remain gated: for example, U-Boot requires the advertised HTTP cancellation contract. Verify each required workflow for the specific pair; successful connection alone is insufficient. |
| Older GUI/CLI with the current Host | The current authentication, presence and control requirements still apply. See the [request compatibility matrix](../getting-started/http-authentication-migration.md#default-controlled-sessions); there is no fallback that restores old write permissions. |
| Missing or mismatched file companion metadata | Host Information reports the bundled CLI version as unknown. This is not proof that a manually substituted executable is compatible or authentic; restore and verify the complete matching package. |
| Windows upgrade/removal while a GUI or Host is running | The installer guard refuses the operation without terminating processes. The native installer check verifies refusal and retention of its isolated Host configuration after later uninstall. |
| Interrupted installation or replacement after the process guard | Not covered by the refusal check. Verify interruption/recovery on each candidate OS and artifact pair before claiming transactional installation or automatic rollback. Do not run a partial set of application files. |
| Preferences, favorites and workspace across versions | Current restart tests cover supported stored state and live-Session revalidation. They do not certify migration or downgrade of every historical profile schema; verify each selected pair with disposable copies. No test should replay unsent input or recreate closed Sessions. |
| Downgrade across Host authentication migration | An older binary can re-enable old authorization and cannot read new-domain changes. Follow the [migration and rollback boundary](../getting-started/http-authentication-migration.md#upgrade-and-verify); never run old and new versions against the same live authentication state. |

For each selected pair, record supported, rejected or not verified, the exact artifacts, exercised
workflows and reason for any gap. Test invalid credentials and absent capabilities as well as the
success path. Preserve a separate client's live Session when the GUI exits. Record profile retention,
installer interruption and rollback independently; a connection test, checksum or same-build restart
cannot stand in for those checks. Signing and notarization are separate from these compatibility checks.

## Native device acceptance

Package acceptance above uses fake serial I/O and a loopback SSH server. A release candidate also
needs the following checks on native Windows, Linux and macOS, each on amd64 and arm64. Cross-builds,
WSL and synthetic composition events do not establish native desktop, driver or IME support.

Keep a local evidence sheet for each run: commit from `package-manifest.json`, package/companion
SHA-256, OS/architecture, driver and device model, exact actions, expected/actual result, and
PASS/FAIL/not-run status. Record serial port, baud, data bits, parity, stop bits and flow control;
mark assumed parameters explicitly. Do not publish device addresses, credentials or raw console
captures in repository documentation. No-output observation proves neither RX nor TX correctness.

Use a fresh test account/profile and an isolated `CHANNELTERM_HOST_CONFIG_DIR`. Start only the
packaged companion, bind its HTTP Host to `127.0.0.1:0`, and use the printed endpoint in
`CHANNELTERM_GUI_ENDPOINT`. Its readiness line means the listener is ready; authentication and
Session setup still need to succeed. Never stop an existing Host to free a test port. Use a new
trust database for an authorized SSH target; first-contact acceptance is trust on first use,
not independent fingerprint verification. A changed or revoked key must still fail closed.

| Check | Expected evidence and recovery |
| --- | --- |
| Package startup | Match packaged app/helper versions and hashes; start outside the checkout with empty PATH, spaces/non-ASCII paths and a clean profile. Repeat in a clean OS/account. Record OS security prompts and unsigned-package limitations. |
| Serial receive/transmit | Open with the recorded parameters; observe first. Only send explicitly permitted bytes to an identified idle prompt. Verify actual bytes/encoding against independent device output, not local echo alone. Empty reads are inconclusive. |
| Serial ownership/release | Keep a registered independent monitor; exit GUI and verify the same Session survives. End the last use, wait beyond the configured grace plus sweep interval, then reopen the endpoint with a new Session ID or another serial application. Explicitly close only test-owned Sessions. |
| Physical faults | With device-owner permission, test unplug/replug, port contention, sustained output and flow control. Record loss notifications, closed/stale targets and explicit recovery. Never substitute a simulated port for this result. |
| SSH interoperability | Record server/version and authentication method. Test first trust, changed-key refusal, password/key/encrypted-key access, PTY resize and authorized disconnect/slow-write cases. An empty-password rejection is an authentication failure, not proof that SSH transport is broken; do not guess passwords. |
| Shared control | Two clients must share the same Session while one exits. Check read-only grants, instance migration, takeover during owned work, recovery failure and explicit AI resumption. Verify rejected/uncertain input is never replayed. |
| Files | Use only disposable authorized paths. Check serial and dedicated SSH transfers, hashes, cancellation and partial files; independent SSH cancellation must not send Ctrl+C into a PTY. Delete only test-created files after verification. |
| Native interaction | Exercise a real OS IME, clipboard, multiline confirmation, nine locales, narrow windows, native menus and window/tab restoration. Synthesized composition events cover dispatch logic only. Closed Sessions and unsent input must not return. |
| Shutdown | Close test Sessions through their clients, disconnect test clients and stop only the test-owned Host. For explicit idle exit, verify listener shutdown and an explicit restart path; HTTP traffic cannot wake an exited process. |

Record each cell separately for all six native targets. Windows package automation and an actual
serial open/idle-release cycle do not establish all hardware scenarios, SSH authentication modes,
IME behavior, installers in fresh OS images or other native targets. The CLI has a
[limited line-mode footer](../reference/cli.md#local-status-bar); clear-screen/full-screen/resize
checks must verify its downgrade and cleanup, not claim that a fixed footer survives those modes.


### Line metadata verification

`output-fold.test.ts` checks independently collapsed ranges, wrapped headers and
attachment-bound menu validation. The native `output-fold-checks.mjs` collapses two commands,
expands one, verifies live output through an independent observer, and finds hidden text.

`line-metadata.test.ts` covers bounded anchors/pending callbacks, unknown history, first-observation
semantics, wrap mapping, alternate screens, explicit reset/disposal and late completions. Menu and
workspace tests cover independent default-on columns, absent/stale attachment targets, strict
validation and backward-compatible per-tab choices without persisting output or authority.

`line-sidebar-checks.mjs`, included in production and development desktop runs, uses public xterm
behavior and a disposable fake-I/O Host. It checks collection before enabling columns and while a
tab is hidden; original Host bytes and body-only clipboard; Unicode soft wraps; native window/font
changes and row-coordinate alignment; scrolling across more than 5,000 retained lines; explicit
Host overwrite, clear and restart; alternate-screen suspension; and nine-locale native menu state.
The fixture retains only 1 KiB: retention tests feed sub-capacity batches and wait for parsing;
intentional flood tests separately exercise loss. These checks do not prove native behavior on
unavailable desktop targets or device event timing.

The desktop acceptance runner also exercises the terminal directory with two disposable
loopback Hosts, distinct credentials and the same short reference and opaque Session ID.
The GUI checks cover the Sessions/LAN activity rail, history after closing tabs, drag reordering across windows and restarts, connection status filters, and suspended LAN polling. The directory checks double-click a stored serial connection with its Host disconnected, verify one new live Session, and reattach to loopback SSH through transport-specific rows. The fixture closes one serial channel while its Host remains reachable to check independent Host and Session status lights. The U-Boot regression holds the renderer list snapshot fixed between two confirmed attempts to exercise control-version rotation. The COM8 and COM9 fixtures use in-memory streams. The MCP tests use the SDK client with
an independent agent credential. No physical port or operator credential is required.
