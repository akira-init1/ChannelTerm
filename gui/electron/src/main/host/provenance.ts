import type { OperationSource } from "../../shared/provenance";
import { object, incompatible } from "./validation";
import { integer, text } from "./validation";
import type { TransferStatus } from "../../shared/transfers";

/** Pick public fields rather than forwarding an evolving Host object to Renderer. */
export function operationSource(value: unknown): OperationSource | undefined {
  if (value === undefined || value === null) return undefined;
  const raw = object(value);
  const field = (key: string, limit = 128) => {
    const value = raw[key] ?? "";
    if (
      typeof value !== "string" ||
      value.length > limit ||
      /[\x00-\x1f\x7f]/.test(value)
    )
      throw incompatible();
    return value;
  };
  const clientId = field("client_id"),
    kind = field("kind", 16);
  return {
    clientId,
    name: field("name"),
    kind: clientId && (kind === "user" || kind === "agent") ? kind : "unknown",
    instanceId: field("instance_id"),
    parentInstanceId: field("parent_instance_id"),
    operationId: field("operation_id"),
    entry: field("entry", 32),
    declaredEntry: field("declared_entry", 32),
    presence: "unknown",
  };
}

export function managedTransferSummaries(value: unknown): TransferStatus[] {
  if (!Array.isArray(value) || value.length > 128) throw incompatible();
  return value.map((raw) => {
    const item = object(raw),
      id = text(item.transfer_id),
      state = text(item.state);
    const sessionId =
      item.session_id === undefined ? "" : text(item.session_id);
    const sessionTransferId =
      item.session_transfer_id === undefined
        ? ""
        : text(item.session_transfer_id);
    if (
      !/^FT-[A-Za-z0-9]{16,64}$/.test(id) ||
      sessionId.length > 512 ||
      sessionTransferId.length > 512 ||
      /[\x00-\x1f\x7f]/.test(sessionId + sessionTransferId) ||
      !["serial", "sftp", "scp"].includes(text(item.transport)) ||
      !["send", "receive"].includes(text(item.direction)) ||
      !["running", "completed", "cancelled", "failed"].includes(state)
    )
      throw incompatible();
    return {
      id: sessionTransferId || id,
      managedId: id,
      sessionId,
      initiator: operationSource(item.client),
      scope: "host",
      direction: text(item.direction),
      source: "",
      destination: "",
      state:
        state === "running"
          ? "transferring"
          : (state as "completed" | "cancelled" | "failed"),
      transferred: integer(item.bytes),
      total: integer(item.total_bytes),
      speed: 0,
      released: state !== "running",
    };
  });
}
