import type { MessageDescriptor } from "@lingui/core";

// Lingui 5 extracts these official annotated descriptors without a runtime macro
// transform. Translations belong in PO catalogs; this file owns IDs and English defaults.
export const menuMessages = [
  /* i18n */ { id: "session.close", message: "Close shared Session..." },
  /* i18n */ { id: "session.closeConfirm", message: "Close for everyone" },
  /* i18n */ {
    id: "session.closeWarning",
    message:
      "This releases the connection for every client and may interrupt transfers, debugging or remote processes. The Host may close an active leased Session. Closing a tab only detaches this view. Continue?",
  },
  /* i18n */ {
    id: "session.closeBusy",
    message:
      "Closure is blocked by an active operation. Finish it and refresh Sessions before trying again.",
  },
  /* i18n */ {
    id: "session.closeStale",
    message:
      "This view or Session has ended. Cancel and refresh Sessions; no replacement target was selected.",
  },
  /* i18n */ {
    id: "session.closeFailed",
    message:
      "Closure was not confirmed. Cancel and refresh Sessions to check the result before trying again. The request was not retried.",
  },
  /* i18n */ { id: "tab.move", message: "Move Tab to New Window" },
  /* i18n */ { id: "tab.list", message: "Terminal tabs" },
  /* i18n */ { id: "tab.empty", message: "New Tab" },
  /* i18n */ { id: "tab.moving", message: "Moving tab..." },
  /* i18n */ {
    id: "tab.moveFailed",
    message:
      "Could not move the tab. The original tab was retained; no device was reopened or input replayed.",
  },
  /* i18n */ {
    id: "tab.restoreHint",
    message:
      "Moved tab restored from recent Host output (up to 64 KiB). Full screen state and earlier history may be unavailable.",
  },
  /* i18n */ { id: "menu.host", message: "Host" },
  /* i18n */ { id: "menu.window", message: "Window" },
  /* i18n */ { id: "session.choose", message: "Attach Session..." },
  /* i18n */ { id: "session.files", message: "File Transfer" },
  /* i18n */ { id: "session.info", message: "Session Information..." },
  /* i18n */ {
    id: "session.emptyHint",
    message: "Use Session > New Connection to open a Session.",
  },
  /* i18n */ { id: "connection.protocol", message: "Protocol" },
  /* i18n */ { id: "window.new", message: "New Window" },
  /* i18n */ { id: "tab.new", message: "New Tab" },
  /* i18n */ { id: "window.tab.close", message: "Close Tab" },
  /* i18n */ { id: "window.close", message: "Close Window" },
  /* i18n */ { id: "tab.next", message: "Next Tab" },
  /* i18n */ { id: "tab.previous", message: "Previous Tab" },
  /* i18n */ { id: "window.next", message: "Next Window" },
  /* i18n */ { id: "window.previous", message: "Previous Window" },
  /* i18n */ { id: "language.system", message: "Follow System" },
  /* i18n */ { id: "settings.preferences", message: "Preferences..." },
  /* i18n */ { id: "file.resume", message: "Resume transfer" },
  /* i18n */ {
    id: "file.resumable",
    message: "Keep a checkpoint for manual resume",
  },
  /* i18n */ {
    id: "file.resumeHint",
    message:
      "Snapshots use additional disk space. Resume on the same Host and serial endpoint after reconnecting manually. Source and confirmed bytes are checked; writes are never replayed automatically.",
  },
  /* i18n */ {
    id: "file.noCheckpoints",
    message: "No saved transfers for this endpoint.",
  },
  /* i18n */ {
    id: "file.publicationUncertain",
    message:
      "Publication is uncertain. Inspect the destination manually; this transfer will not be replayed.",
  },
  /* i18n */ { id: "connection.custom", message: "Custom…" },
  /* i18n */ { id: "connection.customBaud", message: "Custom baud rate" },
  /* i18n */ { id: "terminal.settings", message: "Terminal settings" },
  /* i18n */ { id: "terminal.encoding", message: "Character encoding" },
  /* i18n */ { id: "terminal.view", message: "View mode" },
  /* i18n */ { id: "terminal.text", message: "Text" },
  /* i18n */ { id: "terminal.hex", message: "Hex (read-only)" },
  /* i18n */ {
    id: "terminal.description",
    message: "Local description (optional)",
  },
  /* i18n */ { id: "terminal.apply", message: "Apply" },
  /* i18n */ {
    id: "terminal.localHint",
    message:
      "Choose the device's text encoding. Settings apply only to this client; file transfers keep their original bytes. Description: one line, up to 512 UTF-8 bytes.",
  },
  /* i18n */ { id: "file.send", message: "Send to device" },
  /* i18n */ { id: "file.receive", message: "Receive from device" },
  /* i18n */ { id: "file.parent", message: "Up" },
  /* i18n */ { id: "file.open", message: "Open" },
  /* i18n */ { id: "file.remotePath", message: "Device path" },
  /* i18n */ { id: "file.terminalDirectory", message: "Terminal directory" },
  /* i18n */ { id: "file.file", message: "File" },
  /* i18n */ { id: "file.directory", message: "Folder" },
  /* i18n */ { id: "file.other", message: "Unsupported" },
  /* i18n */ { id: "file.loading", message: "Reading directory\u2026" },
  /* i18n */ { id: "file.truncated", message: "First 256 entries shown." },
  /* i18n */ { id: "file.chooseFile", message: "Choose file" },
  /* i18n */ { id: "file.chooseFolder", message: "Choose folder" },
  /* i18n */ { id: "file.saveFolder", message: "Choose save folder" },
  /* i18n */ { id: "file.receiveFolder", message: "Receive this folder" },
  /* i18n */ { id: "file.transfers", message: "File transfers" },
  /* i18n */ { id: "file.running", message: "Operation running" },
  /* i18n */ { id: "file.transferring", message: "Transferring" },
  /* i18n */ { id: "file.verifying", message: "Verifying" },
  /* i18n */ { id: "file.cleaning", message: "Finishing" },
  /* i18n */ { id: "file.completed", message: "Completed" },
  /* i18n */ { id: "file.cancelled", message: "Cancelled" },
  /* i18n */ { id: "file.failed", message: "Failed" },
  /* i18n */ { id: "file.unknown", message: "Status unknown" },
  /* i18n */ {
    id: "file.cancelQuestion",
    message: "Cancel this file transfer?",
  },
  /* i18n */ { id: "file.continue", message: "Continue" },
  /* i18n */ { id: "file.cancel", message: "Cancel transfer" },
  /* i18n */ {
    id: "file.closeHint",
    message:
      "Finish or cancel the file operation in the transfer panel before closing.",
  },
  /* i18n */ {
    id: "file.idleHint",
    message:
      "Requires an idle Linux shell. Double-click a folder to browse; navigation does not change the terminal directory.",
  },
  /* i18n */ { id: "view.resizeSidebar", message: "Resize Session sidebar" },
  /* i18n */ {
    id: "view.resizeSidebarHelp",
    message: "Drag or use arrow keys to resize. Double-click to reset.",
  },
  /* i18n */ { id: "connection.new", message: "New Connection..." },
  /* i18n */ { id: "connection.serial", message: "Serial connection" },
  /* i18n */ { id: "connection.ssh", message: "SSH connection" },
  /* i18n */ { id: "connection.port", message: "Port" },
  /* i18n */ { id: "connection.baud", message: "Baud rate" },
  /* i18n */ { id: "connection.dataBits", message: "Data bits" },
  /* i18n */ { id: "connection.parity", message: "Parity" },
  /* i18n */ { id: "connection.stopBits", message: "Stop bits" },
  /* i18n */ { id: "connection.flowControl", message: "Flow control" },
  /* i18n */ { id: "connection.label", message: "Label (optional)" },
  /* i18n */ { id: "connection.scan", message: "Scan ports" },
  /* i18n */ { id: "connection.scanning", message: "Scanning…" },
  /* i18n */ {
    id: "connection.invalid",
    message: "Check the connection settings.",
  },
  /* i18n */ { id: "connection.host", message: "Host name or IP" },
  /* i18n */ { id: "connection.user", message: "User name" },
  /* i18n */ { id: "connection.auth", message: "Authentication" },
  /* i18n */ { id: "connection.key", message: "Private key" },
  /* i18n */ { id: "connection.password", message: "Password" },
  /* i18n */ {
    id: "connection.identities",
    message: "Key files on Host (one per line; blank uses defaults)",
  },
  /* i18n */ {
    id: "connection.passphrase",
    message: "Key passphrase (optional)",
  },
  /* i18n */ {
    id: "connection.knownHosts",
    message: "Host known_hosts file (blank uses default)",
  },
  /* i18n */ {
    id: "connection.trust",
    message:
      "Only trusted host keys are accepted. Verify and install the server key in the Host known_hosts file first. Credentials are not saved.",
  },
  /* i18n */ { id: "connection.cancel", message: "Cancel" },
  /* i18n */ { id: "connection.opening", message: "Connecting…" },
  /* i18n */ { id: "connection.open", message: "Connect" },
  /* i18n */ {
    id: "menu.connection",
    message: "Connection",
  },
  /* i18n */ {
    id: "menu.session",
    message: "Session",
  },
  /* i18n */ {
    id: "menu.terminal",
    message: "Terminal",
  },
  /* i18n */ {
    id: "menu.view",
    message: "View",
  },
  /* i18n */ {
    id: "menu.settings",
    message: "Settings",
  },
  /* i18n */ {
    id: "menu.help",
    message: "Help",
  },
  /* i18n */ {
    id: "menu.language",
    message: "Language",
  },
  /* i18n */ {
    id: "host.connect",
    message: "Connect to Host...",
  },
  /* i18n */ {
    id: "host.disconnect",
    message: "Disconnect from Host",
  },
  /* i18n */ {
    id: "session.refresh",
    message: "Refresh Sessions",
  },
  /* i18n */ {
    id: "session.attach",
    message: "Attach Session",
  },
  /* i18n */ {
    id: "tab.close",
    message: "Close tab (detach)",
  },
  /* i18n */ {
    id: "terminal.clear",
    message: "Clear local display",
  },
  /* i18n */ {
    id: "terminal.focus",
    message: "Focus terminal",
  },
  /* i18n */ {
    id: "terminal.resume",
    message: "Resume input",
  },
  /* i18n */ {
    id: "view.sidebar",
    message: "Session sidebar",
  },
  /* i18n */ {
    id: "language.en",
    message: "English",
  },
  /* i18n */ { id: "language.de", message: "Deutsch" },
  /* i18n */ { id: "language.es", message: "Español" },
  /* i18n */ { id: "language.fr", message: "Français" },
  /* i18n */ { id: "language.ko", message: "한국어" },
  /* i18n */ { id: "language.ru", message: "Русский" },
  /* i18n */ { id: "language.zh-TW", message: "繁體中文" },
  /* i18n */ {
    id: "language.zh-CN",
    message: "简体中文",
  },
  /* i18n */ {
    id: "language.ja",
    message: "日本語",
  },
  /* i18n */ {
    id: "help.quick",
    message: "Quick help",
  },
  /* i18n */ {
    id: "app.quit",
    message: "Exit GUI",
  },
  /* i18n */ {
    id: "help.title",
    message: "Using ChannelTerm",
  },
  /* i18n */ {
    id: "help.connect",
    message:
      "The local Host connects or starts automatically. Use Connection > New connection for serial or SSH. Custom Host endpoints connect manually.",
  },
  /* i18n */ {
    id: "help.detach",
    message:
      "Closing a tab detaches only this view. Disconnecting the GUI or exiting it leaves the shared Host and Sessions running.",
  },
  /* i18n */ {
    id: "help.clear",
    message:
      "Clear local display removes local scrollback above the current line. It sends no clear/reset command and leaves Host history intact.",
  },
  /* i18n */ {
    id: "help.close",
    message: "Close help",
  },
  /* i18n */ { id: "file.wire", message: "Transfer encoding" },
  /* i18n */ {
    id: "file.wireHint",
    message:
      "Base64 requires base64 and tr on the device. Raw is for compatible 8-bit links without XON/XOFF. File contents are unchanged.",
  },
  /* i18n */ { id: "view.auxiliary", message: "Secondary sidebar" },
  /* i18n */ { id: "view.panel", message: "Bottom panel" },
  /* i18n */ { id: "view.status", message: "Session status" },
  /* i18n */ { id: "view.layout", message: "Layout controls" },
  /* i18n */ {
    id: "view.resizeAuxiliary",
    message: "Resize secondary sidebar",
  },
  /* i18n */ { id: "panel.close", message: "Hide panel" },
  /* i18n */ { id: "panel.resize", message: "Resize bottom panel" },
  /* i18n */ {
    id: "file.empty",
    message:
      "No file transfers yet. Use the Session menu to send, receive or resume a transfer.",
  },
  /* i18n */ { id: "details.title", message: "Session details" },
  /* i18n */ { id: "details.session", message: "Session" },
  /* i18n */ { id: "details.endpoint", message: "Endpoint" },
  /* i18n */ { id: "details.transport", message: "Transport" },
  /* i18n */ { id: "details.state", message: "State" },
  /* i18n */ { id: "details.id", message: "Session ID" },
  /* i18n */ { id: "details.lease", message: "Active lease" },
  /* i18n */ {
    id: "details.empty",
    message: "Attach a Session to inspect its details.",
  },
  /* i18n */ { id: "settings.shortcuts", message: "Keyboard shortcuts" },
  /* i18n */ {
    id: "shortcuts.hint",
    message:
      "Focus a field and press Ctrl or Command with a letter or number; Alt and Shift are optional. Uses physical key positions. Applied bindings are saved locally and work in the terminal. Dialogs, menus, text fields and IME keep their own keys.",
  },
  /* i18n */ { id: "shortcuts.unassigned", message: "Unassigned" },
  /* i18n */ { id: "shortcuts.remove", message: "Remove" },
  /* i18n */ { id: "shortcuts.reset", message: "Restore defaults" },
  /* i18n */ {
    id: "shortcuts.invalid",
    message:
      "Use Ctrl or Command with a letter or number. Common editing and window shortcuts are reserved.",
  },
  /* i18n */ {
    id: "shortcuts.conflict",
    message:
      "Each shortcut must be unique. Change or remove the duplicate binding.",
  },
  /* i18n */ {
    id: "shortcuts.storageError",
    message:
      "Could not save shortcuts. Check local storage access and try again.",
  },
  /* i18n */ { id: "file.protocol", message: "File protocol" },
  /* i18n */ {
    id: "file.localIdentities",
    message: "Local key files (one per line; blank uses defaults)",
  },
  /* i18n */ {
    id: "file.localKnownHosts",
    message: "Local known_hosts file (blank uses default)",
  },
  /* i18n */ {
    id: "file.localTrust",
    message:
      "Only trusted host keys are accepted. Verify the server key and establish trust with the CLI on this computer before transferring.",
  },
  /* i18n */ {
    id: "file.recursive",
    message: "Download a directory recursively",
  },
  /* i18n */ {
    id: "file.sftpResume",
    message: "Resume an existing partial file (SFTP)",
  },
  /* i18n */ {
    id: "file.sftpResumeHint",
    message:
      "Single files only. Select the same source and destination; matching existing bytes are verified before continuing. SCP cannot resume.",
  },
  /* i18n */ {
    id: "file.sshHint",
    message:
      "Uses a separate SSH connection from this computer. Enter local identity and known-hosts paths. Credentials are not saved. Copies use SHA-256 verification; SCP requires remote sha256sum. Cancellation may leave partial files.",
  },
  /* i18n */ { id: "terminal.copy", message: "Copy terminal selection" },
  /* i18n */ { id: "terminal.paste", message: "Paste into terminal" },
  /* i18n */ { id: "shortcuts.search", message: "Search shortcuts" },
  /* i18n */ { id: "shortcuts.empty", message: "No matching shortcuts" },
  /* i18n */ { id: "settings.display", message: "Display" },
  /* i18n */ { id: "display.fontSize", message: "Font size" },
  /* i18n */ { id: "display.fontFamily", message: "Font family" },
  /* i18n */ {
    id: "display.hint",
    message:
      "Terminal font size: 8-40 px. Ctrl+wheel zooms the terminal. Font families use installed fonts with monospace fallback. Settings are saved locally.",
  },
  /* i18n */ {
    id: "display.storageError",
    message:
      "Could not save display settings. Check local storage access and try again.",
  },
] satisfies MessageDescriptor[];
