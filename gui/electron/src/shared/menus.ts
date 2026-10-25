import type { Attachment } from "./api";
import {
  defaultShortcuts,
  isShortcutBindings,
  shortcutLabel,
  type ShortcutBindings,
} from "./shortcuts";

export const locales = [
  "en",
  "de",
  "es",
  "fr",
  "ja",
  "ko",
  "ru",
  "zh-CN",
  "zh-TW",
] as const;
export type MenuLocale = (typeof locales)[number];
export type LanguagePreference = MenuLocale | "system";

/** Resolve supported browser/OS language tags without storing translated labels. */
export function systemMenuLocale(languages: readonly string[]): MenuLocale {
  for (const language of languages) {
    const tag = language.toLowerCase();
    const exactLocale = locales.find((locale) => locale.toLowerCase() === tag);
    if (exactLocale) return exactLocale;
    if (/^zh(?:-|$)/.test(tag))
      return /(?:hant|tw|hk|mo)/.test(tag) ? "zh-TW" : "zh-CN";
    const base = locales.find((locale) => locale === tag.split("-")[0]);
    if (base) return base;
  }
  return "en";
}

/** Finite window/tab actions shared by native and renderer menus. */
export const windowActions = [
  "window.new",
  "tab.new",
  "window.close",
  "tab.next",
  "tab.previous",
  "window.next",
  "window.previous",
] as const;
export type Action =
  | {
      id:
        | "host.connect"
        | "host.disconnect"
        | "connection.new"
        | "session.choose"
        | (typeof windowActions)[number]
        | "connection.serial"
        | "connection.ssh"
        | "session.refresh"
        | "view.sidebar"
        | "view.auxiliary"
        | "view.panel"
        | "view.transfers"
        | "view.status"
        | "view.activity"
        | "settings.shortcuts"
        | "settings.display"
        | "help.quick"
        | "app.quit";
    }
  | {
      id: "session.attach" | "file.send" | "file.receive" | "file.resume";
      sessionId: string;
    }
  | {
      id:
        | "tab.close"
        | "session.close"
        | "session.copyAI"
        | "tab.move"
        | "session.info"
        | "terminal.clear"
        | "terminal.focus"
        | "terminal.resume"
        | "terminal.copy"
        | "terminal.find"
        | "terminal.paste";
      attachment: Attachment;
    }
  | { id: "language.change"; locale: LanguagePreference }
  | { id: "tab.select" | "tab.closeEmpty"; tabId: string };

/** Display hints only. No menu snapshot grants device or lease authority. */
export interface MenuState {
  locale: MenuLocale;
  languagePreference?: LanguagePreference;
  connected: boolean;
  sshAvailable: boolean;
  busy: boolean;
  sidebarVisible: boolean;
  auxiliaryVisible: boolean;
  panelVisible: boolean;
  shortcuts: ShortcutBindings;
  attachment?: Attachment;
  tabId?: string;
  tabCount?: number;
  windowCount?: number;
  currentOpen: boolean;
  currentSerial: boolean;
  currentSSH?: boolean;
  inputPaused: boolean;
  hexView?: boolean;
  leased: boolean;
}

export interface MenuEntry {
  id: string;
  label: string;
  action?: Action;
  enabled?: boolean;
  checked?: boolean;
  children?: MenuEntry[];
  shortcut?: string;
}

export const initialMenuState: MenuState = {
  locale: "en",
  connected: false,
  sshAvailable: false,
  busy: false,
  sidebarVisible: true,
  auxiliaryVisible: false,
  panelVisible: false,
  shortcuts: defaultShortcuts(false),
  currentOpen: false,
  currentSerial: false,
  inputPaused: false,
  leased: false,
};

function attachedAction(
  state: MenuState,
  id:
    | "tab.close"
    | "session.close"
    | "tab.move"
    | "session.info"
    | "terminal.clear"
    | "terminal.focus"
    | "terminal.resume"
    | "terminal.copy"
    | "terminal.find"
    | "terminal.paste",
): MenuEntry {
  if (id === "tab.close" && !state.attachment && state.tabId)
    return {
      id,
      label: id,
      enabled: !state.busy,
      action: { id: "tab.closeEmpty", tabId: state.tabId },
    };
  return {
    id,
    label: id,
    enabled:
      !!state.attachment &&
      !state.busy &&
      (id !== "session.close" || (state.connected && state.currentOpen)) &&
      (id !== "tab.move" ||
        (state.connected &&
          state.currentOpen &&
          (state.windowCount ?? 1) < 16)),
    action: state.attachment
      ? { id, attachment: { ...state.attachment } }
      : undefined,
  };
}

