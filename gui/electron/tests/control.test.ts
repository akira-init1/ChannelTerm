import type { ControlReply } from "../src/shared/control";
import test from "node:test";
import assert from "node:assert/strict";
import { Client } from "@modelcontextprotocol/sdk/client/index.js";
import { StreamableHTTPClientTransport } from "@modelcontextprotocol/sdk/client/streamableHttp.js";
import { HostClient } from "../src/main/host/client";
import {
  controlSnapshot,
  controlRequest,
  controlOutcome,
} from "../src/main/host/control";
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
    const pendingDebug = first.prepareDebugWrite(attached.id, {
      mode: "hex",
      data: "ff",
      ending: "none",
    });
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
    await assert.rejects(pendingDebug.send(), { code: "cancelled" });
    await assert.rejects(
      first
        .prepareDebugWrite(attached.id, {
          mode: "hex",
          data: "00",
          ending: "none",
        })
        .send(),
      { code: "busy" },
    );
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

test("control requests reject unknown capabilities and outcomes cannot claim unconfirmed recovery", () => {
  const key = {
    session_id: "original",
    version: "CV-abcdefghijklmnop",
    operation_id: "OC-abcdefghijklmnop",
  };
  assert.deepEqual(controlRequest({ ...key, action: "takeover" }), {
    ...key,
    action: "takeover",
  });
  for (const extra of [
    { owner: "private" },
    { action: "begin" },
    { session_id: "bad\n" },
    { version: "" },
    { operation_id: "x" },
  ])
    assert.throws(() =>
      controlRequest({ ...key, action: "takeover", ...extra }),
    );
  const result = {
    ...key,
    status: "taken_over",
    reason: "user_interrupted",
    recovery: "restored",
    retry_allowed: false,
    secret: "private",
  };
  assert.ok(!JSON.stringify(controlOutcome(result)).includes("private"));
  for (const change of [
    { recovery: "pending" },
    { retry_allowed: true },
    { status: "completed" },
  ])
    assert.throws(() => controlOutcome({ ...result, ...change }));
});

test("desktop takeover keeps the original outcome across reconnect and never retargets a successor", async () => {
  const fixture = await startFixture();
  const first = new HostClient(fixture.endpoint, fixture.token),
    second = new HostClient(fixture.endpoint, fixture.token);
  try {
    await first.connect();
    await second.connect();
    const original = (await first.list())[0]!.control!;
    const key = {
      session_id: original.session_id,
      version: original.version,
      operation_id: original.operation_id,
    };
    const taken = await first.control({ ...key, action: "takeover" });
    assert.ok("result" in taken);
    let result: ControlReply = taken;
    for (
      let n = 0;
      n < 100 && "result" in result && result.result.status === "stopping";
      n++
    ) {
      await delay(10);
      result = await first.control({ ...key, action: "result" });
    }
    assert.ok("result" in result && result.result.status === "taken_over");
    assert.equal((await second.list())[0]!.control!.writable, false);
    const human = (await first.list())[0]!.control!;
    await first.control({
      action: "allow",
      session_id: human.session_id,
      version: human.version,
      operation_id: human.operation_id,
    });
    const successor = (await second.list())[0]!.control!;
    const next = {
      session_id: successor.session_id,
      version: successor.version,
      operation_id: successor.operation_id,
    };
    await second.control({ ...next, action: "takeover" });
    await first.disconnect();
    await first.connect();
    const old = await first.control({ ...key, action: "result" });
    assert.ok("result" in old && old.result.version === key.version);
    // Duplicate original takeover is a result lookup, never a new takeover.
    await first.control({ ...key, action: "takeover" });
    assert.equal((await first.list())[0]!.control!.writable, false);
    await assert.rejects(
      first.control({
        action: "allow",
        session_id: human.session_id,
        version: human.version,
        operation_id: human.operation_id,
      }),
      (e: unknown) =>
        e instanceof Error && "code" in e && e.code === "control_stale",
    );
  } finally {
    await first.disconnect();
    await second.disconnect();
    await fixture.stop();
  }
});

