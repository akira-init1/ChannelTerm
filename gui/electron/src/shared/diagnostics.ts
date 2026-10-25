/** Explicit snapshot intent or an existing Host-local archive, never a filesystem path. */
export type DiagnosticRequest =
  | { kind: "snapshot"; attachmentId: string; includeRaw: boolean }
  | { kind: "existing"; diagnosticId: string };

export interface DiagnosticSaved {
  id: string;
  size: number;
  sha256: string;
  name: string;
}

export function isDiagnosticID(value: unknown): value is string {
  return typeof value === "string" && /^DIAG-[a-f0-9]{24}$/.test(value);
}

export function isDiagnosticRequest(
  value: unknown,
): value is DiagnosticRequest {
  if (!value || typeof value !== "object" || Array.isArray(value)) return false;
  const item = value as Record<string, unknown>;
  if (item.kind === "existing")
    return Object.keys(item).length === 2 && isDiagnosticID(item.diagnosticId);
  return (
    item.kind === "snapshot" &&
    Object.keys(item).length === 3 &&
    typeof item.includeRaw === "boolean" &&
    typeof item.attachmentId === "string" &&
    item.attachmentId.length > 0 &&
    item.attachmentId.length <= 512 &&
    !/[\x00-\x1f]/.test(item.attachmentId)
  );
}
