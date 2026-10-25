import { contextBridge, ipcRenderer } from "electron";
import { channels, type DesktopAPI } from "../shared/api";
import { isAction } from "../shared/menus";

const api: DesktopAPI = {
  windowInfo: () => ipcRenderer.invoke(channels.windowInfo),
  newWindow: () => ipcRenderer.invoke(channels.newWindow),
  closeWindow: () => ipcRenderer.invoke(channels.closeWindow),
  focusWindow: (direction) =>
    ipcRenderer.invoke(channels.focusWindow, direction),
  moveTab: (id, inputPaused) =>
    ipcRenderer.invoke(channels.moveTab, id, inputPaused),
  acceptMovedTab: (moveId, id) =>
    ipcRenderer.invoke(channels.acceptMovedTab, moveId, id),
  onWindowCount: (listener) => {
    const receive = (_event: Electron.IpcRendererEvent, count: unknown) => {
      if (
        typeof count === "number" &&
        Number.isInteger(count) &&
        count >= 1 &&
        count <= 16
      )
        listener(count);
    };
    ipcRenderer.on(channels.windowCount, receive);
    return () => ipcRenderer.removeListener(channels.windowCount, receive);
  },
  readClipboard: (id) => ipcRenderer.invoke(channels.readClipboard, id),
  writeClipboard: (id, text) =>
    ipcRenderer.invoke(channels.writeClipboard, id, text),
  copyAITarget: (id, locale) =>
    ipcRenderer.invoke(channels.copyAITarget, id, locale),
  chooseTransferPath: (sessionId, direction, kind) =>
    ipcRenderer.invoke(channels.chooseTransferPath, sessionId, direction, kind),
  browseDirectory: (sessionId, path) =>
    ipcRenderer.invoke(channels.browseDirectory, sessionId, path),
  startTransfer: (request) =>
    ipcRenderer.invoke(channels.startTransfer, request),
  startSSHTransfer: (request) =>
    ipcRenderer.invoke(channels.startSSHTransfer, request),
  sshTransfers: () => ipcRenderer.invoke(channels.sshTransfers),
  cancelSSHTransfer: (id) => ipcRenderer.invoke(channels.cancelSSHTransfer, id),
  transferCheckpoints: (sessionId) =>
    ipcRenderer.invoke(channels.transferCheckpoints, sessionId),
  resumeTransfer: (sessionId, checkpointId) =>
    ipcRenderer.invoke(channels.resumeTransfer, sessionId, checkpointId),
  transfers: (sessionId) => ipcRenderer.invoke(channels.transfers, sessionId),
  cancelTransfer: (sessionId, transferId) =>
    ipcRenderer.invoke(channels.cancelTransfer, sessionId, transferId),
  resolveTransferCancel: (confirmationId, confirm) =>
    ipcRenderer.invoke(channels.resolveTransferCancel, confirmationId, confirm),
  info: () => ipcRenderer.invoke(channels.info),
  connect: () => ipcRenderer.invoke(channels.connect),
  disconnect: () => ipcRenderer.invoke(channels.disconnect),
  list: () => ipcRenderer.invoke(channels.list),
  serialProfiles: () => ipcRenderer.invoke(channels.serialProfiles),
  changeSerialProfile: (request) =>
    ipcRenderer.invoke(channels.changeSerialProfile, request),
  serialPorts: () => ipcRenderer.invoke(channels.serialPorts),
  sshTrustPrompt: () => ipcRenderer.invoke(channels.sshTrustPrompt),
  resolveSSHTrust: (id, accept) =>
    ipcRenderer.invoke(channels.resolveSSHTrust, id, accept),
  openConnection: (request) =>
    ipcRenderer.invoke(channels.openConnection, request),
  attach: (id, settings) => ipcRenderer.invoke(channels.attach, id, settings),
  detach: (id) => ipcRenderer.invoke(channels.detach, id),
  closeSession: (id) => ipcRenderer.invoke(channels.closeSession, id),
  activity: (id) => ipcRenderer.invoke(channels.activity, id),
  saveDiagnostics: (request) =>
    ipcRenderer.invoke(channels.saveDiagnostics, request),
  cancelDiagnostics: () => ipcRenderer.invoke(channels.cancelDiagnostics),
  browseSSH: (request) => ipcRenderer.invoke(channels.browseSSH, request),
  cancelSSHBrowse: () => ipcRenderer.invoke(channels.cancelSSHBrowse),
  read: (id) => ipcRenderer.invoke(channels.read, id),
  write: (id, data) => ipcRenderer.invoke(channels.write, id, data),
  syncMenu: (state) => ipcRenderer.invoke(channels.menuState, state),
  onMenuAction: (listener) => {
    // Never pass the Electron event or an arbitrary IPC subscription to React.
    const receive = (_event: Electron.IpcRendererEvent, action: unknown) => {
      if (isAction(action)) listener(action);
    };
    ipcRenderer.on(channels.menuAction, receive);
    return () => ipcRenderer.removeListener(channels.menuAction, receive);
  },
  quit: () => ipcRenderer.invoke(channels.quit),
  onFileCloseBlocked: (listener) => {
    // A fixed notification only; neither Electron events nor payloads cross the bridge.
    const receive = (_event: Electron.IpcRendererEvent, ...args: unknown[]) => {
      if (args.length === 0) listener();
    };
    ipcRenderer.on(channels.fileCloseBlocked, receive);
    return () => ipcRenderer.removeListener(channels.fileCloseBlocked, receive);
  },
};
contextBridge.exposeInMainWorld("channelterm", Object.freeze(api));
