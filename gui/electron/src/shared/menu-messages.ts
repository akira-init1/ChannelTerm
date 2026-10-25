import type { MessageDescriptor } from "@lingui/core";

// Lingui 5 extracts these official annotated descriptors without a runtime macro
// transform. Translations belong in PO catalogs; this file owns IDs and English defaults.
export const menuMessages = [
  /* i18n */ { id: "diagnostics.close", message: "Close" },
  /* i18n */ { id: "diagnostics.title", message: "Diagnostic ZIP…" },
  /* i18n */ {
    id: "diagnostics.hint",
    message:
      "Export a retained snapshot, not a continuous or complete log. Nothing is uploaded.",
  },
  /* i18n */ { id: "diagnostics.source", message: "Archive source" },
  /* i18n */ {
    id: "diagnostics.snapshot",
    message: "Current Session snapshot",
  },
  /* i18n */ { id: "diagnostics.existing", message: "Existing diagnostic ID" },
  /* i18n */ { id: "diagnostics.id", message: "Diagnostic ID" },
  /* i18n */ {
    id: "diagnostics.raw",
    message: "Include raw terminal output and write contents",
  },
  /* i18n */ {
    id: "diagnostics.rawHint",
    message:
      "Off by default. Raw contents may include passwords or other sensitive data.",
  },
  /* i18n */ {
    id: "diagnostics.existingHint",
    message:
      "The existing archive may already contain raw contents. IDs expire after 15 minutes, eviction or Host exit.",
  },
  /* i18n */ {
    id: "diagnostics.destination",
    message:
      "Choose a new filename. Existing files are never replaced. Downloads are limited to 16 MiB and verified with SHA-256.",
  },
  /* i18n */ {
    id: "diagnostics.invalid",
    message: "Enter a valid DIAG- identifier.",
  },
  /* i18n */ { id: "diagnostics.cancelled", message: "Save cancelled." },
  /* i18n */ { id: "diagnostics.saved", message: "ZIP saved and verified." },
  /* i18n */ { id: "diagnostics.cancel", message: "Cancel save" },
  /* i18n */ { id: "diagnostics.saving", message: "Saving and verifying…" },
  /* i18n */ { id: "diagnostics.save", message: "Save ZIP…" },
  /* i18n */ { id: "activity.title", message: "Session activity" },
  /* i18n */ {
    id: "activity.select",
    message: "Attach a Session to inspect its activity.",
  },
  /* i18n */ {
    id: "activity.hint",
    message:
      "Retained observations only, not a complete audit. Actor labels are recorded sources, not verified client identities. Write contents are hidden. Debug reports are client claims.",
  },
  /* i18n */ {
    id: "activity.dropped",
    message: "Host retention was overwritten; some observations are missing.",
  },
  /* i18n */ {
    id: "activity.truncated",
    message: "Older rows were omitted from this view (200-row limit).",
  },
  /* i18n */ { id: "activity.empty", message: "No retained activity yet." },
  /* i18n */ { id: "activity.time", message: "Host time (UTC)" },
  /* i18n */ { id: "activity.actor", message: "Source" },
  /* i18n */ { id: "activity.event", message: "Event" },
  /* i18n */ { id: "activity.details", message: "Details" },
  /* i18n */ { id: "activity.origin", message: "Observation" },
  /* i18n */ { id: "activity.user", message: "User" },
  /* i18n */ { id: "activity.agent", message: "Agent" },
  /* i18n */ { id: "activity.system", message: "System" },
  /* i18n */ { id: "activity.other", message: "Other / unknown" },
  /* i18n */ { id: "activity.write", message: "Input written" },
  /* i18n */ {
    id: "activity.clientReport",
    message: "Client debug report (not a complete audit)",
  },
  /* i18n */ { id: "activity.history", message: "Initial history" },
  /* i18n */ { id: "activity.observed", message: "Observed later" },

  /* i18n */ { id: "ssh.trustTitle", message: "Trust this SSH server?" },
  /* i18n */ {
    id: "ssh.trustHint",
    message:
      "Verify this fingerprint independently. Approval saves this key in the Host trust file for this connection. Local file transfers use their own trust file. Changed or revoked keys are never accepted. This request expires after two minutes.",
  },
  /* i18n */ { id: "ssh.trustAddress", message: "Server address" },
  /* i18n */ { id: "ssh.trustAlgorithm", message: "Key algorithm" },
  /* i18n */ { id: "ssh.trustFingerprint", message: "SHA-256 fingerprint" },
  /* i18n */ { id: "ssh.trustReject", message: "Reject connection" },
  /* i18n */ { id: "ssh.trustAccept", message: "Trust and connect" },

  /* i18n */ { id: "profiles.title", message: "Serial profiles" },
  /* i18n */ {
    id: "profiles.hint",
    message:
      "Profiles use the Host's CLI configuration. Loading and saving never opens a port. The first saved profile becomes the default.",
  },
  /* i18n */ { id: "profiles.load", message: "Refresh profiles" },
  /* i18n */ { id: "profiles.select", message: "Apply profile" },
  /* i18n */ { id: "profiles.default", message: "default" },
  /* i18n */ { id: "profiles.setDefault", message: "Set as default" },
  /* i18n */ { id: "profiles.name", message: "Profile name" },
  /* i18n */ { id: "profiles.replace", message: "Allow replacing this name" },
  /* i18n */ { id: "profiles.save", message: "Save current settings" },
  /* i18n */ { id: "profiles.saved", message: "Configuration saved." },
  /* i18n */ {
    id: "profiles.wake",
    message: "Send wake carriage return when opening",
  },
  /* i18n */ {
    id: "profiles.wakeHint",
    message:
      "Wake is always off until you select it here, even for profiles with wake enabled. Saving stores this choice.",
  },

  /* i18n */ {
    id: "display.promptTimestamps",
    message: "Local prompt timestamps",
  },
  /* i18n */ {
    id: "display.promptTimestampHint",
    message:
      "Add local receipt time to recognized live shell prompts. Retained history and ordinary log lines are not timestamped; this is not device capture time.",
  },
  /* i18n */ { id: "ssh.advanced", message: "Advanced SSH settings" },
  /* i18n */ {
    id: "ssh.advancedHint",
    message:
      "Applies to new connections only. Zero disables keepalive or write timeout. Write timeout closes the connection; input is never replayed. PTY size is initial only.",
  },
  /* i18n */ {
    id: "ssh.timeoutSeconds",
    message: "Connection timeout (seconds)",
  },
  /* i18n */ {
    id: "ssh.keepAliveSeconds",
    message: "Keepalive interval (seconds)",
  },
  /* i18n */ {
    id: "ssh.keepAliveCount",
    message: "Keepalive response limit (intervals)",
  },
  /* i18n */ {
    id: "ssh.writeTimeoutSeconds",
    message: "Write timeout (seconds)",
  },
  /* i18n */ { id: "ssh.columns", message: "Initial PTY columns" },
  /* i18n */ { id: "ssh.rows", message: "Initial PTY rows" },
  /* i18n */ { id: "ssh.term", message: "PTY terminal type" },
  /* i18n */ { id: "paste.title", message: "Review multiline paste" },
  /* i18n */ {
    id: "paste.warning",
    message:
      "This text contains line breaks and may execute commands on the target. Review it before pasting. Escape bytes are shown as ␛; confirmed text retains xterm's normal paste handling.",
  },
  /* i18n */ { id: "paste.confirm", message: "Paste into this target" },
  /* i18n */ {
    id: "paste.stale",
    message:
      "The target or input state changed. Cancel this draft and paste again after checking the target. Nothing was sent.",
  },
  /* i18n */ { id: "target.copy", message: "Copy for AI" },
  /* i18n */ {
    id: "target.hint",
    message:
      "Copy this target's full ID and connection details, without terminal output or credentials.",
  },
  /* i18n */ { id: "target.copied", message: "Target instructions copied." },
  /* i18n */ {
    id: "target.failed",
    message:
      "Could not copy the target. Refresh Sessions and select an open target, then try again.",
  },
  /* i18n */ { id: "target.empty", message: "No Session" },
  /* i18n */ {
    id: "target.instructions",
    message:
      "Use the connected ChannelTerm MCP to operate on this target:\nName: {name}\nHost: {host}\nsession_id: {sessionId}\nShort reference: {reference}\nConnection: {transport} / {endpoint}\n\nFirst query Sessions and confirm that the full ID, endpoint, and state match.\nIf they do not match, stop. Do not automatically select another device.\nI will specify the operation separately.",
  },
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
      "Updated Hosts ask before trusting an unknown key. Changed or revoked keys are rejected. Credentials are not saved.",
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
  /* i18n */ { id: "terminal.find", message: "Find in terminal" },
  /* i18n */ { id: "search.query", message: "Find retained text" },
  /* i18n */ { id: "search.case", message: "Match case" },
  /* i18n */ { id: "search.previous", message: "Previous match (Shift+Enter)" },
  /* i18n */ { id: "search.next", message: "Next match (Enter)" },
  /* i18n */ { id: "search.selectAll", message: "Select all retained text" },
  /* i18n */ { id: "search.close", message: "Close search (Escape)" },
  /* i18n */ {
    id: "search.scope",
    message: "Current terminal's retained text only.",
  },
  /* i18n */ {
    id: "search.hexScope",
    message: "Search the displayed HEX text, not binary bytes.",
  },
  /* i18n */ { id: "search.none", message: "No matches" },
  /* i18n */ { id: "search.limit", message: "Result limit reached" },
  /* i18n */ {
    id: "search.conflict",
    message:
      "Ctrl+Shift+F is already assigned. Find has no shortcut; use the Terminal menu or assign another key.",
  },
  /* i18n */ { id: "shortcuts.search", message: "Search shortcuts" },
  /* i18n */ { id: "shortcuts.empty", message: "No matching shortcuts" },
  /* i18n */ { id: "settings.display", message: "Display" },
  /* i18n */ { id: "display.fontSize", message: "Font size" },
  /* i18n */ { id: "display.fontFamily", message: "Font family" },
  /* i18n */ { id: "display.terminalStyle", message: "Terminal style" },
  /* i18n */ { id: "display.semantic", message: "Default (CLI colors)" },
  /* i18n */ { id: "display.monochrome", message: "Plain (white on black)" },
  /* i18n */ { id: "display.preview", message: "Terminal style preview" },
  /* i18n */ {
    id: "display.styleHint",
    message:
      "Local display only. Plain mode suppresses semantic and remote colors while preserving terminal controls and selection. Switching styles keeps the current terminal content.",
  },
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
