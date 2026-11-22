import { configuredPeers, listPeers, peerToken, type Peer } from "./host/peers";
import type { OpenTabRow, PeerSessions } from "../shared/api";
import { isTabAlias } from "../shared/workspace";
import { CommandSnippets } from "./snippets";
import { isDebugPayload } from "../shared/debug-send";
import { DesktopFiles, TransferConfirmation } from "./files";
import { DesktopDiagnostics } from "./diagnostics";
import { DesktopUBoot } from "./uboot";
import { isUBootRequest } from "../shared/uboot";
import { DesktopRecordings } from "./logging";
import { DesktopMCPSetup } from "./mcp-setup";
import { isMCPSetupRequest } from "../shared/mcp-setup";
import { isRecordingRequest } from "../shared/logging";
import { setupI18n } from "@lingui/core";
import { menuCatalogs } from "../locales/catalogs";
import { DesktopSSHBrowser } from "./ssh-browser";
import { isDiagnosticRequest } from "../shared/diagnostics";
import { DesktopSSHFiles } from "./ssh-files";
import { copyAITarget } from "./target-copy";
import {
  app,
  clipboard,
  dialog,
  BrowserWindow,
  ipcMain,
  nativeTheme,
  protocol,
  session,
  shell,
} from "electron";
import { readFile, lstat, realpath } from "node:fs/promises";
import { extname, join, resolve, sep } from "node:path";
import { channels, type Result, type WindowInfo } from "../shared/api";
import { randomUUID } from "node:crypto";
import { HostClient } from "./host/client";
import { hostEndpoint, loadToken } from "./host/config";
import { HostSettings, validateEndpoint } from "./host/settings";
import { companionVersion, displayVersion } from "./host/information";
import {
  companionPath,
  defaultHostEndpoint,
  localHostStartup,
} from "./host/startup";
import { HostError, publicError } from "./host/errors";
import { identifier, input } from "./host/validation";
import {
  initialMenuState,
  isMenuState,
  type MenuState,
  type LanguagePreference,
} from "../shared/menus";
import { updateApplicationMenu } from "./menu";
import {
  contentPolicy,
  developmentURL,
  productionURL,
  validateSender,
} from "./security";

protocol.registerSchemesAsPrivileged([
  {
    scheme: "channelterm",
    privileges: { standard: true, secure: true, supportFetchAPI: true },
  },
]);
app.enableSandbox();
// Match native window chrome to the renderer's fixed dark workbench theme.
nativeTheme.themeSource = "dark";

const development =
  process.env.CHANNELTERM_GUI_DEV_URL === developmentURL.slice(0, -1) &&
  !app.isPackaged;
type Handler = (...args: unknown[]) => unknown;

/** Main owns each window's client resources. No context owns a Host Session. */
interface WindowContext {
  window: BrowserWindow;
  host?: HostClient;
  connected: boolean;
  connecting: boolean;
  endpoint: string;
  savingEndpoint: boolean;
  disposed: boolean;
  connectionVersion: number;
  startupController: AbortController;
  transferConfirmation: TransferConfirmation;
  files: DesktopFiles;
  sshFiles: DesktopSSHFiles;
  diagnostics: DesktopDiagnostics;
  uboot: DesktopUBoot;
  ubootConfirming: boolean;
  recordings: DesktopRecordings;
  mcpSetup: DesktopMCPSetup;
  closingRecordings: boolean;
  debugSending: boolean;
  sshBrowser: DesktopSSHBrowser;
  menu: MenuState;
  handlers: Record<string, Handler>;
  autoConnect: boolean;
  languagePreference?: LanguagePreference;
  workspaceSlot: number;
  rows: OpenTabRow[];
  peer?: Peer;
  initialSession?: string;
  incoming?: WindowInfo["incoming"];
  incomingMove?: TabMove;
  outgoingMove?: TabMove;
}

/** A bounded two-window handoff. Only the destination's first-render acknowledgement commits it. */
interface TabMove {
  id: string;
  source: WindowContext;
  target: WindowContext;
  sourceAttachment: string;
  targetAttachment?: string;
  timer: ReturnType<typeof setTimeout>;
  resolve: () => void;
  reject: (error: unknown) => void;
  settled: boolean;
}

let peerListing: Promise<PeerSessions[]> | undefined;
const desktop = {
  windows: new Map<number, WindowContext>(),
  disconnects: new Set<Promise<void>>(),
  quitting: false,
  startup: localHostStartup(
    companionPath(
      app.isPackaged ? process.resourcesPath : resolve(__dirname, ".."),
    ),
  ),
};
const fileExecutable = join(
  app.isPackaged ? process.resourcesPath : resolve(__dirname, ".."),
  "files",
  process.platform === "win32" ? "channelterm.exe" : "channelterm",
);
const commandSnippets = new CommandSnippets(
  join(app.getPath("userData"), "command-snippets.json"),
);
const hostSettings = new HostSettings(
  join(app.getPath("userData"), "host-settings.json"),
  process.env.CHANNELTERM_GUI_ENDPOINT,
);
const cliVersion = companionVersion(
  app.isPackaged ? process.resourcesPath : resolve(__dirname, ".."),
);

function hostInfo(context: WindowContext) {
  const info = context.host?.info();
  return {
    ...(info ?? {
      endpoint: context.endpoint,
      lifetime: "unknown" as const,
    }),
    ...(info?.idlePolicy && context.peer
      ? { idlePolicy: { ...info.idlePolicy, backgroundSwitch: false } }
      : {}),
    autoConnect:
      context.autoConnect || context.endpoint === defaultHostEndpoint,
  };
}

function requestQuit(): void {
  // Return an IPC reply before normal before-quit cleanup closes the renderer.
  setImmediate(() => app.quit());
}

