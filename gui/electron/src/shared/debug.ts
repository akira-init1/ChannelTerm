/** Public observation data only; no debugger or registration capabilities. */
export interface DebugPort {
  role: string;
  perspective: string;
  configured: string;
  address: string;
  port: number;
  state: string;
}
export interface DebugObservation {
  id: string;
  name: string;
  mode: string;
  session_id: string;
  connection: string;
  target_state: string;
  last_target_state: string;
  tunnel_state: string;
  source: string;
  client: { client_id: string; name: string; kind: string };
  updated_at: string;
  stale: boolean;
  ports: DebugPort[];
}
export interface DebugOverview {
  observed_at: string;
  operations: DebugObservation[];
}

/** A cached display never remains authoritative after failed/paused observation. */
export function debugStale(
  snapshot: DebugOverview,
  failed: boolean,
  now = Date.now(),
): boolean {
  return failed || now - Date.parse(snapshot.observed_at) > 10000;
}
