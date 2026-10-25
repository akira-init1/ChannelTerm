/** Sanitized observations, never raw write payloads or lease capabilities. */
export interface ActivityRow {
  id: string;
  timestamp: string;
  actor: string;
  type: string;
  details: string;
  bytes?: number;
  history: boolean;
  clientReport: boolean;
}
export interface ActivitySnapshot {
  rows: ActivityRow[];
  dropped: boolean;
  truncated: boolean;
}
