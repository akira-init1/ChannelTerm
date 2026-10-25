import { DesktopFiles, TransferConfirmation } from "./files";
import { DesktopSSHFiles } from "./ssh-files";
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
import { channels, type Result } from "../shared/api";
import { HostClient } from "./host/client";
import { hostEndpoint, loadToken } from "./host/config";
import {
  companionPath,
  defaultHostEndpoint,
  localHostStartup,
} from "./host/startup";
import { HostError, publicError } from "./host/errors";
import { identifier, input } from "./host/validation";
import { initialMenuState, isMenuState } from "../shared/menus";
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
let window: BrowserWindow | undefined;
let host: HostClient | undefined;
let connecting = false;
let quitting = false;
let connectionVersion = 0;
let startupController = new AbortController();
const startup = localHostStartup(
  companionPath(
    app.isPackaged ? process.resourcesPath : resolve(__dirname, ".."),
  ),
);

const transferConfirmation = new TransferConfirmation();
const files = new DesktopFiles(
  join(
    app.isPackaged ? process.resourcesPath : resolve(__dirname, ".."),
    "files",
    process.platform === "win32" ? "channelterm.exe" : "channelterm",
  ),
);
const sshFiles = new DesktopSSHFiles(
  join(
    app.isPackaged ? process.resourcesPath : resolve(__dirname, ".."),
    "files",
    process.platform === "win32" ? "channelterm.exe" : "channelterm",
  ),
);

function requestQuit(): void {
  // Return an IPC reply before normal before-quit cleanup closes the renderer.
  setImmediate(() => app.quit());
}

function disconnectHost(): Promise<void> {
  transferConfirmation.dismiss();
  files.clearSelections();
  sshFiles.disconnect();
  connectionVersion++;
  startupController.abort();
  return host?.disconnect() ?? Promise.resolve();
}

function currentHost(): HostClient {
  if (!host) throw new HostError("disconnected", "Connect to a Host first.");
  return host;
}