/** Finite shared menu description; neither process accepts renderer-defined commands. */
export function applicationMenu(state: MenuState): MenuEntry[] {
  return [
    {
      id: "session",
      label: "menu.session",
      children: [
        {
          id: "connection.new",
          label: "connection.new",
          enabled: actionEnabled({ id: "connection.new" }, state),
          action: { id: "connection.new" },
        },
        {
          id: "session.choose",
          label: "session.choose",
          enabled: state.connected && !state.busy,
          action: { id: "session.choose" },
        },
        {
          id: "session.refresh",
          label: "session.refresh",
          enabled: state.connected && !state.busy,
          action: { id: "session.refresh" },
        },
        {
          id: "session.files",
          label: "session.files",
          children: fileMenu(
            state,
            state.attachment?.sessionId,
            state.currentOpen &&
              (state.currentSerial || !!state.currentSSH) &&
              (!state.leased || !!state.currentSSH),
            state.currentSerial,
          ),
        },
        attachedAction(state, "tab.close"),
        attachedAction(state, "session.close"),
        attachedAction(state, "session.info"),
      ],
    },
    {
      id: "host",
      label: "menu.host",
      children: [
        {
          id: "host.connect",
          label: "host.connect",
          enabled: !state.connected && !state.busy,
          action: { id: "host.connect" },
        },
        {
          id: "host.disconnect",
          label: "host.disconnect",
          enabled: state.connected && !state.busy,
          action: { id: "host.disconnect" },
        },
      ],
    },
    { id: "terminal", label: "menu.terminal", children: terminalMenu(state) },
    {
      id: "view",
      label: "menu.view",
      children: [
        {
          id: "view.sidebar",
          label: "view.sidebar",
          checked: state.sidebarVisible,
          shortcut: shortcutLabel(state.shortcuts["view.sidebar"]),
          action: { id: "view.sidebar" },
        },
        {
          id: "view.auxiliary",
          label: "view.auxiliary",
          checked: state.auxiliaryVisible,
          shortcut: shortcutLabel(state.shortcuts["view.auxiliary"]),
          action: { id: "view.auxiliary" },
        },
        {
          id: "view.panel",
          label: "view.panel",
          checked: state.panelVisible,
          shortcut: shortcutLabel(state.shortcuts["view.panel"]),
          action: { id: "view.panel" },
        },
        {
          id: "view.transfers",
          label: "file.transfers",
          action: { id: "view.transfers" },
        },
        {
          id: "view.status",
          label: "view.status",
          action: { id: "view.status" },
        },
        {
          id: "view.activity",
          label: "activity.title",
          action: { id: "view.activity" },
        },
      ],
    },
    {
      id: "window",
      label: "menu.window",
      children: [
        ...windowActions.slice(0, 2).map((id) => windowEntry(state, id)),
        {
          ...attachedAction(state, "tab.close"),
          id: "window.tab.close",
          label: "window.tab.close",
        },
        ...windowActions.slice(2).map((id) => windowEntry(state, id)),
        attachedAction(state, "tab.move"),
        { id: "app.quit", label: "app.quit", action: { id: "app.quit" } },
      ],
    },
    {
      id: "settings",
      label: "menu.settings",
      children: [
        {
          id: "settings.display",
          label: "settings.preferences",
          action: { id: "settings.display" },
        },
        {
          id: "settings.shortcuts",
          label: "settings.shortcuts",
          action: { id: "settings.shortcuts" },
        },
        {
          id: "language",
          label: "menu.language",
          children: (["system", ...locales] as const).map((locale) => ({
            id: `language.${locale}`,
            label: `language.${locale}`,
            checked: locale === (state.languagePreference ?? state.locale),
            action: { id: "language.change", locale },
          })),
        },
      ],
    },
    {
      id: "help",
      label: "menu.help",
      children: [
        { id: "help.quick", label: "help.quick", action: { id: "help.quick" } },
      ],
    },
  ];
}

function windowEntry(
  state: MenuState,
  id: (typeof windowActions)[number],
): MenuEntry {
  return {
    id,
    label: id,
    enabled: actionEnabled({ id }, state),
    action: { id },
  };
}

/** Tab context actions capture that tab's current attachment, including background tabs. */
export function tabMenu(state: MenuState): MenuEntry[] {
  return state.attachment
    ? [
        ...sessionMenu(
          state,
          state.attachment.sessionId,
          state.currentOpen && (state.currentSSH || !state.leased),
          state.currentSerial,
          state.currentSSH,
        ),
        attachedAction(state, "tab.move"),
      ]
    : [attachedAction(state, "tab.close")];
}

