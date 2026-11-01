import test from "node:test";
import assert from "node:assert/strict";
import { HostClient } from "../src/main/host/client";
import { debugOverview } from "../src/main/host/debug";
import { debugStale } from "../src/shared/debug";
import { startFixture } from "./fixture";

test("debug overview reads registered evidence without attaching, owning or changing Session", async () => {
  const fixture = await startFixture();
  const client = new HostClient(fixture.endpoint, fixture.token);
  try {
    await client.connect();
    assert.deepEqual((await client.debugOverview()).operations, []);
    const before = await client.list();
    await fixture.command("debug-overview");
    const snapshot = await client.debugOverview();
    const entry = snapshot.operations[0];
    assert.ok(entry);
    assert.equal(entry.source, "client_report");
    assert.equal(entry.target_state, "stopped");
    assert.equal(entry.session_id, "electron-fixture");
    assert.equal(entry.ports[0]!.port, 43210);
    assert.equal(entry.ports[0]!.configured, "127.0.0.1:0");
    assert.equal(entry.ports[1]!.port, 2222);
    assert.equal(entry.ports[2]!.perspective, "remote");
    assert.deepEqual(await client.list(), before);
    assert.deepEqual(
      (await client.debugOverview()).operations,
      snapshot.operations,
    );
    assert.equal(JSON.stringify(snapshot).includes("fixture-owner"), false);
    assert.equal(
      debugStale(snapshot, false, Date.parse(snapshot.observed_at) + 10001),
      true,
    );
    assert.equal(debugStale(snapshot, true), true);
    for (const mutate of [
      (v: typeof snapshot) => {
        v.operations[0]!.ports[0]!.port = 65536;
      },
      (v: typeof snapshot) => {
        v.operations[0]!.connection = "guess";
      },
      (v: typeof snapshot) => {
        v.operations[0]!.updated_at = "yesterday";
      },
      (v: typeof snapshot) => {
        v.operations.push(v.operations[0]!);
      },
    ]) {
      const invalid = structuredClone(snapshot);
      mutate(invalid);
      assert.throws(() => debugOverview(invalid), { code: "incompatible" });
    }
    const sanitized = debugOverview({
      ...snapshot,
      owner: "private",
      operations: snapshot.operations.map((r) => ({ ...r, owner: "private" })),
    });
    assert.equal(JSON.stringify(sanitized).includes("private"), false);
  } finally {
    await client.disconnect();
    await fixture.stop();
  }
});

test("debug observation after disconnect fails without returning a cached live state", async () => {
  const fixture = await startFixture();
  const client = new HostClient(fixture.endpoint, fixture.token);
  try {
    await client.connect();
    await client.disconnect();
    await assert.rejects(client.debugOverview(), { code: "disconnected" });
  } finally {
    await client.disconnect();
    await fixture.stop();
  }
});
