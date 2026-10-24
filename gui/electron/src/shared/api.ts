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
}
export interface Attachment {
  id: string;
  sessionId: string;
}
export interface Output {
  data: number[];
  dropped: boolean;
}

/** One method per IPC channel; no arbitrary tool, URL, file, or process access. */
export interface DesktopAPI {
  info(): Promise<Result<HostInfo>>;
  connect(): Promise<Result<HostInfo>>;
  disconnect(): Promise<Result<void>>;
  list(): Promise<Result<SessionSummary[]>>;
  attach(sessionId: string): Promise<Result<Attachment>>;
  detach(attachmentId: string): Promise<Result<void>>;
  read(attachmentId: string): Promise<Result<Output>>;
  write(attachmentId: string, data: string): Promise<Result<void>>;
}

export const channels = {
  info: "host:info",
  connect: "host:connect",
  disconnect: "host:disconnect",
  list: "host:list",
  attach: "host:attach",
  detach: "host:detach",
  read: "host:read",
  write: "host:write",
} as const;
