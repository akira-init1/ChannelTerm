import type { MessageDescriptor } from "@lingui/core";

// Lingui 5 extracts these official annotated descriptors without a runtime macro
// transform. Translations belong in PO catalogs; this file owns IDs and English defaults.
export const menuMessages = [
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
  /* i18n */ { id: "connection.new", message: "New connection" },
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
    message: "Connect to Host",
  },
  /* i18n */ {
    id: "host.disconnect",
    message: "Disconnect GUI from Host",
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
] satisfies MessageDescriptor[];