function disconnectHost(context: WindowContext): Promise<void> {
  if (context.outgoingMove) failMove(context.outgoingMove);
  if (context.incomingMove) failMove(context.incomingMove);
  context.incoming = undefined;
  context.transferConfirmation.dismiss();
  context.files.clearSelections();
  context.sshFiles.disconnect();
  context.diagnostics.cancel();
  context.uboot.stop();
  context.sshBrowser.cancel();
  context.connectionVersion++;
  context.startupController.abort();
  context.connected = false;
  const host = context.host;
  context.host = undefined;
  const stopped = context.recordings.stopAll();
  const cleanup = Promise.all([stopped, host?.disconnect()]).then(() => {});
  desktop.disconnects.add(cleanup);
  const forget = () => {
    desktop.disconnects.delete(cleanup);
  };
  void cleanup.then(forget, forget);
  return cleanup;
}

function currentHost(context: WindowContext): HostClient {
  if (context.closingRecordings)
    throw new HostError("cancelled", "The window is closing.");
  if (context.savingEndpoint)
    throw new HostError("busy", "Host settings are changing.");
  if (!context.host || context.disposed)
    throw new HostError("disconnected", "Connect to a Host first.");
  return context.host;
}

function windowContexts(): WindowContext[] {
  return [...desktop.windows.values()].filter(
    (context) => !context.disposed && !context.incomingMove,
  );
}

function refreshMenus(): void {
  const count = windowContexts().length;
  for (const context of desktop.windows.values()) {
    context.menu = { ...context.menu, windowCount: Math.max(1, count) };
    context.window.webContents.send(channels.windowCount, Math.max(1, count));
  }
  const selected =
    desktop.windows.get(
      BrowserWindow.getFocusedWindow()?.webContents.id ?? -1,
    ) ?? windowContexts().at(-1);
  if (selected) updateMenu(selected);
}

function updateMenu(context: WindowContext): void {
  updateApplicationMenu(context.window, context.menu, requestQuit, (id) => {
    if (id === "window.new")
      void newWindow(context).catch(() => {
        console.error("Could not create a ChannelTerm window.");
      });
    else if (id === "window.close") {
      if (!context.disposed) context.window.close();
    } else focusWindow(context, id === "window.next" ? "next" : "previous");
  });
}

async function newWindow(source?: WindowContext): Promise<void> {
  const context = makeWindow(
    source?.connected ?? false,
    source?.menu.languagePreference ?? source?.menu.locale,
    source?.endpoint,
  );
  try {
    context.peer = source?.peer;
    await loadWindow(context);
  } catch (error) {
    context.window.destroy();
    throw error;
  }
}

function focusWindow(source: WindowContext, direction: unknown): void {
  if (direction !== "next" && direction !== "previous")
    throw new HostError("invalid_input", "Invalid window direction.");
  const contexts = windowContexts();
  const index = contexts.indexOf(source);
  if (index < 0 || contexts.length < 2) return;
  const target =
    contexts[
      (index + (direction === "next" ? 1 : -1) + contexts.length) %
        contexts.length
    ]!;
  if (target.window.isMinimized()) target.window.restore();
  target.window.show();
  target.window.focus();
}

function failMove(
  move: TabMove,
  error: unknown = new HostError(
    "cancelled",
    "Tab move cancelled; the original tab was retained.",
  ),
): void {
  if (move.settled) return;
  move.settled = true;
  clearTimeout(move.timer);
  move.source.outgoingMove = undefined;
  move.target.incomingMove = undefined;
  move.target.incoming = undefined;
  move.reject(error);
  // Only this provisional window is discarded. The source attachment remains untouched.
  if (!move.target.window.isDestroyed()) move.target.window.destroy();
}

async function moveTab(
  source: WindowContext,
  id: unknown,
  inputPaused: unknown,
  alias: unknown,
): Promise<void> {
  identifier(id);
  if (
    typeof inputPaused !== "boolean" ||
    (alias !== undefined && !isTabAlias(alias))
  )
    throw new HostError("invalid_input", "Invalid tab state.");
  if (source.outgoingMove || source.files.busy || source.sshFiles.busy)
    throw new HostError(
      "busy",
      "Finish the current file operation or tab move first.",
    );
  const sourceHost = currentHost(source);
  const description = sourceHost.describeAttachment(id);
  const target = makeWindow(
    false,
    source.menu.languagePreference ?? source.menu.locale,
    source.endpoint,
  );
  target.peer = source.peer;
  const completed = new Promise<void>((resolve, reject) => {
    const move: TabMove = {
      id: randomUUID(),
      source,
      target,
      sourceAttachment: id,
      resolve,
      reject,
      settled: false,
      timer: setTimeout(
        () =>
          failMove(
            move,
            new HostError(
              "cancelled",
              "The destination window did not become ready; the original tab was retained.",
            ),
          ),
        20000,
      ),
    };
    source.outgoingMove = move;
    target.incomingMove = move;
    void (async () => {
      // Source writes are blocked at IPC while accepted writes settle. Never replay them.
      await sourceHost.settleWrites(id);
      if (move.settled) return;
      // Reuse the authenticated Host endpoint; moving never invokes device creation or startup.
      target.host = new HostClient(
        sourceHost.info().endpoint,
        source.peer ? await peerToken(source.peer) : await loadToken(),
      );
      if (move.settled) {
        await target.host.disconnect();
        return;
      }
      await target.host.connect();
      if (move.settled) return;
      const attachment = await target.host.attach(
        description.attachment.sessionId,
        description.settings,
      );
      if (move.settled) return;
      target.connected = true;
      move.targetAttachment = attachment.id;
      target.incoming = {
        moveId: move.id,
        attachment,
        settings: description.settings,
        alias,
        inputPaused,
      };
      await loadWindow(target);
    })().catch((error: unknown) => failMove(move, error));
  });
  refreshMenus();
  return completed;
}

async function acceptMovedTab(
  target: WindowContext,
  moveId: unknown,
  id: unknown,
): Promise<void> {
  identifier(moveId);
  identifier(id);
  const move = target.incomingMove;
  if (
    !move ||
    move.settled ||
    move.id !== moveId ||
    move.targetAttachment !== id
  )
    throw new HostError("cancelled", "This tab move is no longer pending.");
  const sourceHost = currentHost(move.source);
  sourceHost.validateAttachment(move.sourceAttachment);
  currentHost(target).validateAttachment(id);
  if (move.source.files.busy || move.source.sshFiles.busy) {
    failMove(
      move,
      new HostError(
        "busy",
        "Finish the file operation before moving this tab.",
      ),
    );
    throw new HostError("cancelled", "Tab move cancelled.");
  }
  // Commit before asynchronous detach: a source-window close can no longer revoke the new owner.
  move.settled = true;
  clearTimeout(move.timer);
  move.source.outgoingMove = undefined;
  target.incomingMove = undefined;
  target.incoming = undefined;
  await sourceHost.detach(move.sourceAttachment);
  if (!target.window.isDestroyed()) {
    target.window.show();
    target.window.focus();
  }
  move.resolve();
  refreshMenus();
}

