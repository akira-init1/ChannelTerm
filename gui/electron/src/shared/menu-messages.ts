import type { MessageDescriptor } from "@lingui/core";

// Lingui 5 extracts these official annotated descriptors without a runtime macro
// transform. Translations belong in PO catalogs; this file owns IDs and English defaults.
export const menuMessages = [
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
