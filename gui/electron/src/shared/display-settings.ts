/** Persisted GUI presentation; never sent to the Host or used as a CSS stylesheet. */
export interface DisplaySettings {
  fontSize: number;
  fontFamily: string;
}

export const defaultDisplaySettings: Readonly<DisplaySettings> = {
  fontSize: 14,
  fontFamily: 'Consolas, "Liberation Mono", monospace',
};
export const displayStorageKey = "channelterm.display.v1";

export function isDisplaySettings(value: unknown): value is DisplaySettings {
  if (!value || typeof value !== "object" || Array.isArray(value)) return false;
  const v = value as Record<string, unknown>;
  return (
    Object.keys(v).length === 2 &&
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

/** One local font step per wheel event, clamped at the settings limits. */
export function zoomFontSize(size: number, delta: number): number {
  return Math.max(8, Math.min(40, size - Math.sign(delta)));
}
