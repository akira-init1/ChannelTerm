import { isConnectionRequest, type ConnectionRequest } from "./connections";

export const connectionHistoryKey = "channelterm.connections.v1";
export interface ConnectionRecord {
  request: ConnectionRequest;
  favorite: boolean;
  lastUsed: number;
  uses: number;
}

/** Explicit allowlist: secrets, credential paths and serial wake intent never survive. */
export function connectionPreset(
  request: ConnectionRequest,
): ConnectionRequest {
  if (request.kind === "serial")
    return {
      kind: "serial",
      port: request.port,
      baud: request.baud,
      dataBits: request.dataBits,
      parity: request.parity,
      stopBits: request.stopBits,
      flowControl: request.flowControl,
      label: request.label,
      wake: false,
    };
  return {
    kind: "ssh",
    host: request.host,
    user: request.user,
    port: request.port,
    label: request.label,
    identityFiles: [],
    knownHostsFile: "",
    confirmHostKey: true,
    ...(request.advanced ? { advanced: { ...request.advanced } } : {}),
  };
}

/** Malformed/oversized storage fails closed; reconstructed entries cannot carry extra fields. */
export function restoreConnections(raw: string | null): ConnectionRecord[] {
  if (!raw || raw.length > 131072) return [];
  try {
    const value: unknown = JSON.parse(raw);
    if (!Array.isArray(value) || value.length > 50) return [];
    return value.flatMap((entry: unknown) => {
      if (!entry || typeof entry !== "object") return [];
      const v = entry as Record<string, unknown>;
      if (
        !isConnectionRequest(v.request) ||
        typeof v.favorite !== "boolean" ||
        typeof v.lastUsed !== "number" ||
        !Number.isSafeInteger(v.lastUsed) ||
        v.lastUsed < 0 ||
        v.lastUsed > 8640000000000000 ||
        typeof v.uses !== "number" ||
        !Number.isSafeInteger(v.uses) ||
        v.uses < 0
      )
        return [];
      return [
        {
          request: connectionPreset(v.request),
          favorite: v.request.kind === "ssh" && v.favorite,
          lastUsed: v.lastUsed,
          uses: v.uses,
        },
      ];
    });
  } catch {
    return [];
  }
}

/** Keep at most 50 entries, preserving favorites before evicting old recent connections. */
export function recordConnection(
  entries: ConnectionRecord[],
  request: ConnectionRequest,
  favorite: boolean,
  now = Date.now(),
): ConnectionRecord[] {
  const preset = connectionPreset(request);
  const key = (r: ConnectionRequest) => JSON.stringify(connectionPreset(r));
  const previous = entries.find((entry) => key(entry.request) === key(preset));
  return [
    {
      request: preset,
      favorite: preset.kind === "ssh" && (favorite || !!previous?.favorite),
      lastUsed: now,
      uses: Math.min(Number.MAX_SAFE_INTEGER, (previous?.uses ?? 0) + 1),
    },
    ...entries.filter((entry) => key(entry.request) !== key(preset)),
  ]
    .sort(
      (a, b) =>
        Number(b.favorite) - Number(a.favorite) || b.lastUsed - a.lastUsed,
    )
    .slice(0, 50);
}
