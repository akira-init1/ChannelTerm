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
