# Electron GUI Menus

The existing dark workbench keeps its 32 px, logo-free menu strip. Its seven groups, from left
to right, are **Session, Host, Terminal, View, Window, Settings, Help**. The maximum depth is
three levels, used by **Settings > Language > language name** and **Session > File Transfer > action**.
**Session > New Connection...** directly opens the connection form. Its Protocol selector switches
between the existing Serial and SSH forms; switching discards protocol fields, including secrets.
SSH is disabled unless the connected Host advertises its desktop API.
This reference describes implemented menu behavior, not additional Host capabilities. See
[GUI architecture](../architecture/electron-gui.md) for process boundaries and
[GUI development](../development/electron-gui.md) for startup and verification.

## Implemented actions

**Session > File Transfer > Resume transfer** (`file.resume`) also appears on a serial Session's context menu.
It binds to that clicked open, unleased Session and lists saved jobs for its Host/endpoint. Execution
revalidates the job and target in Main and CLI before acquiring the existing Host Lease. A saved
`publishing` job is visible for inspection with no enabled Resume button. Starting a new transfer
only saves resumable data when **Keep a checkpoint for manual resume** is checked.

Send/Receive also accepts open SSH Sessions, using a separate SFTP/SCP connection. Its dialog
provides a single-file SFTP resume checkbox; the `file.resume` checkpoint action remains serial-only.
See the [SSH file workflow](../getting-started/file-transfer.md#electron-sftpscp-progress-and-resume)
for authentication, verification and cancellation.

| Menu / surface | Action ID | Behavior and availability |
| --- | --- | --- |
| Host > Connect to Host...; Host panel | `host.connect` | Connect to the configured Host while disconnected and idle; the existing default-local companion startup policy is unchanged. |
| Host > Disconnect from Host; Host panel | `host.disconnect` | Release this window's connection and attachments; enabled while connected and idle. |
| Window > Exit GUI | `app.quit` | Exit the Electron app through its normal cleanup path, subject to the active file-operation close guard. |
| Session > Refresh Sessions; refresh button | `session.refresh` | Refresh the Host snapshot while connected and idle. |
| Session > Close tab (detach); tab close icon; attached row/tab context menu | `tab.close` | Detach the captured local attachment. Still available for a closed Session so its view can be dismissed. |
| Session > Close shared Session...; attached row/tab context menu | `session.close` | Confirm closing the captured open shared Session for every client. Requires a live attachment; no shortcut. |
| Status bar > Copy for AI | `session.copyAI` | Copy metadata and guidance for the captured open Session in this window's language; no device write or AI message. |
| Session row context menu > Attach Session; row click | `session.attach` | Attach the clicked open Session by opaque ID. Unavailable for the current attachment, closed/missing rows, offline state, or a pending operation. |
| Terminal > Focus terminal; terminal context menu | `terminal.focus` | Focus the captured xterm view; requires a current idle attachment. |
| Terminal > Clear local display; terminal context menu | `terminal.clear` | Use xterm's local clear operation; requires a current idle attachment, including a read-only/closed view. |
| Terminal > Resume input; error notice | `terminal.resume` | Visible in menus only while input is paused; enabled only for an open, unleased, idle attachment. Does not replay queued input. |
| View > Session sidebar; upper-right toggle; activity rail | `view.sidebar` | Toggle navigation without detaching. Checkmark reflects visibility. |
| View > Secondary sidebar; upper-right toggle | `view.auxiliary` | Toggle the resizable current-Session details sidebar. |
| View > Bottom panel; upper-right toggle | `view.panel` | Toggle the bottom panel, retaining its active tab and preferred height. |
| View > File transfers / Session status | `view.transfers` / `view.status` | Open the bottom panel on the requested tab, including offline/empty states. |
| Terminal > Copy terminal selection / Paste into terminal; terminal context menu | `terminal.copy` / `terminal.paste` | Copy selected xterm text or paste clipboard text through the existing input path. Paste requires writable text view; copy sends no device bytes. |
| Settings > Keyboard shortcuts | `settings.shortcuts` | Search, record, remove, reset and save local layout and terminal clipboard bindings. |
| Settings > Preferences... | `settings.display` | Open the existing Display settings dialog; font changes apply without reattachment. Keyboard shortcuts retain their separate entry. |
| Session > New Connection... | `connection.new` | Open the unified form on a connected idle Host. No protocol submenu. |
| Session > Attach Session... | `session.choose` | Reveal the existing sidebar and focus its search field while connected and idle. Selecting a row reuses `session.attach`; opening the picker does not change the attachment. |
| Session > Session Information... | `session.info` | Reveal the existing details sidebar for the captured current attachment; no new Host query. |
| Settings > Language | `language.change` | Follow the system or select one of the nine supported menu languages. Exactly one preference is checked. |
| Help > Quick help | `help.quick` | Open a local, translated usage dialog. No external navigation. |

Closing a **tab**, closing a **window**, disconnecting the **GUI from Host**, closing a
**shared Session**, and exiting the **GUI** are distinct operations. Tab close releases the
attachment; disconnect releases this client connection. Native window close cleans up that
window; Exit GUI ends the application. None closes the shared Session or stops its Host.
**Close shared Session is not implemented or exposed by the GUI.** Each window can have multiple
independent terminal attachments. Bottom-panel tabs remain local views of transfers and Session status.

Local clear removes xterm scrollback above the current line, retaining that line and parser
state. It does not send `clear`, `reset`, escape sequences, or any other bytes to the device.
It does not change the Host buffer or read cursor, dismiss loss notices, or reset lease state.
Subsequent output continues normally; a new attachment may replay retained Host output.

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
| Exit GUI | `app.quit` | Existing application exit and file-operation guard, independent of Host connection. |

The tab strip uses `tab.select` and the same close/move dispatcher as menus. Arrow keys and
Home/End navigate only while that strip has focus, excluding IME and modifier chords. No new
terminal shortcut is registered. Session selection fills an empty tab, activates an existing view,
or opens another tab. Create an empty tab first to attach the same Session twice. Each view has
independent cursor, input state and subscriptions. Closing a background tab targets that tab only.

Moving reconstructs xterm from recent retained Host bytes (up to 64 KiB); a translated notice
states that full screen state and earlier history may be unavailable. The destination must finish
its first read/render and acknowledge within 20 seconds before the source is removed. Failure
retains the original tab and shows a translated notice; it never reopens a device or retries input.
No dragging, split terminals, saved tab restoration, or complete terminal-state migration is provided.
Host Information has no dedicated handler and is omitted; the Host panel retains endpoint/status.
Session Information reuses the existing details sidebar.

## Targeting, state, and security

The application menu, toolbar/row controls, and menu keyboard activation dispatch the same
finite action IDs to the renderer's existing handlers. Native Electron menus use the same
description and send validated tab/terminal actions through a fixed Preload subscription. Window
actions invoke the same Main handlers used by the narrow renderer IPC methods.
The Main-owned native exit action and fixed IPC `quit` method share the same cleanup handler,
so native quit remains available after a renderer crash or the last macOS window closes. A separate
bounded `syncMenu` message supplies display hints; it accepts no arbitrary menu template,
callback, executable accelerator, command, URL, or credential. Its five validated shortcut
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
help content also use that locale. The preference lasts for the current window only and defaults
to **Follow System**. A new or moved window inherits it; later changes remain independent. System tags resolve to supported catalogs (for example `ja-JP` to `ja`,
`zh-Hant-HK` to `zh-TW`), with English as fallback. Browser `languagechange` notifications update
menus while following the system; explicit selections remain unchanged. This is menu/help
localization, not a claim that Host diagnostics or the entire
workbench have been translated. The language picker follows the README language order and
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
shortcuts. The renderer handles configured layout and clipboard shortcuts under the focus policy below.
On Windows/Linux, the native application menu model is installed but its
window menu bar is removed, preserving the renderer strip and avoiding an extra Alt menu.
macOS retains its system menu and an app menu for Exit GUI; native macOS validation remains
separate from Windows testing.

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
layout and clipboard chords are the exception; clearing their bindings restores terminal handling. OS-reserved shortcuts and
real IME candidate-window behavior require native platform checks.

## Layout shortcuts

| Action | Windows / Linux | macOS |
| --- | --- | --- |
| Session sidebar (left) | Ctrl+B | Command+B |
| Secondary sidebar (right) | Ctrl+Alt+B | Command+Option+B |
| Bottom panel | Ctrl+J | Command+J |
| Copy terminal selection | Ctrl+Shift+C | Ctrl+Shift+C |
| Paste into terminal | Ctrl+Shift+V | Ctrl+Shift+V |

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
defaults. Original three-action preferences retain their layout bindings and gain the default
clipboard bindings. Reset and Apply recovers the defaults without editing Host configuration. Layout
visibility, widths, selected bottom tab and height remain window-local and reset on reopening.

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

## Terminal display

**Settings > Preferences...** opens the existing Display dialog, controlling integer font size (8-40 px, default 14) and font family (up to
256 characters). The default is `Consolas, "Liberation Mono", monospace`. Families use installed
fonts, with a monospace fallback; no fonts are downloaded. Apply and Restore defaults affect
only local presentation. Cancel/Escape discards the draft. **Ctrl+wheel** over the terminal
changes size by one pixel per wheel event within the same limits; it does not zoom the application
chrome, send mouse reports, or issue a remote resize. Normal scrolling is unchanged.
Display changes refit the existing xterm without clearing content, changing cursors or reattaching.
Settings persist in the GUI profile under `channelterm.display.v1`; malformed values use defaults.
If saving fails, the dialog stays open. Wheel changes still apply locally with a storage notice.

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

## Planning boundaries (not implemented)

Except for the disabled Window entries above, these are placement decisions only, not runtime
menu entries or commitments to implement them.

| Proposed category | Unimplemented scope / prerequisite |
| --- | --- |
| Host / Session | Saved profiles and recent connections remain outside the explicit serial/SSH forms. Default-local Host startup is automatic; other endpoints remain explicit. |
| Session | Recent connection history is not implemented. Shared close and multiple tabs follow the explicit contracts above. |
| Terminal | Select All, Find, split panes and remote PTY resize have no existing menu handlers and are not exposed. Existing clipboard, local clear, focus and conditional resume actions remain. |
| View | MCP activity, status-bar visibility and a combined Reset Layout action are unimplemented. Existing sidebar, bottom-panel, transfer and Session-status actions remain; Session status is not a status-bar toggle. |
| Settings | Theme selection is unimplemented and not exposed. Shortcuts and terminal display settings persist locally. Language and layout state remain window-local; endpoint editing and Host process management are absent. |
| Help | Online documentation navigation, updates, and diagnostic export require separate capability/security review. |

No built-in AI Agent, new SSH/file-transfer implementation, Core use case, or MCP protocol change
is implied by this menu organization.
