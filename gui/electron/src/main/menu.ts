import {
  Menu,
  type BrowserWindow,
  type MenuItemConstructorOptions,
} from "electron";
import { setupI18n } from "@lingui/core";
import { channels } from "../shared/api";
import {
  applicationMenu,
  type MenuEntry,
  type MenuState,
} from "../shared/menus";
import { menuCatalogs } from "../locales/catalogs";

/** Rebuild native labels/checks from bounded display hints, never authorization. */
export function updateApplicationMenu(
  window: BrowserWindow,
  state: MenuState,
  quit: () => void,
): void {
  const i18n = setupI18n({ locale: state.locale, messages: menuCatalogs });
  const convert = (entries: MenuEntry[]): MenuItemConstructorOptions[] =>
    entries.map((entry) => ({
      id: entry.id,
      label: i18n._(entry.label),
      enabled: entry.enabled ?? true,
      type:
        entry.action?.id === "language.change"
          ? "radio"
          : entry.checked === undefined
            ? "normal"
            : "checkbox",
      checked: entry.checked,
      submenu: entry.children ? convert(entry.children) : undefined,
      // No roles, accelerators, or mnemonic markers: terminal/IME input belongs
      // to xterm. Native menu clicks use the same guarded renderer dispatcher.
      click: () => {
        // Quit belongs to Main and must still work after the last macOS window
        // closes or the renderer crashes. IPC quit uses this same handler.
        if (entry.action?.id === "app.quit") quit();
        else if (entry.action && !window.isDestroyed())
          window.webContents.send(channels.menuAction, entry.action);
      },
    }));
  const template = convert(applicationMenu(state));
  if (process.platform === "darwin")
    template.unshift({
      label: "ChannelTerm",
      submenu: convert([
        { id: "mac.quit", label: "app.quit", action: { id: "app.quit" } },
      ]),
    });
  Menu.setApplicationMenu(Menu.buildFromTemplate(template));
  // Keep the accepted renderer menu strip; do not add an Alt-activated second
  // menu bar on Windows/Linux. macOS retains its system application menu.
  if (process.platform !== "darwin" && !window.isDestroyed())
    window.setMenu(null);
}
