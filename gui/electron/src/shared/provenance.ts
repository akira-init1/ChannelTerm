/** Public, bounded source data only. Missing identity never implies a human. */
export interface OperationSource {
  clientId: string;
  name: string;
  kind: "user" | "agent" | "unknown";
  instanceId: string;
  parentInstanceId: string;
  operationId: string;
  entry: string;
  declaredEntry: string;
  presence: "online" | "expired" | "ended" | "unknown";
}
