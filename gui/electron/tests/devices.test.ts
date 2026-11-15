import test from "node:test";
import assert from "node:assert/strict";
import { HostClient } from "../src/main/host/client";
import { discoverySnapshot, deviceDecision } from "../src/main/host/devices";
import { isDeviceSelection } from "../src/shared/devices";
import {
  isConnectionRequest,
  type ConnectionRequest,
} from "../src/shared/connections";
import { observeDevices } from "../src/renderer/devices/observations";
import { startFixture } from "./fixture";

const serial: ConnectionRequest = {
  kind: "serial",
  port: "COM-FIXTURE",
  baud: 115200,
  dataBits: 8,
  parity: "none",
  stopBits: "1",
  flowControl: "none",
  label: "Device fixture",
};
const record = {
  device_id: "dev_fixture",
  identity_method: "usb_serial",
  persistent: true,
  transport: "serial",
  endpoint: "COM-FIXTURE",
  state: "present",
  vid: "1234",
  pid: "5678",
  usb_serial: "FIXTURE",
  usb_path: "fixture/usb",
  manufacturer: "Fixture",
  product: "Echo",
  first_seen: "2026-11-16T00:00:00Z",
  last_seen: "2026-11-16T00:00:00Z",
};
const payload = {
  devices: [record],
  scan_state: "current",
  last_scan_at: record.last_seen,
};

test("device snapshots validate bounded identity, freshness and observation history", () => {
  const first = discoverySnapshot(payload);
  assert.equal(first.state, "current");
  assert.equal(discoverySnapshot({ devices: [record] }).state, "unknown");
  for (const change of [
    { scan_state: "invented" },
    { last_scan_at: "" },
    { last_scan_at: "invalid" },
    { devices: new Array(1025).fill(record) },
    { devices: [record, record] },
    { devices: [{ ...record, identity_method: "runtime", persistent: true }] },
    { devices: [{ ...record, endpoint: "bad\nport" }] },
    { devices: [{ ...record, device_id: "" }] },
    { devices: [{ ...record, product: "x".repeat(513) }] },
  ])
    assert.throws(() => discoverySnapshot({ ...payload, ...change }));
  const selected = { deviceId: record.device_id, endpoint: record.endpoint };
  assert.equal(isDeviceSelection(selected), true);
  assert.equal(isConnectionRequest({ ...serial, discovery: selected }), true);
  for (const change of [
    { endpoint: "other" },
    { endpoint: "bad\nport" },
    { deviceId: "" },
    { extra: true },
  ])
    assert.equal(
      isConnectionRequest({ ...serial, discovery: { ...selected, ...change } }),
      false,
    );
  let seen = observeDevices([], first.devices);
  seen = observeDevices(seen.rows, [
    { ...first.devices[0]!, endpoint: "COM-MOVED" },
  ]);
  assert.equal(seen.rows[0]!.previousEndpoint, "COM-FIXTURE");
  seen = observeDevices(seen.rows, []);
  assert.equal(seen.rows[0]!.present, false);
  const many = observeDevices(
    [],
    new Array(129).fill(0).map((_, i) => ({
      ...first.devices[0]!,
      id: `dev_${i}`,
      endpoint: `COM${i}`,
    })),
  );
  assert.equal(many.rows.length, 128);
  assert.equal(many.truncated, true);
  assert.throws(() =>
    deviceDecision(
      {
        transport: "serial",
        endpoint: "other",
        present: true,
        connected: false,
        policy: "ask",
        action: "ask",
      },
      selected,
    ),
  );
  assert.throws(() =>
    deviceDecision(
      {
        transport: "serial",
        endpoint: selected.endpoint,
        present: true,
        connected: true,
        policy: "ask",
        action: "connect",
      },
      selected,
    ),
  );
});