test("authenticated agent waits for the operator, then a protected port rejects reads across reconnects", async () => {
  const f = await startFixture();
  const human = new HostClient(f.endpoint, f.token);
  const agent = new Client({ name: "operator-flow", version: "1" });
  try {
    await human.connect();
    await agent.connect(
      new StreamableHTTPClientTransport(new URL(f.endpoint), {
        requestInit: { headers: { Authorization: "Bearer " + f.agentToken } },
      }),
    );
    const reg = await agent.callTool({
      name: "client_instance",
      arguments: { action: "register", name: "operator-flow", entry: "test" },
    });
    const credential = object(object(reg.structuredContent).credential);
    const call = (
      name: string,
      args: Record<string, unknown>,
      proof?: Record<string, unknown>,
    ) =>
      agent.callTool({
        name,
        arguments: args,
        _meta: {
          "io.channelterm/instance": credential,
          ...(proof ? { "io.channelterm/control": proof } : {}),
        },
      });
    const serial = {
      kind: "serial",
      port: "COM8",
      label: "saved",
      baud: 115200,
      dataBits: 8,
      parity: "none",
      stopBits: "1",
      flowControl: "none",
      wake: false,
    } as const;
    const s = await human.openConnection(serial);
    assert.equal((await human.openConnection(serial)).sessionId, s.sessionId);
    const other = await human.openConnection({ ...serial, port: "COM9" });
    const target = { session_id: s.sessionId };
    const status = async () =>
      (await human.list()).find((row) => row.session_id === s.sessionId)!
        .control!;
    // An ordinary agent write fences its proof before the response reaches the client.
    for (const multiCall of [false, true]) {
      const version = (await status()).version;
      const opened = await call("terminal_control", {
        ...target,
        action: "begin",
        version,
        ...(multiCall ? { multi_call: true } : {}),
      });
      const scope = object(object(opened.structuredContent).control);
      const capture = {
        version: text(scope.version),
        operation_id: text(scope.operation_id),
      };
      const sent = await call(
        "terminal_write",
        { ...target, data: "\r", actor: "agent" },
        capture,
      );
      assert.ok(!sent.isError);
      assert.equal((await status()).state, multiCall ? "active" : "idle");
      if (multiCall) {
        const ended = await call("terminal_control", {
          ...target,
          action: "end",
          ...capture,
        });
        assert.ok(!ended.isError);
      } else {
        const stale = await call(
          "terminal_write",
          { ...target, data: "old", actor: "agent" },
          capture,
        );
        assert.equal(stale.isError, true);
      }
    }
    const idle = await status();
    const begin = await call("terminal_control", {
      ...target,
      action: "begin",
      version: idle.version,
    });
    const active = object(object(begin.structuredContent).control);
    const proof = {
      version: text(active.version),
      operation_id: text(active.operation_id),
    };
    await human.control({
      ...target,
      ...proof,
      action: "takeover",
      locale: "zh-CN",
    });
    const write = await call(
      "terminal_write",
      { ...target, text: "old", actor: "user" },
      proof,
    );
    assert.equal(write.isError, true);
    assert.equal(
      object(object(object(write.structuredContent).result).wait)
        .write_forbidden,
      true,
    );
    const wait = call("terminal_control", {
      ...target,
      ...proof,
      action: "wait",
    });
    let current = await status();
    for (let i = 0; i < 100 && (!current.waiting || !current.writable); i++) {
      await delay(10);
      current = await status();
    }
    assert.equal(current.waiting, true);
    assert.equal(current.writable, true);
    const tab = await human.attach(s.sessionId);
    await human.write(tab.id, "manual");
    await human.control({
      ...target,
      action: "allow",
      version: current.version,
      operation_id: current.operation_id,
    });
    const released = object(object((await wait).structuredContent).wait);
    assert.equal(released.status, "allowed");
    const before = await status();
    const snapshot = await call("terminal_read", target);
    const cursor = object(snapshot.structuredContent).next;
    const pending = call("terminal_read", {
      ...target,
      cursor,
      timeout_ms: 10000,
    });
    await delay(30);
    await human.control({
      ...target,
      action: "protect",
      version: before.version,
      operation_id: before.operation_id,
    });
    assert.equal(
      object(object((await pending).structuredContent).error).code,
      "manual_only",
    );
    assert.equal((await call("terminal_read", target)).isError, true);
    assert.equal(
      (await call("terminal_read", { session_id: other.sessionId })).isError,
      undefined,
    );
    await human.closeSession(tab.id);
    await human.detach(tab.id);
    const reopened = await human.openConnection(serial);
    assert.notEqual(reopened.sessionId, s.sessionId);
    assert.equal(
      (await call("terminal_read", { session_id: reopened.sessionId })).isError,
      true,
    );
  } finally {
    await agent.close();
    await human.disconnect();
    await f.stop();
  }
});