function createHandlers(context: WindowContext): Record<string, Handler> {
  function directoryWindows() {
    return windowContexts().filter(
      (other) =>
        other !== context &&
        other.endpoint === context.endpoint &&
        other.peer?.id === context.peer?.id,
    );
  }
  let copyingTarget = false;
  let deletingLog = false;
  const handlers: Record<string, (...args: unknown[]) => unknown> = {
    [channels.ubootStatus]: () => context.uboot.snapshot(),
    [channels.cancelUBoot]: (id) => context.uboot.cancel(id),
    [channels.startUBoot]: async (id, request) => {
      identifier(id);
      if (!isUBootRequest(request))
        throw new HostError(
          "invalid_input",
          "Provide verified board settings.",
        );
      if (
        context.ubootConfirming ||
        context.uboot.busy ||
        context.debugSending ||
        context.savingEndpoint ||
        context.outgoingMove?.sourceAttachment === id ||
        desktop.quitting
      )
        throw new HostError(
          "busy",
          "The target is changing or an operation is pending.",
        );
      const host = currentHost(context);
      context.ubootConfirming = true;
      try {
        // Completion rotates the Host's control version. Capture the current
        // version before confirmation, even if the sidebar has not polled yet.
        await host.list();
        const prepared = host.prepareUBoot(id, request);
        const i18n = setupI18n({
          locale: context.menu.locale,
          messages: menuCatalogs,
        });
        const details = [
          prepared.sessionId,
          `${i18n._("uboot.mode")}: ${i18n._(request.mode === "reboot" ? "uboot.reboot" : "uboot.waitReset")}`,
          ...(request.mode === "reboot"
            ? [`${i18n._("uboot.command")}: ${request.rebootCommand}`]
            : []),
          `${i18n._("uboot.countdown")}: ${JSON.stringify(request.countdown)}`,
          `${i18n._("uboot.prompt")}: ${JSON.stringify(request.prompt)}`,
          `${i18n._("uboot.bootStarted")}: ${JSON.stringify(request.bootStarted)}`,
          `${i18n._("uboot.interrupt")}: ${request.interruptHex}`,
          `${i18n._("uboot.timeout")}: ${request.timeoutMS}`,
          ...(request.capture ? [i18n._("uboot.capture")] : []),
          i18n._("uboot.noRetry"),
        ].join("\n");
        const answer = await dialog.showMessageBox(context.window, {
          type: "warning",
          message: i18n._("uboot.confirm"),
          detail: details,
          buttons: [i18n._("connection.cancel"), i18n._("uboot.start")],
          defaultId: 0,
          cancelId: 0,
        });
        if (answer.response !== 1) return;
        if (
          context.disposed ||
          currentHost(context) !== host ||
          context.outgoingMove?.sourceAttachment === id
        )
          throw new HostError("cancelled", "U-Boot target changed.");
        return context.uboot.start(id, prepared);
      } finally {
        context.ubootConfirming = false;
      }
    },
    [channels.snippets]: () => commandSnippets.list(),
    [channels.changeSnippet]: (change) => commandSnippets.change(change),
    [channels.debugSend]: async (id, payload) => {
      identifier(id);
      if (!isDebugPayload(payload))
        throw new HostError("invalid_input", "Invalid debug input.");
      if (
        context.ubootConfirming ||
        context.debugSending ||
        context.savingEndpoint ||
        context.outgoingMove?.sourceAttachment === id ||
        desktop.quitting
      )
        throw new HostError(
          "busy",
          "The target is changing or another send is pending.",
        );
      const host = currentHost(context);
      const prepared = host.prepareDebugWrite(id, payload);
      context.debugSending = true;
      try {
        const i18n = setupI18n({
          locale: context.menu.locale,
          messages: menuCatalogs,
        });
        const answer = await dialog.showMessageBox(context.window, {
          type: "warning",
          message: i18n._("send.confirm"),
          detail: `${prepared.sessionId} · ${prepared.encoding} · ${prepared.bytes} B\n\n${prepared.hex}\n\n${i18n._("send.noRetry")}`,
          buttons: [i18n._("connection.cancel"), i18n._("send.send")],
          defaultId: 0,
          cancelId: 0,
        });
        if (answer.response !== 1) return { sent: false, bytes: 0 };
        if (
          context.disposed ||
          currentHost(context) !== host ||
          context.outgoingMove?.sourceAttachment === id
        )
          throw new HostError("cancelled", "Debug input target changed.");
        return { sent: true, bytes: await prepared.send() };
      } finally {
        context.debugSending = false;
      }
    },
    [channels.mcpSetupClients]: () => context.mcpSetup.discover(),
    [channels.mcpSetupRecover]: () => context.mcpSetup.recover(),
    [channels.mcpSetup]: (request) => {
      if (!isMCPSetupRequest(request))
        throw new HostError("invalid_input", "Invalid client setup request.");
      if (
        context.disposed ||
        context.savingEndpoint ||
        context.closingRecordings ||
        desktop.quitting
      )
        throw new HostError("busy", "The window is changing or closing.");
      return context.mcpSetup.perform(
        request,
        context.endpoint,
        async () => {
          const picked = await dialog.showOpenDialog(context.window, {
            properties: ["openDirectory"],
          });
          return picked.canceled ? undefined : picked.filePaths[0];
        },
        async (preview, inspect) => {
          const i18n = setupI18n({
            locale: context.menu.locale,
            messages: menuCatalogs,
          });
          const answer = await dialog.showMessageBox(context.window, {
            type: inspect ? "info" : "warning",
            message: i18n._(inspect ? "setup.inspect" : "setup.confirm"),
            detail: [
              preview.path,
              i18n._("setup.fallback"),
              i18n._("setup.normalization"),
              i18n._("setup.secret"),
              preview.existing && !request.replace
                ? i18n._("setup.preserveNotice")
                : i18n._("setup.newCredential"),
              i18n._("setup.before"),
              preview.before,
              i18n._("setup.after"),
              preview.after,
            ].join("\n\n"),
            buttons: inspect
              ? [i18n._("diagnostics.close")]
              : [i18n._("connection.cancel"), i18n._("setup.apply")],
            defaultId: 0,
            cancelId: 0,
          });
          return !inspect && answer.response === 1;
        },
      );
    },
    [channels.recordings]: () => context.recordings.list(),
    [channels.startRecording]: (request) => {
      if (!isRecordingRequest(request))
        throw new HostError("invalid_input", "Invalid recording settings.");
      return context.recordings.start(
        currentHost(context),
        request,
        async () => {
          const selected = await dialog.showOpenDialog(context.window, {
            properties: ["openDirectory", "createDirectory"],
          });
          return selected.canceled ? undefined : selected.filePaths[0];
        },
      );
    },
    [channels.stopRecording]: (id) => {
      identifier(id);
      return context.recordings.stop(id);
    },
    [channels.deleteRecordingFile]: async () => {
      if (deletingLog)
        throw new HostError("busy", "A log file selection is already open.");
      deletingLog = true;
      try {
        const selected = await dialog.showOpenDialog(context.window, {
          properties: ["openFile"],
          filters: [
            { name: "Log", extensions: ["bin", "txt", "jsonl", "json"] },
          ],
        });
        if (selected.canceled || selected.filePaths.length !== 1) return false;
        const path = resolve(selected.filePaths[0]!);
        const before = await lstat(path);
        if (!before.isFile())
          throw new HostError(
            "invalid_input",
            "Choose one ordinary local log file.",
          );
        const canonical = async (value: string) => {
          const resolved = await realpath(value);
          return process.platform === "win32"
            ? resolved.toLowerCase()
            : resolved;
        };
        const active = async () => {
          const selectedPath = await canonical(path);
          for (const window of desktop.windows.values()) {
            for (const record of window.recordings.list()) {
              if (
                ["starting", "recording", "stopping"].includes(record.state) &&
                record.directory &&
                selectedPath.startsWith(
                  (await canonical(record.directory)) + sep,
                )
              )
                return true;
            }
          }
          return false;
        };
        if (await active())
          throw new HostError(
            "busy",
            "Stop the recording before deleting its files.",
          );
        const i18n = setupI18n({
          locale: context.menu.locale,
          messages: menuCatalogs,
        });
        const answer = await dialog.showMessageBox(context.window, {
          type: "warning",
          message: i18n._("logs.deleteConfirm"),
          detail: path,
          buttons: [i18n._("connection.cancel"), i18n._("logs.trash")],
          defaultId: 0,
          cancelId: 0,
        });
        if (answer.response !== 1) return false;
        const after = await lstat(path);
        if (
          (await active()) ||
          !after.isFile() ||
          before.dev !== after.dev ||
          before.ino !== after.ino ||
          before.size !== after.size ||
          before.mtimeMs !== after.mtimeMs
        )
          throw new HostError(
            "busy",
            "The selected file changed. Select it again.",
          );
        await shell.trashItem(path);
        return true;
      } catch (error) {
        if (error instanceof HostError) throw error;
        throw new HostError(
          "tool_error",
          "Could not move the selected log file to trash.",
        );
      } finally {
        deletingLog = false;
      }
    },
    [channels.saveDiagnostics]: (request) => {
      if (!isDiagnosticRequest(request))
        throw new HostError("invalid_input", "Invalid diagnostic request.");
      return context.diagnostics.save(
        currentHost(context),
        request,
        async () => {
          const selected = await dialog.showSaveDialog(context.window, {
            defaultPath: "channelterm-diagnostics.zip",
            filters: [{ name: "ZIP", extensions: ["zip"] }],
          });
          return selected.canceled ? undefined : selected.filePath;
        },
      );
    },
    [channels.cancelDiagnostics]: () => context.diagnostics.cancel(),
    [channels.browseSSH]: (request) => {
      if (context.files.busy || context.sshFiles.busy)
        throw new HostError("busy", "Finish the current file operation first.");
      return context.sshBrowser.browse(currentHost(context), request);
    },
    [channels.cancelSSHBrowse]: () => context.sshBrowser.cancel(),
    [channels.manageSSH]: (request) => {
      if (
        context.files.busy ||
        context.sshFiles.busy ||
        context.savingEndpoint ||
        desktop.quitting
      )
        throw new HostError("busy", "Finish the current file operation first.");
      const selectedHost = currentHost(context);
      return context.sshBrowser.manage(
        selectedHost,
        request,
        async (intent) => {
          const i18n = setupI18n({
            locale: context.menu.locale,
            messages: menuCatalogs,
          });
          const operationLabel = "sshManage." + intent.operation;
          const answer = await dialog.showMessageBox(context.window, {
            type: "warning",
            message: i18n._("sshManage.confirm"),
            detail: `${intent.sessionId} · ${intent.endpoint}\n${i18n._(operationLabel)}\n${intent.path}${intent.target ? `\n→ ${intent.target}` : ""}\n\n${i18n._("sshManage.boundary")}`,
            buttons: [i18n._("connection.cancel"), i18n._("sshManage.apply")],
            defaultId: 0,
            cancelId: 0,
          });
          return (
            answer.response === 1 &&
            !context.disposed &&
            currentHost(context) === selectedHost
          );
        },
      );
    },
    [channels.copyAITarget]: async (id, locale) => {
      identifier(id);
      if (copyingTarget)
        throw new HostError("busy", "Target copy is already pending.");
      copyingTarget = true;
      try {
        await copyAITarget(currentHost(context), id, locale, (text) =>
          clipboard.writeText(text),
        );
      } finally {
        copyingTarget = false;
      }
    },
    [channels.readClipboard]: async (id) => {
      identifier(id);
      const selectedHost = currentHost(context);
      selectedHost.validateAttachment(id);
      const text = await clipboard.readText();
      // Native clipboard reads are asynchronous; detach/reconnect revokes the request.
      currentHost(context).validateAttachment(id);
      if (text) input(text);
      return text;
    },
    [channels.writeClipboard]: (id, text) => {
      identifier(id);
      input(text);
      currentHost(context).validateAttachment(id);
      return clipboard.writeText(text);
    },
    [channels.connectionPolicies]: (change) =>
      currentHost(context).connectionPolicies(change),
    [channels.peers]: () => {
      if (!peerListing)
        peerListing = listPeers().finally(() => {
          peerListing = undefined;
        });
      return peerListing;
    },
    [channels.attachPeer]: async (id, sessionId) => {
      identifier(id);
      identifier(sessionId);
      const peer = (await configuredPeers()).find((row) => row.id === id);
      if (!peer)
        throw new HostError("configuration", "Unknown configured Host.");
      const target = makeWindow(
        true,
        context.menu.languagePreference,
        peer.endpoint,
      );
      target.peer = peer;
      target.initialSession = sessionId;
      try {
        await loadWindow(target);
      } catch (e) {
        target.window.destroy();
        throw e;
      }
    },
    [channels.publishTabs]: (rows) => {
      if (!Array.isArray(rows) || rows.length > 32)
        throw new HostError("invalid_input", "Invalid tab list.");
      context.rows = rows.map((row) => {
        if (
          !row ||
          typeof row !== "object" ||
          ![
            row.id,
            row.reference,
            row.endpoint,
            row.label,
            row.transport,
            row.connectionLabel,
          ].every(
            (v) =>
              typeof v === "string" &&
              v.length <= 1024 &&
              !/[\x00-\x1f\x7f]/.test(v),
          )
        )
          throw new HostError("invalid_input", "Invalid tab metadata.");
        identifier(row.id);
        return {
          id: row.id,
          label: row.label,
          transport: row.transport,
          connectionLabel: row.connectionLabel,
          endpoint: row.endpoint,
          reference: row.reference,
        };
      });
    },
    [channels.openedTabs]: () => directoryWindows().flatMap((c) => c.rows),
    [channels.focusTab]: (id) => {
      identifier(id);
      const target = directoryWindows().find((c) =>
        c.rows.some((row) => row.id === id),
      );
      if (!target) throw new HostError("closed", "Tab has closed.");
      if (target.window.isMinimized()) target.window.restore();
      target.window.show();
      target.window.focus();
      target.window.webContents.send(channels.menuAction, {
        id: "tab.select",
        tabId: id,
      });
    },
    [channels.windowInfo]: (): WindowInfo => ({
      count: Math.max(1, windowContexts().length),
      workspaceSlot: context.workspaceSlot,
      initialSession: context.initialSession,
      peer: !!context.peer,
      connected: context.connected,
      autoConnect: context.autoConnect,
      languagePreference: context.languagePreference,
      incoming: context.incoming,
    }),
    [channels.newWindow]: () => newWindow(context),
    [channels.closeWindow]: () => {
      setImmediate(() => {
        if (!context.disposed) context.window.close();
      });
    },
    [channels.focusWindow]: (direction) => focusWindow(context, direction),
    [channels.moveTab]: (id, inputPaused, alias) =>
      moveTab(context, id, inputPaused, alias),
    [channels.acceptMovedTab]: (moveId, id) =>
      acceptMovedTab(context, moveId, id),
    [channels.info]: () => hostInfo(context),
    [channels.setBackgroundMCP]: async (enabled) => {
      if (typeof enabled !== "boolean")
        throw new HostError(
          "invalid_input",
          "Expected a background MCP setting.",
        );
      return currentHost(context).setBackgroundMCP(enabled);
    },
    [channels.hostSettings]: async () => {
      if (context.connected) await currentHost(context).refreshIdlePolicy();
      const version = await cliVersion;
      return {
        host: hostInfo(context),
        connected: context.connected,
        environmentOverride: hostSettings.locked,
        desktopVersion: displayVersion(app.getVersion()),
        electronVersion: displayVersion(process.versions.electron),
        cliVersion: version,
      };
    },
    [channels.saveHostEndpoint]: async (value) => {
      validateEndpoint(value);
      if (
        context.savingEndpoint ||
        context.ubootConfirming ||
        context.debugSending ||
        context.mcpSetup.busy ||
        context.recordings.choosing ||
        context.connecting ||
        context.files.busy ||
        context.sshFiles.busy ||
        context.sshBrowser.busy ||
        context.outgoingMove
      )
        throw new HostError(
          "busy",
          "Finish or cancel the current operation before changing Host settings.",
        );
      context.savingEndpoint = true;
      context.host?.discardQueuedInput();
      try {
        const endpoint = await hostSettings.save(value);
        await disconnectHost(context);
        context.endpoint = endpoint;
        context.autoConnect = false;
        return hostInfo(context);
      } finally {
        context.savingEndpoint = false;
      }
    },
    [channels.connect]: async () => {
      if (
        context.ubootConfirming ||
        context.debugSending ||
        context.mcpSetup.busy ||
        context.recordings.choosing ||
        context.files.busy ||
        context.sshFiles.busy ||
        context.sshBrowser.busy
      )
        throw new HostError(
          "busy",
          "Finish or cancel the file operation before reconnecting.",
        );
      if (context.connecting || context.savingEndpoint)
        throw new HostError("invalid_input", "Connection already in progress.");
      context.connecting = true;
      try {
        const cleanup = disconnectHost(context);
        const version = context.connectionVersion;
        await cleanup;
        const controller = new AbortController();
        context.startupController = controller;
        const endpoint = hostEndpoint(context.endpoint).href;
        if (
          version !== context.connectionVersion ||
          context.disposed ||
          desktop.quitting
        )
          throw new HostError("cancelled", "Connection cancelled.");
        const peer = (await configuredPeers()).find(
          (record) => record.endpoint === endpoint,
        );
        if (context.peer && (!peer || peer.id !== context.peer.id))
          throw new HostError(
            "configuration",
            "The configured Host has changed.",
          );
        context.peer = peer;
        if (!peer) await desktop.startup.ensure(endpoint, controller.signal);
        const token = peer ? await peerToken(peer) : await loadToken();
        if (
          version !== context.connectionVersion ||
          context.disposed ||
          desktop.quitting
        )
          throw new HostError("cancelled", "Connection cancelled.");
        const next = new HostClient(endpoint, token);
        context.host = next;
        const info = await next.connect();
        if (
          version !== context.connectionVersion ||
          context.disposed ||
          desktop.quitting
        ) {
          await next.disconnect();
          throw new HostError("cancelled", "Connection cancelled.");
        }
        context.connected = true;
        return info;
      } finally {
        context.connecting = false;
      }
    },
    [channels.disconnect]: () => {
      if (
        context.ubootConfirming ||
        context.debugSending ||
        context.mcpSetup.busy ||
        context.files.busy ||
        context.sshFiles.busy ||
        context.recordings.choosing
      )
        throw new HostError(
          "busy",
          "Finish or cancel the file operation before disconnecting.",
        );
      return disconnectHost(context);
    },
    [channels.chooseTransferPath]: async (id, direction, kind) => {
      identifier(id);
      if (
        (direction !== "send" && direction !== "receive") ||
        (kind !== "file" && kind !== "directory") ||
        !context.window
      )
        throw new HostError("invalid_input", "Invalid file selection.");
      const selectedHost = currentHost(context);
      await selectedHost.validateFileSession(id);
      const result = await dialog.showOpenDialog(context.window, {
        properties: [
          direction === "receive" || kind === "directory"
            ? "openDirectory"
            : "openFile",
        ],
      });
      if (result.canceled || !result.filePaths[0]) return undefined;
      if (context.host !== selectedHost)
        throw new HostError("cancelled", "Host changed during file selection.");
      await selectedHost.validateFileSession(id);
      return context.files.remember(result.filePaths[0], id, direction, kind);
    },
    [channels.selectDroppedFiles]: (id, paths) => {
      identifier(id);
      if (context.sshFiles.busy || context.sshBrowser.busy)
        throw new HostError("busy", "Finish the SSH file operation first.");
      return context.files.selectDropped(currentHost(context), id, paths);
    },
    [channels.transferPreference]: () => context.files.preference(),
    [channels.saveTransferPreference]: async (mode) => {
      if (mode !== "ask" && mode !== "tar" && mode !== "files")
        throw new HostError("invalid_input", "Invalid directory preference.");
      await context.files.preference(mode);
    },
    [channels.browseDirectory]: (id, path) => {
      if (context.sshFiles.busy)
        throw new HostError("busy", "Finish the SSH file operation first.");
      identifier(id);
      return context.files.browse(currentHost(context), id, path);
    },
    [channels.startTransfer]: (request) => {
      if (context.sshFiles.busy)
        throw new HostError("busy", "Finish the SSH file operation first.");
      return context.files.transfer(currentHost(context), request);
    },
    [channels.startSSHTransfer]: (request) => {
      if (
        context.files.busy ||
        context.sshFiles.busy ||
        context.sshBrowser.busy
      )
        throw new HostError("busy", "Finish the current file operation first.");
      const selectedHost = currentHost(context);
      return context.sshFiles.transfer(
        selectedHost,
        request,
        (r) =>
          context.files.takeSelection(r.sessionId, r.selectionId, r.direction),
        async (intent) => {
          const i18n = setupI18n({
            locale: context.menu.locale,
            messages: menuCatalogs,
          });
          const answer = await dialog.showMessageBox(context.window, {
            type: "warning",
            message: i18n._("file.overwriteConfirm"),
            detail: `${intent.sessionId} · ${intent.endpoint}\n${intent.source}\n→ ${intent.destination}\n\n${i18n._(intent.safeReplace ? "file.safeReplaceHint" : "file.overwriteHint")}`,
            buttons: [i18n._("connection.cancel"), i18n._("sshManage.apply")],
            defaultId: 0,
            cancelId: 0,
          });
          return (
            answer.response === 1 &&
            !context.disposed &&
            currentHost(context) === selectedHost
          );
        },
      );
    },
    [channels.sshTransfers]: () => {
      const jobs = context.sshFiles.snapshots();
      return context.host?.decorateTransfers(jobs) ?? jobs;
    },
    [channels.interruptSSHTransfer]: (id) =>
      currentHost(context).interruptSSHTransfer(id),
    [channels.managedTransfers]: () => currentHost(context).managedTransfers(),
    [channels.cancelSSHTransfer]: (id) => context.sshFiles.cancel(id),
    [channels.transferCheckpoints]: (id) => {
      identifier(id);
      return context.files.checkpoints(currentHost(context), id);
    },
    [channels.resumeTransfer]: (id, checkpointId) => {
      if (context.sshFiles.busy)
        throw new HostError("busy", "Finish the SSH file operation first.");
      identifier(id);
      return context.files.resume(currentHost(context), id, checkpointId);
    },
    [channels.transfers]: (id) => {
      identifier(id);
      return currentHost(context).transfers(id);
    },
    [channels.cancelTransfer]: (id, transferId) => {
      identifier(id);
      identifier(transferId);
      return context.transferConfirmation.begin(
        currentHost(context),
        id,
        transferId,
      );
    },
    [channels.resolveTransferCancel]: (id, confirm) =>
      context.transferConfirmation.resolve(id, confirm),
    [channels.list]: () => currentHost(context).list(),
    [channels.control]: (request) => currentHost(context).control(request),
    [channels.debugOverview]: () => currentHost(context).debugOverview(),
    [channels.serialProfiles]: () => currentHost(context).serialProfiles(),
    [channels.changeSerialProfile]: (request) =>
      currentHost(context).changeSerialProfile(request),
    [channels.serialPorts]: () => currentHost(context).serialPorts(),
    [channels.devices]: () => currentHost(context).devices(),
    [channels.deviceDecision]: (selection) =>
      currentHost(context).deviceDecision(selection),
    [channels.sshTrustPrompt]: () => currentHost(context).sshTrust.current(),
    [channels.resolveSSHTrust]: (id, accept) =>
      currentHost(context).sshTrust.decide(id, accept),
    [channels.openConnection]: (request) =>
      currentHost(context).openConnection(request),
    [channels.resize]: (id, size) => {
      identifier(id);
      if (context.outgoingMove)
        throw new HostError("busy", "Tab move is in progress.");
      return currentHost(context).resize(id, size);
    },
    [channels.resizeControl]: (id, action) => {
      identifier(id);
      if (context.outgoingMove)
        throw new HostError("busy", "Tab move is in progress.");
      return currentHost(context).resizeControl(id, action);
    },
    [channels.attach]: async (id, settings) => {
      identifier(id);
      const attachment = await currentHost(context).attach(id, settings);
      if (context.initialSession === id) context.initialSession = undefined;
      return attachment;
    },
    [channels.detach]: (id) => {
      identifier(id);
      currentHost(context).validateAttachment(id);
      if (context.outgoingMove?.sourceAttachment === id)
        failMove(context.outgoingMove);
      return currentHost(context).detach(id);
    },
    [channels.closeSession]: (id) => {
      identifier(id);
      if (context.files.busy || context.sshFiles.busy || context.outgoingMove)
        throw new HostError(
          "busy",
          "Finish the file operation or tab move first.",
        );
      return currentHost(context).closeSession(id);
    },
    [channels.activity]: (id) => {
      identifier(id);
      return currentHost(context).activity(id);
    },
    [channels.read]: (id) => {
      identifier(id);
      return currentHost(context).read(id);
    },
    [channels.write]: (id, data) => {
      identifier(id);
      input(data);
      if (context.outgoingMove?.sourceAttachment === id)
        throw new HostError("busy", "This tab is moving; input was discarded.");
      return currentHost(context).write(id, data);
    },
    [channels.menuState]: (state) => {
      if (!isMenuState(state))
        throw new HostError("invalid_input", "Invalid menu state.");
      context.menu = {
        ...state,
        windowCount: Math.max(1, windowContexts().length),
      };
      if (context.window.isFocused() || windowContexts().length === 1)
        updateMenu(context);
    },
    [channels.quit]: requestQuit,
  };
  return handlers;
}

