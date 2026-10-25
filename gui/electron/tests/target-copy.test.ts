import test from "node:test";
import assert from "node:assert/strict";
import { copyAITarget, targetInstructions } from "../src/main/target-copy";
import { HostClient } from "../src/main/host/client";
import { locales } from "../src/shared/menus";
import { startFixture } from "./fixture";

const target = {
  session_id: "full-opaque-session-id-123456789",
  session_ref: "SER-7",
  transport: "serial",
  endpoint: "COM8",
  label: "Board 测试",
  state: "open",
};
const endpoint = "http://127.0.0.1:37099/mcp";

test("all nine copy templates preserve complete identifiers and contain only selected metadata", () => {
  const messages = new Set<string>();
  for (const locale of locales) {
    const text = targetInstructions(target, endpoint, locale);
    for (const value of [
      target.session_id,
      target.session_ref,
      target.label,
      target.endpoint,
      endpoint,
      "serial",
      "session_id",
      "Sessions",
    ])
      assert.ok(text.includes(value), `${locale}: ${value}`);
    assert.doesNotMatch(
      text,
      /\{(?:name|host|sessionId|reference|transport|endpoint)\}/,
    );
    assert.equal(text.split("\n").length, 10);
    messages.add(text);
  }
  assert.equal(messages.size, 9);
  assert.match(
    targetInstructions({ ...target, label: "" }, endpoint, "en"),
    /Name: COM8/,
  );
});

test("copy rejects multiline, oversized or deceptive metadata and credential-bearing Host URLs", () => {
  for (const key of [
    "session_id",
    "session_ref",
    "transport",
    "endpoint",
    "label",
  ] as const)
    for (const value of [
      "a\nb",
      "a\rb",
      "a\u2028b",
      "a\u202eb",
      "a\x00b",
      "字".repeat(1400),
    ])
      assert.throws(
        () => targetInstructions({ ...target, [key]: value }, endpoint, "en"),
        { code: "invalid_input" },
      );
  assert.throws(() =>
    targetInstructions(target, "http://user:secret@127.0.0.1:37099/mcp", "en"),
  );
  assert.throws(
    () =>
      targetInstructions(target, "http://127.0.0.1/" + "x".repeat(4096), "en"),
    { code: "invalid_input" },
  );
});

test("copy reads the exact live target and rejects foreign, stale, closed and invalid-language requests", async () => {
  const fixture = await startFixture();
  const client = new HostClient(fixture.endpoint, fixture.token);
  const other = new HostClient(fixture.endpoint, fixture.token);
  const written: string[] = [];
  const write = (text: string) => {
    written.push(text);
  };
  try {
    await client.connect();
    await other.connect();
    const sessionId = (await client.list())[0]!.session_id;
    const active = await client.attach(sessionId);
    const foreign = await other.attach(sessionId);
    await copyAITarget(client, active.id, "zh-CN", write);
    assert.match(written[0]!, /请通过已连接的/);
    assert.ok(written[0]!.includes(sessionId));
    assert.ok(written[0]!.includes(fixture.endpoint));
    assert.ok(!written[0]!.includes(fixture.token));
    assert.ok(!written[0]!.includes("Host ready"));
    await assert.rejects(copyAITarget(client, foreign.id, "en", write), {
      code: "cancelled",
    });
    await assert.rejects(copyAITarget(client, active.id, "unknown", write), {
      code: "invalid_input",
    });
    await client.detach(active.id);
    await assert.rejects(copyAITarget(client, active.id, "en", write), {
      code: "cancelled",
    });
    const replacement = await client.attach(sessionId);
    await client.closeSession(replacement.id);
    await assert.rejects(copyAITarget(client, replacement.id, "en", write), {
      code: "closed",
    });
    assert.equal(written.length, 1);
  } finally {
    await client.disconnect();
    await other.disconnect();
    await fixture.stop();
  }
});

test("detaching while target metadata is pending revokes clipboard access", async () => {
  const fixture = await startFixture();
  const client = new HostClient(fixture.endpoint, fixture.token);
  const original = globalThis.fetch;
  let release!: () => void;
  let requested!: () => void;
  const gate = new Promise<void>((resolve) => {
    release = resolve;
  });
  const started = new Promise<void>((resolve) => {
    requested = resolve;
  });
  let writes = 0;
  try {
    await client.connect();
    const active = await client.attach((await client.list())[0]!.session_id);
    globalThis.fetch = async (input, init) => {
      const body =
        typeof init?.body === "string" ? JSON.parse(init.body) : undefined;
      if (body?.params?.name === "terminal_list_sessions") {
        requested();
        await gate;
      }
      return original(input, init);
    };
    const pending = copyAITarget(client, active.id, "en", () => {
      writes++;
    });
    const rejected = assert.rejects(pending, { code: "cancelled" });
    await started;
    await client.detach(active.id);
    release();
    await rejected;
    assert.equal(writes, 0);
  } finally {
    release();
    globalThis.fetch = original;
    await client.disconnect();
    await fixture.stop();
  }
});
