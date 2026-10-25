import type { ConnectionRequest } from "./connections";

/** File intent is bounded; local paths are held by Main behind an opaque selection ID. */
export interface LocalSelection {
  id: string;
  name: string;
}
export interface RemoteDirectory {
  path: string;
  entries: { name: string; kind: "file" | "directory" | "other" }[];
  truncated: boolean;
}
export interface TransferStatus {
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

/** Explicit credentials are transient; Main binds the target to the live SSH Session. */
export interface SSHTransferRequest {
  sessionId: string;
  direction: "send" | "receive";
  selectionId: string;
  remotePath: string;
  protocol: "sftp" | "scp";
  resume: boolean;
  recursive: boolean;
  connection: Extract<ConnectionRequest, { kind: "ssh" }>;
}
export interface TransferRequest {
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
  id: string;
  name: string;
  direction: "send" | "receive";
  kind: "file" | "directory";
  offset: number;
  total: number;
  phase: "preparing" | "transferring" | "publishing";
}
