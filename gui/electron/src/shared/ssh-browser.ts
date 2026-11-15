import type { ConnectionRequest } from "./connections";

/** Read-only file intent on the dedicated SSH connection, bound to a live Host Session. */
export interface SSHBrowseRequest {
  sessionId: string;
  operation: "ls" | "stat";
  path: string;
  connection: Extract<ConnectionRequest, { kind: "ssh" }>;
}
/** Validated metadata only; file contents and credentials never appear in a listing. */
export interface SSHFileEntry {
  name: string;
  kind: "file" | "directory" | "symlink" | "other";
  size: number;
  mode: string;
  modified: string;
}
/** Complete bounded snapshot, exposed only after successful companion exit. */
export interface SSHBrowseResult {
  path: string;
  entries: SSHFileEntry[];
}

export type SSHManageOperation = "mkdir" | "rename" | "rm" | "rmdir";
/** Finite mutation intent; no recursive deletion or overwrite capability. */
export interface SSHManageRequest {
  sessionId: string;
  operation: SSHManageOperation;
  path: string;
  target?: string;
  connection: Extract<ConnectionRequest, { kind: "ssh" }>;
}
export type SSHManageReason =
  | ""
  | "not_found"
  | "already_exists"
  | "permission_denied"
  | "operation_failed"
  | "cancelled"
  | "timeout"
  | "target_changed";
/** Acknowledgement is not a fresh directory snapshot; failures can be uncertain. */
export interface SSHManageResult {
  operation: SSHManageOperation;
  path: string;
  target?: string;
  state: "completed" | "failed" | "cancelled";
  outcome: "confirmed" | "unknown" | "not_started";
  reason: SSHManageReason;
}