function registerIPC(): void {
  for (const channel of Object.values(channels).filter(
    (channel) =>
      ![
        channels.menuAction,
        channels.fileCloseBlocked,
        channels.windowCount,
      ].includes(channel as typeof channels.menuAction),
  )) {
    ipcMain.handle(
      channel,
      async (event, ...args: unknown[]): Promise<Result<unknown>> => {
        try {
          const context = desktop.windows.get(event.sender.id);
          validateSender(event, context?.window.webContents, development);
          if (!context || context.disposed)
            throw new HostError("unauthorized", "Unknown window.");
          const handler = context.handlers[channel];
          if (!handler)
            throw new HostError("invalid_input", "Unknown desktop operation.");
          if (
            context.incomingMove &&
            ![
              channels.windowInfo,
              channels.publishTabs,
              channels.info,
              channels.list,
              channels.read,
              channels.menuState,
              channels.acceptMovedTab,
            ].includes(channel as typeof channels.read)
          )
            throw new HostError("busy", "The incoming tab is not ready yet.");
          if (
            context.peer &&
            [
              channels.saveHostEndpoint,
              channels.setBackgroundMCP,
              channels.mcpSetup,
              channels.manageSSH,
              channels.browseSSH,
              channels.startTransfer,
              channels.startSSHTransfer,
              channels.resumeTransfer,
              channels.changeSerialProfile,
              channels.openConnection,
            ].includes(channel as typeof channels.startTransfer)
          )
            throw new HostError(
              "unauthorized",
              "This action requires the local Host window.",
            );
          const expected = [
            channels.attachPeer,
            channels.resize,
            channels.resizeControl,
            channels.acceptMovedTab,
            channels.attach,
            channels.write,
            channels.debugSend,
            channels.startUBoot,
            channels.writeClipboard,
            channels.copyAITarget,
            channels.browseDirectory,
            channels.cancelTransfer,
            channels.resolveTransferCancel,
            channels.resolveSSHTrust,
            channels.resumeTransfer,
            channels.selectDroppedFiles,
          ].includes(channel as typeof channels.write)
            ? 2
            : channel === channels.chooseTransferPath ||
                channel === channels.moveTab
              ? 3
              : [
                    channels.connectionPolicies,
                    channels.publishTabs,
                    channels.focusTab,
                    channels.control,
                    channels.cancelUBoot,
                    channels.startRecording,
                    channels.mcpSetup,
                    channels.changeSnippet,
                    channels.stopRecording,
                    channels.saveHostEndpoint,
                    channels.setBackgroundMCP,
                    channels.focusWindow,
                    channels.detach,
                    channels.closeSession,
                    channels.read,
                    channels.activity,
                    channels.saveDiagnostics,
                    channels.browseSSH,
                    channels.manageSSH,
                    channels.readClipboard,
                    channels.menuState,
                    channels.openConnection,
                    channels.changeSerialProfile,
                    channels.deviceDecision,
                    channels.startTransfer,
                    channels.startSSHTransfer,
                    channels.cancelSSHTransfer,
                    channels.interruptSSHTransfer,
                    channels.transfers,
                    channels.transferCheckpoints,
                    channels.saveTransferPreference,
                  ].includes(channel as typeof channels.detach)
                ? 1
                : 0;
          if (args.length !== expected)
            throw new HostError("invalid_input", "Unexpected IPC arguments.");
          return { ok: true, value: await handler(...args) };
        } catch (error) {
          return { ok: false, error: publicError(error) };
        }
      },
    );
  }
}

