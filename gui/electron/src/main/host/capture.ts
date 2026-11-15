import { integer, text, incompatible } from "./validation";

/** Raw Session range, independent of terminal display and transfer suppression. */
export interface CapturedOutput {
  data: Uint8Array;
  start: number;
  next: number;
  dropped: boolean;
}

/** Validate bytes and cursor continuity before any local recording can claim completeness. */
export function capturedOutput(
  result: Record<string, unknown>,
  cursor?: number,
): CapturedOutput {
  const next = integer(result.next);
  const count = integer(result.bytes_read);
  const encoded = text(result.data);
  if (
    result.encoding !== "base64" ||
    typeof result.dropped !== "boolean" ||
    count > 65536 ||
    encoded.length > 87384
  )
    throw incompatible();
  const data = Buffer.from(encoded, "base64");
  if (
    data.length !== count ||
    data.toString("base64") !== encoded ||
    next < count ||
    (cursor !== undefined &&
      (next - count < cursor || (!result.dropped && next - count !== cursor)))
  )
    throw incompatible();
  return { data, start: next - count, next, dropped: result.dropped };
}

/** A Main-owned read attachment; closing ends only this client's monitor use. */
export interface OutputCapture {
  sessionId: string;
  start: number;
  read(): Promise<CapturedOutput | undefined>;
  close(): Promise<void>;
}
