# Electron GUI Menus

The existing dark workbench keeps its 32 px, logo-free menu strip. Its six groups are
**Connection, Session, Terminal, View, Settings, Help**. The maximum depth is three levels,
used by **Settings > Language > language name** and **Connection > New connection > Serial / SSH**.
This reference describes implemented menu behavior, not additional Host capabilities. See
[GUI architecture](../architecture/electron-gui.md) for process boundaries and
[GUI development](../development/electron-gui.md) for startup and verification.

## Implemented actions

**Session > Resume transfer** (`file.resume`) also appears on a serial Session's context menu.
It binds to that clicked open, unleased Session and lists saved jobs for its Host/endpoint. Execution
revalidates the job and target in Main and CLI before acquiring the existing Host Lease. A saved
`publishing` job is visible for inspection with no enabled Resume button. Starting a new transfer
only saves resumable data when **Keep a checkpoint for manual resume** is checked.

| Menu / surface | Action ID | Behavior and availability |
| --- | --- | --- |
| Connection > Connect to Host; Host panel | `host.connect` | Connect to the configured existing Host; enabled while disconnected and idle. Does not start a Host. |
| Connection > Disconnect GUI from Host; Host panel | `host.disconnect` | Release this GUI's connection and attachment; enabled while connected and idle. |
| Connection > Exit GUI | `app.quit` | Exit the Electron app through its normal cleanup path, subject to the active file-operation close guard. |
| Session > Refresh Sessions; refresh button | `session.refresh` | Refresh the Host snapshot while connected and idle. |
| Session > Close tab (detach); tab close icon; attached row/tab context menu | `tab.close` | Detach the captured local attachment. Still available for a closed Session so its view can be dismissed. |
| Session row context menu > Attach Session; row click | `session.attach` | Attach the clicked open Session by opaque ID. Unavailable for the current attachment, closed/missing rows, offline state, or a pending operation. |
| Terminal > Focus terminal; terminal context menu | `terminal.focus` | Focus the captured xterm view; requires a current idle attachment. |
| Terminal > Clear local display; terminal context menu | `terminal.clear` | Use xterm's local clear operation; requires a current idle attachment, including a read-only/closed view. |
| Terminal > Resume input; error notice | `terminal.resume` | Visible in menus only while input is paused; enabled only for an open, unleased, idle attachment. Does not replay queued input. |
| View > Session sidebar; activity rail | `view.sidebar` | Toggle navigation without detaching. Checkmark reflects visibility. |
| Connection > New connection > Serial / SSH | `connection.serial` / `connection.ssh` | Open a connection form on a connected idle Host. SSH additionally requires the desktop API capability. |
| Settings > Language | `language.change` | Switch to one of the nine supported menu languages listed below. Checkmark reflects the active language. |
| Help > Quick help | `help.quick` | Open a local, translated usage dialog. No external navigation. |

Closing a **tab**, disconnecting the **GUI from Host**, closing a **shared Session**, and
exiting the **GUI** are different operations. The first two release different client resources;
exiting ends the app after client cleanup. None closes the shared Session or stops its Host.
**Close shared Session is not implemented or exposed by the GUI.** The desktop still has only
one attachment, not multiple independent tabs.

Local clear removes xterm scrollback above the current line, retaining that line and parser
state. It does not send `clear`, `reset`, escape sequences, or any other bytes to the device.
It does not change the Host buffer or read cursor, dismiss loss notices, or reset lease state.
Subsequent output continues normally; a new attachment may replay retained Host output.

## Targeting, state, and security

The application menu, toolbar/row controls, and menu keyboard activation dispatch the same
finite action IDs to the renderer's existing handlers. Native Electron menus use the same
description and send only a validated renderer action through a fixed Preload subscription.
The Main-owned native exit action and fixed IPC `quit` method share the same cleanup handler,
so native quit remains available after a renderer crash or the last macOS window closes. A separate
bounded `syncMenu` message supplies display hints; it accepts no arbitrary menu template,
callback, accelerator, command, URL, or credential. A fixed `quit` method requests app cleanup.

A right-click captures that row's opaque Session ID or that terminal/tab's attachment ID and
Session ID. It never changes selection merely to open the menu. A non-current row cannot close
the current tab. Execution checks the latest local state again, and Main rechecks the requested
Session against the Host before replacing an attachment. A missing/rejected target leaves the
previous attachment in place. After reattachment, callbacks holding the previous attachment
handle cannot clear, focus, resume, or close the new view.

Offline state disables connection creation, refresh, disconnect, and attachment actions. A connected empty list
still allows refresh and disconnect. Closed rows cannot attach; an existing closed view can
still be cleared locally or detached. Pending connection/attachment changes disable conflicting
actions. Local terminal actions do not require a lease because they send no device bytes.
Menu hints never grant permission: Main validates IPC caller/arguments and attachment ownership,
and every device write still passes through authoritative Host/Application checks. Rejected or
uncertain writes are not automatically replayed.

Unimplemented operations are absent, not convincing enabled placeholders. Checkmarks describe
local view/language state only. Renderer reload/crash resets Main's menu hints and releases its
Host client resources.

## Translation and keyboard behavior

Lingui Core runs in Main and Renderer with separate instances of the same compiled PO catalogs. Renderer owns the
active menu locale and synchronizes it with native labels/checks; open context-menu labels and
help content also use that locale. The preference lasts for the current window only and defaults
to English. This is menu/help localization, not a claim that Host diagnostics or the entire
workbench have been translated. The language picker follows the README language order and
uses native names so it remains recognizable after switching:

| Locale | Picker label |
| --- | --- |
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

Menus register no Electron accelerators, mnemonic markers, default action roles, or global
keyboard listeners. On Windows/Linux, the native application menu model is installed but its
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
function keys, and IME composition remain in the terminal input path. OS-reserved shortcuts and
real IME candidate-window behavior require native platform checks.

## File actions

**Session > Send to device / Receive from device** (`file.send` / `file.receive`) are also
available on serial Session context menus. They bind the clicked opaque ID, require an open
serial Session, and revalidate Host lease state before local selection or client startup.
The dialog browses child-shell directory snapshots and uses native local file/folder selection;
the transfer panel renders Host events from all observed clients. Main rejects disconnect,
reconnect and app/window exit during a GUI-owned file operation until completion or cancellation.
See the [file workflow](../getting-started/file-transfer.md#electron-file-picker-and-progress).

## Planning boundaries (not implemented)

These are placement decisions only, not runtime menu entries or commitments to implement them.

| Proposed category | Unimplemented scope / prerequisite |
| --- | --- |
| Connection | Saved profiles and recent connections remain outside the explicit serial/SSH forms. Default-local Host startup is automatic; other endpoints remain explicit. |
| Session | Closing a shared Session needs a separately reviewed destructive action, explicit target/confirmation semantics, and Host authority. Multiple tabs and recent history are also absent. |
| Terminal | Split panes and remote PTY resize need their own supported lifecycle/capability work. |
| View | A separate MCP activity panel remains unimplemented. |
| Settings | Persistent preferences, endpoint editing, and Host process management are not provided by language switching. |
| Help | Online documentation navigation, updates, and diagnostic export require separate capability/security review. |

No built-in AI Agent, new SSH/file-transfer implementation, Core use case, or MCP protocol change
is implied by this menu organization.