function registerIPC(): void {
  const handlers: Record<string, (...args: unknown[]) => unknown> = {
    [channels.readClipboard]: async (id) => {
      identifier(id);
      const selectedHost = currentHost();
      selectedHost.validateAttachment(id);
      const text = await clipboard.readText();
      // Native clipboard reads are asynchronous; detach/reconnect revokes the request.
      currentHost().validateAttachment(id);
      if (text) input(text);
      return text;
    },
    [channels.writeClipboard]: (id, text) => {
      identifier(id);
      input(text);
      currentHost().validateAttachment(id);
      return clipboard.writeText(text);
    },
    [channels.info]: () => ({
      endpoint: hostEndpoint(process.env.CHANNELTERM_GUI_ENDPOINT).href,
      lifetime: "unknown",
      autoConnect:
        hostEndpoint(process.env.CHANNELTERM_GUI_ENDPOINT).href ===
        defaultHostEndpoint,
    }),
    [channels.connect]: async () => {
      if (files.busy || sshFiles.busy)
        throw new HostError(
          "busy",
          "Finish or cancel the file operation before reconnecting.",
        );
      if (connecting)
        throw new HostError("invalid_input", "Connection already in progress.");
      connecting = true;
      try {
        const cleanup = disconnectHost();
        const version = connectionVersion;
        await cleanup;
        const controller = new AbortController();
        startupController = controller;
        const endpoint = hostEndpoint(
          process.env.CHANNELTERM_GUI_ENDPOINT,
        ).href;
        if (version !== connectionVersion || !window || quitting)
          throw new HostError("cancelled", "Connection cancelled.");
        await startup.ensure(endpoint, controller.signal);
        const token = await loadToken();
        if (version !== connectionVersion || !window || quitting)
          throw new HostError("cancelled", "Connection cancelled.");
        const next = new HostClient(endpoint, token);
        host = next;
        return await next.connect();
      } finally {
        connecting = false;
      }
    },
    [channels.disconnect]: () => {
      if (files.busy || sshFiles.busy)
        throw new HostError(
          "busy",
          "Finish or cancel the file operation before disconnecting.",
        );
      return disconnectHost();
    },
    [channels.chooseTransferPath]: async (id, direction, kind) => {
      identifier(id);
      if (
        (direction !== "send" && direction !== "receive") ||
        (kind !== "file" && kind !== "directory") ||
        !window
      )
        throw new HostError("invalid_input", "Invalid file selection.");
      const selectedHost = currentHost();
      await selectedHost.validateFileSession(id);
      const result = await dialog.showOpenDialog(window, {
        properties: [
          direction === "receive" || kind === "directory"
            ? "openDirectory"
            : "openFile",
        ],
      });
      if (result.canceled || !result.filePaths[0]) return undefined;
      if (host !== selectedHost)
        throw new HostError("cancelled", "Host changed during file selection.");
      await selectedHost.validateFileSession(id);
      return files.remember(result.filePaths[0], id, direction);
    },
    [channels.browseDirectory]: (id, path) => {
      if (sshFiles.busy)
        throw new HostError("busy", "Finish the SSH file operation first.");
      identifier(id);
      return files.browse(currentHost(), id, path);
    },
    [channels.startTransfer]: (request) => {
      if (sshFiles.busy)
        throw new HostError("busy", "Finish the SSH file operation first.");
      return files.transfer(currentHost(), request);
    },
    [channels.startSSHTransfer]: (request) => {
      if (files.busy || sshFiles.busy)
        throw new HostError("busy", "Finish the current file operation first.");
      return sshFiles.transfer(currentHost(), request, (r) =>
        files.takeSelection(r.sessionId, r.selectionId, r.direction),
      );
    },
    [channels.sshTransfers]: () => sshFiles.snapshots(),
    [channels.cancelSSHTransfer]: (id) => sshFiles.cancel(id),
    [channels.transferCheckpoints]: (id) => {
      identifier(id);
      return files.checkpoints(currentHost(), id);
    },
    [channels.resumeTransfer]: (id, checkpointId) => {
      if (sshFiles.busy)
        throw new HostError("busy", "Finish the SSH file operation first.");
      identifier(id);
      return files.resume(currentHost(), id, checkpointId);
    },
    [channels.transfers]: (id) => {
      identifier(id);
      return currentHost().transfers(id);
    },
    [channels.cancelTransfer]: (id, transferId) => {
      identifier(id);
      identifier(transferId);
      return transferConfirmation.begin(currentHost(), id, transferId);
    },
    [channels.resolveTransferCancel]: (id, confirm) =>
      transferConfirmation.resolve(id, confirm),
    [channels.list]: () => currentHost().list(),
    [channels.serialPorts]: () => currentHost().serialPorts(),
    [channels.openConnection]: (request) =>
      currentHost().openConnection(request),
    [channels.attach]: (id, settings) => {
      identifier(id);
      return currentHost().attach(id, settings);
    },
    [channels.detach]: (id) => {
      identifier(id);
      return currentHost().detach(id);
    },
    [channels.read]: (id) => {
      identifier(id);
      return currentHost().read(id);
    },
    [channels.write]: (id, data) => {
      identifier(id);
      input(data);
      return currentHost().write(id, data);
    },
    [channels.menuState]: (state) => {
      if (!isMenuState(state))
        throw new HostError("invalid_input", "Invalid menu state.");
      if (window) updateApplicationMenu(window, state, requestQuit);
    },
    [channels.quit]: requestQuit,
  };
  for (const [channel, handler] of Object.entries(handlers)) {
    ipcMain.handle(
      channel,
      async (event, ...args: unknown[]): Promise<Result<unknown>> => {
        try {
          validateSender(event, window?.webContents, development);
          const expected = [
            channels.attach,
            channels.write,
            channels.writeClipboard,
            channels.browseDirectory,
            channels.cancelTransfer,
            channels.resolveTransferCancel,
            channels.resumeTransfer,
          ].includes(channel as typeof channels.write)
            ? 2
            : channel === channels.chooseTransferPath
              ? 3
              : [
                    channels.detach,
                    channels.read,
                    channels.readClipboard,
                    channels.menuState,
                    channels.openConnection,
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

async function createWindow(): Promise<void> {
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
  window = created;
  updateApplicationMenu(created, initialMenuState, requestQuit);
  created.webContents.setWindowOpenHandler(() => ({ action: "deny" }));
  created.webContents.on("will-navigate", (event) => event.preventDefault());
  created.webContents.on("will-attach-webview", (event) =>
    event.preventDefault(),
  );
  // A reload/crash releases client requests just like a window close.
  created.webContents.on("render-process-gone", () => {
    updateApplicationMenu(created, initialMenuState, requestQuit);
    void disconnectHost();
  });
  created.webContents.on(
    "did-start-navigation",
    (_event, _url, _inPlace, mainFrame) => {
      if (mainFrame) {
        updateApplicationMenu(created, initialMenuState, requestQuit);
        void disconnectHost();
      }
    },
  );
  created.on("close", (event) => {
    if (files.busy || sshFiles.busy) {
      event.preventDefault();
      created.webContents.send(channels.fileCloseBlocked);
    }
  });
  created.on("closed", () => {
    updateApplicationMenu(created, initialMenuState, requestQuit);
    window = undefined;
    void disconnectHost();
  });
  created.once("ready-to-show", () => created.show());
  await created.loadURL(development ? developmentURL : productionURL);
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
    await createWindow();
    app.on("activate", () => {
      if (!window) void createWindow();
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
  if (quitting) return;
  event.preventDefault();
  if (files.busy || sshFiles.busy) {
    window?.webContents.send(channels.fileCloseBlocked);
    return;
  }
  quitting = true;
  void disconnectHost().finally(() => app.quit());
});
