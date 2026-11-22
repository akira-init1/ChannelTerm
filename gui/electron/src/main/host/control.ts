import type {
  ControlSnapshot,
  ControlRequest,
  ControlOutcome,
} from "../../shared/control";
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
    ...(raw.waiting === true ? { waiting: true } : {}),
    ...(raw.manual_only === true ? { manual_only: true } : {}),
    ai_paused: raw.ai_paused,
    source: operationSource(raw.client),
  };
}

/** Reject unknown IPC fields and preserve the caller's exact captured target. */
export function controlRequest(value: unknown): ControlRequest {
  const r = object(value);
  if (
    Object.keys(r).some(
      (k) =>
        ![
          "action",
          "session_id",
          "version",
          "operation_id",
          "locale",
          "wait_seconds",
        ].includes(k),
    )
  )
    throw incompatible();
  if (
    r.action !== "takeover" &&
    r.action !== "allow" &&
    r.action !== "result" &&
    r.action !== "protect" &&
    r.action !== "unprotect"
  )
    throw incompatible();
  if (
    r.locale !== undefined &&
    (typeof r.locale !== "string" || !/^[a-zA-Z-]{1,35}$/.test(r.locale))
  )
    throw incompatible();
  if (
    r.wait_seconds !== undefined &&
    (typeof r.wait_seconds !== "number" ||
      !Number.isInteger(r.wait_seconds) ||
      r.wait_seconds < 1 ||
      r.wait_seconds > 3600)
  )
    throw incompatible();
  const checked = controlSnapshot({
    ...r,
    state: "idle",
    writable: false,
    ai_paused: false,
  });
  return {
    action: r.action,
    ...(r.locale !== undefined ? { locale: r.locale } : {}),
    ...(r.wait_seconds !== undefined ? { wait_seconds: r.wait_seconds } : {}),
    session_id: checked.session_id,
    version: checked.version,
    operation_id: checked.operation_id,
  };
}
export function controlOutcome(value: unknown): ControlOutcome {
  const r = object(value);
  const target = controlSnapshot({
    ...r,
    state: "idle",
    writable: false,
    ai_paused: false,
  });
  if (
    !["stopping", "taken_over", "failed"].includes(text(r.status)) ||
    !["user_interrupted", "control_expired"].includes(text(r.reason)) ||
    !["pending", "restored", "session_closed"].includes(text(r.recovery)) ||
    r.retry_allowed !== false
  )
    throw incompatible();
  if (
    (r.status === "stopping" && r.recovery !== "pending") ||
    (r.status === "taken_over" && r.recovery !== "restored") ||
    (r.status === "failed" && r.recovery !== "session_closed")
  )
    throw incompatible();
  return {
    session_id: target.session_id,
    version: target.version,
    operation_id: target.operation_id,
    status: r.status as ControlOutcome["status"],
    reason: r.reason as ControlOutcome["reason"],
    recovery: r.recovery as ControlOutcome["recovery"],
    retry_allowed: false,
    source: operationSource(r.requested_by),
  };
}
