import { contextBridge, ipcRenderer } from "electron";
import { channels, type DesktopAPI } from "../shared/api";

const api: DesktopAPI = {
  info: () => ipcRenderer.invoke(channels.info),
  connect: () => ipcRenderer.invoke(channels.connect),
  disconnect: () => ipcRenderer.invoke(channels.disconnect),
  list: () => ipcRenderer.invoke(channels.list),
  attach: (id) => ipcRenderer.invoke(channels.attach, id),
  detach: (id) => ipcRenderer.invoke(channels.detach, id),
  read: (id) => ipcRenderer.invoke(channels.read, id),
  write: (id, data) => ipcRenderer.invoke(channels.write, id, data),
};
contextBridge.exposeInMainWorld("channelterm", Object.freeze(api));
