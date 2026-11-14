import test from "node:test";
import assert from "node:assert/strict";
import { Client } from "@modelcontextprotocol/sdk/client/index.js";
import { StreamableHTTPClientTransport } from "@modelcontextprotocol/sdk/client/streamableHttp.js";
import { HostClient } from "../src/main/host/client";
import { controlSnapshot } from "../src/main/host/control";
import { object, text } from "../src/main/host/validation";
import { startFixture } from "./fixture";
import { setTimeout as delay } from "node:timers/promises";

test("control snapshots reject invalid generations and drop private capabilities", () => {
  const source = {
    session_id: "target",
    version: "CV-abcdefghijklmnop",
    operation_id: "",
    state: "idle",
    writable: true,
    ai_paused: false,
    secret: "private",
  };
  assert.ok(!JSON.stringify(controlSnapshot(source)).includes("private"));
  for (const changed of [
    { version: "" },
    { state: "invented" },
    { writable: "yes" },
    { operation_id: "CU-abcdefghijklmnop" },
  ])
    assert.throws(() => controlSnapshot({ ...source, ...changed }));
});

test("Host fences old desktop input, pins takeover to one instance and requires explicit allowance", async () => {
  const fixture = await startFixture();
  const first = new HostClient(fixture.endpoint, fixture.token);
  const observer = new Client({ name: "control-test", version: "test" });
  try {
    await first.connect();
    const target = (await first.list())[0]!.session_id;
    const attached = await first.attach(target);
    await observer.connect(
      new StreamableHTTPClientTransport(new URL(fixture.endpoint), {
        requestInit: { headers: { Authorization: `Bearer ${fixture.token}` } },
      }),
    );
    const reg = await observer.callTool({
      name: "client_instance",
      arguments: { action: "register", name: "operator", entry: "test" },
    });
    const credential = object(object(reg.structuredContent).credential);
    const call = async (args: Record<string, unknown>) => {
      const r = await observer.callTool({
        name: "terminal_control",
        arguments: { session_id: target, ...args },
        _meta: { "io.channelterm/instance": credential },
      });
      assert.equal(r.isError, undefined, JSON.stringify(r.structuredContent));
      return object(r.structuredContent);
    };
    const current = object((await call({ action: "status" })).control);
    const key = { version: text(current.version) };
    let result = object((await call({ action: "takeover", ...key })).result);
    for (let n = 0; n < 100 && result.status === "stopping"; n++) {
      await delay(10);
      result = object((await call({ action: "result", ...key })).result);
    }
    assert.equal(result.status, "taken_over");
    await assert.rejects(first.write(attached.id, "must-not-replay\n"));
    const controlled = (await first.list())[0]!.control!;
    assert.equal(controlled.writable, false);
    assert.equal(controlled.ai_paused, true);
    await call({ action: "allow", version: controlled.version });
    await first.list();
    await first.write(attached.id, "new-explicit-input\n");
    assert.equal((await first.list())[0]!.control!.writable, true);
  } finally {
    await first.disconnect();
    await observer.close();
    await fixture.stop();
  }
});