export function terminalMenu(state: MenuState): MenuEntry[] {
  const items = [
    attachedAction(state, "terminal.focus"),
    attachedAction(state, "terminal.clear"),
    {
      ...attachedAction(state, "terminal.find"),
      shortcut: shortcutLabel(state.shortcuts["terminal.find"]),
    },
    ...(["terminal.copy", "terminal.paste"] as const).map((id) => ({
      ...attachedAction(state, id),
      shortcut: shortcutLabel(state.shortcuts[id]),
      enabled:
        !!state.attachment &&
        !state.busy &&
        (id === "terminal.copy" ||
          (state.connected &&
            state.currentOpen &&
            !state.hexView &&
            !state.leased &&
            !state.inputPaused)),
    })),
  ];
  if (state.inputPaused)
    items.push({
      ...attachedAction(state, "terminal.resume"),
      enabled:
        !!state.attachment &&
        state.connected &&
        state.currentOpen &&
        !state.leased &&
        !state.busy,
    });
  return items;
}

function fileMenu(
  state: MenuState,
  sessionId: string | undefined,
  available: boolean,
  resumable = true,
): MenuEntry[] {
  return (["file.send", "file.receive", "file.resume"] as const).map((id) => ({
    id,
    label: id,
    enabled:
      !!sessionId &&
      available &&
      state.connected &&
      !state.busy &&
      (id !== "file.resume" || resumable),
    action: sessionId ? { id, sessionId } : undefined,
  }));
}

/** Capture the clicked opaque ID, never substitute the current selection at execution. */
export function sessionMenu(
  state: MenuState,
  sessionId: string,
  open: boolean,
  serial = false,
  ssh = false,
): MenuEntry[] {
  return [
    {
      id: "session.attach",
      label: "session.attach",
      enabled:
        state.connected &&
        !state.busy &&
        open &&
        sessionId !== state.attachment?.sessionId,
      action: { id: "session.attach", sessionId },
    },
    {
      ...attachedAction(state, "tab.close"),
      enabled:
        !!state.attachment &&
        !state.busy &&
        state.attachment.sessionId === sessionId,
      action:
        state.attachment?.sessionId === sessionId
          ? { id: "tab.close", attachment: { ...state.attachment } }
          : undefined,
    },
    ...fileMenu(state, sessionId, open && (serial || ssh), serial),
    ...(state.attachment?.sessionId === sessionId
      ? [attachedAction(state, "session.close")]
      : []),
  ];
}

