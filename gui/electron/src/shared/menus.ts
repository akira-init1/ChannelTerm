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

/** Reserved menu contracts; lifecycle handlers must exist before enabling these actions. */
export const unavailableWindowActions = [
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
        | (typeof unavailableWindowActions)[number]
        | "connection.serial"
        | "connection.ssh"
        | "session.refresh"
        | "view.sidebar"
        | "view.auxiliary"
        | "view.panel"
        | "view.transfers"
        | "view.status"
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
        | "session.info"
        | "terminal.clear"
        | "terminal.focus"
        | "terminal.resume"
        | "terminal.copy"
        | "terminal.paste";
      attachment: Attachment;
    }
  | { id: "language.change"; locale: LanguagePreference };

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
    | "session.info"
    | "terminal.clear"
    | "terminal.focus"
    | "terminal.resume"
    | "terminal.copy"
    | "terminal.paste",
): MenuEntry {
  return {
    id,
    label: id,
    enabled: !!state.attachment && !state.busy,
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
          enabled: state.connected && !state.busy,
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
      ],
    },
    {
      id: "window",
      label: "menu.window",
      children: [
        ...unavailableWindowActions.slice(0, 2).map(unavailableWindowEntry),
        {
          ...attachedAction(state, "tab.close"),
          id: "window.tab.close",
          label: "window.tab.close",
        },
        ...unavailableWindowActions.slice(2).map(unavailableWindowEntry),
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

function unavailableWindowEntry(
  id: (typeof unavailableWindowActions)[number],
): MenuEntry {
  return { id, label: id, enabled: false, action: { id } };
}

export function terminalMenu(state: MenuState): MenuEntry[] {
  const items = [
    attachedAction(state, "terminal.focus"),
    attachedAction(state, "terminal.clear"),
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
    case "session.info":
    case "terminal.copy":
    case "terminal.paste":
    case "terminal.clear":
    case "terminal.focus":
    case "terminal.resume":
      return (
        exact(value, ["id", "attachment"]) && isAttachment(value.attachment)
      );
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
  if (unavailableWindowActions.some((id) => id === action.id)) return false;
  switch (action.id) {
    case "host.connect":
      return !state.connected && !state.busy;
    case "connection.ssh":
      return state.connected && state.sshAvailable && !state.busy;
    case "host.disconnect":
    case "connection.new":
    case "session.choose":
    case "connection.serial":
    case "session.refresh":
    case "file.send":
    case "file.receive":
    case "file.resume":
    case "session.attach":
      return state.connected && !state.busy;
    case "tab.close":
    case "session.info":
    case "terminal.copy":
    case "terminal.paste":
    case "terminal.clear":
    case "terminal.focus":
    case "terminal.resume":
      return (
        !state.busy &&
        state.attachment?.id === action.attachment.id &&
        state.attachment.sessionId === action.attachment.sessionId &&
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
