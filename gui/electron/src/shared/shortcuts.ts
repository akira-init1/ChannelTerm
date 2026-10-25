/** Local layout commands only; bindings never name arbitrary application actions. */
export const layoutActions = [
  "view.sidebar",
  "view.auxiliary",
  "view.panel",
] as const;
export type LayoutAction = (typeof layoutActions)[number];
export const shortcutActions = [
  ...layoutActions,
  "terminal.copy",
  "terminal.paste",
  "terminal.find",
] as const;
export type ShortcutBindings = Record<(typeof shortcutActions)[number], string>;

export function defaultShortcuts(mac: boolean): ShortcutBindings {
  const modifier = mac ? "Meta" : "Ctrl";
  return {
    "view.sidebar": `${modifier}+B`,
    "view.auxiliary": `${modifier}+Alt+B`,
    "view.panel": `${modifier}+J`,
    "terminal.copy": "Ctrl+Shift+C",
    "terminal.paste": "Ctrl+Shift+V",
    "terminal.find": "Ctrl+Shift+F",
  };
}

/** Canonical physical letter/digit combinations, with common editing/OS keys reserved. */
export function validShortcut(value: unknown): value is string {
  if (typeof value !== "string") return false;
  if (value === "") return true;
  if (!/^(Ctrl|Meta)(\+Alt)?(\+Shift)?\+[A-Z0-9]$/.test(value)) return false;
  if (/^Ctrl\+Shift\+[CV]$/.test(value)) return true;
  return !/^(Ctrl|Meta)\+(?:Shift\+)?[ACVXZYWQRL]$/.test(value);
}

export function isShortcutBindings(value: unknown): value is ShortcutBindings {
  if (!value || typeof value !== "object" || Array.isArray(value)) return false;
  const entries = Object.entries(value);
  const assigned = entries.map(([, binding]) => binding).filter(Boolean);
  return (
    entries.length === shortcutActions.length &&
    entries.every(
      ([id, binding]) =>
        shortcutActions.some((action) => action === id) &&
        validShortcut(binding),
    ) &&
    new Set(assigned).size === assigned.length
  );
}

/** Preserve prior layout/clipboard bindings; a new Find binding never steals a saved chord. */
export function restoreShortcuts(
  value: unknown,
  mac: boolean,
): ShortcutBindings {
  if (isShortcutBindings(value)) return value;
  if (value && typeof value === "object" && !Array.isArray(value)) {
    const keys = Object.keys(value);
    const previous =
      keys.length === layoutActions.length
        ? layoutActions
        : shortcutActions.filter((id) => id !== "terminal.find");
    if (
      keys.length !== previous.length ||
      !keys.every((key) => previous.some((id) => id === key))
    )
      return defaultShortcuts(mac);
    const migrated = { ...defaultShortcuts(mac), ...value };
    for (const id of shortcutActions)
      if (!(id in value) && Object.values(value).includes(migrated[id]))
        migrated[id] = "";
    if (isShortcutBindings(migrated)) return migrated;
  }
  return defaultShortcuts(mac);
}

export interface ShortcutKeyEvent {
  code: string;
  ctrlKey: boolean;
  metaKey: boolean;
  altKey: boolean;
  shiftKey: boolean;
  isComposing: boolean;
  keyCode: number;
  getModifierState: (key: string) => boolean;
}

/** Ignore IME and AltGr; physical codes keep Option+letter usable on macOS. */
export function shortcutFromEvent(event: ShortcutKeyEvent): string | undefined {
  if (
    event.isComposing ||
    event.keyCode === 229 ||
    event.getModifierState("AltGraph") ||
    event.ctrlKey === event.metaKey
  )
    return;
  const key = /^(?:Key([A-Z])|Digit([0-9]))$/.exec(event.code);
  if (!key) return;
  const binding = `${event.metaKey ? "Meta" : "Ctrl"}${event.altKey ? "+Alt" : ""}${event.shiftKey ? "+Shift" : ""}+${key[1] ?? key[2]}`;
  return validShortcut(binding) ? binding : undefined;
}

export function shortcutLabel(binding: string): string {
  return binding.startsWith("Meta")
    ? binding
        .replace("Meta", "⌘")
        .replace("Alt", "⌥")
        .replace("Shift", "⇧")
        .replaceAll("+", "")
    : binding;
}
