/** Local layout commands only; bindings never name arbitrary application actions. */
export const layoutActions = [
  "view.sidebar",
  "view.auxiliary",
  "view.panel",
] as const;
export type LayoutAction = (typeof layoutActions)[number];
export type ShortcutBindings = Record<LayoutAction, string>;

export function defaultShortcuts(mac: boolean): ShortcutBindings {
  const modifier = mac ? "Meta" : "Ctrl";
  return {
    "view.sidebar": `${modifier}+B`,
    "view.auxiliary": `${modifier}+Alt+B`,
    "view.panel": `${modifier}+J`,
  };
}

/** Canonical physical letter/digit combinations, with common editing/OS keys reserved. */
export function validShortcut(value: unknown): value is string {
  if (typeof value !== "string") return false;
  if (value === "") return true;
  if (!/^(Ctrl|Meta)(\+Alt)?(\+Shift)?\+[A-Z0-9]$/.test(value)) return false;
  return !/^(Ctrl|Meta)\+(?:Shift\+)?[ACVXZYWQRL]$/.test(value);
}

export function isShortcutBindings(value: unknown): value is ShortcutBindings {
  if (!value || typeof value !== "object" || Array.isArray(value)) return false;
  const entries = Object.entries(value);
  const assigned = entries.map(([, binding]) => binding).filter(Boolean);
  return (
    entries.length === layoutActions.length &&
    entries.every(
      ([id, binding]) =>
        layoutActions.some((action) => action === id) && validShortcut(binding),
    ) &&
    new Set(assigned).size === assigned.length
  );
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
