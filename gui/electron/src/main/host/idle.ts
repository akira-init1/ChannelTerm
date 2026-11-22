import type { HostInfo } from "../../shared/api";
import { incompatible, integer, object } from "./validation";

/** Only effective policy crosses IPC; presence and authority stay in Host. */
export function idlePolicy(
  value: unknown,
): NonNullable<HostInfo["idlePolicy"]> {
  const policy = object(value);
  const sessionTimeoutMs = integer(policy.session_timeout_ms);
  const exitTimeoutMs = integer(policy.exit_timeout_ms);
  for (const value of [sessionTimeoutMs, exitTimeoutMs])
    if (value !== 0 && (value < 1000 || value > 86400000)) throw incompatible();
  const mode = policy.mode;
  const origin = policy.origin;
  if (
    (mode !== "keep" && mode !== "release" && mode !== "exit") ||
    (origin !== "manual" && origin !== "desktop")
  )
    throw incompatible();
  if (
    (mode === "keep" && (sessionTimeoutMs !== 0 || exitTimeoutMs !== 0)) ||
    (mode === "release" && (sessionTimeoutMs === 0 || exitTimeoutMs !== 0)) ||
    (mode === "exit" && (sessionTimeoutMs === 0 || exitTimeoutMs === 0))
  )
    throw incompatible();
  return {
    mode,
    origin,
    sessionTimeoutMs,
    exitTimeoutMs,
    ...(policy.background_switch === true &&
    origin === "desktop" &&
    mode !== "keep"
      ? { backgroundSwitch: true }
      : {}),
  };
}
