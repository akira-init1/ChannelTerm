/** Explicit bounded debug input; never a timer, broadcast or stored execution. */
export interface DebugPayload {
  mode: "text" | "hex";
  data: string;
  ending: "none" | "lf" | "cr" | "crlf";
}

export const debugByteLimit = 4096;
export const snippetLimit = 32;
export interface CommandSnippet extends DebugPayload {
  name: string;
}
export type SnippetChange =
  | { action: "save"; snippet: CommandSnippet; confirmPlaintext: true }
  | { action: "remove"; name: string };

export function isDebugPayload(value: unknown): value is DebugPayload {
  if (!value || typeof value !== "object" || Array.isArray(value)) return false;
  const row = value as Record<string, unknown>;
  return (
    Object.keys(row).length === 3 &&
    (row.mode === "text" || row.mode === "hex") &&
    typeof row.data === "string" &&
    row.data.length > 0 &&
    row.data.length <= 16384 &&
    !/[\uD800-\uDFFF]/u.test(row.data) &&
    typeof row.ending === "string" &&
    ["none", "lf", "cr", "crlf"].includes(row.ending)
  );
}

export function isCommandSnippet(value: unknown): value is CommandSnippet {
  if (!value || typeof value !== "object" || Array.isArray(value)) return false;
  const row = value as Record<string, unknown>;
  const { name, ...payload } = row;
  return (
    typeof name === "string" &&
    name === name.trim() &&
    name.length > 0 &&
    name.length <= 64 &&
    !/[\x00-\x1f\x7f\uD800-\uDFFF]/u.test(name) &&
    isDebugPayload(payload)
  );
}
