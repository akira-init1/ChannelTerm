# Electron GUI Development

The [Electron prototype](../architecture/electron-gui.md) attaches to existing Sessions in a
running Go Host. Source responsibilities and directories are in the
[repository map](../architecture/repository-map.md#electron-source-ownership); this guide owns
installation, execution, checks, and troubleshooting.

## Dependencies and initialization

Use Node.js 22.12 or newer (a supported LTS release), npm, and a native desktop session. Dependencies
are pinned by the independent `gui/electron/package-lock.json`. Electron, React, TypeScript, Vite,
xterm.js, Lingui Core, and the MCP TypeScript SDK are local to that project. No nested repository or global npm
package is required. Go 1.26+ is needed only for the existing Host and the isolated integration fixture.

From the repository root:

```powershell
Set-Location gui/electron
npm.cmd ci
npm.cmd run dev
```

On Linux/macOS use `cd gui/electron` and `npm` in place of `npm.cmd`. The `.cmd` form avoids Windows
PowerShell script-execution policy errors without changing system policy. Electron may download its
platform runtime on first launch; dependency and runtime downloads require network access.

`dev` builds Main/Preload once, starts Vite on `127.0.0.1:5173`, and opens the native desktop window.
Renderer changes use Vite refresh. Restart `dev` after Main, Preload, shared bridge, or build-script
changes; a renderer reload disconnects its Host client. Closing Electron also stops this development
server. Port 5173 is strict: a conflict fails startup rather than loading an unrelated server.

Build and launch the production assets independently:

```powershell
npm.cmd run build
npm.cmd start
```

`build` type-checks TypeScript, bundles Main and sandbox-compatible Preload with esbuild, builds
the React/xterm renderer with Vite, and collects runtime license texts into `dist/`. `start` loads
those assets through the local application scheme. This produces runnable application assets,
not an installer. Node/npm are development requirements; a future packaged GUI includes its own
Electron runtime and can include a platform-matched Go Host. See the
[release boundaries](../architecture/electron-gui.md#multiple-frontends-and-release-boundaries).

## Connect to an existing Host

Keep the already-running Host that owns the intended Session. Follow the
[MCP Host workflow](../getting-started/mcp-server.md) or the
[shared Session workflow](../getting-started/shared-session.md) to start it when necessary; do not
start another Host merely because the GUI is disconnected. This prototype never starts one itself.

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

1. Select **Connect to Host** in the bottom-left Host panel, then a Session in the sidebar. The initial screen
   contains a recent output snapshot, not an unlimited log.
2. Type in the terminal. Input goes through `terminal_write` with actor `user`; Ctrl+C is ordinary
   terminal input when input is enabled. No command delimiter is added by the adapter.
3. A Host lease makes the view read-only. A rejected write discards queued input; **Resume input**
   acknowledges recovery when the Host no longer reports a lease. It does not replay old input.
4. **Close tab (detach)** (the current attachment's close icon), **Disconnect Host**, or closing the window leaves the shared Session running.
   There is deliberately no close-Session button.

The sidebar's Host panel shows status and its configured endpoint. The activity-rail terminal
button or **View** menu hides/reopens the sidebar without detaching. **Help** shows a local
usage summary. **Settings > Language** switches menu/help text between English, Simplified Chinese,
and Japanese for the current window. Menus close with Escape or an outside click. See the
[menu reference](../reference/gui-menus.md) for actions, targeting, and unimplemented planning boundaries.
Search filters Session references, labels,
endpoints, and transport names; Serial/SSH/Other group headers expand or collapse their rows.
An empty connected Host shows
**No Sessions available**; open one with the CLI and use **Refresh**. On connection failure,
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

The Node tests cover authenticated list/attach/read/write, timeout and cancellation, duplicate detach
and disconnect, stale attachment rejection, lease contention and recovery, buffer overwrite, Session
close, Host disconnect, credential redaction, redirect refusal, IPC source/argument checks, and
independent client survival. They do not prove hardware or remote-network behavior.

`test:desktop` builds the production bundle and uses Playwright's Electron driver to launch a real
visible native window against that fixture. It checks isolation settings, missing Node globals,
keyboard round trips, displayed output, lease input blocking, overwrite notices, reconnect,
detach/reattach, and window close while another client continues to use the Host. Workbench checks
resize the native window to 760 × 500, 1180 × 780, and 1500 × 900, verifying visible controls,
terminal fitting, and no document overflow. They cover disconnected, connected-empty, selection,
attached, and failure/retry views, plus long labels, closed entries, and a scrolling 40-Session list.
They also check transport grouping, case-insensitive search, no-match feedback, group collapse
across refresh, and continued attachment/input after hiding and reopening navigation.
Menu checks cover native/renderer language and checkmark synchronization, sidebar actions,
keyboard opening, Escape/focus restoration, outside-click dismissal, and disabled offline/empty/
closed actions. They reject stale attachment callbacks and missing non-current context targets,
verify local clear leaves Host history intact and sends no bytes, and exit the GUI with an
independent observer still attached. Ctrl+C/Ctrl+L/Ctrl+W, Alt and F1 pass through xterm.
Chromium CDP composition/commit checks cover Chinese input without duplicate bytes; real OS IME
candidate windows still need manual native testing. No global shortcut is registered.
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
| Token unavailable | Start the intended Host under the same user, or supply its existing token to Main. GUI does not create tokens. |
| Authentication rejected | Verify the configured endpoint and Main environment match the intended Host. Do not disable authentication or change CORS/Origin rules. |
| No Sessions | Open a Session through an existing CLI/MCP workflow on that Host, then refresh. GUI cannot open a device. |
| Lease busy | Continue reading; wait for the owning operation. GUI neither steals nor releases leases. |
| Output overwritten | Earlier bytes are unavailable. xterm resets and continues from retained output; the warning remains while attached. Detaching removes that view's notice. |
| Session closed | Refresh and select an open Session. Reattachment cannot reopen a closed device. |
| Host disconnected / uncertain write | Check Host ownership/liveness, reconnect explicitly, and inspect remote state before retyping. Input is never automatically retried. |
| Full-screen application size differs | xterm fits locally; MCP has no remote PTY resize tool. Use the CLI owner for remote sizing. |
| Window will not launch | Check Electron download completion and native display availability. Scripts clear inherited `ELECTRON_RUN_AS_NODE` for the child process. |
| Race tests fail before compilation | Install the supported C compiler and enable CGO for race testing; ordinary production Go builds remain CGO-free. |

Targets are Windows/Linux/macOS, x64/arm64. Native Windows x64 desktop testing is separate from
cross-compilation; each other native OS/architecture still needs runtime validation. Real serial
settings and hardware are not involved in the fixture. Installer packaging, companion-Host
discovery/startup, signing/notarization, update delivery, and release automation remain unimplemented.
