import type {
  SSHManageRequest,
  SSHManageResult,
  SSHManageReason,
} from "../shared/ssh-browser";
import { isConnectionRequest } from "../shared/connections";
import { HostError } from "./host/errors";
import { object, incompatible, identifier } from "./host/validation";

/** Validate finite mutation intent separately from read-only browse requests. */
export function sshManageRequest(value: unknown): SSHManageRequest {
  const r = object(value);
  if (
    Object.keys(r).some(
      (key) =>
        !["sessionId", "operation", "path", "target", "connection"].includes(
          key,
        ),
    ) ||
    !["mkdir", "rename", "rm", "rmdir"].includes(String(r.operation))
  )
    throw new HostError("invalid_input", "Invalid SFTP management request.");
  identifier(r.sessionId);
  if (!isConnectionRequest(r.connection) || r.connection.kind !== "ssh")
    throw new HostError("invalid_input", "Invalid SFTP connection.");
  const operation = r.operation;
  if (
    operation !== "mkdir" &&
    operation !== "rename" &&
    operation !== "rm" &&
    operation !== "rmdir"
  )
    throw incompatible();
  // Absolute, named entries make confirmation independent of a server's home directory.
  function namedPath(path: unknown): path is string {
    return (
      typeof path === "string" &&
      path.length <= 4096 &&
      !/[\x00-\x1f\x7f]/.test(path) &&
      path.startsWith("/") &&
      path !== "/" &&
      !path.endsWith("/") &&
      !path.split("/").some((part) => part === "." || part === "..")
    );
  }
  if (!namedPath(r.path))
    throw new HostError(
      "invalid_input",
      "Choose an absolute named SFTP entry.",
    );
  let target: string | undefined;
  if (operation === "rename") {
    if (!namedPath(r.target) || r.target === r.path)
      throw new HostError(
        "invalid_input",
        "Choose a distinct absolute rename target.",
      );
    target = r.target;
  } else if (r.target !== undefined)
    throw new HostError("invalid_input", "Unexpected rename target.");
  return {
    sessionId: r.sessionId,
    operation,
    path: r.path,
    ...(target ? { target } : {}),
    connection: r.connection,
  };
}

/** Require one final acknowledged result and matching process exit; never parse stderr. */
export class SSHManagementReceipt {
  private started = false;
  private result?: SSHManageResult;
  constructor(private readonly request: SSHManageRequest) {}
  accept(value: unknown): void {
    const r = object(value);
    if (this.result) throw incompatible();
    if (!this.started) {
      if (r.type !== "started") throw incompatible();
      this.started = true;
      return;
    }
    const state = r.type;
    if (
      (state !== "completed" && state !== "failed" && state !== "cancelled") ||
      r.operation !== this.request.operation
    )
      throw incompatible();
    const reason = r.error_code;
    if (
      !isReason(reason) ||
      (state === "completed"
        ? r.outcome !== "confirmed" || reason !== ""
        : r.outcome !== "unknown" || reason === "")
    )
      throw incompatible();
    this.result = {
      operation: this.request.operation,
      path: this.request.path,
      ...(this.request.target ? { target: this.request.target } : {}),
      state,
      outcome: state === "completed" ? "confirmed" : "unknown",
      reason,
    };
  }
  finish(code: number | null): SSHManageResult {
    if (
      !this.started ||
      !this.result ||
      code === null ||
      (this.result.state === "completed") !== (code === 0)
    )
      throw incompatible();
    return { ...this.result };
  }
}

function isReason(value: unknown): value is SSHManageReason {
  return (
    value === "" ||
    value === "not_found" ||
    value === "already_exists" ||
    value === "permission_denied" ||
    value === "operation_failed" ||
    value === "cancelled" ||
    value === "timeout"
  );
}
