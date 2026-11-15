/** Host-assigned identity and last successful observation; endpoints are not durable IDs. */
export interface DiscoveredDevice {
  id: string;
  identity: "usb_serial" | "usb_path" | "runtime" | "unknown";
  persistent: boolean;
  transport: string;
  endpoint: string;
  vid: string;
  pid: string;
  usbSerial: string;
  usbPath: string;
  manufacturer: string;
  product: string;
  firstSeen: string;
  lastSeen: string;
}

/** Host clock determines freshness; retained rows can outlive successful discovery. */
export interface DiscoverySnapshot {
  devices: DiscoveredDevice[];
  state: "current" | "failed" | "stale" | "unknown";
  lastScan: string;
}

/** Transient selection fence, never saved in connection presets. */
export interface DeviceSelection {
  deviceId: string;
  endpoint: string;
}

/** A read-only Host decision is advice for discovery, not an instruction to open. */
export interface DeviceDecision {
  present: boolean;
  connected: boolean;
  policy: "ask" | "auto" | "deny";
  action: "none" | "ask" | "connect" | "deny";
  reason: "device_not_present" | "already_connected" | "";
  sessionId?: string;
  sessionRef?: string;
}

/** Accept only a finite target; Main rechecks the ID against the current Host snapshot. */
export function isDeviceSelection(value: unknown): value is DeviceSelection {
  if (!value || typeof value !== "object" || Array.isArray(value)) return false;
  const v = value as Record<string, unknown>;
  return (
    Object.keys(v).length === 2 &&
    [v.deviceId, v.endpoint].every(
      (text) =>
        typeof text === "string" &&
        text.trim().length > 0 &&
        text.length <= 512 &&
        !/[\x00-\x1f\x7f]/.test(text),
    )
  );
}