/** Register ownership before loading any renderer code. Hidden handoff windows stay provisional. */
function makeWindow(
  autoConnect = false,
  languagePreference?: LanguagePreference,
  endpoint?: string,
): WindowContext {
  if (endpoint === undefined) {
    try {
      endpoint = hostSettings.endpoint();
    } catch {
      // Keep the settings/recovery UI reachable without echoing invalid environment input.
      endpoint = "";
      autoConnect = false;
    }
  }
  if (desktop.quitting || desktop.windows.size >= 16)
    throw new HostError("busy", "Close a window before opening another.");
  const created = new BrowserWindow({
    title: "ChannelTerm",
    width: 1180,
    height: 780,
    minWidth: 760,
    minHeight: 500,
    backgroundColor: "#101318",
    show: false,
    autoHideMenuBar: true,
    webPreferences: {
      preload: join(__dirname, "../preload/index.cjs"),
      contextIsolation: true,
      sandbox: true,
      nodeIntegration: false,
      webSecurity: true,
      webviewTag: false,
    },
  });
  const context: WindowContext = {
    window: created,
    rows: [],
    workspaceSlot: Array.from({ length: 16 }, (_, index) => index).find(
      (slot) =>
        ![...desktop.windows.values()].some(
          (entry) => entry.workspaceSlot === slot,
        ),
    )!,
    connected: false,
    connecting: false,
    endpoint,
    savingEndpoint: false,
    disposed: false,
    connectionVersion: 0,
    startupController: new AbortController(),
    transferConfirmation: new TransferConfirmation(),
    files: new DesktopFiles(fileExecutable),
    sshFiles: new DesktopSSHFiles(fileExecutable),
    diagnostics: new DesktopDiagnostics(),
    uboot: new DesktopUBoot(),
    ubootConfirming: false,
    recordings: new DesktopRecordings(),
    mcpSetup: new DesktopMCPSetup(fileExecutable),
    closingRecordings: false,
    debugSending: false,
    sshBrowser: new DesktopSSHBrowser(fileExecutable),
    menu: { ...initialMenuState },
    handlers: {},
    autoConnect,
    languagePreference,
  };
  context.handlers = createHandlers(context);
  desktop.windows.set(created.webContents.id, context);
  created.webContents.setWindowOpenHandler(() => ({ action: "deny" }));
  created.webContents.on("will-navigate", (event) => event.preventDefault());
  created.webContents.on("will-attach-webview", (event) =>
    event.preventDefault(),
  );
  created.webContents.on("render-process-gone", () => {
    context.menu = { ...initialMenuState };
    void disconnectHost(context);
    refreshMenus();
  });
  let initialNavigation = true;
  created.webContents.on(
    "did-start-navigation",
    (_event, _url, _inPlace, mainFrame) => {
      if (!mainFrame) return;
      if (initialNavigation) {
        initialNavigation = false;
        return;
      }
      context.menu = { ...initialMenuState };
      void disconnectHost(context);
      refreshMenus();
    },
  );
  created.on("close", (event) => {
    if (
      context.ubootConfirming ||
      context.debugSending ||
      context.mcpSetup.busy ||
      context.files.busy ||
      context.sshFiles.busy ||
      context.recordings.choosing
    ) {
      event.preventDefault();
      created.webContents.send(channels.fileCloseBlocked);
      return;
    }
    if (context.recordings.active) {
      event.preventDefault();
      if (!context.closingRecordings) {
        context.closingRecordings = true;
        void context.recordings.stopAll().finally(() => {
          context.closingRecordings = false;
          if (!created.isDestroyed()) created.close();
        });
      }
    }
  });
  const contentsId = created.webContents.id;
  created.on("closed", () => {
    context.disposed = true;
    desktop.windows.delete(contentsId);
    void disconnectHost(context);
    refreshMenus();
  });
  created.on("focus", () => updateMenu(context));
  created.once("ready-to-show", () => {
    if (!context.incomingMove) created.show();
  });
  refreshMenus();
  return context;
}

