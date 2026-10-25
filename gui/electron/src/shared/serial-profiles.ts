import { isConnectionRequest } from "./connections";

/** Resolved Host configuration only; no GUI-owned profile store or config path. */
export interface SerialProfile {
  port: string;
  baud: number;
  dataBits: number;
  parity: string;
  stopBits: string;
  flowControl: string;
  wake: boolean;
}
export interface SerialProfiles {
  default: string;
  profiles: { name: string; profile: SerialProfile }[];
}
export type SerialProfileChange =
  | { action: "save"; name: string; profile: SerialProfile; replace: boolean }
  | { action: "default"; name: string };

export function isProfileName(value: unknown): value is string {
  return (
    typeof value === "string" &&
    value.length > 0 &&
    value === value.trim() &&
    new TextEncoder().encode(value).length <= 128 &&
    !/[\x00-\x1f\x7f]/.test(value)
  );
}
export function isSerialProfile(value: unknown): value is SerialProfile {
  if (!value || typeof value !== "object" || Array.isArray(value)) return false;
  const p = value as Record<string, unknown>;
  return (
    Object.keys(p).length === 7 &&
    typeof p.wake === "boolean" &&
    typeof p.port === "string" &&
    new TextEncoder().encode(p.port).length <= 512 &&
    isConnectionRequest({ ...p, kind: "serial", label: "" })
  );
}
export function isSerialProfiles(value: unknown): value is SerialProfiles {
  if (!value || typeof value !== "object" || Array.isArray(value)) return false;
  const s = value as Record<string, unknown>;
  return (
    Object.keys(s).length === 2 &&
    (s.default === "" || isProfileName(s.default)) &&
    Array.isArray(s.profiles) &&
    s.profiles.length <= 256 &&
    s.profiles.every((p: unknown) => {
      if (!p || typeof p !== "object" || Array.isArray(p)) return false;
      const entry = p as Record<string, unknown>;
      return (
        Object.keys(entry).length === 2 &&
        isProfileName(entry.name) &&
        isSerialProfile(entry.profile)
      );
    }) &&
    new Set(s.profiles.map((p) => p.name)).size === s.profiles.length
  );
}
export function isSerialProfileChange(
  value: unknown,
): value is SerialProfileChange {
  if (!value || typeof value !== "object" || Array.isArray(value)) return false;
  const v = value as Record<string, unknown>;
  return (
    isProfileName(v.name) &&
    (v.action === "default"
      ? Object.keys(v).length === 2
      : v.action === "save" &&
        Object.keys(v).length === 4 &&
        typeof v.replace === "boolean" &&
        isSerialProfile(v.profile))
  );
}
