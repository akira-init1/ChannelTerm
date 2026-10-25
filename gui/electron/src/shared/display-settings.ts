/** Persisted GUI presentation; never sent to the Host or used as a CSS stylesheet. */
export interface DisplaySettings {
  fontSize: number;
  fontFamily: string;
  terminalStyle: "semantic" | "monochrome";
}

export const defaultDisplaySettings: Readonly<DisplaySettings> = {
  fontSize: 14,
  fontFamily: 'Consolas, "Liberation Mono", monospace',
  terminalStyle: "semantic",
};
export const displayStorageKey = "channelterm.display.v1";

export function isDisplaySettings(value: unknown): value is DisplaySettings {
  if (!value || typeof value !== "object" || Array.isArray(value)) return false;
  const v = value as Record<string, unknown>;
  return (
    Object.keys(v).length === 3 &&
    (v.terminalStyle === "semantic" || v.terminalStyle === "monochrome") &&
    Number.isInteger(v.fontSize) &&
    typeof v.fontSize === "number" &&
    v.fontSize >= 8 &&
    v.fontSize <= 40 &&
    typeof v.fontFamily === "string" &&
    v.fontFamily.trim().length > 0 &&
    v.fontFamily.length <= 256 &&
    !/[\x00-\x1f\x7f;{}]/.test(v.fontFamily)
  );
}

/** Upgrade font-only v1 preferences without discarding the user's font choices. */
export function restoreDisplaySettings(value: unknown): DisplaySettings {
  if (isDisplaySettings(value)) return value;
  if (value && typeof value === "object" && !Array.isArray(value)) {
    const old = value as Record<string, unknown>;
    if (Object.keys(old).length === 2 && !("terminalStyle" in old)) {
      const upgraded = { ...old, terminalStyle: "semantic" };
      if (isDisplaySettings(upgraded)) return upgraded;
    }
  }
  return { ...defaultDisplaySettings };
}

/** One local font step per wheel event, clamped at the settings limits. */
export function zoomFontSize(size: number, delta: number): number {
  return Math.max(8, Math.min(40, size - Math.sign(delta)));
}
