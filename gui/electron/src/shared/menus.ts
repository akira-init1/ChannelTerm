import type { Attachment } from "./api";

export const locales = ["en", "zh-CN", "ja"] as const;
export type MenuLocale = (typeof locales)[number];
export type Action =
  | {
      id:
        | "host.connect"
        | "host.disconnect"
        | "session.refresh"
        | "view.sidebar"
        | "help.quick"
        | "app.quit";
    }
  | { id: "session.attach"; sessionId: string }
  | {
      id: "tab.close" | "terminal.clear" | "terminal.focus" | "terminal.resume";
      attachment: Attachment;
    }
  | { id: "language.change"; locale: MenuLocale };

/** Display hints only. No menu snapshot grants device or lease authority. */
export interface MenuState {
  locale: MenuLocale;
  connected: boolean;
  busy: boolean;
  sidebarVisible: boolean;
  attachment?: Attachment;
  currentOpen: boolean;
  inputPaused: boolean;
  leased: boolean;
}

export interface MenuEntry {
  id: string;
  label: string;
  action?: Action;
  enabled?: boolean;
  checked?: boolean;
  children?: MenuEntry[];
}

export const initialMenuState: MenuState = {
  locale: "en",
  connected: false,
  busy: false,
  sidebarVisible: true,
  currentOpen: false,
  inputPaused: false,
  leased: false,
};

function attachedAction(
  state: MenuState,
  id: "tab.close" | "terminal.clear" | "terminal.focus" | "terminal.resume",
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
      id: "connection",
      label: "menu.connection",
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
        { id: "app.quit", label: "app.quit", action: { id: "app.quit" } },
      ],
    },
    {
      id: "session",
      label: "menu.session",
      children: [
        {
          id: "session.refresh",
          label: "session.refresh",
          enabled: state.connected && !state.busy,
          action: { id: "session.refresh" },
        },
        attachedAction(state, "tab.close"),
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
          action: { id: "view.sidebar" },
        },
      ],
    },
    {
      id: "settings",
      label: "menu.settings",
      children: [
        {
          id: "language",
          label: "menu.language",
          children: locales.map((locale) => ({
            id: `language.${locale}`,
            label: `language.${locale}`,
            checked: locale === state.locale,
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

export function terminalMenu(state: MenuState): MenuEntry[] {
  const items = [
    attachedAction(state, "terminal.focus"),
    attachedAction(state, "terminal.clear"),
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

/** Capture the clicked opaque ID, never substitute the current selection at execution. */
export function sessionMenu(
  state: MenuState,
  sessionId: string,
  open: boolean,
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
    case "session.refresh":
    case "view.sidebar":
    case "help.quick":
    case "app.quit":
      return exact(value, ["id"]);
    case "session.attach":
      return exact(value, ["id", "sessionId"]) && identifier(value.sessionId);
    case "tab.close":
    case "terminal.clear":
    case "terminal.focus":
    case "terminal.resume":
      return (
        exact(value, ["id", "attachment"]) && isAttachment(value.attachment)
      );
    case "language.change":
      return exact(value, ["id", "locale"]) && isMenuLocale(value.locale);
    default:
      return false;
  }
}
export function isMenuState(value: unknown): value is MenuState {
  if (
    !record(value) ||
    !exact(value, [
      "locale",
      "connected",
      "busy",
      "sidebarVisible",
      "attachment",
      "currentOpen",
      "inputPaused",
      "leased",
    ])
  )
    return false;
  return (
    isMenuLocale(value.locale) &&
    [
      "connected",
      "busy",
      "sidebarVisible",
      "currentOpen",
      "inputPaused",
      "leased",
    ].every((key) => typeof value[key] === "boolean") &&
    (value.attachment === undefined || isAttachment(value.attachment))
  );
}

/** Recheck live view state before dispatch, including attachment identity after async changes. */
export function actionEnabled(action: Action, state: MenuState): boolean {
  switch (action.id) {
    case "host.connect":
      return !state.connected && !state.busy;
    case "host.disconnect":
    case "session.refresh":
    case "session.attach":
      return state.connected && !state.busy;
    case "tab.close":
    case "terminal.clear":
    case "terminal.focus":
    case "terminal.resume":
      return (
        !state.busy &&
        state.attachment?.id === action.attachment.id &&
        state.attachment.sessionId === action.attachment.sessionId &&
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