function loadWindow(context: WindowContext): Promise<void> {
  return context.window.loadURL(development ? developmentURL : productionURL);
}

void app
  .whenReady()
  .then(async () => {
    await hostSettings.load();
    const desktopSession = session.defaultSession;
    desktopSession.setPermissionRequestHandler(
      (_contents, _permission, callback) => callback(false),
    );
    desktopSession.setPermissionCheckHandler(() => false);
    desktopSession.on("will-download", (event) => event.preventDefault());
    desktopSession.webRequest.onHeadersReceived((details, callback) =>
      callback({
        responseHeaders: {
          ...details.responseHeaders,
          "Content-Security-Policy": [
            contentPolicy(development, process.env.CHANNELTERM_GUI_DEV_NONCE),
          ],
        },
      }),
    );
    const root = resolve(__dirname, "../renderer");
    protocol.handle("channelterm", async (request) => {
      const url = new URL(request.url);
      const path = resolve(root, "." + decodeURIComponent(url.pathname));
      if (
        request.method !== "GET" ||
        url.host !== "app" ||
        !path.startsWith(root + sep)
      )
        return new Response(null, { status: 403 });
      const mime: Record<string, string> = {
        ".html": "text/html",
        ".js": "text/javascript",
        ".css": "text/css",
        ".woff2": "font/woff2",
      };
      if (!mime[extname(path)]) return new Response(null, { status: 404 });
      try {
        return new Response(await readFile(path), {
          headers: {
            "Content-Type": mime[extname(path)]!,
            "Content-Security-Policy": contentPolicy(false),
          },
        });
      } catch {
        return new Response(null, { status: 404 });
      }
    });
    registerIPC();
    await newWindow();
    app.on("activate", () => {
      if (!desktop.windows.size) void newWindow();
    });
  })
  .catch(() => {
    console.error("ChannelTerm desktop startup failed.");
    app.exit(1);
  });

app.on("window-all-closed", () => {
  if (process.platform !== "darwin") app.quit();
});
app.on("before-quit", (event) => {
  if (desktop.quitting) return;
  event.preventDefault();
  const contexts = [...desktop.windows.values()];
  const busy = contexts.filter(
    (context) =>
      context.ubootConfirming ||
      context.debugSending ||
      context.mcpSetup.busy ||
      context.files.busy ||
      context.sshFiles.busy ||
      context.recordings.choosing,
  );
  if (busy.length) {
    for (const context of busy)
      context.window.webContents.send(channels.fileCloseBlocked);
    return;
  }
  desktop.quitting = true;
  for (const context of contexts) void disconnectHost(context);
  // The last native window may already be gone while its presence end is in flight.
  void Promise.allSettled(desktop.disconnects).then(() => app.quit());
});
