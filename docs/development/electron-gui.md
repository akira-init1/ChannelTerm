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
not an installer. `start` and `dev` prepare `dist/host/channelterm[.exe]` with Go only when it
is missing and the default endpoint is selected. They never replace a running companion.
If preparation fails (for example, Go is unavailable), the GUI still opens and can reuse an
existing Host; otherwise its startup error offers retry after repairing the companion.
To update it after Go source changes, finish shared Session work, stop that Host, and run
`npm.cmd run host:build`. Node/npm are development requirements; a future packaged GUI includes
its own Electron runtime and the platform-matched Host under `resources/host/`. See the
[release boundaries](../architecture/electron-gui.md#multiple-frontends-and-release-boundaries).

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
usable. Request/endpoint validation, busy exclusion and secret-free progress are covered. This
does not prove external OpenSSH/SCP interoperability, physical network interruption, or native
file-dialog behavior; those remain separate acceptance checks.

The native desktop suite additionally exercises the SSH file form and progress panel through
Renderer, IPC and the real CLI: cancel an upload, manually resume, and download identical bytes.
It substitutes only the native picker result with a disposable local path; the production Main
selection/endpoint checks still run. Screenshots are `test-results/ssh-file-dialog.png` and
`ssh-file-progress.png`. The SCP option disables resume; SCP server interoperability is covered
separately from this SFTP fixture.
During an owned transfer, native tests also repeat window-close and app-quit requests, verify one
themed notice and no native message box, dismiss with Escape/Continue, and then explicitly cancel
the transfer. `test-results/file-close-notice.png` captures the notice.

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

Use an updated standalone HTTP Host for GUI SSH creation; older Hosts and a foreground
CLI `ssh` owner's sharing endpoint support attachment but do not offer the new creation API.
After rebuilding the Go executable, restart the intended standalone Host before reconnecting
the GUI. Host shutdown closes its Sessions, so finish work in those Sessions first.

Opening the GUI automatically connects to `http://127.0.0.1:37099/mcp`. If that TCP listener
is absent, Main starts the native companion with fixed loopback HTTP arguments, waits for
readiness, and authenticates. No terminal command or existing Session is needed. An existing
listener is reused; wrong credentials, incompatible services, probe timeouts and occupied ports
never start a replacement or select another port. Failures expose **Retry connection**.

The Host intentionally survives GUI exit and manual disconnect so other clients can continue
using shared Sessions. Manual disconnect stays disconnected until explicitly reconnected.
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
The endpoint is configured in Main, not editable by the renderer. No Host config file or listener
policy is changed.

Main reads the Host's existing credential. Supply a custom token through the process environment
using your normal secret-handling procedure;
never put it in React code, a `VITE_` variable, source control, or URL parameters.

**Connection > New connection** opens a serial or SSH form after Host connection:

- Serial: scan the Host's available ports or enter a port manually, set baud/framing/flow control,
  and press Connect. Scanning is read-only. The default form is 115200, 8N1, no flow control;
  no wake byte is sent. Successful setup refreshes the shared list and attaches the new/reused Session.
- SSH: enter host, port and user, then choose password or private-key authentication. Key paths
  and `known_hosts` paths refer to files on the Host. Blank key paths use existing default
  identities; encrypted keys accept a passphrase. Blank trust path uses the Host user's
  `.ssh/known_hosts`. Unknown, changed and revoked keys fail: verify and install the server key
  beforehand. Password inputs clear on submission and credentials are never saved as preferences.

Creation failures keep the existing attachment. An uncertain result is not automatically retried;
refresh Sessions before making another explicit attempt. See the [desktop Host API](../reference/gui-host-api.md)
for the exact security, capability and lifetime boundaries.

1. Wait for automatic local connection (or select **Connect to Host** for a custom endpoint), then a Session in the sidebar or
   **Connection > New connection** to create one. The initial terminal screen
   contains a recent output snapshot, not an unlimited log.
2. Type in the terminal. Input goes through `terminal_write` with actor `user`; Ctrl+C is ordinary
   terminal input when input is enabled. No command delimiter is added by the adapter.
3. A Host lease makes the view read-only. A rejected write discards queued input; **Resume input**
   acknowledges recovery when the Host no longer reports a lease. It does not replay old input.
4. **Close tab (detach)** (the current attachment's close icon), **Disconnect Host**, or closing the window leaves the shared Session running.
   There is deliberately no close-Session button.

The sidebar's Host panel shows status and its configured endpoint. The activity-rail terminal
button or **View** menu hides/reopens the sidebar without detaching. **Help** shows a local
usage summary. **Settings > Language** switches menu/help text among the nine supported languages
for the current window. Menus close with Escape or an outside click. See the
[menu reference](../reference/gui-menus.md) for actions, targeting, and unimplemented planning boundaries.
Search filters Session references, labels,
endpoints, and transport names; Serial/SSH/Other group headers expand or collapse their rows.
An empty connected Host shows
**No Sessions available**; use **Connection > New connection** or open one with the CLI and use **Refresh**. On connection failure,
review the safe error notice, check the configured Host, and select **Retry connection**.
The bottom status bar distinguishes attached input, paused input, and lease read-only state.
See [workbench layout and display states](../architecture/electron-gui.md#workbench-layout-and-display-states)
for sizing and extension boundaries.

## Checks and isolated Host

Run from `gui/electron`:

```powershell
npm.cmd run format:check
npm.cmd run typecheck
npm.cmd test
npm.cmd run test:desktop
npm.cmd run test:desktop:dev
```

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
Menu checks cover native/renderer language and checkmark synchronization, sidebar actions,
keyboard opening, Escape/focus restoration, outside-click dismissal, and disabled offline/empty/
closed actions. They reject stale attachment callbacks and missing non-current context targets,
verify local clear leaves Host history intact and sends no bytes, and exit the GUI with an
independent observer still attached. Ctrl+C/Ctrl+L/Ctrl+W, Alt and F1 pass through xterm.
Chromium CDP composition/commit checks cover Chinese input without duplicate bytes; real OS IME
candidate windows still need manual native testing. No OS-global shortcut is registered; the
renderer handles only assigned layout chords under the documented focus policy.
Empty/large-list and missing-context-target checks replace list IPC snapshots in disposable test processes;
connection, terminal I/O, leases, and lifecycle checks use the real isolated Go Host. A fresh Electron
process restores production IPC for subsequent recovery checks. No test-only controls ship in the app.
Screenshots are saved under ignored `test-results/desktop.png` and `test-results/workbench-*.png`;
`workbench-menu.png` and `workbench-menu-ja.png` capture menu surfaces;
layout failures also report element bounds and save `workbench-layout-failure.png`.
The test closes its own windows and fixture.
This requires an interactive desktop; Linux CI needs its own display setup.
`test:desktop:dev` runs the same desktop scenario with Vite and its nonce-protected refresh
preamble, checking the development entry point as well as the production bundle.

The Go gates remain independent; from the repository root run the applicable commands in
[Building and testing](building-and-testing.md). The fixture's Go composition source is included
in normal Go package compilation without invoking npm. No Node tool is needed to build the CLI.

## Troubleshooting and limits

| Symptom | Check / recovery |
| --- | --- |
| Companion missing / startup failed | For source builds run `npm.cmd run host:build`, verify local configuration and port 37099, then retry. Main never searches PATH. |
| Token unavailable | Check that the Host runs under the same user, or supply its existing token to Main. The Host creates tokens; Electron does not. |
| Authentication rejected | Verify the configured endpoint and Main environment match the intended Host. Do not disable authentication or change CORS/Origin rules. |
| No Sessions | Use Connection > New connection, or open a Session through an existing CLI/MCP workflow and refresh. SSH creation requires the updated standalone Host API. |
| Lease busy | Continue reading; wait for the owning operation. GUI neither steals nor releases leases. |
| Output overwritten | Earlier bytes are unavailable. xterm resets and continues from retained output; the warning remains while attached. Detaching removes that view's notice. |
| Session closed | Refresh and select an open Session. Reattachment cannot reopen a closed device. |
| Host disconnected / uncertain write | Check Host ownership/liveness, reconnect explicitly, and inspect remote state before retyping. Input is never automatically retried. |
| Full-screen application size differs | xterm fits locally; MCP has no remote PTY resize tool. Use the CLI owner for remote sizing. |
| Window will not launch | Check Electron download completion and native display availability. Scripts clear inherited `ELECTRON_RUN_AS_NODE` for the child process. |
| Race tests fail before compilation | Install the supported C compiler and enable CGO for race testing; ordinary production Go builds remain CGO-free. |

Targets are Windows/Linux/macOS, x64/arm64. Native Windows x64 desktop testing is separate from
cross-compilation; each other native OS/architecture still needs runtime validation. Real serial
settings and hardware are not involved in the fixture. Installer packaging, signing/notarization,
update delivery, and release automation remain unimplemented.
