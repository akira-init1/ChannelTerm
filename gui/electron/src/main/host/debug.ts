import type { DebugOverview, DebugObservation } from "../../shared/debug";
import { incompatible, integer, object, text } from "./validation";

function label(value: unknown, limit = 256): string {
  const result = text(value);
  if (
    Buffer.byteLength(result, "utf8") > limit ||
    /[\x00-\x1f\x7f]/.test(result)
  )
    throw incompatible();
  return result;
}
function choice(value: unknown, choices: string[]): string {
  const result = label(value);
  if (!choices.includes(result)) throw incompatible();
  return result;
}
function timestamp(value: unknown): string {
  const result = label(value, 64);
  if (!Number.isFinite(Date.parse(result))) throw incompatible();
  return result;
}
const connection = [
  "unknown",
  "connecting",
  "connected",
  "disconnected",
  "listening",
  "recovering",
];
const target = ["unknown", "running", "stopped", "exited"];

/** Copy only bounded public fields; never forward raw Host objects through IPC. */
export function debugOverview(value: unknown): DebugOverview {
  const data = object(value);
  if (!Array.isArray(data.operations) || data.operations.length > 384)
    throw incompatible();
  const ids = new Set<string>();
  const operations: DebugObservation[] = data.operations.map((entry) => {
    const r = object(entry),
      client = object(r.client);
    const id = label(r.id, 128);
    if (
      !id ||
      ids.has(id) ||
      typeof r.stale !== "boolean" ||
      !Array.isArray(r.ports) ||
      r.ports.length > 3
    )
      throw incompatible();
    ids.add(id);
    const roles = new Set<string>();
    const ports = r.ports.map((item) => {
      const p = object(item);
      const role = choice(p.role, [
        "authenticated_bridge",
        "local_forward",
        "ssh",
        "target",
      ]);
      const port = integer(p.port),
        address = label(p.address);
      const state = choice(p.state, ["observed", "unknown", "not_applicable"]);
      if (
        roles.has(role) ||
        port > 65535 ||
        (state === "observed"
          ? !port || !address
          : port !== 0 || address !== "")
      )
        throw incompatible();
      roles.add(role);
      return {
        role,
        port,
        address,
        state,
        configured: label(p.configured),
        perspective: choice(p.perspective, ["host", "executor", "remote"]),
      };
    });
    return {
      id,
      name: label(r.name, 128),
      mode: choice(r.mode, [
        "serial_bridge",
        "managed",
        "tcp",
        "ssh_tunnel",
        "ssh_stdio",
        "ssh_terminal",
        "ssh_terminal_tcp",
      ]),
      session_id: label(r.session_id, 512),
      connection: choice(r.connection, connection),
      target_state: choice(r.target_state, target),
      last_target_state: choice(r.last_target_state, target),
      tunnel_state: choice(r.tunnel_state, [...connection, "not_applicable"]),
      source: choice(r.source, ["host", "manual", "client_report"]),
      client: {
        client_id: label(client.client_id, 128),
        name: label(client.name, 128),
        kind: label(client.kind, 32),
      },
      updated_at: timestamp(r.updated_at),
      stale: r.stale,
      ports,
    };
  });
  return { observed_at: timestamp(data.observed_at), operations };
}
