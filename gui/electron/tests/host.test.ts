import test from "node:test";
import assert from "node:assert/strict";
import { createServer } from "node:http";
import { once } from "node:events";
import { HostClient } from "../src/main/host/client";
import { HostError, publicError } from "../src/main/host/errors";
import { startFixture } from "./fixture";

test("real Go Host: output, ordinary writes, lease authority, cancellation, overwrite, and independent clients", async () => {
  const fixture = await startFixture();
  const client = new HostClient(fixture.endpoint, fixture.token);
  const observer = new HostClient(fixture.endpoint, fixture.token);
  try {
    assert.equal((await client.connect()).lifetime, "persistent");
    await observer.connect();
    const list = await client.list();
    const id = list[0]!.session_id;
    assert.equal(list[0]!.label, "Isolated echo");
    const attachment = await client.attach(id);
    client.validateAttachment(attachment.id);
    assert.throws(() => client.validateAttachment("expired"), {
      code: "cancelled",
    });
    const second = await observer.attach(id);
    const snapshot = await client.read(attachment.id);
    assert.equal(snapshot.replay, true);
    assert.match(Buffer.from(snapshot.data).toString(), /Host ready/);
    await client.write(attachment.id, "GUI input\r");
    const live = await client.read(attachment.id);
    assert.equal(live.replay, false);
    assert.equal(Buffer.from(live.data).toString(), "GUI input\r");
    assert.match(
      Buffer.from((await observer.read(second.id)).data).toString(),
      /GUI input/,
    );
    assert.deepEqual(await client.read(attachment.id), {
      data: [],
      dropped: false,
    });

    // A disappeared right-click target is revalidated in Main before replacing
    // the existing attachment; no operation falls back to the selected Session.
    await assert.rejects(client.attach("missing-context-target"), {
      code: "closed",
    });
    await client.write(attachment.id, "still-attached-after-stale-target");
    assert.equal(
      Buffer.from((await client.read(attachment.id)).data).toString(),
      "still-attached-after-stale-target",
    );

    await fixture.command("lease");
    assert.equal((await client.list())[0]!.lease?.type, "terminal");
    await assert.rejects(
      client.write(attachment.id, "MUST-NOT-ARRIVE"),
      (error: unknown) => error instanceof HostError && error.code === "busy",
    );
    await fixture.command("output", "read-while-busy");
    const busyOutput = Buffer.from(
      (await client.read(attachment.id)).data,
    ).toString();
    assert.equal(busyOutput, "read-while-busy");
    await fixture.command("release");
    await client.write(attachment.id, "after-lease");
    assert.equal(
      Buffer.from((await client.read(attachment.id)).data).toString(),
      "after-lease",
    );

    const waiting = client.read(attachment.id);
    const rejected = assert.rejects(
      waiting,
      (error: unknown) =>
        error instanceof HostError && error.code === "cancelled",
    );
    await client.detach(attachment.id);
    await rejected;
    await client.detach(attachment.id);
    await assert.rejects(client.write(attachment.id, "stale"), {
      code: "cancelled",
    });
    const again = await client.attach(id);
    await client.read(again.id);
    await fixture.command("flood");
    const overwritten = await client.read(again.id);
    assert.equal(overwritten.dropped, true);
    assert.equal(overwritten.replay, true);
    assert.match(
      Buffer.from(overwritten.data).toString(),
      /retained-after-overwrite/,
    );

    await client.disconnect();
    await client.disconnect();
    assert.equal((await observer.list())[0]!.state, "open");
    await observer.write(second.id, "observer-survives");
    assert.match(
      Buffer.from((await observer.read(second.id)).data).toString(),
      /observer-survives/,
    );
    await fixture.command("close");
    await assert.rejects(observer.write(second.id, "closed"), {
      code: "closed",
    });
  } finally {
    await client.disconnect();
    await observer.disconnect();
    await fixture.stop();
  }
});

test("authentication failure and Host disconnect never expose the credential", async () => {
  const fixture = await startFixture();
  const wrong = new HostClient(fixture.endpoint, "obviously-wrong-test-token");
  const client = new HostClient(fixture.endpoint, fixture.token);
  try {
    await assert.rejects(wrong.connect(), (error: unknown) => {
      assert.equal(publicError(error).code, "unauthorized");
      assert.ok(
        !JSON.stringify(publicError(error)).includes(
          "obviously-wrong-test-token",
        ),
      );
      return true;
    });
    await client.connect();
    const attachment = await client.attach(
      (await client.list())[0]!.session_id,
    );
    await client.read(attachment.id);
    const waiting = client.read(attachment.id);
    const rejected = assert.rejects(waiting, (error: unknown) => {
      assert.equal(publicError(error).code, "disconnected");
      assert.ok(!JSON.stringify(publicError(error)).includes(fixture.token));
      return true;
    });
    await fixture.stop();
    await rejected;
  } finally {
    await wrong.disconnect();
    await client.disconnect();
    await fixture.stop();
  }
});

test("redirects cannot forward bearer credentials to another listener", async () => {
  let received = false;
  const target = createServer((_request, response) => {
    received = true;
    response.end();
  }).listen(0, "127.0.0.1");
  await once(target, "listening");
  const targetAddress = target.address();
  assert.ok(targetAddress && typeof targetAddress !== "string");
  const source = createServer((_request, response) => {
    response.writeHead(307, {
      Location: `http://127.0.0.1:${targetAddress.port}/mcp`,
    });
    response.end();
  }).listen(0, "127.0.0.1");
  await once(source, "listening");
  const address = source.address();
  assert.ok(address && typeof address !== "string");
  const client = new HostClient(
    `http://127.0.0.1:${address.port}/mcp`,
    "fake-redirect-token",
  );
  try {
    await assert.rejects(client.connect());
    assert.equal(received, false);
  } finally {
    await client.disconnect();
    source.closeAllConnections();
    target.closeAllConnections();
    source.close();
    target.close();
  }
});