function record(value: unknown): value is Record<string, unknown> {
  return !!value && typeof value === "object" && !Array.isArray(value);
}
function exact(value: Record<string, unknown>, keys: string[]): boolean {
  return Object.keys(value).every((key) => keys.includes(key));
}
function identifier(value: unknown): value is string {
  return (
    typeof value === "string" &&
    value.length > 0 &&
    value.length <= 512 &&
    !/[\x00-\x1f\x7f]/.test(value)
  );
}
function isAttachment(value: unknown): value is Attachment {
  return (
    record(value) &&
    exact(value, ["id", "sessionId"]) &&
    identifier(value.id) &&
    identifier(value.sessionId)
  );
}
export function isMenuLocale(value: unknown): value is MenuLocale {
  return locales.some((locale) => locale === value);
}
export function isAction(value: unknown): value is Action {
  if (!record(value)) return false;
  switch (value.id) {
    case "host.connect":
    case "host.disconnect":
    case "connection.new":
    case "session.choose":
    case "window.new":
    case "tab.new":
    case "window.close":
    case "tab.next":
    case "tab.previous":
    case "window.next":
    case "window.previous":
    case "connection.serial":
    case "connection.ssh":
    case "session.refresh":
    case "view.sidebar":
    case "view.auxiliary":
    case "view.panel":
    case "view.transfers":
    case "view.status":
    case "view.activity":
    case "settings.shortcuts":
    case "settings.display":
    case "help.quick":
    case "app.quit":
      return exact(value, ["id"]);
    case "file.send":
    case "file.receive":
    case "file.resume":
    case "session.attach":
      return exact(value, ["id", "sessionId"]) && identifier(value.sessionId);
    case "tab.close":
    case "session.close":
    case "session.copyAI":
    case "tab.move":
    case "session.info":
    case "terminal.copy":
    case "terminal.find":
    case "terminal.paste":
    case "terminal.clear":
    case "terminal.focus":
    case "terminal.resume":
      return (
        exact(value, ["id", "attachment"]) && isAttachment(value.attachment)
      );
    case "tab.select":
    case "tab.closeEmpty":
      return exact(value, ["id", "tabId"]) && identifier(value.tabId);
    case "language.change":
      return (
        exact(value, ["id", "locale"]) &&
        (value.locale === "system" || isMenuLocale(value.locale))
      );
    default:
      return false;
  }
}
export function isMenuState(value: unknown): value is MenuState {
  if (
    !record(value) ||
    !exact(value, [
      "locale",
      "languagePreference",
      "connected",
      "sshAvailable",
      "busy",
      "sidebarVisible",
      "auxiliaryVisible",
      "panelVisible",
      "shortcuts",
      "attachment",
      "tabId",
      "tabCount",
      "windowCount",
      "currentOpen",
      "currentSerial",
      "currentSSH",
      "inputPaused",
      "hexView",
      "leased",
    ])
  )
    return false;
  return (
    isMenuLocale(value.locale) &&
    (value.tabId === undefined || identifier(value.tabId)) &&
    (value.tabCount === undefined ||
      (typeof value.tabCount === "number" &&
        Number.isInteger(value.tabCount) &&
        value.tabCount >= 0 &&
        value.tabCount <= 32)) &&
    (value.windowCount === undefined ||
      (typeof value.windowCount === "number" &&
        Number.isInteger(value.windowCount) &&
        value.windowCount >= 1 &&
        value.windowCount <= 16)) &&
    (value.languagePreference === undefined ||
      value.languagePreference === "system" ||
      isMenuLocale(value.languagePreference)) &&
    (value.hexView === undefined || typeof value.hexView === "boolean") &&
    (value.currentSSH === undefined || typeof value.currentSSH === "boolean") &&
    isShortcutBindings(value.shortcuts) &&
    [
      "connected",
      "sshAvailable",
      "busy",
      "sidebarVisible",
      "auxiliaryVisible",
      "panelVisible",
      "currentOpen",
      "currentSerial",
      "inputPaused",
      "leased",
    ].every((key) => typeof value[key] === "boolean") &&
    (value.attachment === undefined || isAttachment(value.attachment))
  );
}

/** Recheck live view state before dispatch, including attachment identity after async changes. */
export function actionEnabled(action: Action, state: MenuState): boolean {
  switch (action.id) {
    case "window.new":
      return (state.windowCount ?? 1) < 16;
    case "window.close":
      return true;
    case "window.next":
    case "window.previous":
      return (state.windowCount ?? 1) > 1;
    case "tab.new":
      return !state.busy && (state.tabCount ?? 0) < 32;
    case "tab.next":
    case "tab.previous":
      return !state.busy && (state.tabCount ?? 0) > 1;
    case "tab.select":
      return !state.busy;
    case "tab.closeEmpty":
      return !state.busy && state.tabId === action.tabId && !state.attachment;
    case "host.connect":
      return !state.connected && !state.busy;
    case "connection.ssh":
      return (
        state.sshAvailable && actionEnabled({ id: "connection.new" }, state)
      );
    case "connection.new":
    case "connection.serial":
      return (
        state.connected &&
        !state.busy &&
        ((state.tabCount ?? 0) < 32 || !state.attachment)
      );
    case "host.disconnect":
    case "session.choose":
    case "session.refresh":
    case "file.send":
    case "file.receive":
    case "file.resume":
    case "session.attach":
      return state.connected && !state.busy;
    case "tab.close":
    case "session.close":
    case "session.copyAI":
    case "tab.move":
    case "session.info":
    case "terminal.copy":
    case "terminal.find":
    case "terminal.paste":
    case "terminal.clear":
    case "terminal.focus":
    case "terminal.resume":
      return (
        !state.busy &&
        state.attachment?.id === action.attachment.id &&
        state.attachment.sessionId === action.attachment.sessionId &&
        (!["session.close", "session.copyAI"].includes(action.id) ||
          (state.connected && state.currentOpen)) &&
        (action.id !== "tab.move" ||
          (state.connected &&
            state.currentOpen &&
            (state.windowCount ?? 1) < 16)) &&
        (action.id !== "terminal.paste" ||
          (state.connected &&
            state.currentOpen &&
            !state.hexView &&
            !state.leased &&
            !state.inputPaused)) &&
        (action.id !== "terminal.resume" ||
          (state.connected &&
            state.currentOpen &&
            !state.leased &&
            state.inputPaused))
      );
    default:
      return true;
  }
}
