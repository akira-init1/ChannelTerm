import {
  normalizeTerminalEncoding,
  type TerminalEncoding,
} from "./terminal-encodings";
import { isConnectionIdentity } from "./highlight-rules";

/** Client-local presentation only; never serialized into Host Session metadata. */
export interface TerminalSettings {
  encoding: TerminalEncoding;
  view: "text" | "hex";
  description: string;
  /** Local saved-connection reference only; confers no Host or input capability. */
  highlightProfile?: string;
}

export const defaultTerminalSettings: Readonly<TerminalSettings> = {
  encoding: "utf-8",
  view: "text",
  description: "",
};

/** Rechecked in Main before replacing an attachment or accepting input. */
export function isTerminalSettings(value: unknown): value is TerminalSettings {
  if (!value || typeof value !== "object" || Array.isArray(value)) return false;
  const v = value as Record<string, unknown>;
  return (
    Object.keys(v).every((key) =>
      ["encoding", "view", "description", "highlightProfile"].includes(key),
    ) &&
    typeof v.encoding === "string" &&
    normalizeTerminalEncoding(v.encoding) === v.encoding &&
    (v.view === "text" || v.view === "hex") &&
    (v.highlightProfile === undefined ||
      isConnectionIdentity(v.highlightProfile)) &&
    typeof v.description === "string" &&
    v.description.length <= 512 &&
    !/[\uD800-\uDFFF]/u.test(v.description) &&
    new TextEncoder().encode(v.description).length <= 512 &&
    !/[\x00-\x1f\x7f]/.test(v.description)
  );
}
