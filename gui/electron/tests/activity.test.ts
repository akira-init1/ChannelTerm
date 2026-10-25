import test from "node:test";
import assert from "node:assert/strict";
import { ActivityFeed } from "../src/main/host/activity";
import { HostClient } from "../src/main/host/client";
import { startFixture } from "./fixture";

const stamp = "2026-10-25T12:00:00+09:00";
function activity(next: number, count = 1, dropped = false) {
  return {
    next,
    dropped,
    events: Array.from({ length: count }, () => ({
      timestamp: stamp,
      actor: "agent",
      operation: "write",
      encoding: "base64",
      data: Buffer.from("private-input").toString("base64"),
    })),
  };
}
function events(next: number, count = 1, dropped = false) {
  return {
    next,
    dropped,
    events: Array.from({ length: count }, (_, index) => ({
      id: next - count + index,
      session_id: "session",
      timestamp: stamp,
      actor: "system",
      type: "LEASE_ACQUIRED",
      metadata: {
        type: "terminal",
        owner: "private-owner",
        command: "private-command",
        password: "private-password",
      },
    })),
  };
}

test("activity observations keep independent cursors and strip write payloads and private metadata", () => {
  const feed = new ActivityFeed("session");
  assert.deepEqual(feed.parameters("activity"), {
    session_id: "session",
    max_events: 32,
  });
  const initial = feed.apply(activity(8), events(40));
  assert.equal(initial.rows.length, 2);
  assert.ok(initial.rows.every((row) => row.history));
  assert.equal(initial.rows[0]!.timestamp, "2026-10-25T03:00:00.000Z");
  assert.equal(initial.rows.find((row) => row.bytes !== undefined)!.bytes, 13);
  assert.doesNotMatch(JSON.stringify(initial), /private-|cHJpdmF0ZS1pbnB1dA/);
  assert.deepEqual(feed.parameters("events"), {
    session_id: "session",
    max_events: 32,
    cursor: 40,
    timeout_ms: 1,
  });
  const next = feed.apply(activity(9), undefined);
  assert.equal(feed.eventCursor, 40);
  assert.equal(next.rows.filter((row) => !row.history).length, 1);
  next.rows[0]!.details = "mutated";
  assert.doesNotMatch(
    JSON.stringify(feed.apply(undefined, undefined)),
    /mutated/,
  );
});

test("activity pages validate atomically, bound retention and keep loss notices", () => {
  const feed = new ActivityFeed("session");
  feed.apply(activity(1), events(1));
  for (const bad of [
    { ...events(2), next: 3 },
    { ...events(2), events: [{ ...events(2).events[0], id: 0 }] },
    {
      ...events(2),
      events: [{ ...events(2).events[0], session_id: "foreign" }],
    },
    {
      ...events(2),
      events: [{ ...events(2).events[0], timestamp: "not-a-time" }],
    },
    events(34, 33),
  ]) {
    assert.throws(() => feed.apply(activity(2), bad), { code: "incompatible" });
    assert.equal(feed.activityCursor, 1);
    assert.equal(feed.eventCursor, 1);
  }
  for (const data of ["?===", "abc", "===="])
    assert.throws(
      () =>
        feed.apply(
          { ...activity(2), events: [{ ...activity(2).events[0], data }] },
          undefined,
        ),
      { code: "incompatible" },
    );
  let snapshot = feed.apply(activity(50, 1, true), events(50, 1, true));
  assert.equal(snapshot.dropped, true);
  assert.throws(() => feed.apply(activity(51, 32, true), undefined), {
    code: "incompatible",
  });
  for (let cursor = 82; cursor <= 306; cursor += 32)
    snapshot = feed.apply(activity(cursor, 32), undefined);
  assert.equal(snapshot.rows.length, 200);
  assert.equal(snapshot.truncated, true);
  assert.equal(snapshot.dropped, true);
});

test("client debug reports remain bounded plain claims instead of complete audits", () => {
  const page = events(1);
  const row = {
    ...page.events[0]!,
    type: "DEBUG_CLIENT_REPORT",
    metadata: {
      text: "<script>\u001b" + "x".repeat(1000),
      complete_audit: true,
      secret: "private",
    },
  };
  const snapshot = new ActivityFeed("session").apply(undefined, {
    ...page,
    events: [row],
  });
  assert.equal(snapshot.rows[0]!.clientReport, true);
  assert.equal(snapshot.rows[0]!.details.length, 512);
  assert.doesNotMatch(
    JSON.stringify(snapshot),
    /private|complete_audit|\\u001b/,
  );
});

test("real Host activity is independent of output, handles timeout and rejects foreign, stale and overlapping reads", async () => {
  const fixture = await startFixture();
  const client = new HostClient(fixture.endpoint, fixture.token);
  const observer = new HostClient(fixture.endpoint, fixture.token);
  try {
    await client.connect();
    await observer.connect();
    const sessionId = (await client.list())[0]!.session_id;
    const active = await client.attach(sessionId),
      foreign = await observer.attach(sessionId);
    await client.read(active.id);
    await assert.rejects(client.activity(foreign.id), { code: "cancelled" });
    await fixture.command("agent-input", "secret-agent-input");
    const snapshot = await client.activity(active.id);
    assert.ok(
      snapshot.rows.some(
        (row) => row.actor === "agent" && row.bytes === 18 && row.history,
      ),
    );
    assert.doesNotMatch(JSON.stringify(snapshot), /secret-agent-input/);
    assert.match(
      Buffer.from((await client.read(active.id)).data).toString(),
      /secret-agent-input/,
    );
    const pending = client.activity(active.id);
    await assert.rejects(client.activity(active.id), { code: "busy" });
    assert.deepEqual(await pending, snapshot);
    await fixture.command("lease");
    const leased = await client.activity(active.id);
    assert.ok(
      leased.rows.some((row) => row.type === "LEASE_ACQUIRED" && !row.history),
    );
    await fixture.command("release");
    await fixture.command("debug-report", "client-reported-result");
    assert.ok(
      (await client.activity(active.id)).rows.some(
        (row) => row.clientReport && row.details === "client-reported-result",
      ),
    );
    const stale = client.activity(active.id);
    const rejected = assert.rejects(stale, { code: "cancelled" });
    await client.detach(active.id);
    await rejected;
    await assert.rejects(client.activity(active.id), { code: "cancelled" });
    await observer.write(foreign.id, "survives-detach");
  } finally {
    await client.disconnect();
    await observer.disconnect();
    await fixture.stop();
  }
});
