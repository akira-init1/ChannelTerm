/** Local recording intent; Main selects destinations and owns the capture cursor. */
export interface RecordingRequest {
  attachmentId: string;
  format: "raw" | "text";
  segmentMiB: number;
  segments: number;
}

/** No bytes or credentials cross IPC; locations refer only to user-selected log output. */
export interface RecordingStatus {
  id: string;
  sessionId: string;
  directory: string;
  format: "raw" | "text";
  state: "starting" | "recording" | "stopping" | "stopped" | "failed";
  bytes: number;
  gaps: number;
  rotations: number;
  error?: "source" | "disk";
}

export function isRecordingRequest(value: unknown): value is RecordingRequest {
  if (!value || typeof value !== "object") return false;
  const data = value as Record<string, unknown>;
  return (
    Object.keys(data).length === 4 &&
    typeof data.attachmentId === "string" &&
    data.attachmentId.length > 0 &&
    data.attachmentId.length <= 512 &&
    !/[\x00-\x1f\x7f]/.test(data.attachmentId) &&
    (data.format === "raw" || data.format === "text") &&
    typeof data.segmentMiB === "number" &&
    [1, 4, 8, 16, 64].includes(data.segmentMiB) &&
    typeof data.segments === "number" &&
    Number.isInteger(data.segments) &&
    data.segments >= 1 &&
    data.segments <= 8
  );
}
