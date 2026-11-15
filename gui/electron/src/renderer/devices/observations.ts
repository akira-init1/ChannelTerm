import type { DiscoveredDevice } from "../../shared/devices";

/** Window-local observations, not an identity registry or persistent connection profile. */
export interface DeviceObservation extends DiscoveredDevice {
  present: boolean;
  previousEndpoint?: string;
}

function key(device: DiscoveredDevice): string {
  // Legacy Hosts can omit IDs. This fallback tracks an observation only, never a selection fence.
  return device.id
    ? `id:${device.id}`
    : JSON.stringify([device.transport, device.endpoint]);
}

/** Apply only a successful snapshot. Failed reads must leave presence unknown, not absent. */
export function observeDevices(
  previous: DeviceObservation[],
  current: DiscoveredDevice[],
): { rows: DeviceObservation[]; truncated: boolean } {
  const old = new Map(previous.map((device) => [key(device), device]));
  const present = new Set(current.map(key));
  const rows = current.map((device) => {
    const before = old.get(key(device));
    return {
      ...device,
      present: true,
      ...(before?.endpoint !== undefined && before.endpoint !== device.endpoint
        ? { previousEndpoint: before.endpoint }
        : before?.previousEndpoint
          ? { previousEndpoint: before.previousEndpoint }
          : {}),
    };
  });
  rows.push(
    ...previous
      .filter((device) => !present.has(key(device)))
      .map((device) => ({ ...device, present: false })),
  );
  return { rows: rows.slice(0, 128), truncated: rows.length > 128 };
}
