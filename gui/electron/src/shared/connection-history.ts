import { isConnectionRequest, type ConnectionRequest } from "./connections";
import {
  isConnectionIdentity,
  readHighlightRules,
  type HighlightRule,
} from "./highlight-rules";

export const connectionHistoryKey = "channelterm.connections.v1";
export const connectionHighlightsChanged =
  "channelterm:connection-highlights-changed";
export const maxConnectionHistoryLength = 131072;
export interface ConnectionRecord {
  id?: string;
  request: ConnectionRequest;
  favorite: boolean;
  lastUsed: number;
  uses: number;
  highlights?: HighlightRule[];
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
  if (!raw || raw.length > maxConnectionHistoryLength) return [];
  try {
    const value: unknown = JSON.parse(raw);
    if (!Array.isArray(value) || value.length > 50) return [];
    const claims = new Map<string, number>();
    for (const entry of value) {
      if (
        entry &&
        typeof entry === "object" &&
        "id" in entry &&
        isConnectionIdentity(entry.id)
      )
        claims.set(entry.id, (claims.get(entry.id) ?? 0) + 1);
    }
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
      const id =
        isConnectionIdentity(v.id) && claims.get(v.id) === 1 ? v.id : undefined;
      const highlights = id ? readHighlightRules(v.highlights) : undefined;
      return [
        {
          request: connectionPreset(v.request),
          favorite: v.request.kind === "ssh" && v.favorite,
          lastUsed: v.lastUsed,
          uses: v.uses,
          ...(id ? { id } : {}),
          ...(highlights ? { highlights } : {}),
        },
      ];
    });
  } catch {
    return [];
  }
}

/** Match the complete safe preset; passwords, credential paths and wake intent never define identity. */
export function findConnection(
  entries: readonly ConnectionRecord[],
  request: ConnectionRequest,
): ConnectionRecord | undefined {
  const key = JSON.stringify(connectionPreset(request));
  return entries.find(
    (entry) => JSON.stringify(connectionPreset(entry.request)) === key,
  );
}

/** Keep at most 50 entries, preserving favorites before evicting old recent connections. */
export function recordConnection(
  entries: ConnectionRecord[],
  request: ConnectionRequest,
  favorite: boolean,
  now = Date.now(),
  newId?: string,
): ConnectionRecord[] {
  const preset = connectionPreset(request);
  const key = (r: ConnectionRequest) => JSON.stringify(connectionPreset(r));
  const previous = findConnection(entries, preset);
  const id = isConnectionIdentity(previous?.id) ? previous.id : newId;
  return [
    {
      request: preset,
      favorite: preset.kind === "ssh" && (favorite || !!previous?.favorite),
      lastUsed: now,
      uses: Math.min(Number.MAX_SAFE_INTEGER, (previous?.uses ?? 0) + 1),
      ...(isConnectionIdentity(id) ? { id } : {}),
      ...(isConnectionIdentity(id) && previous?.highlights
        ? { highlights: previous.highlights.map((rule) => ({ ...rule })) }
        : {}),
    },
    ...entries.filter((entry) => key(entry.request) !== key(preset)),
  ]
    .sort(
      (a, b) =>
        Number(b.favorite) - Number(a.favorite) || b.lastUsed - a.lastUsed,
    )
    .slice(0, 50);
}
