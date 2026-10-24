import type { SessionSummary } from "../../shared/api";
import { HostError } from "./errors";

export function object(value: unknown): Record<string, unknown> {
  if (!value || typeof value !== "object" || Array.isArray(value))
    throw incompatible();
  return value as Record<string, unknown>;
}
export function incompatible(): HostError {
  return new HostError(
    "incompatible",
    "Host returned an unsupported result. Update compatible Host and GUI versions.",
  );
}
export function text(value: unknown): string {
  if (typeof value !== "string") throw incompatible();
  return value;
}
export function integer(value: unknown): number {
  if (typeof value !== "number" || !Number.isSafeInteger(value) || value < 0)
    throw incompatible();
  return value;
}
export function sessions(value: unknown): SessionSummary[] {
  const entries = object(value).sessions;
  if (!Array.isArray(entries) || entries.length > 10000) throw incompatible();
  return entries.map((entry) => {
    const item = object(entry);
    const lease = item.lease === undefined ? undefined : object(item.lease);
    return {
      session_id: text(item.session_id),
      session_ref: text(item.session_ref),
      transport: text(item.transport),
      endpoint: text(item.endpoint),
      label: text(item.label),
      state: text(item.state),
      ...(lease
        ? { lease: { type: text(lease.type), state: text(lease.state) } }
        : {}),
    };
  });
}

export function identifier(value: unknown): asserts value is string {
  if (
    typeof value !== "string" ||
    value.length === 0 ||
    value.length > 512 ||
    /[\x00-\x1f]/.test(value)
  ) {
    throw new HostError(
      "invalid_input",
      "Invalid Session or attachment identifier.",
    );
  }
}

export function input(value: unknown): asserts value is string {
  if (
    typeof value !== "string" ||
    value.length === 0 ||
    Buffer.byteLength(value, "utf8") > 65536
  ) {
    throw new HostError(
      "invalid_input",
      "Input must contain between 1 and 65536 UTF-8 bytes.",
    );
  }
}
