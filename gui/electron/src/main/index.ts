import { DesktopFiles, TransferConfirmation } from "./files";
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
} from "electron";
import { readFile } from "node:fs/promises";
import { extname, join, resolve, sep } from "node:path";
import { channels, type Result, type WindowInfo } from "../shared/api";
import { randomUUID } from "node:crypto";
import { HostClient } from "./host/client";
import { hostEndpoint, loadToken } from "./host/config";
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
  disposed: boolean;
  connectionVersion: number;
  startupController: AbortController;
  transferConfirmation: TransferConfirmation;
  files: DesktopFiles;
  sshFiles: DesktopSSHFiles;
  menu: MenuState;
  handlers: Record<string, Handler>;
  autoConnect: boolean;
  languagePreference?: LanguagePreference;
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

const desktop = {
  windows: new Map<number, WindowContext>(),
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
  context.connectionVersion++;
  context.startupController.abort();
  context.connected = false;
  const host = context.host;
  context.host = undefined;
  return host?.disconnect() ?? Promise.resolve();
}

function currentHost(context: WindowContext): HostClient {
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
  );
  try {
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
): Promise<void> {
  identifier(id);
  if (typeof inputPaused !== "boolean")
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
  );
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
        await loadToken(),
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
  let copyingTarget = false;
  const handlers: Record<string, (...args: unknown[]) => unknown> = {
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
    [channels.windowInfo]: (): WindowInfo => ({
      count: Math.max(1, windowContexts().length),
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
    [channels.moveTab]: (id, inputPaused) => moveTab(context, id, inputPaused),
    [channels.acceptMovedTab]: (moveId, id) =>
      acceptMovedTab(context, moveId, id),
    [channels.info]: () => ({
      ...(context.host?.info() ?? {
        endpoint: hostEndpoint(process.env.CHANNELTERM_GUI_ENDPOINT).href,
        lifetime: "unknown",
      }),
      autoConnect:
        context.autoConnect ||
        hostEndpoint(process.env.CHANNELTERM_GUI_ENDPOINT).href ===
          defaultHostEndpoint,
    }),
    [channels.connect]: async () => {
      if (context.files.busy || context.sshFiles.busy)
        throw new HostError(
          "busy",
          "Finish or cancel the file operation before reconnecting.",
        );
      if (context.connecting)
        throw new HostError("invalid_input", "Connection already in progress.");
      context.connecting = true;
      try {
        const cleanup = disconnectHost(context);
        const version = context.connectionVersion;
        await cleanup;
        const controller = new AbortController();
        context.startupController = controller;
        const endpoint = hostEndpoint(
          process.env.CHANNELTERM_GUI_ENDPOINT,
        ).href;
        if (
          version !== context.connectionVersion ||
          context.disposed ||
          desktop.quitting
        )
          throw new HostError("cancelled", "Connection cancelled.");
        await desktop.startup.ensure(endpoint, controller.signal);
        const token = await loadToken();
        if (
          version !== context.connectionVersion ||
          context.disposed ||
          desktop.quitting
        )
          throw new HostError("cancelled", "Connection cancelled.");
        const next = new HostClient(endpoint, token);
        context.host = next;
        const info = await next.connect();
        context.connected = true;
        return info;
      } finally {
        context.connecting = false;
      }
    },
    [channels.disconnect]: () => {
      if (context.files.busy || context.sshFiles.busy)
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
      return context.files.remember(result.filePaths[0], id, direction);
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
      if (context.files.busy || context.sshFiles.busy)
        throw new HostError("busy", "Finish the current file operation first.");
      return context.sshFiles.transfer(currentHost(context), request, (r) =>
        context.files.takeSelection(r.sessionId, r.selectionId, r.direction),
      );
    },
    [channels.sshTransfers]: () => context.sshFiles.snapshots(),
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
    [channels.serialProfiles]: () => currentHost(context).serialProfiles(),
    [channels.changeSerialProfile]: (request) =>
      currentHost(context).changeSerialProfile(request),
    [channels.serialPorts]: () => currentHost(context).serialPorts(),
    [channels.sshTrustPrompt]: () => currentHost(context).sshTrust.current(),
    [channels.resolveSSHTrust]: (id, accept) =>
      currentHost(context).sshTrust.decide(id, accept),
    [channels.openConnection]: (request) =>
      currentHost(context).openConnection(request),
    [channels.attach]: (id, settings) => {
      identifier(id);
      return currentHost(context).attach(id, settings);
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
              channels.info,
              channels.list,
              channels.read,
              channels.menuState,
              channels.acceptMovedTab,
            ].includes(channel as typeof channels.read)
          )
            throw new HostError("busy", "The incoming tab is not ready yet.");
          const expected = [
            channels.moveTab,
            channels.acceptMovedTab,
            channels.attach,
            channels.write,
            channels.writeClipboard,
            channels.copyAITarget,
            channels.browseDirectory,
            channels.cancelTransfer,
            channels.resolveTransferCancel,
            channels.resolveSSHTrust,
            channels.resumeTransfer,
          ].includes(channel as typeof channels.write)
            ? 2
            : channel === channels.chooseTransferPath
              ? 3
              : [
                    channels.focusWindow,
                    channels.detach,
                    channels.closeSession,
                    channels.read,
                    channels.activity,
                    channels.readClipboard,
                    channels.menuState,
                    channels.openConnection,
                    channels.changeSerialProfile,
                    channels.startTransfer,
                    channels.startSSHTransfer,
                    channels.cancelSSHTransfer,
                    channels.transfers,
                    channels.transferCheckpoints,
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
): WindowContext {
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
    connected: false,
    connecting: false,
    disposed: false,
    connectionVersion: 0,
    startupController: new AbortController(),
    transferConfirmation: new TransferConfirmation(),
    files: new DesktopFiles(fileExecutable),
    sshFiles: new DesktopSSHFiles(fileExecutable),
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
    if (context.files.busy || context.sshFiles.busy) {
      event.preventDefault();
      created.webContents.send(channels.fileCloseBlocked);
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
    (context) => context.files.busy || context.sshFiles.busy,
  );
  if (busy.length) {
    for (const context of busy)
      context.window.webContents.send(channels.fileCloseBlocked);
    return;
  }
  desktop.quitting = true;
  void Promise.all(contexts.map(disconnectHost)).finally(() => app.quit());
});
