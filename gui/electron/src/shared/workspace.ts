import { locales, type LanguagePreference } from "./menus";
import {
  defaultTerminalSettings,
  isTerminalSettings,
  type TerminalSettings,
} from "./terminal-settings";
import type { SessionSummary } from "./api";

export const preferencesKey = "channelterm.preferences.v1";
export const workspaceKey = "channelterm.workspace.v1";

/** Local presentation defaults. No Host, input queue or terminal screen state. */
export interface WorkbenchPreferences {
  language: LanguagePreference;
  sidebarVisible: boolean;
  sidebarView: "sessions" | "debug";
  sidebarWidth: number | null;
  auxiliaryVisible: boolean;
  auxiliaryWidth: number;
  panelVisible: boolean;
  panelTab: "transfers" | "status" | "activity";
  panelHeight: number;
  terminal: TerminalSettings;
}
export const defaultWorkbenchPreferences: WorkbenchPreferences = {
  language: "system",
  sidebarVisible: true,
  sidebarView: "sessions",
  sidebarWidth: null,
  auxiliaryVisible: false,
  auxiliaryWidth: 260,
  panelVisible: false,
  panelTab: "transfers",
  panelHeight: 220,
  terminal: { ...defaultTerminalSettings },
};

function object(value: unknown): value is Record<string, unknown> {
  return !!value && typeof value === "object" && !Array.isArray(value);
}
function bounded(value: unknown, max: number): value is string {
  return (
    typeof value === "string" &&
    value.length <= max &&
    !/[\x00-\x1f\x7f]/.test(value)
  );
}
function size(value: unknown, min: number, max: number): value is number {
  return (
    typeof value === "number" &&
    Number.isFinite(value) &&
    value >= min &&
    value <= max
  );
}
function parse(raw: string | null, max: number): unknown {
  if (!raw || raw.length > max) return undefined;
  try {
    return JSON.parse(raw);
  } catch {
    return undefined;
  }
}

/** Validate and project persisted preferences, ignoring unknown data rather than retaining it. */
export function restoreWorkbenchPreferences(
  raw: string | null,
): WorkbenchPreferences {
  const v = parse(raw, 8192);
  if (
    !object(v) ||
    !(
      v.language === "system" || locales.some((locale) => locale === v.language)
    ) ||
    typeof v.sidebarVisible !== "boolean" ||
    (v.sidebarView !== "sessions" && v.sidebarView !== "debug") ||
    (v.sidebarWidth !== null && !size(v.sidebarWidth, 180, 480)) ||
    typeof v.auxiliaryVisible !== "boolean" ||
    !size(v.auxiliaryWidth, 180, 480) ||
    typeof v.panelVisible !== "boolean" ||
    (v.panelTab !== "transfers" &&
      v.panelTab !== "status" &&
      v.panelTab !== "activity") ||
    !size(v.panelHeight, 100, 2000) ||
    !isTerminalSettings(v.terminal)
  )
    return {
      ...defaultWorkbenchPreferences,
      terminal: { ...defaultTerminalSettings },
    };
  return {
    language: v.language as LanguagePreference,
    sidebarVisible: v.sidebarVisible,
    sidebarView: v.sidebarView,
    sidebarWidth: v.sidebarWidth,
    auxiliaryVisible: v.auxiliaryVisible,
    auxiliaryWidth: v.auxiliaryWidth,
    panelVisible: v.panelVisible,
    panelTab: v.panelTab,
    panelHeight: v.panelHeight,
    terminal: {
      encoding: v.terminal.encoding,
      view: v.terminal.view,
      description: v.terminal.description,
    },
  };
}

export interface WorkspaceTarget {
  sessionId: string;
  label: string;
  transport: string;
  endpoint: string;
}
export interface WorkspaceTab {
  target?: WorkspaceTarget;
  settings: TerminalSettings;
}
export interface WorkspaceSnapshot {
  endpoint: string;
  tabs: WorkspaceTab[];
  selected: number;
}

/** One bounded window slot; reject another Host and never resolve short references as IDs. */
export function restoreWorkspace(
  raw: string | null,
  endpoint: string,
): WorkspaceSnapshot | undefined {
  const v = parse(raw, 131072);
  if (
    !object(v) ||
    v.endpoint !== endpoint ||
    !bounded(v.endpoint, 2048) ||
    !Array.isArray(v.tabs) ||
    v.tabs.length > 32 ||
    !size(v.selected, -1, v.tabs.length - 1) ||
    !Number.isInteger(v.selected)
  )
    return undefined;
  const tabs: WorkspaceTab[] = [];
  for (const entry of v.tabs) {
    if (!object(entry) || !isTerminalSettings(entry.settings)) return undefined;
    const target = entry.target;
    if (
      target !== undefined &&
      (!object(target) ||
        !bounded(target.sessionId, 512) ||
        !target.sessionId ||
        !bounded(target.label, 128) ||
        !bounded(target.transport, 64) ||
        !bounded(target.endpoint, 1024))
    )
      return undefined;
    tabs.push({
      settings: {
        encoding: entry.settings.encoding,
        view: entry.settings.view,
        description: entry.settings.description,
      },
      ...(object(target)
        ? {
            target: {
              sessionId: String(target.sessionId),
              label: String(target.label),
              transport: String(target.transport),
              endpoint: String(target.endpoint),
            },
          }
        : {}),
    });
  }
  return { endpoint, tabs, selected: v.selected };
}

/** A saved target is only a display hint; match current authoritative metadata before attach. */
export function workspaceSession(
  target: WorkspaceTarget,
  sessions: SessionSummary[],
): SessionSummary | undefined {
  return sessions.find(
    (session) =>
      session.session_id === target.sessionId &&
      session.state === "open" &&
      session.transport === target.transport &&
      session.endpoint === target.endpoint,
  );
}
