import test from "node:test";
import assert from "node:assert/strict";
import { HostClient } from "../src/main/host/client";
import { RemoteSize } from "../src/main/host/resize";
import { ResizeQueue } from "../src/renderer/terminal/resize";
import { startFixture } from "./fixture";

test("SSH creator controls real PTY size; reuse, takeover and detach preserve Session", async () => {
  const fixture = await startFixture();
  const owner = new HostClient(fixture.endpoint, fixture.token);
  const other = new HostClient(fixture.endpoint, fixture.token);
  try {
    await owner.connect();
    await other.connect();
    const request = {
      kind: "ssh",
      host: "127.0.0.1",
      port: fixture.sshPort,
      user: "fixture-user",
      password: "fixture-password",
      identityFiles: [],
      knownHostsFile: fixture.knownHosts,
      label: "Resize fixture",
    } as const;
    const opened = await owner.openConnection(request);
    const a = await owner.attach(opened.sessionId);
    assert.equal(a.remoteSize, true);
    assert.equal(a.ownsRemoteSize, true);
    assert.deepEqual(Object.keys(a).sort(), [
      "id",
      "ownsRemoteSize",
      "remoteSize",
      "sessionId",
    ]);
    assert.equal((await other.openConnection(request)).reused, true);
    const b = await other.attach(opened.sessionId);
    assert.equal(b.ownsRemoteSize, false);
    assert.equal(await other.resize(b.id, { columns: 90, rows: 30 }), false);
    await assert.rejects(other.resize(a.id, { columns: 100, rows: 40 }), {
      code: "cancelled",
    });
    for (const size of [
      { columns: 0, rows: 1 },
      { columns: 1001, rows: 1 },
      { columns: 1.5, rows: 1 },
      { columns: 80, rows: 24, owner: "injected" },
    ])
      await assert.rejects(owner.resize(a.id, size), { code: "invalid_input" });
    assert.equal(await owner.resize(a.id, { columns: 132, rows: 43 }), true);
    let output = "";
    const deadline = Date.now() + 5000;
    while (!output.includes("PTY SIZE 132 43") && Date.now() < deadline) {
      const chunk = await owner.read(a.id);
      output += new TextDecoder().decode(new Uint8Array(chunk.data));
    }
    assert.match(output, /PTY SIZE 132 43/);
    assert.equal(await other.resizeControl(b.id, "take"), true);
    assert.equal(await owner.resize(a.id, { columns: 80, rows: 24 }), false);
    await owner.detach(a.id); // A stale release cannot revoke the new controller.
    assert.equal(await other.resizeControl(b.id, "status"), true);
    assert.equal(await other.resize(b.id, { columns: 101, rows: 31 }), true);
    assert.equal(await other.resizeControl(b.id, "release"), false);
    assert.equal(await other.resize(b.id, { columns: 80, rows: 24 }), false);
    assert.equal(await other.resizeControl(b.id, "take"), true);
    await other.detach(b.id);
    const c = await owner.attach(opened.sessionId);
    assert.equal(c.ownsRemoteSize, false);
    assert.equal(await owner.resizeControl(c.id, "status"), false);
    assert.equal(
      (await owner.list()).find((s) => s.session_id === opened.sessionId)
        ?.state,
      "open",
    );
    const serial = await owner.attach("electron-fixture");
    assert.equal(serial.remoteSize, false);
    assert.equal(
      await owner.resize(serial.id, { columns: 80, rows: 24 }),
      false,
    );
    await assert.rejects(owner.resizeControl(serial.id, "take"), {
      code: "incompatible",
    });
  } finally {
    await owner.disconnect();
    await other.disconnect();
    await fixture.stop();
  }
});

test("late takeover response is released during disposal; requests stay bounded", async () => {
  const original = globalThis.fetch;
  const actions: string[] = [];
  let respond!: (response: Response) => void;
  try {
    globalThis.fetch = async (_url, init) => {
      const body = JSON.parse(String(init?.body)) as {
        action: string;
        owner: string;
      };
      actions.push(body.action);
      if (body.action === "take")
        return new Promise<Response>((resolve) => {
          respond = resolve;
        });
      assert.equal(body.owner, "a".repeat(64));
      assert.equal(init?.signal?.aborted, false);
      return Response.json({ owned: false });
    };
    const connection = new AbortController();
    const size = new RemoteSize(
      new URL("http://127.0.0.1:1/mcp"),
      "fake-token",
      "fake-session",
      "",
      connection.signal,
    );
    const take = size.request("take");
    await assert.rejects(size.request("take"), { code: "busy" });
    const disposing = size.dispose();
    connection.abort();
    respond(Response.json({ owned: true, owner: "a".repeat(64) }));
    await take;
    await disposing;
    await size.dispose();
    assert.deepEqual(actions, ["take", "release"]);
    await assert.rejects(size.request("take"), { code: "cancelled" });
  } finally {
    globalThis.fetch = original;
  }
});

test("geometry coalesces latest size and disposes without replaying pending work", async () => {
  const sent: number[] = [];
  let finish!: () => void;
  let started!: () => void;
  const ready = new Promise<void>((resolve) => {
    started = resolve;
  });
  const queue = new ResizeQueue(
    async (size) => {
      sent.push(size.columns);
      started();
      await new Promise<void>((resolve) => {
        finish = resolve;
      });
    },
    (error) => {
      throw error;
    },
    1,
  );
  queue.schedule(80, 24);
  queue.schedule(100, 30);
  assert.equal(queue.busy, true);
  await ready;
  queue.schedule(120, 40);
  queue.schedule(132, 43);
  assert.deepEqual(sent, [100]);
  queue.dispose();
  finish();
  await new Promise((resolve) => setTimeout(resolve, 10));
  assert.deepEqual(sent, [100]);
});

test("disconnect during size discovery cannot revive a connected client", async () => {
  const fixture = await startFixture();
  const client = new HostClient(fixture.endpoint, fixture.token);
  const original = globalThis.fetch;
  let respond!: (response: Response) => void;
  let reached!: () => void;
  const ready = new Promise<void>((resolve) => {
    reached = resolve;
  });
  try {
    globalThis.fetch = async (url, init) => {
      if (new URL(url.toString()).pathname === "/gui/v1/resize") {
        reached();
        return new Promise<Response>((resolve) => {
          respond = resolve;
        });
      }
      return original(url, init);
    };
    const connecting = client.connect();
    await ready;
    await client.disconnect();
    respond(Response.json({ available: true }));
    await assert.rejects(connecting, { code: "cancelled" });
  } finally {
    globalThis.fetch = original;
    await client.disconnect();
    await fixture.stop();
  }
});

test("geometry sends only the latest pending dimensions after an in-flight change", async () => {
  const sent: number[] = [];
  let finish!: () => void;
  let started!: () => void;
  let completed!: () => void;
  const ready = new Promise<void>((resolve) => {
    started = resolve;
  });
  const done = new Promise<void>((resolve) => {
    completed = resolve;
  });
  const queue = new ResizeQueue(
    async (size) => {
      sent.push(size.columns);
      if (sent.length === 1) {
        started();
        await new Promise<void>((resolve) => {
          finish = resolve;
        });
      } else completed();
    },
    (error) => {
      throw error;
    },
    1,
  );
  try {
    queue.schedule(80, 24);
    await ready;
    queue.schedule(100, 30);
    queue.schedule(132, 43);
    finish();
    await done;
    await new Promise((resolve) => setTimeout(resolve, 0));
    assert.equal(queue.busy, false);
    assert.deepEqual(sent, [80, 132]);
  } finally {
    queue.dispose();
  }
});
