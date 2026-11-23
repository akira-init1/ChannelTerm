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
  folder?: string;
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
          favorite: v.favorite,
          lastUsed: v.lastUsed,
          uses: v.uses,
          ...(isFolderName(v.folder) ? { folder: v.folder } : {}),
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
      favorite: favorite || !!previous?.favorite,
      lastUsed: now,
      uses: Math.min(Number.MAX_SAFE_INTEGER, (previous?.uses ?? 0) + 1),
      ...(previous?.folder ? { folder: previous.folder } : {}),
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

function isFolderName(value: unknown): value is string {
  return (
    typeof value === "string" &&
    value.length > 0 &&
    value.length <= 128 &&
    value === value.trim() &&
    !/[\x00-\x1f\x7f]/.test(value)
  );
}

export type DirectoryEdit =
  "folder" | "copy" | "favorite" | "forget" | "rename";

/** Apply a local edit to the latest history. Legacy rows match by their full safe preset. */
export function editConnection(
  entries: ConnectionRecord[],
  target: ConnectionRecord,
  edit: DirectoryEdit,
  value = "",
  newId?: string,
): ConnectionRecord[] | undefined {
  const current = target.id
    ? entries.find((entry) => entry.id === target.id)
    : findConnection(entries, target.request);
  if (!current) return;
  if (edit === "forget") return entries.filter((entry) => entry !== current);
  let updated = { ...current };
  if (edit === "folder") {
    if (value && !isFolderName(value)) return;
    if (value) updated.folder = value;
    else delete updated.folder;
  } else if (edit === "favorite") {
    updated.favorite = !target.favorite;
  } else {
    const request = { ...current.request, label: value };
    const existing = findConnection(entries, request);
    if (
      !isConnectionRequest(request) ||
      (existing && (existing !== current || edit === "copy"))
    )
      return;
    updated = { ...updated, request };
    if (edit === "copy") {
      if (
        !value.trim() ||
        !isConnectionIdentity(newId) ||
        entries.length >= 50 ||
        entries.some((entry) => entry.id === newId)
      )
        return;
      return [...entries, { ...updated, id: newId, uses: 0, lastUsed: 0 }];
    }
  }
  return entries.map((entry) => (entry === current ? updated : entry));
}

/** Public Host endpoint corresponding to the credential-free preset. */
export function presetEndpoint(q: ConnectionRequest): string {
  if (q.kind === "serial") return q.port;
  const host =
    q.host.includes(":") && !q.host.startsWith("[")
      ? "[" + q.host + "]"
      : q.host;
  return q.user + "@" + host + ":" + q.port;
}

/** A Host snapshot has no local preset ID. Resolve shared endpoints only when the label disambiguates them. */
export function connectionForSession(
  entries: ConnectionRecord[],
  session: { transport: string; endpoint: string; label: string },
) {
  const candidates = entries.filter(
    (entry) =>
      entry.request.kind === session.transport &&
      presetEndpoint(entry.request) === session.endpoint,
  );
  if (candidates.length === 1) return candidates[0];
  const named = candidates.filter(
    (entry) => entry.request.label === session.label,
  );
  return named.length === 1 ? named[0] : undefined;
}

// A Session first used through attachment supplies its endpoint. Unknown
// transport settings are the editable new-form defaults, never auto-applied.
export function sharedPreset(s: {
  transport: string;
  endpoint: string;
  label: string;
}): ConnectionRequest | undefined {
  let request: ConnectionRequest;
  if (s.transport === "serial")
    request = {
      kind: "serial",
      port: s.endpoint,
      label: s.label,
      baud: 115200,
      dataBits: 8,
      parity: "none",
      stopBits: "1",
      flowControl: "none",
      wake: false,
    };
  else if (s.transport === "ssh") {
    const match = /^(.+)@(?:\[([^\]]+)\]|([^:]+)):(\d+)$/.exec(s.endpoint);
    if (!match) return;
    request = {
      kind: "ssh",
      user: match[1]!,
      host: match[2] ?? match[3]!,
      port: Number(match[4]),
      label: s.label,
      identityFiles: [],
      knownHostsFile: "",
    };
  } else return;
  return isConnectionRequest(request) ? request : undefined;
}
