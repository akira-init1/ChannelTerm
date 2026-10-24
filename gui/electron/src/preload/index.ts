import { contextBridge, ipcRenderer } from "electron";
import { channels, type DesktopAPI } from "../shared/api";
import { isAction } from "../shared/menus";

const api: DesktopAPI = {
  chooseTransferPath: (sessionId, direction, kind) =>
    ipcRenderer.invoke(channels.chooseTransferPath, sessionId, direction, kind),
  browseDirectory: (sessionId, path) =>
    ipcRenderer.invoke(channels.browseDirectory, sessionId, path),
  startTransfer: (request) =>
    ipcRenderer.invoke(channels.startTransfer, request),
  transfers: (sessionId) => ipcRenderer.invoke(channels.transfers, sessionId),
  cancelTransfer: (sessionId, transferId) =>
    ipcRenderer.invoke(channels.cancelTransfer, sessionId, transferId),
  resolveTransferCancel: (confirmationId, confirm) =>
    ipcRenderer.invoke(channels.resolveTransferCancel, confirmationId, confirm),
  info: () => ipcRenderer.invoke(channels.info),
  connect: () => ipcRenderer.invoke(channels.connect),
  disconnect: () => ipcRenderer.invoke(channels.disconnect),
  list: () => ipcRenderer.invoke(channels.list),
  serialPorts: () => ipcRenderer.invoke(channels.serialPorts),
  openConnection: (request) =>
    ipcRenderer.invoke(channels.openConnection, request),
  attach: (id) => ipcRenderer.invoke(channels.attach, id),
  detach: (id) => ipcRenderer.invoke(channels.detach, id),
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
};
contextBridge.exposeInMainWorld("channelterm", Object.freeze(api));
