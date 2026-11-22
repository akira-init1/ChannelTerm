# Electron GUI Menus

The existing dark workbench keeps its 32 px, logo-free menu strip. Its seven groups, from left
to right, are **Session, Host, Terminal, View, Window, Settings, Help**. The maximum depth is
three levels, used by **Settings > Language > language name** and **Session > File Transfer > action**.
**Session > New Connection...** directly opens the connection form. Its Protocol selector switches
between the existing Serial and SSH forms; switching discards protocol fields, including secrets.
SSH is disabled unless the connected Host advertises its desktop API.
For an unknown SSH key, an updated Host can request confirmation of the current handshake.
The dialog displays address, algorithm and SHA-256 fingerprint, focuses Reject initially, and expires
after two minutes. Approval saves the Host trust record; changed/revoked keys remain rejected.
See [first-trust semantics](gui-host-api.md#ssh-first-trust-confirmation) for cancellation and the
distinction from dedicated local file-transfer trust.
The Serial form includes **Serial profiles**: refresh, apply a profile, save current settings with
explicit overwrite opt-in, and set an existing profile as default. Profiles share the Host CLI
configuration, not GUI preferences. Applying a profile resets wake to off; sending the wake carriage
return requires a fresh checkbox selection. See [profile semantics](gui-host-api.md#serial-profiles).

Click the serial **Port** field once to list available ports, or type an endpoint manually.
Suggestions show the endpoint and the product name reported by the Host, for example
`COM8 — USB-SERIAL CH340`. Ports without a product name show just the endpoint.
Typing filters by either name. Choosing a suggestion fills in only the endpoint.
Press **Connect** to open it.
**Connect** and **Cancel** stay below the scrollable form. Serial encoding, view mode and description
are under **Terminal settings**. **Scan ports** opens **Device discovery** beside the serial fields
when space permits, or below them in a narrow window. Expand **Device details** for the identity
type, device ID and USB metadata. While visible, discovery refreshes read-only Host observations
and shows presence/disappearance, persistent USB-serial or USB-location identity, runtime/unknown
identity, metadata and previous endpoints. Selecting a row shows the Host's policy, decision and
existing Session when known; selection never connects. Press **Connect** explicitly. A changed ID,
endpoint, failed scan or stale scan requires refresh/reselection. Manual typing or applying a profile
clears the identity binding. **Hide discovery** stops view polling; Main still rechecks any retained
selection before opening. Older Hosts without scan-health metadata support manual entry only.
External occupancy and access permissions are not inferred from enumeration. Details and limits are
owned by the [device module](../modules/device-registry.md) and [GUI architecture](../architecture/electron-gui.md).
This reference describes implemented menu behavior, not additional Host capabilities. See
[GUI architecture](../architecture/electron-gui.md) for process boundaries and
[GUI development](../development/electron-gui.md) for startup and verification.

## Implemented actions

Dropping local files uses the same transfer flow as Send. Serial drops target the current shell
directory, or the displayed directory when dropped into the send browser. Directory batches use
the shared [directory preference](configuration.md#toml-schema), asking once by default. SSH drops
populate the dedicated credential form; a drop in its browser chooses that displayed directory.
See [limits, cancellation and destination behavior](../getting-started/file-transfer.md#shared-directory-preference-and-desktop-drops).

**Session > File Transfer > Resume transfer** (`file.resume`) also appears on a serial Session's context menu.
It binds to that clicked open, unleased Session and lists saved jobs for its Host/endpoint. Execution
revalidates the job and target in Main and CLI before acquiring the existing Host Lease. A saved
`publishing` job is visible for inspection with no enabled Resume button. Starting a new transfer
only saves resumable data when **Keep a checkpoint for manual resume** is checked.

Send/Receive also accepts open SSH Sessions, using a separate SFTP/SCP connection. Its dialog
provides a single-file SFTP resume checkbox; the `file.resume` checkpoint action remains serial-only.
Selecting SFTP also offers **Browse SFTP**: explicitly list a directory, open a listed
subdirectory, inspect properties, or choose a file/directory path for the transfer form.
Choosing a path does not start a transfer. Symbolic links and other special entries can be
inspected but cannot be selected for transfer. Browsing uses the form's local credentials and
trust file, does not grant first-host trust, and supports cancellation or closing the dialog.
Lists are bounded; unsupported names/metadata or excessive listings produce an error rather
than a partial selectable result. The form retains entered credentials only while open and
clears password fields before transfer dispatch.
The browser's explicit create/rename/remove actions reuse this dialog, without another menu group.
They require absolute named paths and native confirmation of the exact target. Rename does not
overwrite; `rmdir` accepts empty directories only. Cancellation and connection loss can leave an
unknown result, so the UI discards old listings and requests fresh inspection before any retry.
See the [SSH file workflow](../getting-started/file-transfer.md#electron-sftpscp-progress-and-resume)
for authentication, verification and cancellation.

| Menu / surface | Action ID | Behavior and availability |
| --- | --- | --- |
| Terminal > Debug send and snippets | `send.open` | Open an explicit text/HEX sender for the captured open, unleased attachment. No shortcut, timer or broadcast. |
| Terminal > Enter U-Boot… | `uboot.open` | Open the [board-specific operation panel](../getting-started/serial-terminal.md#electron-operation-panel) for the captured writable, open, unleased serial attachment. Requires Host HTTP cancellation capability; no shortcut or automatic reset. |
| Terminal > Continuous recording; recording status indicator | `logs.open` | Start explicit raw/text output recording for the captured view, inspect this window's records, stop them, or select one local log file for confirmed trash. Available offline for retained results. |
| Host > Configure or repair MCP clients | `setup.open` | Inspect one local client, verify existing access, or explicitly confirm file-adapter setup/repair. Available offline; no token or configuration path enters Renderer. See the [setup workflow](../getting-started/mcp-server.md#desktop-client-setup-and-repair). |
| Host > Host information and connection | `host.settings` | Show Main-owned versions, connection capabilities and the local endpoint, including while offline. Saving disconnects this window; no shortcut or Host-stop action. |
| Host > Connect to Host...; Host panel | `host.connect` | Connect to the configured Host while disconnected and idle; the existing default-local companion startup policy is unchanged. |
| Host > Disconnect from Host; Host panel | `host.disconnect` | Release this window's connection and attachments; enabled while connected and idle. |
| Window > Exit GUI | `app.quit` | Exit the Electron app through its normal cleanup path, subject to the active file-operation close guard. |
| Session > Refresh Sessions; refresh button | `session.refresh` | Refresh the Host snapshot while connected and idle. |
| Session > Close tab (detach); tab close icon; attached row/tab context menu | `tab.close` | Detach the captured local attachment. Still available for a closed Session so its view can be dismissed. |
| Session > Close shared Session...; attached row/tab context menu | `session.close` | Confirm closing the captured open shared Session for every client. Requires a live attachment; no shortcut. |
| Status bar > Copy for AI | `session.copyAI` | Copy metadata and guidance for the captured open Session in this window's language; no device write or AI message. |
| Session row context menu > Attach Session; row click | `session.attach` | Attach the clicked open Session by opaque ID. Unavailable for the current attachment, closed/missing rows, offline state, or a pending operation. |
| Terminal > Focus terminal; terminal context menu | `terminal.focus` | Focus the captured xterm view; requires a current idle attachment. |
| Terminal > Find in terminal; terminal context menu | `terminal.find` | Search the captured attachment's retained local text, including read-only/closed views. No Host history query or device write. |
| Terminal > Auxiliary display > Show system timestamps / Show sequence numbers / Show fold markers | `terminal.timestamps` / `terminal.sequence` / `terminal.foldMarkers` | Independent per-view checkboxes, initially on, with no shortcuts. Bound to the captured attachment; available for retained read-only/closed output. |
| Terminal > Clear local display; terminal context menu | `terminal.clear` | Use xterm's local clear operation; requires a current idle attachment, including a read-only/closed view. |
| Terminal > Expand all command outputs; terminal context menu | `terminal.fold` | Open all collapsed command blocks in this text view. Individual commands have a fold button in the left margin. |
| Terminal > Resume input; error notice | `terminal.resume` | Visible in menus only while input is paused; enabled only for an open, unleased, idle attachment. Does not replay queued input. |
| View > Session sidebar; upper-right toggle; activity rail | `view.sidebar` | Toggle navigation without detaching. Checkmark reflects visibility. |
| View > Secondary sidebar; upper-right toggle | `view.auxiliary` | Toggle the resizable current-Session details sidebar. |
| View > Bottom panel; upper-right toggle | `view.panel` | Toggle the bottom panel, retaining its active tab and preferred height. |
| View > File transfers / Session status / Session activity | `view.transfers` / `view.status` / `view.activity` | Open the bottom panel on the requested tab, including offline/empty states. |
| Terminal > Copy terminal selection / Paste into terminal; terminal context menu | `terminal.copy` / `terminal.paste` | Copy selected xterm text or paste clipboard text through the existing input path. Paste requires writable text view; copy sends no device bytes. |
| Settings > Keyboard shortcuts | `settings.shortcuts` | Search, record, remove, reset and save local layout, clipboard and terminal search bindings. |
| Settings > File transfer | `settings.transfers` | Read/save the shared local CLI/GUI directory choice: ask, TAR or per-file. Applies to serial directories; SSH remains recursive. |
| Settings > Preferences... | `settings.display` | Open Display settings and terminal defaults for new views; font changes apply without reattachment. Keyboard shortcuts retain their separate entry. |
| Session > New Connection... | `connection.new` | Open the unified form on a connected idle Host. No protocol submenu. |
| Session > Attach Session... | `session.choose` | Reveal the existing sidebar and focus its search field while connected and idle. Selecting a row reuses `session.attach`; opening the picker does not change the attachment. |
| Session > Session Information... | `session.info` | Reveal the existing details sidebar for the captured current attachment; no new Host query. |
| Settings > Language | `language.change` | Follow the system or select one of the nine supported menu languages. Exactly one preference is checked. |
| Help > Diagnostic ZIP... | `help.diagnostics` | Export the captured attachment snapshot or download an existing Host diagnostic ID to a new local file. Requires a connected idle Host. |
| Help > Quick help | `help.quick` | Open a local, translated usage dialog. No external navigation. |

Closing a **tab**, closing a **window**, disconnecting the **GUI from Host**, closing a
**shared Session**, and exiting the **GUI** are distinct operations. Tab close releases the
attachment; disconnect releases this client connection. Native window close cleans up that
window; Exit GUI ends the application. These cleanup actions do not close the shared Session or
stop its Host. Only the separately confirmed **Close shared Session...** action closes the shared
connection for every client. Each window can have multiple
independent terminal attachments. Bottom-panel tabs remain local views of transfers, Session status and retained Session activity.

Local clear removes xterm scrollback above the current line, retaining that line and parser
state. It does not send `clear`, `reset`, escape sequences, or any other bytes to the device.
It does not change the Host buffer or read cursor, dismiss loss notices, or reset lease state.
Subsequent output continues normally; a new attachment may replay retained Host output.

The SSH transfer dialog's **Advanced transfer options** expose explicit overwrite, SFTP per-file
safe replacement and a bounded operation deadline. Resume conflicts are enforced in Main;
overwrite requires native target confirmation with Cancel selected by default. The transfer panel
shows final verified-file counts and typed failure/uncertain outcomes. See the
[file transfer workflow](../getting-started/file-transfer.md#electron-sftpscp-progress-and-resume)
for defaults, destination rules and recovery limits.

## Window action contracts

The always-visible status bar shows the selected target's shared label (or endpoint), short
reference and an abbreviated opaque ID. Hover reveals the full ID; narrow windows retain the
reference and **Copy for AI** button while hiding the longer display fields. Copying always uses
the full ID, configured loopback Host endpoint, transport and endpoint from a fresh Host snapshot.
Main revalidates the captured attachment after the query, rejects closed/foreign/stale handles and
multiline, control-containing or oversized metadata, and writes a bounded plain-text clipboard
message. This does not include output, credentials or local descriptions, send a message to an AI,
or grant control. The guidance asks the recipient to query Sessions, verify the exact target and
stop on a mismatch, then await a separate operation instruction. Labels and the entire template
follow the current window's language, preserving actual metadata values and protocol identifiers.
The clipboard action is unavailable without an open selected target; it has no shortcut or submenu.

**Close shared Session...** displays the captured label, endpoint and full Session ID, with
Cancel focused by default. Confirming calls the existing authenticated `terminal_close` once;
Main validates attachment ownership and rejects stale/foreign handles or a local file operation/tab
move. It never substitutes the selected tab or a short reference. The Host owns closure: its current
contract can close a leased Session and interrupt transfers, debugging or remote processes. This
is not a safe operation-cancellation or lease-release command. Other clients lose that Session too.
Cancel/Escape sends no close request. An unconfirmed result is not retried; cancel the dialog and
refresh Sessions before deciding what to do next. The view keeps its retained output, and input is
paused when confirmation is submitted. Ordinary tab/window closure, disconnect and app exit still
only release this client's resources. The Host process and other Sessions remain running.

The Window menu lists these entries in order. Actions use stable IDs and captured targets;
Main owns BrowserWindow creation, focus and close. Tab state belongs to its renderer.

| Entry | Action ID | Availability / responsibility |
| --- | --- | --- |
| New Window | `window.new` | Create an independent window, up to 16 including pending moves. Reuse the configured Host and inherit the source language preference. Available offline. |
| New Tab | `tab.new` | Create an empty terminal tab and reveal the Session chooser, up to 32 tabs per window while idle. It opens no device; New Connection remains separate. |
| Close Tab | `tab.close` / `tab.closeEmpty` (menu entry `window.tab.close`) | Detach only the captured attachment, including a closed Session view, or remove the captured empty tab. Requires an idle tab; the last tab does not close the window. |
| Close Window | `window.close` | Close only the invoking window through the same guarded native close path as its title bar. Its owned file operations can block closure. |
| Next Tab / Previous Tab | `tab.next` / `tab.previous` | Cycle in strip order, wrapping; enabled for more than one tab while idle. |
| Next Window / Previous Window | `window.next` / `window.previous` | Focus/restore the adjacent committed window in creation order, wrapping; requires more than one window. Pending handoff windows are excluded. |
| Move Tab to New Window; tab context menu | `tab.move` | Move the captured open attachment, including a background tab, when connected and idle with window capacity. Preserve source until destination acknowledgement; owned file work blocks the move. |
| Split right / Split down; tab context menu | `tab.splitRight` / `tab.splitDown` | Show two equal panes using an independent attachment to the captured open Session. Requires connection, idle state and a free tab slot; an existing visible pair can change orientation at the limit without another attachment. |
| Single view | `view.single` | Show only the focused tab, retaining the companion tab and attachment. Available while idle with an active split. |
| Exit GUI | `app.quit` | Existing application exit and file-operation guard, independent of Host connection. |

The tab strip uses `tab.select` and the same close/move dispatcher as menus. Arrow keys and
Home/End navigate only while that strip has focus. Shift+Left/Right reorder the focused tab;
IME composition and other modifier chords are ignored. Drag a tab before or after another tab
in the same window to reorder it. Neither operation replaces attachments or changes selection. No new
terminal shortcut is registered. Session selection fills an empty tab, activates an existing view,
or opens another tab. Create an empty tab first to attach the same Session twice. Each view has
independent cursor, input state and subscriptions. Closing a background tab targets that tab only.

Moving reconstructs xterm from recent retained Host bytes (up to 64 KiB); a translated notice
states that full screen state and earlier history may be unavailable. The destination must finish
its first read/render and acknowledge within 20 seconds before the source is removed. Failure
retains the original tab and shows a translated notice; it never reopens a device or retries input.
Complete terminal-state migration is not provided; limited workspace recovery is described below.

Split panes keep independent output cursors, including when they show the same Session. Click a
pane or focus its heading/terminal to select it; the selected heading is marked and underlined.
Only that pane accepts input, subject to the same read-only, Lease and operation-control checks.
Selecting another tab replaces the focused pane without swapping its position. Closing or moving
either pane returns to one visible view without closing the shared Session. Single view retains
the hidden companion; splitting again creates a new observer tab. The total remains bounded to
32 tabs, with at most two visible panes and no nested splits or input broadcast. SSH observers fit
locally until explicit size-control takeover; pane focus and layout changes grant no authority.

## Targeting, state, and security

The application menu, toolbar/row controls, and menu keyboard activation dispatch the same
finite action IDs to the renderer's existing handlers. Native Electron menus use the same
description and send validated tab/terminal actions through a fixed Preload subscription. Window
actions invoke the same Main handlers used by the narrow renderer IPC methods.
The Main-owned native exit action and fixed IPC `quit` method share the same cleanup handler,
so native quit remains available after a renderer crash or the last macOS window closes. A separate
bounded `syncMenu` message supplies display hints; it accepts no arbitrary menu template,
callback, executable accelerator, command, URL, or credential. Its six validated shortcut
strings are display hints only; native menus append their labels without registering accelerators.
A fixed `quit` method requests app cleanup.

A right-click captures that row's opaque Session ID or that terminal/tab's attachment ID and
Session ID. It never changes selection merely to open the menu. A non-current row cannot close
the current tab. Execution checks the latest local state again, and Main rechecks the requested
Session against the Host before replacing an attachment. A missing/rejected target leaves the
previous attachment in place. After reattachment, callbacks holding the previous attachment
handle cannot clear, focus, copy, paste, resume, or close the new view.

Offline state disables connection creation, refresh, disconnect, and attachment actions. A connected empty list
still allows refresh and disconnect. Closed rows cannot attach; an existing closed view can
still be cleared locally or detached. Pending connection/attachment changes disable conflicting
actions. Copy, clear and focus send no device bytes; paste is ordinary terminal input and
remains subject to input pause, hex-view, lifecycle and Host lease checks.
Menu hints never grant permission: Main validates IPC caller/arguments and attachment ownership,
and every device write still passes through authoritative Host/Application checks. Rejected or
uncertain writes are not automatically replayed.

Unimplemented operations are absent. Checkmarks describe
local view/language state only. Renderer reload/crash resets Main's menu hints and releases its
Host client resources.

## Translation and keyboard behavior

Lingui Core runs in Main and Renderer with separate instances of the same compiled PO catalogs. Renderer owns the
active menu locale and synchronizes it with native labels/checks; open context-menu labels and
help content also use that locale. The preference defaults to **Follow System** and is saved for
future windows/restarts. A new or moved window inherits the source preference; later changes in live
windows remain independent. System tags resolve to supported catalogs (for example `ja-JP` to `ja`,
`zh-Hant-HK` to `zh-TW`), with English as fallback. Browser `languagechange` notifications update
menus and workbench text while following the system; explicit selections remain unchanged.
Session lists, status/empty states, disconnect and output-loss notices, accessibility labels,
and form recovery messages use the same nine catalogs. Errors retain stable codes or message IDs
and are translated at render time, including an already-open dialog after a language change.
Recovery guidance uses safe structured reasons (including SSH authentication and host-key rejection),
never diagnostic-string matching. Unknown failures use generic guidance rather than exposing raw
error text. Inspect the Host diagnostics for technical detail. Terminal bytes, user labels and paths,
protocol identifiers, operation metadata and logs remain unchanged; they are not translated.
The language picker follows the README language order and
uses native names so it remains recognizable after switching:

| Locale | Picker label |
| --- | --- |
| `system` | Follow System |
| `en` | English |
| `de` | Deutsch |
| `es` | Español |
| `fr` | Français |
| `ja` | 日本語 |
| `ko` | 한국어 |
| `ru` | Русский |
| `zh-CN` | 简体中文 |
| `zh-TW` | 繁體中文 |

Adding a message requires complete translations in all nine catalogs.

The official Lingui 5.9.0 CLI extracts annotated message definitions into independent PO files
and compiles them before both process builds. Generated modules are not maintained by hand.
Strict production checks reject missing target translations; non-strict development compilation
uses the English source fallback. See the [translation maintenance workflow](../development/electron-gui.md#menu-translations-lingui-590).
No runtime ICU compiler or translation service is added. The Node 22.12 minimum, Go dependencies,
and CGO requirements are unchanged; build tooling and its notices remain GUI-local.

Menus register no Electron accelerators, mnemonic markers, default action roles, or OS-global
shortcuts. The renderer handles configured layout, clipboard and search shortcuts under the focus policy below.
On Windows/Linux, the native application menu model is installed but its
window menu bar is removed, preserving the renderer strip and avoiding an extra Alt menu.
macOS retains its system menu and an app menu for Exit GUI; native macOS validation remains
separate from Windows testing.
A native application-menu snapshot belongs to the window that supplied it. Delayed callbacks
are ignored after focus moves to another window; captured attachment actions are never redirected.
Choose the action again from the focused window's current menu. Exit GUI remains app-wide;
New Window also remains available after the last macOS window closes. Right-click menus retain
their explicit row/tab targets.

Tab reaches menu buttons. Left/Right moves between focused top-level buttons; Down or Enter
opens one. Hover alone does not open a closed menu. After clicking a top-level group, moving
the pointer over another group switches its popup without another click. Moving away without
clicking keeps menu mode active. Choosing an action, clicking the active group or outside the
menus, pressing Escape/Tab, or losing window focus ends menu mode; context menus do not arm it.
Within a popup, Up/Down/Home/End moves through enabled entries and Enter activates
the same action dispatcher. After opening a top-level menu, hovering a parent row expands its
submenu to the right; it flips left near the window edge. Click and Right also open it; Left
returns focus to the parent. Escape closes
the popup and restores its opener; an outside click dismisses it. These handlers run only inside
the focused menu and ignore composition/229 events. Terminal Ctrl+C/Ctrl+L/Ctrl+W, Alt sequences,
function keys, and IME composition remain in the terminal input path. Explicitly assigned
layout, clipboard and search chords are the exception; clearing their bindings restores terminal handling. OS-reserved shortcuts and
real IME candidate-window behavior require native platform checks.

## Layout shortcuts

| Action | Windows / Linux | macOS |
| --- | --- | --- |
| Session sidebar (left) | Ctrl+B | Command+B |
| Secondary sidebar (right) | Ctrl+Alt+B | Command+Option+B |
| Bottom panel | Ctrl+J | Command+J |
| Copy terminal selection | Ctrl+Shift+C | Ctrl+Shift+C |
| Paste into terminal | Ctrl+Shift+V | Ctrl+Shift+V |
| Find in terminal | Ctrl+Shift+F | Ctrl+Shift+F |

These defaults follow the [VS Code sidebar layout](https://code.visualstudio.com/docs/configure/custom-layout)
and [sidebar/panel shortcuts](https://code.visualstudio.com/docs/editing/getting-started/tips-and-tricks#toggle-sidebar).
The upper-left pane icons and View checkmarks reflect the same state. Hover tooltips, renderer
menus, and native menu labels display the current bindings. No shortcut is active outside this
application window. Window-manager interception still depends on the desktop environment.

**Settings > Keyboard shortcuts** searches by translated action name, action ID, or displayed
key combination, without changing hidden draft bindings. It records a combination when its
binding field is focused. Press Ctrl
or Command with a physical A-Z or 0-9 key; Alt/Option and Shift are optional. Physical positions
keep Option-modified letters usable on macOS and do not depend on the produced character.
Plain typing, function keys, Alt-only combinations, both Ctrl and Command together, and
Ctrl/Command with A/C/V/X/Z/Y/W/Q/R/L (also with Shift, except Ctrl+Shift+C/V) are not assignable. These reserved
editing/window combinations retain their existing behavior. Duplicate bindings disable Apply.
**Remove** unassigns a command; **Restore defaults** updates the draft; **Apply** validates and
saves; Cancel/Escape discards the draft. A storage failure leaves the dialog open with an error.

Bindings are stored in the GUI profile's renderer local storage under
`channelterm.layout-shortcuts.v1` and survive reopening on the same origin/profile. Production
and development origins have separate settings. Invalid stored data falls back to platform
defaults. Original three-action and five-action preferences retain every saved binding and gain
new defaults only when their chords are free. If Ctrl+Shift+F is already assigned, Find remains
unbound and a notice links to Keyboard shortcuts. Reset and Apply recovers the defaults without editing Host configuration. Layout
visibility, widths, selected bottom tab and height remain independent in live windows and persist
as defaults for reopening; see [workspace recovery](#preferences-and-workspace-recovery).

Only exact modifier matches dispatch, once per press; key repeats are consumed without repeated
toggling. IME composition/229 and AltGr are ignored. Open dialogs, focused menus, and ordinary
input/select/contenteditable fields retain their keys. When xterm is focused, assigned layout
chords are intercepted before xterm and send no bytes; unassigned chords retain terminal handling.
Layout actions use the same dispatcher from menus, icons and shortcuts and do not detach,
close, write, or resize a remote Session. Clipboard shortcuts dispatch only with terminal focus;
ordinary editors retain their own clipboard behavior. Copy uses only xterm's selection and does
nothing without a selection. Paste uses xterm's newline/bracketed-paste handling and the existing
bounded input queue. Text containing CR or LF first opens **Review multiline paste**, showing the
target label, full Session ID and bounded content. Cancel/Escape sends nothing; confirmation is
single-use. Escape bytes appear as `␛` in the preview only. A Lease, read-only transition, tab change
or replacement invalidates the draft permanently; reopening input does not replay it. Detaching
discards the preview. Menu, shortcut and native terminal paste share this flow. Confirmed paste
can execute commands containing newlines; no extra line-ending delimiter is added.
Main validates the caller and current attachment for text-only clipboard IPC, limited to 64 KiB
of UTF-8 per operation. Empty clipboard text does nothing; oversized text is rejected rather
than truncated. Late replies cannot paste into a replacement view. Browser clipboard permission
remains denied. Native Windows testing does not establish macOS/Linux
window-manager or physical IME behavior.

## Current terminal search

**Find in terminal** opens an attachment-local search bar. It matches literal text in xterm's
finite retained buffer, with case sensitivity, previous/next buttons, Enter/Shift+Enter navigation,
and a current/total count. It does not search other tabs, Host history or exported logs. HEX mode
searches the displayed hexadecimal text. All matches and the current match have distinct temporary
decorations, including in Black and white mode. Closing with Escape or the close button clears
decorations and the search selection, then returns terminal focus. Hiding or detaching the tab
discards the search. Opening Find expands folded output before looking for matches.

Queries are limited to 256 characters, updates debounce for 100 ms, and highlights stop at 1000
matches with an explicit limit notice; this is not an exact count beyond the cap. **Select all
retained text** selects the current buffer for ordinary terminal copying. Query text and navigation
never write to the device. Ctrl+Shift+F may refocus the search input; other editors, menus, dialogs,
IME composition and AltGr keep their existing key handling. Ordinary terminal clipboard shortcuts
remain scoped to terminal focus.

## Terminal display

Click the small minus beside a recognized shell command to collapse its output. Hover or
focus the marker to show its guide. A plus marks a collapsed command. Click the plus
to reopen that command, or choose **Terminal > Expand all
command outputs**. Opening Find reveals all retained text, including folded matches.

The next recognized prompt closes a block. The fold margin recognizes the latest 512
retained shell integration prompt markers (`OSC 133;A`). Common shell, PowerShell, drive-path and
bootloader prompts are recognized from the displayed text. Raw serial logs without
recognizable prompts have no command boundaries. Wrapped command rows stay together.
Folds belong to one text view and clear on reset, output loss, detach or alternate-screen
entry. The live terminal keeps accepting input and output during folding. Scrollback
trimming can remove a block. Historical rows above a fold are projected in plain text.

**Settings > Preferences...** opens the existing Display dialog, controlling integer font size (8-40 px, default 14) and font family (up to
256 characters). The default is `Consolas, "Liberation Mono", monospace`. Families use installed
fonts, with a monospace fallback; no fonts are downloaded. Apply and Restore defaults affect
only local presentation. Cancel/Escape discards the draft. **Ctrl+wheel** over the terminal
changes size by one pixel per wheel event within the same limits; it does not zoom the application
chrome or send mouse reports. An SSH size-controlling view also forwards its newly fitted geometry. Normal scrolling is unchanged.
Display changes refit the existing xterm without clearing content, changing cursors or reattaching.
**Terminal style** offers **Default (CLI colors)** and **Plain (white on black)**, with a local
preview and Restore defaults. Default applies the shared semantic palette to safe plain text;
remote ANSI/full-screen output retains its own control semantics. Plain suppresses both local
semantic colors and remote foreground/background colors, including retained output, while keeping
selection, cursor visibility and terminal controls. It does not change raw output or HEX bytes.
Switching back restores the retained terminal attributes; it does not recover lost history.
In Default style, `user@host:path$ ` and `[user@host]:path$ ` color the user, host, path and
final `$` or `#`. Punctuation and echoed commands retain their original colors. The
`[user@host path]# ` form displays immediately in its original colors.
The GUI displays receipt times in the [line metadata columns](#system-timestamps-and-sequence-columns).
Existing display preferences retain font and style choices. The retired inline prompt timestamp
setting is ignored when loading older profiles.
If saving fails, the dialog stays open. Wheel changes still apply locally with a storage notice.

### Connection keyword rules

Open **New Connection... > Recent connections and SSH favorites > Keyword highlights...** for one
saved record. Add or edit literal text and color, enable/disable rules, move them up/down, remove
one, or clear the current draft. **Save connection rules** applies that draft; Cancel keeps saved
rules. Up to 32 rules are allowed, each with 1–128 UTF-16 code units, no control characters or lone
surrogates, and a six-digit RGB color. Earlier enabled rules win overlapping characters before the
shared defaults. Matching is case-sensitive and does not interpret regular expressions.

The current semantic style applies rules to future safe text only; retained cells are not recolored.
Incomplete lines flush unchanged on rule changes. Prompts, remote ANSI state, full-screen/alternate
screens and HEX keep their existing behavior. Plain style suppresses local colors. Search
decorations and normal text copy remain independent; Host/CLI/MCP bytes and default rules do not change.

Templates hold named rule lists. Import appends independent copies within the 32-rule limit;
changing or deleting a template never changes previously imported connections. Saving a new
template or replacing/deleting the selected one takes effect immediately, separately from saving
connection rules. Up to 10 templates are stored in `channelterm.highlight-templates.v1`, with
1–64 UTF-16 code units per name and at most 65,536 stored UTF-16 code units. Storage errors keep
the editor open for explicit retry or Cancel; a removed connection is never silently recreated.

Use the saved connection explicitly to associate its rules with a terminal view. Its local UUID
survives reuse, split views, window moves and validated workspace restore; it is not a Session ID,
short reference or bare port. Legacy records gain an ID on explicit use or editing; existing
unbound views keep defaults until the saved connection is explicitly opened. Rebinding an existing
view uses the normal attachment replacement and its retained-output limit. Missing/deleted/corrupt
profiles fall back to defaults. General terminal presentation changes retain the binding; new-view
defaults do not inherit it.

## File actions

**Session > File Transfer > Send to device / Receive from device** (`file.send` / `file.receive`) are also
available on serial and SSH Session context menus. They bind the clicked opaque ID and require an
open Session. Serial transfers revalidate Host lease state, browse child-shell directory snapshots
and render Host events from all observed clients. SSH transfers use explicit remote paths and a
dedicated connection, with progress from this desktop's CLI child. Both use native local selection.
Main rejects disconnect,
reconnect and app/window exit during a GUI-owned file operation until completion or cancellation.
Blocked window-close and app-quit attempts show one themed in-app notice and open the transfer
panel. Continue or Escape dismisses the notice without cancelling the transfer or retrying close;
the user must finish/cancel the operation and explicitly close again. Main remains the close guard.
See the [file workflow](../getting-started/file-transfer.md#electron-file-picker-and-progress).

## Session activity

**View > Session activity** opens read-only observations for the selected attachment. The first
read takes at most the latest 32 activity entries and 32 structured events; subsequent reads
follow independent cursors. The view retains at most 200 rows and separately reports Host
retention loss and local truncation. Hiding it pauses polling; reopening resumes its cursors.
Detaching discards that attachment's view. It neither consumes terminal output nor changes a Lease.

Rows distinguish initial history from later observations, display Host timestamps in UTC and
show user/Agent/system source labels. These labels are recorded sources, not verified client
identities. Input rows show only byte counts. Structured rows retain protocol event names and
bounded allowlisted metadata for connection, Lease, operation and transfer outcomes. Debug
reports are explicitly client claims, not a complete GDB audit. No raw input payload or Lease
owner capability crosses into Renderer. Reports may still contain client-provided text.

This is finite retained evidence, not durable logging or a total ordering across the two streams;
equal-millisecond timestamps use a stable stream/sequence tie-break. Empty and disconnected tabs
show a selection hint. Read errors remain visible and polling resumes without replaying writes.

## Diagnostic ZIP export

**Help > Diagnostic ZIP...** offers a snapshot of the attachment captured when the dialog opens,
or an existing `DIAG-` identifier from the connected Host. Snapshot defaults are metadata-only;
**Include raw terminal output and write contents** explicitly opts into sensitive retained bytes.
Existing archives are downloaded as created and may already include raw data. They are not
redacted again by the GUI. The [Host contract](mcp-tools.md#terminal_export_diagnostics) owns ZIP
contents, retention, expiry and completeness limits. This is a finite snapshot, not ongoing logging.

The native save dialog chooses a **new** local filename. Existing files and symlinks are rejected,
including after a native overwrite prompt; choose another name. Main verifies immutable size and
SHA-256 while downloading bounded chunks. Success shows only the basename, diagnostic ID, byte
count and hash. The GUI never uploads the file or exposes a destination path through Renderer IPC.
Cancel, disconnect, detach of the snapshot attachment, malformed data, hash mismatch or Host
failure removes the incomplete local file; removal failures are explicitly reported. An uncertain
export is not automatically retried. Unknown, expired and evicted IDs show an unavailable-archive
error; create another snapshot. A download has a 60-second budget including destination selection.

## Debug send and snippets

**Terminal > Debug send and snippets** captures one attachment. Text uses that view's selected
encoding and rejects lossy conversion; HEX accepts only hexadecimal digits and ASCII space, tab,
CR or LF, with an even nonzero digit count. `0x` prefixes, odd nibbles and other whitespace are errors.
Choose no appended ending (default), LF, CR or CRLF. The final payload is limited to 4096 bytes,
including the ending; the editor accepts at most 16384 UTF-16 code units and rejects unpaired surrogates.
An empty body cannot be used to send an accidental Enter.

**Preview and send** opens a native confirmation with the opaque Session ID, encoding, byte count
and exact final HEX bytes. Cancel is the default. Confirmation sends those captured bytes through
the existing Host authorization/control/Lease path and bounded attachment write queue. A changed or
detached target/control invalidates pending input; it never substitutes another selected Session.
HEX display remains read-only for ordinary keyboard/paste, but this separate explicit operation can
send confirmed bytes. Closing the window or changing its endpoint waits for an active confirmation/send.
No timer, broadcast or automatic retry is provided. A successful byte count means the Host accepted
input, not that a command succeeded. After a failed/cancelled in-flight request, delivery may be
uncertain: inspect the device before a new explicit send. Closing the panel discards its draft.

Named snippets save only after the plaintext-consent checkbox is selected. Saving the same name
replaces that entry; loading never sends. Delete removes the named entry without affecting devices.
The library holds at most 32 entries, each with a 1–64 character name and the bounded payload above,
in `command-snippets.json` under Electron user data. The file is **unencrypted**: do not store passwords
or secrets. Drafts are not saved automatically and nothing executes on startup. Main serializes edits,
reads the current library for each mutation and atomically replaces it; invalid files or write errors
leave the previous library intact. See [desktop local state](configuration.md#desktop-debug-snippets).

## Continuous recording

**Terminal > Continuous recording** records new Session output after an explicit start. Choose
raw bytes (`.bin`) or readable UTF-8 (`.txt`), a segment limit (1/4/8/16/64 MiB) and 1–8 retained
segments; the defaults are 8 MiB and four segments. A native folder picker creates a new
`channelterm-log-<id>` subdirectory. The dialog shows its full selected location, actual state,
cumulative successfully indexed data bytes, gap count and removed-segment count. It is separate from
the current screen/scrollback and the finite diagnostic ZIP snapshot. There is no automatic start,
resume, history backfill, input replay or remote reconnect.

Recording starts at the Host tail cursor observed after registering this background attachment.
It continues independently of hidden/closed terminal tabs, without taking SSH size control. Closing
the dialog does not stop it. **Stop recording**, window disconnect/endpoint change, window close or
normal app exit cancels pending reads and finishes the accepted disk batch before reporting stopped.
The Host and shared Session remain open. Read/connection failure ends that recording; a disk failure
does not trigger further reads or retries. Slow storage can delay stopping and cause Host history
overwrite, which is recorded as a gap. A later recording is a new explicit operation.

Raw files retain exact received bytes, including transfer/debug protocol ranges suppressed from the
terminal view. Text uses streaming UTF-8, replacement characters for invalid/incomplete sequences,
and `\xNN` escapes for C0/C1 controls except LF and tab. It does not emulate ANSI screen operations,
interpret CR as cursor movement, redact secrets, or infer a device event time. Output may include
echoed commands or sensitive transfer data; unsent drafts and the separate write/activity stream are
not collected. Text decoding resets at a gap; local capture times describe observation, not the time
the device produced individual bytes.

Each numbered data file has a `.jsonl` index. A `segment` header gives format, Session and start
cursor. `output` records carry source `from`/`to` cursors, data `offset`/`bytes` and `receivedAt`.
`gap` records mark a missing source range; text decoder tails can have `decoder-end` records.
The limit includes both data and index, and rotation removes only this recording's oldest segments.
`manifest.json` records version 1, current segment names, limits, state, cumulative counters and the
last cursor when updated. It is not a live per-byte acknowledgment.

The writer syncs data before appending/syncing its index record. A normal stop writes a stopped
manifest. After a crash, a recording marker, missing manifest/index or incomplete last JSON line must
be treated as incomplete: only complete index entries whose ranges fit the data file confirm bytes.
An unindexed tail must not be described as a complete record. Partial files remain after errors for
inspection; filesystem/power-loss guarantees still depend on the storage system. Rotation deliberately
discards old ranges even when capture had no gap. At most four recordings are active and 16 statuses
retained per window; data never passes through Renderer.

**Choose a local log file to delete…** opens a native single-file picker, then confirms the exact path
before moving only that file to the OS trash. It rejects directories, symlinks, files changed during
confirmation and files under an active recording in any window of this process. It does not delete
the whole recording directory or search for files. Inspect paired data/index files before selecting
one; trash support and recovery depend on the OS. Stopping a recording never deletes its retained files.

## Planning boundaries (not implemented)

The remaining scope below is not a commitment to implement additional menu entries. Existing
actions follow the contracts above; connection history and workspace recovery are described below.

| Proposed category | Unimplemented scope / prerequisite |
| --- | --- |
| Host / Session | Automatic device reconnection and recreation of closed Sessions are not provided. Recent connections and SSH favorites prefill explicit forms; see [recent connections and SSH favorites](#recent-connections-and-ssh-favorites). Shared close and multiple tabs follow the contracts above. |
| View | A complete MCP request audit, status-bar visibility and a combined Reset Layout action are unimplemented. Session activity shows retained Host observations, not a complete audit; Session status is not a status-bar toggle. |
| Settings | Application theme selection and Host process management are absent. Local endpoint settings are available under Host; local preferences and workspace recovery are described below. |
| Help | Online documentation navigation and updates remain unimplemented. Diagnostic ZIP export follows the explicit local-save contract below. |

No built-in AI Agent, new SSH/file-transfer implementation, Core use case, or MCP protocol change
is implied by this menu organization.

## SSH size toolbar

On updated Hosts, each SSH terminal has **Use this view for remote size** / **Release size control**
and a display hint identifying local-only fitting or control by this view. The explicit take action
replaces the previous size controller; it does not change input permissions. See the
[desktop contract](gui-host-api.md#remote-pty-size-control).

Serial text views offer **Insert stty command** for an interactive Linux shell. This types the
pane's fitted dimensions through ordinary terminal input. Enter executes the command. See
[serial prompt wrapping](../getting-started/serial-terminal.md#long-prompts-overwrite-the-start-of-the-line).

## Debug sidebar

**View → Debug** and the accessible Debug activity-rail button share one action. Clicking the
active rail view hides the sidebar; switching between Sessions and Debug preserves terminal
tabs and local selection/filter state. The sidebar offers refresh, filter, expandable read-only
details and **Locate Session**, which focuses an existing Session row without attaching it.
Full Session IDs are used for associations; missing Sessions stay unavailable and independent
network operations appear without a Session. Session debug badges and the current-Session footer
entry open the same sidebar. A debug lease alone never proves target execution state.

The sidebar shows registered bridges, managed GDB and registered TCP/SSH observations, with
separate connection/target/tunnel status, configured versus observed endpoints, endpoint roles,
provenance and update time. Unavailable/expired observations remain explicitly unknown. See the
[unified contract](mcp-tools.md#unified-debug-observation). No debugger control actions are added.

## Recent connections and SSH favorites

**Session > New Connection... > Recent connections and SSH favorites** lists up to 50
successful GUI connections, with names, last-use times and use counts. Select a record to prefill
its form, review the settings and explicitly connect. SSH records can be favorited/unfavorited;
any record can be forgotten. Favorites are retained before older recent records when the bound
is reached. The connection label names the record; editing it creates a distinct preset.

The GUI profile stores allowlisted serial/SSH parameters, optional local profile UUIDs and
[keyword rules](#connection-keyword-rules) in renderer local storage under
`channelterm.connections.v1` (at most 131,072 UTF-16 code units). Passwords, private-key passphrases, identity/trust-file paths,
terminal input and output are excluded. Re-enter authentication and custom credential paths.
Serial wake is always reset to off. Selecting a record never connects, sends input or grants trust;
existing Host validation and SSH host-key confirmation still apply. Failed connection attempts do
not update successful-use history. Storage failure reports a notice without retrying the connection.

These records are local form presets, separate from the Host-owned serial profiles shared with
CLI. CLI has no persistent SSH profiles; this feature adds no CLI profile compatibility. Storage
is shared by windows in the same GUI profile; independent simultaneous edits use last-writer wins.

## Preferences and workspace recovery

Language menu choices, sidebar visibility/view/width, secondary-sidebar visibility/width and bottom
panel visibility/tab/height are saved locally. **Settings > Preferences...** also provides encoding,
text/hex view and description defaults for new terminal views without cached Session-specific
settings. Applying defaults leaves current tabs unchanged; restored tabs keep their saved settings.
Font and shortcut settings retain their
existing storage and behavior. Display Reset resets only the display fields, not terminal defaults.

On restart, the window restores saved tab targets, order, selection and the optional two-pane layout.
After connecting to the same configured Host, it rechecks full Session IDs, transport and endpoint against the live Session
list. Surviving Sessions receive fresh client attachments without remote-size control; unavailable
targets show a notice and can be closed or replaced by explicitly selecting a live Session.
Restore never recreates a closed Session,
connects to a device, restores size-control ownership or resends input. Only up to 64 KiB of currently
retained Host output can rebuild each view; complete terminal screen state and earlier history are
not recoverable. No terminal output or unsent input is stored on disk by this feature.

The GUI profile's renderer local storage uses `channelterm.preferences.v1` (language/layout/terminal
defaults, maximum accepted JSON length 8,192 UTF-16 code units) and `channelterm.workspace.v1.0`
through `.15` (per-window snapshots, maximum accepted JSON length 131,072 UTF-16 code units and
32 tabs each). The optional `split` object stores `axis` (`columns` or `rows`) and distinct `first`
and `second` tab indices; `selected` must identify one of those tabs. Malformed split data falls
back to single view while retaining valid tabs. Older snapshots need no migration. Slots are assigned
in window-opening order using the lowest free slot. Additional windows are opened manually; an
incoming moved tab replaces the saved slot's view. Current windows remain independent; preference
edits and concurrent application processes sharing a profile use last-writer wins. Invalid data
falls back to defaults or no workspace. Persistence failure shows a notice without retrying I/O.

Explicitly closing tabs updates the saved workspace while connected. Disconnect/Host failure
preserves the last saved view snapshot; manually connecting again starts the current empty view
unless this is the initial restart recovery. Recovery waits for a successful Host connection and
does not change existing automatic-local-Host startup or explicit remote-Host connection policy.

## Human interruption and control

The current Session's control strip offers **Interrupt AI** (when the public credential kind is
agent), **Take control**, and **Allow AI operations**. These are local strip actions, not global
keyboard shortcuts or additional menu groups. Confirmation shows the captured full Session ID,
operation ID and version. Switching tabs cannot change that confirmation's target; a stale request
is rejected or returns its original retained outcome, never cancels a successor.

Processing and unconfirmed results keep that target's input paused. **Query original result**
reads the same key; it does not retry a mutation. The strip retains one result in window memory
across Host disconnect/reconnect until dismissed or replaced. It is not saved across application
restart and remains subject to [Host result retention](mcp-tools.md#session-control). Recovery
confirmation does not itself grant input; the current Host snapshot must also be writable for this
instance. Another window's successful takeover is not this window's ownership.

**Allow AI operations** has its own confirmation and removes the pause for new operations only.
Stopped tasks and queued input are never replayed. Unknown external executors or failed recovery
can close the original Session; remote background work may continue. Debug takeover uses Host
recovery; this is not a general debugger command console or arbitrary process-kill facility.

The transfer panel keeps its existing exact local SSH cancellation. For public Host-managed
SFTP/SCP rows it offers a separate confirmation bound to the immutable managed transfer ID. A live
user instance with management permission can interrupt that task without receiving its private
paths or writing to a PTY. Wait for final task state: an accepted request can still be cleaning up,
and completion may win the race. Independent SSH cancellation neither takes Session control nor
pauses future independent SSH tasks. Serial tasks use Session takeover or the existing safe
transfer-cancellation flow.


## System timestamps and sequence columns

**Terminal > Auxiliary display** controls two independent columns for the current view. New views
show the local time `[HH:MM:SS]` first, followed by the line number, fold control and terminal text.
Split and moved views use the same defaults. Switching tabs preserves each choice. Workspace
restore reapplies saved choices and rebuilds the display from retained output.
The controls require an attached view and operate independently of global font and style settings.
Each column fits the widest label currently visible in it. Turning either off gives its width
back to the terminal. **Show fold markers** controls the folding margin separately. Turning
it off expands existing folds and removes their buttons. With both metadata columns off
and markers enabled, the buttons use the terminal's existing left inset. Beside a foldable
command, hover over the left edge of the text pane or Tab to the fold button to reveal it.
On a Linux serial shell, [reapply the terminal size](../getting-started/serial-terminal.md#long-prompts-overwrite-the-start-of-the-line)
if the shell still wraps at the previous column.

The timestamp is the local time this view first observed an output batch, not the device's event
time. Hover shows the full UTC timestamp. Historical output without time evidence shows **—** with
an **Unknown historical time** tooltip. Sequence numbers identify first-observed logical lines in
this view, not file line numbers or a complete Session history. Soft-wrapped rows share their line's
time/number; retained rewritten lines keep their first observation. Both columns stay outside
terminal selection and search, so ordinary copy contains only terminal text.

Metadata collection continues while columns or tabs are hidden, with bounded reads and retention.
It does not enable disk recording or survive GUI exit. Delayed output is timestamped when observed;
missing historical times cannot be recovered. Font changes, wrapping and scrolling follow the
retained xterm rows. HEX and alternate screens show a dash with an explanatory tooltip; returning
to normal text restores the chosen columns. Local clear, reset and output loss discard stale anchors;
sequence allocation continues for the view lifetime. Output loss remains explicitly reported.
