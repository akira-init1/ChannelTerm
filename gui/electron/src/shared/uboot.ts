import type { ClientError } from "./api";

/** One explicit board-specific attempt; never persisted or retried automatically. */
export interface UBootRequest {
  mode: "wait_reset" | "reboot";
  rebootCommand: string;
  verifiedIdleBash: boolean;
  countdown: string;
  prompt: string;
  bootStarted: string[];
  interruptHex: string;
  timeoutMS: number;
  capture: boolean;
}

export const ubootStatuses = [
  "entered",
  "boot_started",
  "timeout",
  "cancelled",
  "session_closed",
  "output_lost",
  "write_failed",
  "input_pending",
  "unsupported_shell",
  "cleanup_failed",
  "failed",
] as const;
export type UBootStatus = (typeof ubootStatuses)[number];

/** Host evidence, with unknown diagnostic internals excluded from the renderer. */
export interface UBootOutcome {
  operationId: string;
  sessionId: string;
  status: UBootStatus;
  outputStart: number;
  outputEnd: number;
  countdownCursor: number;
  promptCursor: number;
  countdownMatched: boolean;
  promptMatched: boolean;
  bytesWritten: number;
  elapsedMS: number;
  dropped: boolean;
  diagnosticId?: string;
  diagnosticUnavailable: boolean;
}

/** One window-local attempt; cancellation without a Host receipt leaves outcome unknown. */
export interface UBootJob {
  id: string;
  attachmentId: string;
  sessionId: string;
  state: "waiting" | "ready" | "stopping" | "finished";
  operationId?: string;
  outcome?: UBootOutcome;
  error?: ClientError;
}

function boundedUTF8(value: unknown, maximum: number): value is string {
  return (
    typeof value === "string" &&
    value.length > 0 &&
    value.length <= maximum &&
    !/[\uD800-\uDFFF]/u.test(value) &&
    new TextEncoder().encode(value).length <= maximum
  );
}

/** Preserve literal spaces and exact bytes; missing settings have no guessed defaults. */
export function isUBootRequest(value: unknown): value is UBootRequest {
  if (!value || typeof value !== "object" || Array.isArray(value)) return false;
  const row = value as Record<string, unknown>;
  if (
    Object.keys(row).length !== 9 ||
    (row.mode !== "wait_reset" && row.mode !== "reboot") ||
    typeof row.rebootCommand !== "string" ||
    typeof row.verifiedIdleBash !== "boolean" ||
    !boundedUTF8(row.countdown, 256) ||
    !boundedUTF8(row.prompt, 256) ||
    /[\r\n]/.test(row.prompt) ||
    !Array.isArray(row.bootStarted) ||
    row.bootStarted.length < 1 ||
    row.bootStarted.length > 8 ||
    !row.bootStarted.every((value) => boundedUTF8(value, 256)) ||
    typeof row.interruptHex !== "string" ||
    !/^(?:[0-9a-fA-F]{2}){1,32}$/.test(row.interruptHex) ||
    !Number.isSafeInteger(row.timeoutMS) ||
    typeof row.timeoutMS !== "number" ||
    row.timeoutMS < 1 ||
    row.timeoutMS > 180000 ||
    typeof row.capture !== "boolean"
  )
    return false;
  return row.mode === "wait_reset"
    ? row.rebootCommand === "" && !row.verifiedIdleBash
    : row.verifiedIdleBash &&
        boundedUTF8(row.rebootCommand, 65536) &&
        row.rebootCommand.trim().length > 0 &&
        !/\p{Cc}/u.test(row.rebootCommand);
}
