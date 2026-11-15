/** Narrow local client-configuration intent; paths and credentials stay in Main. */
export interface MCPSetupRequest {
  client: "codex" | "claude" | "opencode" | "zoo";
  scope: "user" | "project";
  action: "inspect" | "configure" | "verify";
  replace: boolean;
}

export interface MCPSetupClient {
  id: MCPSetupRequest["client"];
  detected: boolean;
  version?: string;
  official: "unavailable" | "needs_explicit_file_fallback";
}

export interface MCPSetupResult {
  state:
    | "cancelled"
    | "viewed"
    | "preserved"
    | "applied"
    | "verified_existing"
    | "recovered_applied"
    | "rolled_back"
    | "none"
    | "failed";
  clientId?: string;
  permissions?: "read_only" | "unknown";
  code?:
    | "configuration"
    | "changed"
    | "authentication"
    | "permission"
    | "unavailable"
    | "network"
    | "apply"
    | "recovery"
    | "companion";
}

export function isMCPSetupRequest(value: unknown): value is MCPSetupRequest {
  if (!value || typeof value !== "object" || Array.isArray(value)) return false;
  const row = value as Record<string, unknown>;
  return (
    Object.keys(row).length === 4 &&
    typeof row.client === "string" &&
    typeof row.scope === "string" &&
    typeof row.action === "string" &&
    ["codex", "claude", "opencode", "zoo"].includes(String(row.client)) &&
    ["user", "project"].includes(String(row.scope)) &&
    ["inspect", "configure", "verify"].includes(String(row.action)) &&
    typeof row.replace === "boolean" &&
    !(row.client === "codex" && row.scope !== "user")
  );
}
