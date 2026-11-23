import type { MessageDescriptor } from "@lingui/core";

// Lingui 5 extracts these official annotated descriptors without a runtime macro
// transform. Translations belong in PO catalogs; this file owns IDs and English defaults.
export const menuMessages = [
  /* i18n */ { id: "directory.newSession", message: "New Session" },
  /* i18n */ { id: "directory.newFolder", message: "New Folder" },
  /* i18n */ { id: "directory.folderName", message: "Folder name" },
  /* i18n */ {
    id: "directory.folderFailed",
    message:
      "Could not create the folder. Use a unique name of 1–128 characters and keep at most 256 created folders.",
  },
  /* i18n */ { id: "directory.actions", message: "Connection actions" },
  /* i18n */ { id: "directory.allProtocols", message: "All protocols" },
  /* i18n */ { id: "directory.copy", message: "Duplicate connection…" },
  /* i18n */ {
    id: "directory.editFailed",
    message:
      "Could not save this change. Check the name, available history space, and whether the connection still exists.",
  },
  /* i18n */ { id: "directory.favorite", message: "Add to favorites" },
  /* i18n */ { id: "directory.focus", message: "Go to terminal" },
  /* i18n */ {
    id: "directory.folderHint",
    message:
      "Choose or enter a folder name. Leave empty to move out of a folder.",
  },
  /* i18n */ { id: "directory.moveFolder", message: "Move to folder…" },
  /* i18n */ { id: "directory.rename", message: "Rename connection…" },
  /* i18n */ { id: "tab.rename", message: "Rename Tab…" },
  /* i18n */ { id: "tab.resetName", message: "Restore Default Name" },
  /* i18n */ { id: "connection.newShell", message: "New SSH terminal" },
  /* i18n */ {
    id: "connection.share",
    message: "View an existing Session in a new tab",
  },
  /* i18n */ { id: "connection.settings", message: "Connection settings" },
  /* i18n */ { id: "connection.savePreset", message: "Save connection" },
  /* i18n */ { id: "directory.filter", message: "Connection status" },
  /* i18n */ { id: "directory.all", message: "All" },
  /* i18n */ { id: "directory.online", message: "Connected" },
  /* i18n */ { id: "directory.offline", message: "Disconnected" },
  /* i18n */ {
    id: "directory.reorder",
    message: "Drag to reorder, or use Alt+↑ / Alt+↓.",
  },
  /* i18n */ { id: "directory.empty", message: "No connections used yet." },
  /* i18n */ {
    id: "directory.noHosts",
    message: "No trusted Hosts configured.",
  },
  /* i18n */ { id: "directory.lan", message: "LAN" },
  /* i18n */ { id: "control.protect", message: "Block AI access" },
  /* i18n */ { id: "control.manual", message: "🔒 Manual only" },
  /* i18n */ { id: "control.ask", message: "Ask before taking over" },
  /* i18n */ { id: "control.waitLimit", message: "AI wait limit (seconds)" },
  /* i18n */ { id: "control.operating", message: "AI is operating" },
  /* i18n */ { id: "control.continue", message: "Continue AI task" },
  /* i18n */ { id: "control.mine", message: "Take over" },
  /* i18n */ { id: "uboot.open", message: "Enter U-Boot…" },
  /* i18n */ { id: "uboot.mode", message: "Reset mode" },
  /* i18n */ {
    id: "uboot.reboot",
    message: "Send one explicit reboot command",
  },
  /* i18n */ { id: "uboot.waitReset", message: "Wait for manual reset" },
  /* i18n */ { id: "uboot.command", message: "Verified reboot command" },
  /* i18n */ { id: "uboot.countdown", message: "Literal countdown marker" },
  /* i18n */ {
    id: "uboot.prompt",
    message: "Exact prompt line, including spaces",
  },
  /* i18n */ {
    id: "uboot.bootStarted",
    message: "Boot-start markers (one per line)",
  },
  /* i18n */ {
    id: "uboot.interrupt",
    message: "Exact interrupt bytes (HEX pairs, no spaces)",
  },
  /* i18n */ { id: "uboot.timeout", message: "Timeout (milliseconds)" },
  /* i18n */ {
    id: "uboot.capture",
    message:
      "Retain raw terminal evidence in a diagnostic archive for up to 15 minutes. It may contain sensitive data.",
  },
  /* i18n */ {
    id: "uboot.noRetry",
    message:
      "One attempt only. Cancellation sends no recovery keys and cannot undo a submitted reboot. Never repeat a reset automatically.",
  },
  /* i18n */ {
    id: "uboot.confirm",
    message: "Confirm this board-specific U-Boot attempt",
  },
  /* i18n */ { id: "uboot.start", message: "Review and start" },
  /* i18n */ {
    id: "uboot.idle",
    message:
      "I verified an idle interactive Bash prompt and authorize this reboot command.",
  },
  /* i18n */ {
    id: "uboot.help",
    message:
      "Use settings verified for this board and firmware. No interrupt key or reboot command is guessed. Do not use this at an already-idle U-Boot prompt.",
  },
  /* i18n */ {
    id: "uboot.validation",
    message:
      "Markers: 1–256 UTF-8 bytes; 1–8 boot-start markers. Interrupt: 1–32 bytes. Timeout: 1–180000 ms. All patterns are literal; spaces are preserved.",
  },
  /* i18n */ {
    id: "uboot.unknown",
    message:
      "Outcome unconfirmed. Inspect the original Session and activity before another action.",
  },
  /* i18n */ {
    id: "uboot.waiting",
    message: "Arming the Host observer. Do not reset yet.",
  },
  /* i18n */ {
    id: "uboot.ready",
    message:
      "Host observer ready for this attempt. You may perform the authorized manual reset.",
  },
  /* i18n */ {
    id: "uboot.rebootReady",
    message:
      "Host observer armed; waiting for the explicitly requested reboot. Do not reset again.",
  },
  /* i18n */ {
    id: "uboot.stopping",
    message: "Stopping this request. A submitted reboot cannot be undone.",
  },
  /* i18n */ {
    id: "uboot.evidence",
    message: "Host evidence: bytes written · elapsed time · output cursors",
  },
  /* i18n */ {
    id: "uboot.archiveFailed",
    message: "The diagnostic archive could not be created.",
  },
  /* i18n */ { id: "uboot.saveEvidence", message: "Save diagnostic archive…" },
  /* i18n */ { id: "uboot.stop", message: "Stop attempt" },
  /* i18n */ {
    id: "uboot.result.entered",
    message: "Fresh U-Boot prompt confirmed.",
  },
  /* i18n */ {
    id: "uboot.result.boot_started",
    message: "Boot proceeded; U-Boot entry was not confirmed.",
  },
  /* i18n */ {
    id: "uboot.result.timeout",
    message: "Timed out without confirming entry.",
  },
  /* i18n */ {
    id: "uboot.result.cancelled",
    message: "Host confirmed cancellation.",
  },
  /* i18n */ {
    id: "uboot.result.session_closed",
    message: "The Session closed during the attempt.",
  },
  /* i18n */ {
    id: "uboot.result.output_lost",
    message: "Output evidence was lost; automatic interruption stopped.",
  },
  /* i18n */ {
    id: "uboot.result.write_failed",
    message: "Input delivery failed or is uncertain.",
  },
  /* i18n */ {
    id: "uboot.result.input_pending",
    message: "The Host refused reboot because terminal input is pending.",
  },
  /* i18n */ {
    id: "uboot.result.unsupported_shell",
    message: "The reboot command requires an idle Bash shell.",
  },
  /* i18n */ {
    id: "uboot.result.cleanup_failed",
    message: "Entry evidence was observed, but ownership cleanup failed.",
  },
  /* i18n */ {
    id: "uboot.result.failed",
    message: "The Host could not confirm entry.",
  },

  /* i18n */ { id: "terminal.auxiliary", message: "Auxiliary display" },
  /* i18n */ { id: "terminal.timestamps", message: "Show system timestamps" },
  /* i18n */ { id: "terminal.sequence", message: "Show sequence numbers" },
  /* i18n */ { id: "terminal.foldMarkers", message: "Show fold markers" },
  /* i18n */ { id: "terminal.fold", message: "Expand all command outputs" },
  /* i18n */ { id: "terminal.expandOutput", message: "Expand command output" },
  /* i18n */ { id: "terminal.collapseOutput", message: "Fold command output" },
  /* i18n */ {
    id: "line.hint",
    message:
      "Local first-observation batch time and logical-line sequence. Not device event time or complete history. Wrapped rows share metadata; copy includes terminal text only.",
  },
  /* i18n */ { id: "line.received", message: "System receipt time:" },
  /* i18n */ { id: "line.unknown", message: "Unknown historical time" },
  /* i18n */ {
    id: "line.unavailable",
    message:
      "Line metadata is paused in HEX and alternate-screen views. Your display choices are retained.",
  },

  /* i18n */ { id: "highlight.remove", message: "Remove rule" },
  /* i18n */ { id: "highlight.title", message: "Keyword highlights…" },
  /* i18n */ {
    id: "highlight.hint",
    message:
      "Local literal rules for this saved connection. Earlier rules win overlaps. Up to 32 rules, 128 UTF-16 units each. Changes affect future safe text; ANSI, full-screen output, prompts and HEX remain unchanged.",
  },
  /* i18n */ { id: "highlight.enabled", message: "Enabled" },
  /* i18n */ { id: "highlight.text", message: "Literal text" },
  /* i18n */ { id: "highlight.color", message: "Color" },
  /* i18n */ { id: "highlight.up", message: "Move rule up" },
  /* i18n */ { id: "highlight.down", message: "Move rule down" },
  /* i18n */ { id: "highlight.add", message: "Add rule" },
  /* i18n */ { id: "highlight.clear", message: "Clear this list" },
  /* i18n */ { id: "highlight.templates", message: "Reusable templates" },
  /* i18n */ { id: "highlight.template", message: "Template" },
  /* i18n */ { id: "highlight.templateName", message: "Template name" },
  /* i18n */ {
    id: "highlight.templateHint",
    message:
      "Import appends independent copies. Template saves take effect immediately; they never update imported connections. Save connection rules separately.",
  },
  /* i18n */ { id: "highlight.import", message: "Import copies" },
  /* i18n */ { id: "highlight.saveTemplate", message: "Save as new template" },
  /* i18n */ {
    id: "highlight.replaceTemplate",
    message: "Replace selected template",
  },
  /* i18n */ {
    id: "highlight.deleteTemplate",
    message: "Delete selected template",
  },
  /* i18n */ { id: "highlight.save", message: "Save connection rules" },
  /* i18n */ {
    id: "highlight.invalid",
    message:
      "Check rule text, colors and template name. Limits: 32 rules per list, 10 templates, 64 UTF-16 units per template name.",
  },
  /* i18n */ {
    id: "highlight.saveError",
    message:
      "Cannot save or load these rules. Storage may be unavailable, full, or changed in another window. The saved connection was not recreated. Keep this dialog open to retry or cancel.",
  },
  /* i18n */ { id: "send.open", message: "Debug send and snippets" },
  /* i18n */ {
    id: "send.help",
    message:
      "Text uses this attachment’s encoding. HEX accepts complete byte pairs and ASCII whitespace. The final payload, including the line ending, is limited to 4096 bytes.",
  },
  /* i18n */ { id: "send.mode", message: "Input format" },
  /* i18n */ { id: "send.text", message: "Text" },
  /* i18n */ { id: "send.data", message: "Content" },
  /* i18n */ { id: "send.ending", message: "Append line ending" },
  /* i18n */ { id: "send.none", message: "None" },
  /* i18n */ { id: "send.preview", message: "Preview and send" },
  /* i18n */ {
    id: "send.confirm",
    message: "Confirm exact bytes sent to this Session",
  },
  /* i18n */ { id: "send.send", message: "Send these bytes" },
  /* i18n */ {
    id: "send.noRetry",
    message:
      "No automatic retry. After failure or cancellation during delivery, inspect the device before sending again.",
  },
  /* i18n */ { id: "send.snippets", message: "Saved snippets" },
  /* i18n */ { id: "send.choose", message: "Load a snippet without sending" },
  /* i18n */ { id: "send.name", message: "Snippet name" },
  /* i18n */ {
    id: "send.plaintext",
    message:
      "I approve saving this content unencrypted on this computer, replacing the same name. Do not save passwords or secrets.",
  },
  /* i18n */ { id: "send.save", message: "Save or replace snippet" },
  /* i18n */ { id: "send.remove", message: "Delete named snippet" },
  /* i18n */ { id: "send.sent", message: "Host accepted bytes:" },
  /* i18n */ { id: "send.cancelled", message: "Cancelled before sending." },
  /* i18n */ { id: "send.saved", message: "Snippet saved. Nothing was sent." },
  /* i18n */ { id: "send.removed", message: "Snippet deleted." },
  /* i18n */ {
    id: "send.loaded",
    message: "Snippet loaded. Review it before sending.",
  },

  /* i18n */ {
    id: "setup.secret",
    message:
      "The saved file contains a private client credential. Do not share or commit it.",
  },
  /* i18n */ { id: "setup.open", message: "Configure or repair MCP clients" },
  /* i18n */ {
    id: "setup.intro",
    message:
      "Choose one client and an explicit scope. Review the exact target file and redacted changes in the native dialog before writing.",
  },
  /* i18n */ {
    id: "setup.noneDetected",
    message:
      "No supported client detected. Select a known client explicitly to inspect its configuration.",
  },
  /* i18n */ { id: "setup.client", message: "MCP client" },
  /* i18n */ { id: "setup.scope", message: "Configuration scope" },
  /* i18n */ { id: "setup.user", message: "User configuration" },
  /* i18n */ { id: "setup.project", message: "Selected project folder" },
  /* i18n */ {
    id: "setup.replace",
    message: "Explicitly replace an existing ChannelTerm entry",
  },
  /* i18n */ {
    id: "setup.fallback",
    message:
      "Official commands are checked first. File-adapter fallback requires confirmation when commands cannot provide durable authentication and safe replacement preview.",
  },
  /* i18n */ {
    id: "setup.newCredential",
    message:
      "New credentials are independent and read-only. Safe rollback removes the new credential. Old credentials are never revoked automatically.",
  },
  /* i18n */ { id: "setup.inspect", message: "View redacted configuration" },
  /* i18n */ { id: "setup.verify", message: "Verify existing access" },
  /* i18n */ {
    id: "setup.configure",
    message: "Preview configuration or repair",
  },
  /* i18n */ {
    id: "setup.working",
    message: "Checking local client configuration…",
  },
  /* i18n */ {
    id: "setup.reload",
    message:
      "Restart or reload the chosen client and check its MCP status after saving. This independent read-only probe does not prove the client loaded the configuration. Project overrides may take precedence.",
  },
  /* i18n */ { id: "setup.recover", message: "Inspect interrupted setup" },
  /* i18n */ {
    id: "setup.confirm",
    message: "Apply this configuration using the file adapter?",
  },
  /* i18n */ { id: "setup.apply", message: "Confirm file-adapter change" },
  /* i18n */ {
    id: "setup.normalization",
    message:
      "The selected entry is replaced in full. Other settings are preserved by value. Serialization normalizes formatting and removes TOML comments.",
  },
  /* i18n */ {
    id: "setup.preserveNotice",
    message:
      "An entry exists. It will remain unchanged because explicit replacement is not selected.",
  },
  /* i18n */ { id: "setup.before", message: "Before (redacted)" },
  /* i18n */ { id: "setup.after", message: "After (redacted)" },
  /* i18n */ { id: "setup.detected", message: "Detected" },
  /* i18n */ { id: "setup.notDetected", message: "Not detected" },
  /* i18n */ {
    id: "setup.result.cancelled",
    message: "Cancelled. No configuration was applied.",
  },
  /* i18n */ { id: "setup.result.viewed", message: "Redacted preview closed." },
  /* i18n */ {
    id: "setup.result.preserved",
    message:
      "Existing entry preserved. Verify it, or explicitly select replacement to repair it.",
  },
  /* i18n */ {
    id: "setup.result.applied",
    message:
      "Configuration saved and independent read-only access verified. Client reload is still required.",
  },
  /* i18n */ {
    id: "setup.result.verified_existing",
    message:
      "Existing credential passed a read-only probe. Client reload and effective permissions are not inferred.",
  },
  /* i18n */ {
    id: "setup.result.recovered_applied",
    message:
      "Interrupted setup had already saved its configuration. Its credential was preserved; reload and verify the client.",
  },
  /* i18n */ {
    id: "setup.result.rolled_back",
    message:
      "Unpublished setup recovered. New credential removed; original configuration preserved.",
  },
  /* i18n */ {
    id: "setup.result.none",
    message: "No interrupted setup needs recovery.",
  },
  /* i18n */ {
    id: "setup.permissions.read_only",
    message: "New credential: read-only",
  },
  /* i18n */ {
    id: "setup.permissions.unknown",
    message: "Effective permissions: not verified",
  },
  /* i18n */ {
    id: "setup.error.configuration",
    message:
      "Configuration is damaged, shadowed, unavailable, or uses an unsupported scope/version. Inspect the client configuration; no unsafe replacement was performed.",
  },
  /* i18n */ {
    id: "setup.error.changed",
    message: "Configuration changed after preview. Review a fresh preview.",
  },
  /* i18n */ {
    id: "setup.error.authentication",
    message:
      "Credential missing or authentication rejected. Check the Host domain, then explicitly repair this client.",
  },
  /* i18n */ {
    id: "setup.error.permission",
    message:
      "Host denied permission. Check authorization; credential replacement does not grant writes.",
  },
  /* i18n */ {
    id: "setup.error.unavailable",
    message:
      "Host authentication unavailable. Check its configuration before creating another credential.",
  },
  /* i18n */ {
    id: "setup.error.network",
    message:
      "Host could not be reached or verified. Check endpoint and readiness.",
  },
  /* i18n */ {
    id: "setup.error.apply",
    message:
      "Setup did not complete. Inspect recovery and the original configuration before another change.",
  },
  /* i18n */ {
    id: "setup.error.recovery",
    message:
      "External changes or cleanup failure prevent safe recovery. Preserve files and registrations for manual inspection.",
  },
  /* i18n */ {
    id: "setup.error.companion",
    message:
      "Client-setup companion unavailable. Update or rebuild the matching CLI.",
  },
  /* i18n */ { id: "logs.open", message: "Continuous recording" },
  /* i18n */ {
    id: "logs.warning",
    message:
      "Output may contain secrets, echoed input and file-transfer bytes. No automatic redaction. Unsent input is not recorded.",
  },
  /* i18n */ {
    id: "logs.lifecycle",
    message:
      "Records new output from the start cursor, including hidden or closed tabs. Disconnecting this window or exiting stops recording. Old segments are removed at the selected limit; gaps are recorded, never filled with invented history.",
  },
  /* i18n */ {
    id: "logs.recovery",
    message:
      "Each segment has a JSONL cursor index. Complete index records acknowledge synced data. After a crash, a recording marker, missing index or partial final line means the tail is unverified. Text is UTF-8 with replacement for invalid bytes and escaped controls; it is not a terminal screen snapshot.",
  },
  /* i18n */ { id: "logs.format", message: "Recording format" },
  /* i18n */ { id: "logs.raw", message: "Original bytes (.bin)" },
  /* i18n */ { id: "logs.text", message: "Readable UTF-8 (.txt)" },
  /* i18n */ {
    id: "logs.segmentSize",
    message: "Data and index limit per segment",
  },
  /* i18n */ { id: "logs.segments", message: "Retained segments" },
  /* i18n */ { id: "logs.start", message: "Choose folder and start recording" },
  /* i18n */ { id: "logs.stop", message: "Stop recording" },
  /* i18n */ { id: "logs.starting", message: "Starting recording" },
  /* i18n */ { id: "logs.recording", message: "Recording" },
  /* i18n */ { id: "logs.stopping", message: "Stopping and flushing" },
  /* i18n */ { id: "logs.stopped", message: "Recording stopped" },
  /* i18n */ { id: "logs.failed", message: "Recording failed" },
  /* i18n */ { id: "logs.gaps", message: "Gaps" },
  /* i18n */ { id: "logs.rotations", message: "Removed segments" },
  /* i18n */ {
    id: "logs.error.source",
    message:
      "Output capture ended unexpectedly. Inspect the index for retained ranges; reconnecting will not resume this recording.",
  },
  /* i18n */ {
    id: "logs.error.disk",
    message:
      "Log write failed. Check free space and permissions. Only indexed bytes are confirmed; partial files remain for inspection.",
  },
  /* i18n */ {
    id: "logs.delete",
    message: "Choose a local log file to delete…",
  },
  /* i18n */ {
    id: "logs.deleteConfirm",
    message: "Move the selected log file to trash?",
  },
  /* i18n */ { id: "logs.trash", message: "Move to trash" },
  /* i18n */ {
    id: "host.settings",
    message: "Host information and connection",
  },
  /* i18n */ { id: "host.settings.available", message: "Available" },
  /* i18n */ { id: "host.settings.unavailable", message: "Unavailable" },
  /* i18n */ { id: "host.settings.desktop", message: "Desktop version" },
  /* i18n */ { id: "host.settings.cli", message: "Bundled CLI version" },
  /* i18n */ { id: "host.settings.resize", message: "Remote terminal sizing" },
  /* i18n */ {
    id: "host.settings.transfers",
    message: "Managed file transfers",
  },
  /* i18n */ { id: "host.settings.endpoint", message: "Local Host endpoint" },
  /* i18n */ {
    id: "host.settings.override",
    message:
      "CHANNELTERM_GUI_ENDPOINT overrides saved settings. Change the environment and restart to edit the endpoint.",
  },
  /* i18n */ {
    id: "host.settings.hint",
    message:
      "Save disconnects this window and discards pending input. Other windows and shared Sessions remain open. Connect explicitly afterward. New windows inherit their source window; restarting uses the saved default. Only numeric loopback HTTP is supported.",
  },
  /* i18n */ {
    id: "host.settings.versions",
    message:
      "Unknown means no verified version is available. The bundled CLI version requires matching package metadata; it does not identify the running Host. Capabilities describe this connection, not permission to operate a device.",
  },
  /* i18n */ {
    id: "host.settings.apply",
    message: "Save and disconnect this window",
  },
  /* i18n */ {
    id: "host.settings.backgroundMCP",
    message: "Keep MCP running in the background after disconnect",
  },

  /* i18n */ { id: "workbench.new", message: "New" },
  /* i18n */ { id: "workbench.closing", message: "Closing" },
  /* i18n */ {
    id: "recovery.connection_failed",
    message:
      "Host could not open this connection. Check settings and refresh Sessions before trying again.",
  },
  /* i18n */ {
    id: "recovery.connection_uncertain",
    message:
      "Connection result is uncertain. Refresh Sessions to inspect the result before a new request; the request was not replayed.",
  },
  /* i18n */ {
    id: "recovery.diagnostic_unavailable",
    message:
      "Diagnostic archive unavailable or request failed. It may have expired or been evicted; create a new snapshot.",
  },
  /* i18n */ {
    id: "recovery.profile_exists",
    message:
      "Profile already exists. Choose another name or explicitly enable replacement to overwrite it.",
  },

  /* i18n */ {
    id: "recovery.ssh_host_key",
    message:
      "SSH host key rejected. Verify the server key through a trusted channel and update the Host known_hosts file before connecting.",
  },
  /* i18n */ {
    id: "recovery.ssh_authentication",
    message:
      "SSH authentication failed. Check the selected identity, password or key passphrase.",
  },

  /* i18n */ { id: "workbench.connecting", message: "Connecting to Host…" },
  /* i18n */ { id: "workbench.disconnecting", message: "Disconnecting Host…" },
  /* i18n */ { id: "workbench.connected", message: "Host connected" },
  /* i18n */ { id: "workbench.failed", message: "Host connection failed" },
  /* i18n */ { id: "workbench.disconnected", message: "Host disconnected" },
  /* i18n */ { id: "workbench.attaching", message: "Attaching Session…" },
  /* i18n */ { id: "workbench.detaching", message: "Detaching Session…" },
  /* i18n */ { id: "workbench.opening", message: "Opening connection…" },
  /* i18n */ { id: "workbench.unavailable", message: "Host unavailable" },
  /* i18n */ { id: "workbench.noHost", message: "No Host connected" },
  /* i18n */ {
    id: "workbench.sessionUnavailable",
    message: "Session unavailable",
  },
  /* i18n */ { id: "workbench.noSessions", message: "No Sessions available" },
  /* i18n */ { id: "workbench.select", message: "Select a Session" },
  /* i18n */ { id: "workbench.wait", message: "Please wait." },
  /* i18n */ {
    id: "workbench.retryHint",
    message:
      "Check that your configured Host is running, then retry the connection.",
  },
  /* i18n */ {
    id: "workbench.connectHint",
    message: "Connect to your running Host using the Host panel.",
  },
  /* i18n */ {
    id: "workbench.selectHint",
    message: "Choose Session > Attach Session… to view an existing terminal.",
  },
  /* i18n */ { id: "workbench.navigation", message: "Workspace navigation" },
  /* i18n */ {
    id: "workbench.toggleSessions",
    message: "Toggle Session sidebar",
  },
  /* i18n */ { id: "workbench.sessions", message: "Sessions" },
  /* i18n */ { id: "workbench.noResults", message: "No Sessions found." },
  /* i18n */ {
    id: "workbench.listHint",
    message: "Connect a Host to list Sessions.",
  },
  /* i18n */ { id: "workbench.host", message: "Host connection" },
  /* i18n */ { id: "workbench.loading", message: "Loading configuration…" },
  /* i18n */ { id: "workbench.retry", message: "Retry connection" },
  /* i18n */ {
    id: "workbench.detachHint",
    message: "Detaching leaves Sessions running.",
  },
  /* i18n */ { id: "workbench.terminal", message: "Terminal workspace" },
  /* i18n */ { id: "workbench.notices", message: "Terminal notices" },
  /* i18n */ {
    id: "workbench.attachmentHost",
    message:
      "This Host belongs to another attachment. It may stop when its owner exits.",
  },
  /* i18n */ {
    id: "workbench.dropped",
    message:
      "Output overwritten in the Host buffer. The terminal was reset; older bytes are unavailable.",
  },
  /* i18n */ {
    id: "workbench.lease",
    message: "Lease active. Reading continues; input is disabled.",
  },
  /* i18n */ {
    id: "workbench.closedHint",
    message:
      "Session closed or unavailable. Detach and select an open Session.",
  },
  /* i18n */ { id: "workbench.status", message: "Workbench status" },
  /* i18n */ { id: "workbench.offline", message: "Offline" },
  /* i18n */ {
    id: "workbench.readOnly",
    message: "Session unavailable · Read-only",
  },
  /* i18n */ {
    id: "workbench.leaseReadOnly",
    message: "Read-only · Lease active",
  },
  /* i18n */ { id: "workbench.paused", message: "Input paused" },
  /* i18n */ { id: "workbench.enabled", message: "Attached · Input enabled" },
  /* i18n */ { id: "workbench.unattached", message: "No attachment" },
  /* i18n */ { id: "workbench.hostOwned", message: "Host-owned Sessions" },
  /* i18n */ { id: "workbench.search", message: "Search Sessions" },
  /* i18n */ { id: "workbench.noMatch", message: "No matching Sessions." },
  /* i18n */ { id: "workbench.open", message: "Open" },
  /* i18n */ { id: "workbench.closed", message: "Closed" },
  /* i18n */ { id: "workbench.unknown", message: "Unknown" },
  /* i18n */ { id: "workbench.io", message: "Terminal output and input" },
  /* i18n */ { id: "workbench.menu", message: "Application menu" },
  /* i18n */ { id: "workbench.workbenchMenu", message: "Workbench menu" },
  /* i18n */ {
    id: "recovery.control_stale",
    message:
      "Control changed or the instance expired. Refresh and inspect the current owner before new input. Discarded input is not replayed.",
  },
  /* i18n */ {
    id: "recovery.configuration",
    message:
      "Check the Host endpoint and local configuration, then connect explicitly.",
  },
  /* i18n */ {
    id: "recovery.disconnected",
    message:
      "Check the Host and reconnect explicitly. Input was not retried; delivery may be uncertain. Inspect remote state before new input.",
  },
  /* i18n */ {
    id: "recovery.unauthorized",
    message:
      "Access denied. Check the credential permissions and current control owner. Do not disable authentication.",
  },
  /* i18n */ {
    id: "recovery.incompatible",
    message:
      "The Host response or capability is incompatible. Use matching desktop and Host versions.",
  },
  /* i18n */ {
    id: "recovery.busy",
    message:
      "An operation or Lease is active. Wait for it to finish and refresh. Discarded input is not queued for replay.",
  },
  /* i18n */ {
    id: "recovery.closed",
    message:
      "The target is closed or no longer available. Refresh and select an open target; it will not be recreated automatically.",
  },
  /* i18n */ {
    id: "recovery.cancelled",
    message:
      "The request ended or was cancelled. Refresh to check its result before any new action; do not retry input automatically.",
  },
  /* i18n */ {
    id: "recovery.invalid_input",
    message:
      "Check the entered settings, path and target. Correct them before submitting again.",
  },
  /* i18n */ {
    id: "recovery.tool_error",
    message:
      "The operation failed. Check the target and Host diagnostics, then refresh its state. Do not replay input with an unknown result.",
  },
  /* i18n */ {
    id: "recovery.input_queue",
    message:
      "Input queue is full. Remaining input was discarded. Inspect the target before entering new input.",
  },
  /* i18n */ {
    id: "workbench.sessionCount",
    message: "{count, plural, one {# Session} other {# Sessions}}",
  },

  /* i18n */ {
    id: "control.sshWarning",
    message:
      "Cancel this independent SSH task? Its dedicated connection will close; terminal Sessions stay open. Partial files may remain.",
  },
  /* i18n */ { id: "control.title", message: "Session control" },
  /* i18n */ { id: "control.interrupt", message: "Interrupt AI" },
  /* i18n */ { id: "control.takeover", message: "Take control" },
  /* i18n */ { id: "control.allow", message: "Allow AI operations" },
  /* i18n */ { id: "control.paused", message: "AI paused" },
  /* i18n */ {
    id: "control.warning",
    message:
      "Stop this captured operation and take control? Recovery may close this Session. Remote background work may continue.",
  },
  /* i18n */ {
    id: "control.allowWarning",
    message:
      "Allow new AI operations? Cancelled work and discarded input will not restart.",
  },
  /* i18n */ { id: "control.confirm", message: "Confirm" },
  /* i18n */ { id: "control.dismiss", message: "Dismiss" },
  /* i18n */ {
    id: "control.processing",
    message: "Request pending \u00b7 input requires Host confirmation",
  },
  /* i18n */ {
    id: "control.stopping",
    message: "Stopping \u00b7 recovery pending; keep input paused",
  },
  /* i18n */ {
    id: "control.taken_over",
    message: "Recovery confirmed \u00b7 AI remains paused",
  },
  /* i18n */ {
    id: "control.failed",
    message:
      "Recovery failed \u00b7 original Session closed; remote work may continue",
  },
  /* i18n */ {
    id: "control.unknown",
    message:
      "Result unconfirmed or request denied. Query the original request; do not retry input.",
  },
  /* i18n */ {
    id: "control.stale",
    message:
      "Operation changed or completed. Refresh and inspect before a new request.",
  },
  /* i18n */ {
    id: "control.allowed",
    message: "AI pause removed \u00b7 no work restarted",
  },
  /* i18n */ { id: "control.query", message: "Query original result" },

  /* i18n */ { id: "source.user", message: "User credential" },
  /* i18n */ { id: "source.agent", message: "AI credential" },
  /* i18n */ { id: "source.unknown", message: "Source unconfirmed" },
  /* i18n */ { id: "source.declared", message: "client-declared" },
  /* i18n */ { id: "source.onlinePresence", message: "Instance online" },
  /* i18n */ { id: "source.expiredPresence", message: "Instance expired" },
  /* i18n */ { id: "source.endedPresence", message: "Instance ended" },
  /* i18n */ {
    id: "source.unknownPresence",
    message: "Instance state unknown",
  },
  /* i18n */ { id: "source.initiator", message: "Initiator" },
  /* i18n */ {
    id: "source.local",
    message: "This window only · independent SSH connection",
  },
  /* i18n */ { id: "source.host", message: "Host-managed task" },
  /* i18n */ {
    id: "source.correlation",
    message: "Target and correlation IDs",
  },
  /* i18n */ {
    id: "source.resumed",
    message: "Resume attempt · current initiator shown above",
  },
  /* i18n */ {
    id: "source.original",
    message: "Original source (checkpoint report, not Host-verified)",
  },
  /* i18n */ { id: "settings.transfers", message: "File transfer" },
  /* i18n */ {
    id: "file.directoryQuestion",
    message: "Folder transfer method",
  },
  /* i18n */ {
    id: "file.directoryHint",
    message:
      "TAR is uncompressed and extracted after transfer. Both methods produce folders.",
  },
  /* i18n */ { id: "file.mode.tar", message: "Pack as TAR and extract" },
  /* i18n */ { id: "file.mode.files", message: "Copy files individually" },
  /* i18n */ { id: "file.mode.ask", message: "Ask each time" },
  /* i18n */ { id: "file.rememberMode", message: "Remember for CLI and GUI" },
  /* i18n */ {
    id: "file.modeResumeConflict",
    message:
      "Per-file mode cannot keep a resume checkpoint. Disable resume or choose TAR.",
  },
  /* i18n */ {
    id: "file.preferenceHint",
    message:
      "Shared with CLI on this computer and user account. Applies to serial directories; SSH uses recursive copy.",
  },
  /* i18n */ { id: "file.savePreference", message: "Save preference" },
  /* i18n */ { id: "file.startBatch", message: "Start transfer" },
  /* i18n */ {
    id: "file.dropDestination",
    message:
      "Choose the destination directory and confirm SSH credentials for this batch.",
  },
  /* i18n */ {
    id: "file.dropHint",
    message: "Drop to send; serial uses the current shell directory",
  },
  /* i18n */ {
    id: "file.dropUnavailable",
    message:
      "Select an available Session and finish any current operation before dropping files.",
  },
  /* i18n */ {
    id: "recovery.host_legacy_credentials",
    message:
      "Old Host configuration detected; new credentials are missing. Finish shared Session work, stop and update the Host, then restart the GUI. Keep the old files for migration. Retrying alone cannot upgrade the Host.",
  },
  /* i18n */ {
    id: "recovery.host_identity_missing",
    message:
      "Host identity is missing. Check that GUI and Host use the same account and CHANNELTERM_HOST_CONFIG_DIR. Start the matching updated Host, then retry.",
  },
  /* i18n */ {
    id: "recovery.host_credential_missing",
    message:
      "Local management credential is missing. Check the Host account and configuration directory. Finish shared Session work before restarting the matching Host. Do not delete the Host identity or reuse a shared token.",
  },
  /* i18n */ {
    id: "recovery.host_credential_invalid",
    message:
      "Host identity or credential is invalid. Restore a trusted backup or ask the Host administrator to diagnose it. Do not blindly delete or regenerate credentials.",
  },
  /* i18n */ {
    id: "recovery.host_credential_unreadable",
    message:
      "Cannot read Host credentials. Check file permissions and that GUI and Host use the same account and configuration directory. No credentials were changed.",
  },
  /* i18n */ {
    id: "recovery.host_companion_outdated",
    message:
      "Companion Host is outdated or its build failed; automatic launch is blocked. Finish shared Session work and stop its Host, run npm run host:build in gui/electron, then restart the GUI.",
  },
  /* i18n */ { id: "debug.title", message: "Debug" },
  /* i18n */ { id: "debug.refresh", message: "Refresh debug overview" },
  /* i18n */ { id: "debug.filter", message: "Filter debug operations" },
  /* i18n */ {
    id: "debug.offline",
    message: "Host disconnected; state unknown",
  },
  /* i18n */ {
    id: "debug.unavailable",
    message: "Overview unavailable; previous state is stale",
  },
  /* i18n */ { id: "debug.loading", message: "Loading debug overview…" },
  /* i18n */ {
    id: "debug.stale",
    message: "Stale observation; current state unknown",
  },
  /* i18n */ {
    id: "debug.scope",
    message: "Registered operations only; no process or port scan",
  },
  /* i18n */ { id: "debug.updated", message: "Updated" },
  /* i18n */ { id: "debug.empty", message: "No registered debug operations" },
  /* i18n */ { id: "debug.serial", message: "Serial" },
  /* i18n */ { id: "debug.ssh", message: "SSH" },
  /* i18n */ { id: "debug.unassociated", message: "No associated Session" },
  /* i18n */ { id: "debug.serial_bridge", message: "Serial RSP bridge" },
  /* i18n */ { id: "debug.managed", message: "Host-managed GDB" },
  /* i18n */ { id: "debug.tcp", message: "TCP GDB" },
  /* i18n */ { id: "debug.ssh_tunnel", message: "SSH tunnel GDB" },
  /* i18n */ { id: "debug.ssh_stdio", message: "SSH stdio GDB" },
  /* i18n */ { id: "debug.ssh_terminal", message: "GDB in SSH terminal" },
  /* i18n */ {
    id: "debug.ssh_terminal_tcp",
    message: "TCP GDB in SSH terminal",
  },
  /* i18n */ { id: "debug.connection", message: "Connection" },
  /* i18n */ { id: "debug.targetState", message: "Target state" },
  /* i18n */ { id: "debug.unknown", message: "Unknown" },
  /* i18n */ { id: "debug.connecting", message: "Connecting" },
  /* i18n */ { id: "debug.connected", message: "Connected" },
  /* i18n */ { id: "debug.disconnected", message: "Disconnected" },
  /* i18n */ { id: "debug.listening", message: "Listening" },
  /* i18n */ { id: "debug.recovering", message: "Recovering terminal" },
  /* i18n */ { id: "debug.running", message: "Running" },
  /* i18n */ { id: "debug.stopped", message: "Stopped" },
  /* i18n */ { id: "debug.exited", message: "Exited" },
  /* i18n */ { id: "debug.operation", message: "Operation ID" },
  /* i18n */ { id: "debug.source", message: "Evidence source" },
  /* i18n */ { id: "debug.source.host", message: "Host observation" },
  /* i18n */ {
    id: "debug.source.manual",
    message: "Manual registration; unobserved",
  },
  /* i18n */ {
    id: "debug.source.client_report",
    message: "Client report; not Host verification",
  },
  /* i18n */ { id: "debug.client", message: "Registered client" },
  /* i18n */ { id: "debug.lastState", message: "Last observed target state" },
  /* i18n */ { id: "debug.tunnel", message: "Tunnel connection" },
  /* i18n */ { id: "debug.session", message: "Full Session ID" },
  /* i18n */ {
    id: "debug.sessionMissing",
    message: "Associated Session unavailable",
  },
  /* i18n */ {
    id: "debug.role.authenticated_bridge",
    message: "Authenticated bridge endpoint",
  },
  /* i18n */ {
    id: "debug.role.local_forward",
    message: "Local forwarding endpoint",
  },
  /* i18n */ { id: "debug.role.ssh", message: "SSH server" },
  /* i18n */ { id: "debug.role.target", message: "Debug target endpoint" },
  /* i18n */ { id: "debug.perspective.host", message: "Host" },
  /* i18n */ { id: "debug.perspective.executor", message: "Executor" },
  /* i18n */ { id: "debug.perspective.remote", message: "Remote host" },
  /* i18n */ { id: "debug.observed", message: "Observed endpoint" },
  /* i18n */ { id: "debug.configured", message: "Configured endpoint" },
  /* i18n */ { id: "debug.not_applicable", message: "Not applicable" },
  /* i18n */ { id: "debug.locate", message: "Locate Session" },

  /* i18n */ {
    id: "terminal.sizeOwned",
    message: "Remote size follows this view",
  },
  /* i18n */ { id: "terminal.sizeLocal", message: "Local fitting only" },
  /* i18n */ {
    id: "terminal.serialSizeInsert",
    message: "Insert stty command",
  },
  /* i18n */ {
    id: "terminal.sizeTake",
    message: "Use this view for remote size",
  },
  /* i18n */ { id: "terminal.sizeRelease", message: "Release size control" },
  /* i18n */ {
    id: "terminal.sizeHint",
    message:
      "Switching size control stops other views from resizing this shared SSH terminal. It does not change input permissions.",
  },
  /* i18n */ { id: "sshBrowser.title", message: "Browse SFTP" },
  /* i18n */ {
    id: "sshBrowser.hint",
    message:
      "Read-only listing and properties use a separate SSH connection and the credentials in this form. Selecting a path does not transfer files.",
  },
  /* i18n */ { id: "sshBrowser.path", message: "Directory or entry path" },
  /* i18n */ { id: "sshBrowser.list", message: "List directory" },
  /* i18n */ { id: "sshBrowser.stat", message: "Properties" },
  /* i18n */ { id: "sshBrowser.useDirectory", message: "Use directory path" },
  /* i18n */ { id: "sshBrowser.loading", message: "Reading..." },
  /* i18n */ { id: "sshBrowser.location", message: "Listed directory" },
  /* i18n */ { id: "sshBrowser.empty", message: "Directory is empty." },
  /* i18n */ { id: "sshBrowser.file", message: "File" },
  /* i18n */ { id: "sshBrowser.directory", message: "Directory" },
  /* i18n */ { id: "sshBrowser.symlink", message: "Symbolic link" },
  /* i18n */ { id: "sshBrowser.other", message: "Other" },
  /* i18n */ { id: "sshBrowser.open", message: "Open directory" },
  /* i18n */ { id: "sshBrowser.use", message: "Use path" },
  /* i18n */ { id: "sshBrowser.hide", message: "Hide browser" },
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
  /* i18n */ { id: "connection.serialTab", message: "Serial" },
  /* i18n */ { id: "connection.inDevelopment", message: "In development" },
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
  /* i18n */ {
    id: "history.title",
    message: "Recent and saved connections",
  },
  /* i18n */ {
    id: "history.hint",
    message:
      "Local form presets only; select, review and connect explicitly. Credentials and credential paths must be entered again. Serial profiles remain shared with CLI.",
  },
  /* i18n */ { id: "history.unfavorite", message: "Remove favorite" },
  /* i18n */ { id: "history.remove", message: "Forget connection" },
  /* i18n */ {
    id: "history.storageError",
    message:
      "Could not save connection history. Check local storage; do not repeat a successful connection.",
  },
  /* i18n */ {
    id: "workspace.defaults",
    message: "Terminal defaults for new views",
  },
  /* i18n */ {
    id: "workspace.hint",
    message:
      "Workspace view restored. Only recent Host output (up to 64 KiB) is available; full screen state and unsent input are not restored.",
  },
  /* i18n */ {
    id: "workspace.missing",
    message:
      "The saved Session is closed, missing or changed. It was not recreated. Close this tab or explicitly choose a live Session.",
  },
  /* i18n */ {
    id: "workspace.storageError",
    message:
      "Could not save or load local preferences/workspace. Current views remain usable; check local storage access.",
  },
  /* i18n */ { id: "sshManage.mkdir", message: "Create directory" },
  /* i18n */ { id: "sshManage.rename", message: "Rename entry" },
  /* i18n */ { id: "sshManage.rm", message: "Remove file or symlink" },
  /* i18n */ { id: "sshManage.rmdir", message: "Remove empty directory" },
  /* i18n */ { id: "sshManage.confirm", message: "Confirm SFTP change" },
  /* i18n */ { id: "sshManage.apply", message: "Apply change" },
  /* i18n */ { id: "sshManage.review", message: "Review change" },
  /* i18n */ {
    id: "sshManage.boundary",
    message:
      "Exact paths only. Rename does not overwrite; deletion is not recursive. A disconnected or cancelled operation may already have taken effect. No automatic retry.",
  },
  /* i18n */ { id: "sshManage.target", message: "New absolute path" },
  /* i18n */ {
    id: "sshManage.confirmed",
    message:
      "Server confirmed the operation. List again to inspect current contents.",
  },
  /* i18n */ {
    id: "sshManage.unknown",
    message:
      "Final state is unknown. Inspect the original target before another operation.",
  },
  /* i18n */ {
    id: "sshManage.not_started",
    message: "No SFTP mutation was started.",
  },
  /* i18n */ {
    id: "sshManage.not_found",
    message: "The server reported a missing entry.",
  },
  /* i18n */ {
    id: "sshManage.already_exists",
    message: "The server reported that the target exists.",
  },
  /* i18n */ {
    id: "sshManage.permission_denied",
    message: "The server denied access.",
  },
  /* i18n */ {
    id: "sshManage.operation_failed",
    message:
      "Operation failed or could not be confirmed. Some servers provide no precise reason; check paths, access and whether a directory is empty.",
  },
  /* i18n */ {
    id: "sshManage.cancelled",
    message: "The request was cancelled.",
  },
  /* i18n */ { id: "sshManage.timeout", message: "The operation timed out." },
  /* i18n */ {
    id: "sshManage.target_changed",
    message: "The selected Session or endpoint changed.",
  },
  /* i18n */ { id: "file.advanced", message: "Advanced transfer options" },
  /* i18n */ {
    id: "file.overwrite",
    message: "Allow replacing existing files / merging directories",
  },
  /* i18n */ {
    id: "file.overwriteHint",
    message:
      "Overwrite can truncate existing files. Cancellation or failure may leave partial changes; inspect the target before retrying.",
  },
  /* i18n */ {
    id: "file.overwriteConfirm",
    message: "Confirm transfer overwrite",
  },
  /* i18n */ {
    id: "file.safeReplace",
    message: "Stage and replace each file (SFTP only)",
  },
  /* i18n */ {
    id: "file.safeReplaceHint",
    message:
      "Requires overwrite. Each file is verified before replacement; the whole directory is not atomic. Interrupted cleanup can leave staging files.",
  },
  /* i18n */ {
    id: "file.operationTimeout",
    message: "Operation timeout in seconds (0 disables, maximum 86400)",
  },
  /* i18n */ {
    id: "file.outcome.confirmed",
    message: "Transfer and verification confirmed.",
  },
  /* i18n */ {
    id: "file.outcome.unknown",
    message:
      "Final destination state is uncertain. Inspect it before another transfer.",
  },
  /* i18n */ {
    id: "file.outcome.not_started",
    message: "Transfer did not start.",
  },
  /* i18n */ { id: "file.completedFiles", message: "Completed files" },
  /* i18n */ { id: "file.verifiedFiles", message: "SHA-256 verified files" },
  /* i18n */ {
    id: "file.payloadBytes",
    message: "Copied payload (including partial files)",
  },
  /* i18n */ {
    id: "file.verificationFailed",
    message:
      "SHA-256 verification failed. The destination did not match the source.",
  },
  /* i18n */ { id: "devices.title", message: "Device discovery" },
  /* i18n */ { id: "devices.refresh", message: "Refresh devices" },
  /* i18n */ { id: "devices.hide", message: "Hide discovery" },
  /* i18n */ {
    id: "devices.boundary",
    message:
      "Discovery never opens ports or sends probe bytes. Only this Host’s Sessions are known; other applications’ occupancy and OS access remain unknown until an explicit connection attempt.",
  },
  /* i18n */ {
    id: "devices.stale",
    message: "Presence is unknown. Retained observations may be outdated.",
  },
  /* i18n */ { id: "devices.empty", message: "No devices in the latest scan." },
  /* i18n */ {
    id: "devices.limit",
    message: "Showing up to 128 recent device observations.",
  },
  /* i18n */ { id: "devices.unknown", message: "Unknown" },
  /* i18n */ { id: "devices.present", message: "Present" },
  /* i18n */ { id: "devices.absent", message: "Not present" },
  /* i18n */ { id: "devices.persistent", message: "Persistent ID" },
  /* i18n */ { id: "devices.runtimeOnly", message: "This Host process only" },
  /* i18n */ {
    id: "devices.identity.usb_serial",
    message: "USB serial identity",
  },
  /* i18n */ {
    id: "devices.identity.usb_path",
    message: "USB location identity, not physical identity",
  },
  /* i18n */ {
    id: "devices.identity.runtime",
    message: "Insufficient or conflicting identity evidence",
  },
  /* i18n */ {
    id: "devices.identity.unknown",
    message: "Identity unavailable",
  },
  /* i18n */ { id: "devices.previous", message: "Previous endpoint" },
  /* i18n */ { id: "devices.details", message: "Device details" },
  /* i18n */ { id: "devices.usbSerial", message: "USB serial number" },
  /* i18n */ { id: "devices.usbPath", message: "USB location" },
  /* i18n */ { id: "devices.lastSeen", message: "Last observed" },
  /* i18n */ { id: "devices.select", message: "Select this device" },
  /* i18n */ { id: "devices.policy", message: "Host policy" },
  /* i18n */ { id: "devices.action", message: "Discovery decision" },
  /* i18n */ { id: "devices.policy.ask", message: "Ask" },
  /* i18n */ { id: "devices.policy.auto", message: "Automatic" },
  /* i18n */ { id: "devices.policy.deny", message: "Deny" },
  /* i18n */ { id: "devices.action.none", message: "No action" },
  /* i18n */ { id: "devices.action.ask", message: "Ask before connecting" },
  /* i18n */ {
    id: "devices.action.connect",
    message: "Eligible for connection",
  },
  /* i18n */ { id: "devices.action.deny", message: "Connection denied" },
  /* i18n */ {
    id: "devices.connected",
    message: "Already connected on this Host",
  },
  /* i18n */ {
    id: "devices.externalUnknown",
    message:
      "No Session on this Host; external occupancy and access are unknown.",
  },
  /* i18n */ {
    id: "devices.reselect",
    message: "Refresh and select the device again, or enter a port manually.",
  },
  /* i18n */ {
    id: "devices.explicit",
    message:
      "Selection does not connect. Connect is an explicit request; discovery policy is not an access permission.",
  },
  /* i18n */ {
    id: "recovery.discovery_unavailable",
    message:
      "Device discovery is unavailable. Check the Host; manual port entry remains available.",
  },
  /* i18n */ {
    id: "recovery.discovery_stale",
    message:
      "The Host scan failed or is outdated. Refresh before selecting a device.",
  },
  /* i18n */ {
    id: "recovery.device_changed",
    message:
      "The selected device disappeared or changed identity or endpoint. Refresh and select it again.",
  },
  /* i18n */ {
    id: "recovery.serial_not_found",
    message:
      "The port was not found. Refresh discovery and check the device connection.",
  },
  /* i18n */ {
    id: "recovery.serial_permission_denied",
    message:
      "The operating system denied port access. Check device permissions before retrying.",
  },
  /* i18n */ {
    id: "recovery.serial_busy",
    message:
      "The port is busy. Check its owner before retrying; no process was stopped.",
  },
  /* i18n */ {
    id: "recovery.serial_busy_or_denied",
    message:
      "The port is busy or access was denied; the driver cannot distinguish them. Check ownership and permissions.",
  },
  /* i18n */ {
    id: "recovery.serial_open_failed",
    message:
      "The port could not be opened. The cause is unknown; check the device and Host diagnostics before retrying.",
  },
  /* i18n */ {
    id: "tab.reorderHint",
    message:
      "Drag to reorder, or press Shift+Left/Right while this tab button has focus.",
  },
  /* i18n */ { id: "tab.splitRight", message: "Split right" },
  /* i18n */ { id: "tab.splitDown", message: "Split down" },
  /* i18n */ { id: "view.single", message: "Single view" },
  /* i18n */ { id: "split.focused", message: "Focused view" },
  /* i18n */ { id: "split.focus", message: "Focus this view" },
] satisfies MessageDescriptor[];
