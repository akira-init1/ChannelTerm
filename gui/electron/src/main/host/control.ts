import type { ControlSnapshot } from "../../shared/control";
import { operationSource } from "./provenance";
import { object, text, incompatible } from "./validation";

/** Select only bounded public state, never accept a renderer-issued capability. */
export function controlSnapshot(value: unknown): ControlSnapshot {
  const raw = object(value);
  const state = raw.state;
  const version = text(raw.version);
  const session_id = text(raw.session_id);
  const operation_id =
    raw.operation_id === undefined ? "" : text(raw.operation_id);
  if (
    !/^CV-[A-Za-z0-9]{16,64}$/.test(version) ||
    session_id.length > 512 ||
    !session_id ||
    /[\x00-\x1f\x7f]/.test(session_id) ||
    (operation_id && !/^OC-[A-Za-z0-9]{16,64}$/.test(operation_id)) ||
    (state !== "idle" &&
      state !== "active" &&
      state !== "stopping" &&
      state !== "human" &&
      state !== "failed") ||
    typeof raw.writable !== "boolean" ||
    typeof raw.ai_paused !== "boolean"
  )
    throw incompatible();
  return {
    session_id,
    version,
    operation_id,
    state,
    writable: raw.writable,
    ai_paused: raw.ai_paused,
    source: operationSource(raw.client),
  };
}
