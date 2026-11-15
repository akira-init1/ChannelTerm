import type { ClientError, ErrorCode } from "./api";

/** Every public error has local recovery text; protocol bodies are never UI copy. */
export const recoveryMessages: Record<ErrorCode, string> = {
  input_queue: "recovery.input_queue",
  control_stale: "recovery.control_stale",
  host_legacy_credentials: "recovery.host_legacy_credentials",
  host_identity_missing: "recovery.host_identity_missing",
  host_credential_missing: "recovery.host_credential_missing",
  host_credential_invalid: "recovery.host_credential_invalid",
  host_credential_unreadable: "recovery.host_credential_unreadable",
  host_companion_outdated: "recovery.host_companion_outdated",
  configuration: "recovery.configuration",
  disconnected: "recovery.disconnected",
  unauthorized: "recovery.unauthorized",
  incompatible: "recovery.incompatible",
  busy: "recovery.busy",
  closed: "recovery.closed",
  cancelled: "recovery.cancelled",
  invalid_input: "recovery.invalid_input",
  tool_error: "recovery.tool_error",
};

/** Store the ID, not a translated string, so existing errors follow language changes. */
export function clientErrorMessageId(error: unknown): string {
  if (
    typeof error === "object" &&
    error !== null &&
    "code" in error &&
    typeof error.code === "string" &&
    Object.hasOwn(recoveryMessages, error.code)
  ) {
    if (
      (error.code === "invalid_input" || error.code === "closed") &&
      "reason" in error &&
      error.reason === "device_changed"
    )
      return "recovery.device_changed";
    if (
      error.code === "tool_error" &&
      "reason" in error &&
      (error.reason === "ssh_host_key" ||
        error.reason === "ssh_authentication" ||
        error.reason === "connection_failed" ||
        error.reason === "connection_uncertain" ||
        error.reason === "diagnostic_unavailable" ||
        error.reason === "discovery_unavailable" ||
        error.reason === "discovery_stale" ||
        error.reason === "serial_not_found" ||
        error.reason === "serial_permission_denied" ||
        error.reason === "serial_busy" ||
        error.reason === "serial_busy_or_denied" ||
        error.reason === "serial_open_failed")
    ) {
      return `recovery.${error.reason}`;
    }
    if (
      error.code === "busy" &&
      "reason" in error &&
      error.reason === "profile_exists"
    )
      return "recovery.profile_exists";
    return recoveryMessages[error.code as ErrorCode];
  }
  return "recovery.tool_error";
}

/** Translate structured diagnostics without inspecting or exposing message text. */
export function hostRecoveryMessage(
  error: ClientError,
  t: (id: string) => string,
): string {
  return t(clientErrorMessageId(error));
}

/** Unknown future states remain explicit and never imply writable input. */
export function sessionStateMessage(
  state: string,
  t: (id: string) => string,
): string {
  switch (state) {
    case "new":
      return t("workbench.new");
    case "connecting":
      return t("debug.connecting");
    case "open":
      return t("workbench.open");
    case "closing":
      return t("workbench.closing");
    case "closed":
      return t("workbench.closed");
    case "failed":
      return t("file.failed");
    default:
      return t("workbench.unknown");
  }
}
