import type {
  LocalSelection,
  RemoteDirectory,
  TransferRequest,
  TransferStatus,
  TransferCheckpoint,
  SSHTransferRequest,
} from "./transfers";
import type {
  Action,
  MenuState,
  LanguagePreference,
  MenuLocale,
} from "./menus";
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
  sshAdvanced?: boolean;
  autoConnect?: boolean;
}
export interface Attachment {
  id: string;
  sessionId: string;
}
export interface Output {
  data: number[];
  /** Retained snapshot/gap recovery: terminal queries must not produce new device input. */
  replay?: boolean;
  /** Optional client-decoded text; raw bytes remain available for hex inspection. */
  text?: string;
  dropped: boolean;
}

/** Window-local bootstrap data, never credentials or permission to close a shared Session. */
export interface WindowInfo {
  count: number;
  connected: boolean;
  autoConnect: boolean;
  languagePreference?: LanguagePreference;
  incoming?: {
    moveId: string;
    attachment: Attachment;
    settings: TerminalSettings;
    inputPaused: boolean;
  };
}

/** One method per IPC channel; no arbitrary tool, URL, file, or process access. */
export interface DesktopAPI {
  windowInfo(): Promise<Result<WindowInfo>>;
  newWindow(): Promise<Result<void>>;
  closeWindow(): Promise<Result<void>>;
  focusWindow(direction: "next" | "previous"): Promise<Result<void>>;
  moveTab(attachmentId: string, inputPaused: boolean): Promise<Result<void>>;
  acceptMovedTab(moveId: string, attachmentId: string): Promise<Result<void>>;
  onWindowCount(listener: (count: number) => void): () => void;
  readClipboard(attachmentId: string): Promise<Result<string>>;
  writeClipboard(attachmentId: string, text: string): Promise<Result<void>>;
  copyAITarget(attachmentId: string, locale: MenuLocale): Promise<Result<void>>;
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
  startSSHTransfer(request: SSHTransferRequest): Promise<Result<void>>;
  sshTransfers(): Promise<Result<TransferStatus[]>>;
  cancelSSHTransfer(id: string): Promise<Result<void>>;
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
  /** Explicit shared close, separate from attachment cleanup. */
  closeSession(attachmentId: string): Promise<Result<void>>;
  read(attachmentId: string): Promise<Result<Output>>;
  write(attachmentId: string, data: string): Promise<Result<void>>;
  syncMenu(state: MenuState): Promise<Result<void>>;
  onMenuAction(listener: (action: Action) => void): () => void;
  onFileCloseBlocked(listener: () => void): () => void;
  quit(): Promise<Result<void>>;
}

export const channels = {
  windowInfo: "window:info",
  newWindow: "window:new",
  closeWindow: "window:close",
  focusWindow: "window:focus",
  moveTab: "window:move-tab",
  acceptMovedTab: "window:accept-tab",
  windowCount: "window:count",
  readClipboard: "terminal:clipboard-read",
  writeClipboard: "terminal:clipboard-write",
  copyAITarget: "session:copy-ai-target",
  chooseTransferPath: "files:choose",
  browseDirectory: "files:browse",
  startTransfer: "files:transfer",
  startSSHTransfer: "files:ssh-start",
  sshTransfers: "files:ssh-status",
  cancelSSHTransfer: "files:ssh-cancel",
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
  closeSession: "host:close-session",
  read: "host:read",
  write: "host:write",
  menuState: "menu:state",
  menuAction: "menu:action",
  fileCloseBlocked: "files:close-blocked",
  quit: "app:quit",
} as const;
