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
export interface TransferRequest {
  /** Omitted older requests use the safe Base64 default. */
  wire?: "base64" | "raw";
  sessionId: string;
  direction: "send" | "receive";
  selectionId: string;
  remotePath: string;
}
