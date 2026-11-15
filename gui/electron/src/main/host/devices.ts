import type {
  DeviceDecision,
  DeviceSelection,
  DiscoveredDevice,
  DiscoverySnapshot,
} from "../../shared/devices";
import { incompatible, object } from "./validation";

function bounded(value: unknown, maximum = 512, empty = true): string {
  if (
    typeof value !== "string" ||
    value.length > maximum ||
    (!empty && !value.trim()) ||
    /[\x00-\x1f\x7f]/.test(value)
  )
    throw incompatible();
  return value;
}
function timestamp(value: unknown): string {
  const result = bounded(value, 64, false);
  if (!Number.isFinite(Date.parse(result))) throw incompatible();
  return result;
}

/** Older Hosts without scan health remain readable, but cannot validate a device selection. */
export function discoverySnapshot(value: unknown): DiscoverySnapshot {
  const r = object(value);
  const state = r.scan_state ?? "unknown";
  if (
    state !== "current" &&
    state !== "failed" &&
    state !== "stale" &&
    state !== "unknown"
  )
    throw incompatible();
  const lastScan =
    r.last_scan_at === undefined || r.last_scan_at === ""
      ? ""
      : timestamp(r.last_scan_at);
  if (state === "current" && !lastScan) throw incompatible();
  return { devices: discoveredDevices(r.devices), state, lastScan };
}

/** Bound and validate USB metadata without assigning identities or inferring OS access. */
export function discoveredDevices(value: unknown): DiscoveredDevice[] {
  if (!Array.isArray(value) || value.length > 1024) throw incompatible();
  const ids = new Set<string>();
  const endpoints = new Set<string>();
  return value.map((entry) => {
    const r = object(entry);
    const id = bounded(r.device_id);
    const method = r.identity_method;
    if (
      (method !== "usb_serial" &&
        method !== "usb_path" &&
        method !== "runtime" &&
        method !== "") ||
      typeof r.persistent !== "boolean" ||
      r.state !== "present" ||
      ((method === "runtime" || method === "") && r.persistent) ||
      (method !== "" && !id)
    )
      throw incompatible();
    const transport = bounded(r.transport, 64, false);
    const endpoint = bounded(r.endpoint, 512, false);
    const key = JSON.stringify([transport, endpoint]);
    if (endpoints.has(key) || (id && ids.has(id))) throw incompatible();
    endpoints.add(key);
    if (id) ids.add(id);
    return {
      id,
      identity: method || "unknown",
      persistent: r.persistent,
      transport,
      endpoint,
      vid: bounded(r.vid),
      pid: bounded(r.pid),
      usbSerial: bounded(r.usb_serial),
      usbPath: bounded(r.usb_path),
      manufacturer: bounded(r.manufacturer),
      product: bounded(r.product),
      firstSeen: timestamp(r.first_seen),
      lastSeen: timestamp(r.last_seen),
    };
  });
}

/** Validate the Host's finite decision for the requested endpoint; never execute its action. */
export function deviceDecision(
  value: unknown,
  selected: DeviceSelection,
): DeviceDecision {
  const r = object(value);
  if (
    r.transport !== "serial" ||
    r.endpoint !== selected.endpoint ||
    typeof r.present !== "boolean" ||
    typeof r.connected !== "boolean" ||
    (r.policy !== "ask" && r.policy !== "auto" && r.policy !== "deny") ||
    (r.action !== "none" &&
      r.action !== "ask" &&
      r.action !== "connect" &&
      r.action !== "deny")
  )
    throw incompatible();
  const reason = r.reason ?? "";
  if (
    reason !== "" &&
    reason !== "device_not_present" &&
    reason !== "already_connected"
  )
    throw incompatible();
  if (
    !r.present
      ? reason !== "device_not_present" || r.action !== "none"
      : r.connected
        ? reason !== "already_connected" || r.action !== "none"
        : reason !== ""
  )
    throw incompatible();
  return {
    present: r.present,
    connected: r.connected,
    policy: r.policy,
    action: r.action,
    reason,
    ...(r.present && r.connected
      ? {
          sessionId: bounded(r.session_id, 512, false),
          sessionRef: bounded(r.session_ref),
        }
      : {}),
  };
}
