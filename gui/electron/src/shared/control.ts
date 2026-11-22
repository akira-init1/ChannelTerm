import type { OperationSource } from "./provenance";

/** Public fencing/observation only; Host owns all write and takeover authority. */
export interface ControlSnapshot {
  session_id: string;
  version: string;
  operation_id: string;
  state: "idle" | "active" | "stopping" | "human" | "failed";
  writable: boolean;
  ai_paused: boolean;
  waiting?: boolean;
  manual_only?: boolean;
  source?: OperationSource;
}

/** A captured observation, never an authorization secret or a request to retry. */
export interface ControlTarget {
  session_id: string;
  version: string;
  operation_id: string;
}
export interface ControlOutcome extends ControlTarget {
  status: "stopping" | "taken_over" | "failed";
  reason: "user_interrupted" | "control_expired";
  recovery: "pending" | "restored" | "session_closed";
  retry_allowed: false;
  source?: OperationSource;
}
export type ControlRequest = ControlTarget & {
  action: "takeover" | "allow" | "result" | "protect" | "unprotect";
  locale?: string;
  wait_seconds?: number;
};
export type ControlReply =
  { control: ControlSnapshot } | { result: ControlOutcome };
