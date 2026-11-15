import type { ConnectionRequest } from "./connections";
import type { OperationSource } from "./provenance";

/** File intent is bounded; local paths are held by Main behind an opaque selection ID. */
export interface LocalSelection {
  id: string;
  name: string;
  kind?: "file" | "directory";
}

/** Local client choice, independent of Host managed-transfer grants. */
export type DirectoryPreference = "ask" | "tar" | "files";
export interface RemoteDirectory {
  path: string;
  entries: { name: string; kind: "file" | "directory" | "other" }[];
  truncated: boolean;
}
/** Completed counters are observed before connection cleanup; failures never prove rollback. */
export interface SSHTransferResult {
  reported: boolean;
  files: number;
  bytes: number;
  verifiedFiles: number;
  outcome: "confirmed" | "unknown" | "not_started";
  reason:
    | ""
    | "not_found"
    | "already_exists"
    | "permission_denied"
    | "timeout"
    | "cancelled"
    | "verification_failed"
    | "operation_failed";
}
export interface TransferStatus {
  sshResult?: SSHTransferResult;
  initiator?: OperationSource;
  originalSource?: OperationSource;
  checkpointId?: string;
  managedId?: string;
  scope?: "session" | "host" | "local";
  resumed?: boolean;
  /** Dedicated SSH jobs are local client operations, not Session lease events. */
  protocol?: "sftp" | "scp";
  id: string;
  sessionId: string;
  direction: string;
  source: string;
  destination: string;
  state:
    | "transferring"
    | "verifying"
    | "cleaning"
    | "completed"
    | "cancelled"
    | "failed"
    | "unknown";
  transferred: number;
  total: number;
  speed: number;
  sha256?: string;
  released: boolean;
}

/** Retained history preserves identity, but cannot assert current liveness or active progress. */
export function retainedTransfer(job: TransferStatus): TransferStatus {
  return {
    ...job,
    state: job.released ? job.state : "unknown",
    sshResult: job.sshResult
      ? {
          ...job.sshResult,
          outcome: job.released ? job.sshResult.outcome : "unknown",
        }
      : undefined,
    initiator: job.initiator
      ? { ...job.initiator, presence: "unknown" }
      : undefined,
    originalSource: job.originalSource
      ? { ...job.originalSource, presence: "unknown" }
      : undefined,
  };
}

/** Explicit credentials are transient; Main binds the target to the live SSH Session. */
export interface SSHTransferRequest {
  sessionId: string;
  direction: "send" | "receive";
  selectionId: string;
  remotePath: string;
  protocol: "sftp" | "scp";
  resume: boolean;
  recursive: boolean;
  /** Older requests omit these and retain no-overwrite/no-deadline behavior. */
  overwrite?: boolean;
  safeReplace?: boolean;
  operationTimeoutSeconds?: number;
  connection: Extract<ConnectionRequest, { kind: "ssh" }>;
}
export interface TransferRequest {
  directoryMode?: "tar" | "files";
  resumable?: boolean;
  /** Omitted older requests use the safe Base64 default. */
  wire?: "base64" | "raw";
  sessionId: string;
  direction: "send" | "receive";
  selectionId: string;
  remotePath: string;
}

/** Persisted intent summary only; Main/CLI retain paths and validate the selected peer. */
export interface TransferCheckpoint {
  originalSource?: OperationSource;
  id: string;
  name: string;
  direction: "send" | "receive";
  kind: "file" | "directory";
  offset: number;
  total: number;
  phase: "preparing" | "transferring" | "publishing";
}
