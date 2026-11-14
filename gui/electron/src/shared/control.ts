import type { OperationSource } from "./provenance";

/** Public fencing/observation only; Host owns all write and takeover authority. */
export interface ControlSnapshot {
  session_id: string;
  version: string;
  operation_id: string;
  state: "idle" | "active" | "stopping" | "human" | "failed";
  writable: boolean;
  ai_paused: boolean;
  source?: OperationSource;
}