test("real Registry discovery never opens ports and rejects failed, disappeared, moved or ambiguous selections", async () => {
  const fixture = await startFixture();
  const client = new HostClient(fixture.endpoint, fixture.token);
  try {
    await client.connect();
    const initial = await client.devices();
    const original = initial.devices[0]!;
    const selected = { deviceId: original.id, endpoint: original.endpoint };
    assert.equal(initial.state, "current");
    assert.equal(original.identity, "usb_serial");
    assert.equal((await client.deviceDecision(selected)).connected, false);
    assert.equal((await fixture.command("device-open-count")).data, "0");
    await fixture.command("devices", "failed");
    const failed = await client.devices();
    assert.equal(failed.state, "failed");
    assert.deepEqual(failed.devices, initial.devices);
    await assert.rejects(
      client.openConnection({ ...serial, discovery: selected }),
      { reason: "discovery_stale" },
    );
    await fixture.command("devices", "absent");
    assert.deepEqual((await client.devices()).devices, []);
    await assert.rejects(client.deviceDecision(selected), {
      reason: "device_changed",
    });
    await assert.rejects(
      client.openConnection({ ...serial, discovery: selected }),
      { reason: "device_changed" },
    );
    await fixture.command("devices", "moved");
    const moved = (await client.devices()).devices[0]!;
    assert.equal(moved.id, original.id);
    assert.equal(moved.endpoint, "COM-MOVED");
    await assert.rejects(
      client.openConnection({ ...serial, discovery: selected }),
      { reason: "device_changed" },
    );
    await fixture.command("devices", "ambiguous");
    const ambiguous = (await client.devices()).devices;
    assert.equal(ambiguous.length, 2);
    assert.equal(new Set(ambiguous.map((d) => d.id)).size, 2);
    assert.ok(
      ambiguous.every(
        (d) =>
          d.identity === "runtime" && !d.persistent && d.id !== original.id,
      ),
    );
    await assert.rejects(
      client.openConnection({ ...serial, discovery: selected }),
      { reason: "device_changed" },
    );
    assert.equal((await fixture.command("device-open-count")).data, "0");
    await fixture.command("devices", "");
    const current = (await client.devices()).devices[0]!;
    const fence = { deviceId: current.id, endpoint: current.endpoint };
    const opened = await client.openConnection({ ...serial, discovery: fence });
    const decision = await client.deviceDecision(fence);
    assert.equal(decision.connected, true);
    assert.equal(decision.reason, "already_connected");
    assert.equal(decision.action, "none");
    assert.equal(decision.sessionId, opened.sessionId);
    assert.equal((await fixture.command("device-open-count")).data, "1");
    await client.disconnect();
    await client.connect();
    assert.ok(
      (await client.list()).some(
        (s) => s.session_id === opened.sessionId && s.state === "open",
      ),
    );
  } finally {
    await client.disconnect();
    await fixture.stop();
  }
});

test("typed serial access reasons survive MCP without probing or retrying", async () => {
  const fixture = await startFixture();
  const client = new HostClient(fixture.endpoint, fixture.token);
  try {
    await client.connect();
    let count = 0;
    for (const reason of [
      "serial_not_found",
      "serial_permission_denied",
      "serial_busy",
      "serial_busy_or_denied",
      "serial_open_failed",
    ]) {
      await fixture.command("device-open-reason", reason);
      await assert.rejects(client.openConnection(serial), {
        code: "tool_error",
        reason,
      });
      assert.equal(
        (await fixture.command("device-open-count")).data,
        String(++count),
      );
    }
    await fixture.command("device-open-reason", "");
    assert.ok((await client.openConnection(serial)).sessionId);
  } finally {
    await client.disconnect();
    await fixture.stop();
  }
});

test("selection waits for an older read then obtains a fresh snapshot; disconnect cancels pending discovery", async () => {
  const fixture = await startFixture();
  const client = new HostClient(fixture.endpoint, fixture.token);
  const originalFetch = globalThis.fetch;
  let release = () => {};
  try {
    await client.connect();
    const initial = (await client.devices()).devices[0]!;
    const selected = { deviceId: initial.id, endpoint: initial.endpoint };
    let entered!: () => void;
    let reached = new Promise<void>((done) => {
      entered = done;
    });
    let gate = new Promise<void>((done) => {
      release = done;
    });
    let hold = true;
    let reads = 0;
    globalThis.fetch = async (input, init) => {
      const body =
        typeof init?.body === "string" ? JSON.parse(init.body) : undefined;
      const response = await originalFetch(input, init);
      if (
        body?.method === "tools/call" &&
        body.params?.name === "terminal_list_devices"
      ) {
        reads++;
        if (hold) {
          hold = false;
          entered();
          await gate;
        }
      }
      return response;
    };
    const observing = client.devices();
    assert.equal(client.devices(), observing, "concurrent reads must coalesce");
    await reached;
    const opening = client.openConnection({ ...serial, discovery: selected });
    const rejected = assert.rejects(opening, { reason: "device_changed" });
    await fixture.command("devices", "moved");
    release();
    assert.equal((await observing).devices[0]!.endpoint, initial.endpoint);
    await rejected;
    assert.equal(reads, 2, "explicit open must read after the older request");
    assert.equal((await fixture.command("device-open-count")).data, "0");
    gate = new Promise<void>((done) => {
      release = done;
    });
    reached = new Promise<void>((done) => {
      entered = done;
    });
    hold = true;
    const pending = client.devices();
    const cancelled = assert.rejects(pending);
    await reached;
    await client.disconnect();
    release();
    await cancelled;
    await client.connect();
    assert.equal((await client.devices()).devices[0]!.endpoint, "COM-MOVED");
  } finally {
    release();
    globalThis.fetch = originalFetch;
    await client.disconnect();
    await fixture.stop();
  }
});
