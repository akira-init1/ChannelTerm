import type {
  LocalSelection,
  RemoteDirectory,
  TransferRequest,
  TransferStatus,
  TransferCheckpoint,
} from "./transfers";
import type { Action, MenuState } from "./menus";
import type { ConnectionRequest, OpenedConnection } from "./connections";
import type { TerminalSettings } from "./terminal-settings";

/** Display-only snapshots. The Host remains authoritative for lifecycle and leases. */
export interface SessionSummary {
  session_id: string;
  session_ref: string;
  transport: string;
  endpoint: string;
  label: string;
  state: string;
  lease?: { type: string; state: string };
}

export type ErrorCode =
  | "configuration"
  | "disconnected"
  | "unauthorized"
  | "incompatible"
  | "busy"
  | "closed"
  | "cancelled"
  | "invalid_input"
  | "tool_error";

export interface ClientError {
  code: ErrorCode;
  message: string;
}
export type Result<T> =
  { ok: true; value: T } | { ok: false; error: ClientError };
export interface HostInfo {
  endpoint: string;
  lifetime: "persistent" | "attachment" | "unknown";
  sshAvailable?: boolean;
  autoConnect?: boolean;
}
export interface Attachment {
  id: string;
  sessionId: string;
}
export interface Output {
  data: number[];
  /** Optional client-decoded text; raw bytes remain available for hex inspection. */
  text?: string;
  dropped: boolean;
}

/** One method per IPC channel; no arbitrary tool, URL, file, or process access. */
export interface DesktopAPI {
  chooseTransferPath(
    sessionId: string,
    direction: "send" | "receive",
    kind: "file" | "directory",
  ): Promise<Result<LocalSelection | undefined>>;
  browseDirectory(
    sessionId: string,
    path: string,
  ): Promise<Result<RemoteDirectory>>;
  startTransfer(request: TransferRequest): Promise<Result<void>>;
  transferCheckpoints(sessionId: string): Promise<Result<TransferCheckpoint[]>>;
  resumeTransfer(
    sessionId: string,
    checkpointId: string,
  ): Promise<Result<void>>;
  transfers(sessionId: string): Promise<Result<TransferStatus[]>>;
  cancelTransfer(
    sessionId: string,
    transferId: string,
  ): Promise<Result<string>>;
  resolveTransferCancel(
    confirmationId: string,
    confirm: boolean,
  ): Promise<Result<void>>;
  info(): Promise<Result<HostInfo>>;
  connect(): Promise<Result<HostInfo>>;
  disconnect(): Promise<Result<void>>;
  list(): Promise<Result<SessionSummary[]>>;
  serialPorts(): Promise<Result<string[]>>;
  openConnection(request: ConnectionRequest): Promise<Result<OpenedConnection>>;
  attach(
    sessionId: string,
    settings?: TerminalSettings,
  ): Promise<Result<Attachment>>;
  detach(attachmentId: string): Promise<Result<void>>;
  read(attachmentId: string): Promise<Result<Output>>;
  write(attachmentId: string, data: string): Promise<Result<void>>;
  syncMenu(state: MenuState): Promise<Result<void>>;
  onMenuAction(listener: (action: Action) => void): () => void;
  quit(): Promise<Result<void>>;
}

export const channels = {
  chooseTransferPath: "files:choose",
  browseDirectory: "files:browse",
  startTransfer: "files:transfer",
  transferCheckpoints: "files:checkpoints",
  resumeTransfer: "files:resume",
  transfers: "files:status",
  cancelTransfer: "files:cancel",
  resolveTransferCancel: "files:cancel-answer",
  info: "host:info",
  connect: "host:connect",
  disconnect: "host:disconnect",
  list: "host:list",
  serialPorts: "host:serial-ports",
  openConnection: "host:open-connection",
  attach: "host:attach",
  detach: "host:detach",
  read: "host:read",
  write: "host:write",
  menuState: "menu:state",
  menuAction: "menu:action",
  quit: "app:quit",
} as const;
