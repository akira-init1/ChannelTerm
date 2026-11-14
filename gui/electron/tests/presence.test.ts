import test from "node:test";
import assert from "node:assert/strict";
import { Client } from "@modelcontextprotocol/sdk/client/index.js";
import { StreamableHTTPClientTransport } from "@modelcontextprotocol/sdk/client/streamableHttp.js";
import { HostClient } from "../src/main/host/client";
import { object, text } from "../src/main/host/validation";
import { startFixture } from "./fixture";

test("window instances distinguish views, pin file intent, preserve output provenance and end without closing Sessions", async () => {
  const fixture = await startFixture();
  const first = new HostClient(fixture.endpoint, fixture.token);
  const second = new HostClient(fixture.endpoint, fixture.token);
  const observer = new Client({ name: "presence-test", version: "test" });
  try {
    await observer.connect(
      new StreamableHTTPClientTransport(new URL(fixture.endpoint), {
        requestInit: { headers: { Authorization: `Bearer ${fixture.token}` } },
      }),
    );
    const call = async (name: string, args: Record<string, unknown>) => {
      const result = await observer.callTool({ name, arguments: args });
      assert.equal(result.isError, undefined);
      return object(result.structuredContent);
    };
    const views = async () => {
      const value = (await call("client_instance", { action: "list" }))
        .instances;
      assert.ok(Array.isArray(value));
      return value.map(object);
    };
    await first.connect();
    await second.connect();
    const initial = await views();
    assert.equal(initial.length, 2);
    assert.notEqual(initial[0]!.id, initial[1]!.id);
    assert.ok(
      initial.every(
        (view) => Array.isArray(view.uses) && view.uses.length === 0,
      ),
    );
    const target = (await first.list())[0]!.session_id;
    const a = await first.attach(target),
      b = await first.attach(target);
    await second.attach(target);
    const attached = await views();
    assert.deepEqual(
      attached.map((view) => (view.uses as unknown[]).length).sort(),
      [1, 2],
    );
    await first.write(a.id, "presence-test\n");
    const activity = await call("terminal_read_activity", {
      session_id: target,
    });
    assert.ok(Array.isArray(activity.events));
    const source = object(object(activity.events.at(-1)).client);
    assert.equal(source.entry, "mcp");
    assert.equal(source.declared_entry, "electron");
    assert.ok(initial.some((view) => view.id === source.instance_id));
    assert.ok(text(source.operation_id).startsWith("CU-"));
    const operation = await first.fileOperation(target);
    assert.ok(operation.delegation.length >= 20);
    const delegated = await call("client_instance", {
      action: "register",
      name: "Child executor",
      entry: "cli",
      delegation: operation.delegation,
    });
    const child = object(delegated.instance),
      credential = object(delegated.credential);
    assert.equal(child.parent_instance_id, source.instance_id);
    assert.ok(text(child.parent_operation_id).startsWith("CU-"));
    const publicJSON = JSON.stringify(await views());
    assert.ok(!publicJSON.includes(operation.delegation));
    assert.ok(!publicJSON.includes(text(credential.secret)));
    await call("client_instance", { action: "end", credential });
    await operation.done();
    await operation.done();
    await first.detach(b.id);
    await first.disconnect();
    const remaining = await views();
    assert.equal(remaining.filter((view) => view.state === "online").length, 1);
    assert.equal((await second.list())[0]!.state, "open");
    await second.disconnect();
    assert.ok(
      (await views()).every(
        (view) =>
          view.state === "ended" && (view.uses as unknown[]).length === 0,
      ),
    );
  } finally {
    await first.disconnect();
    await second.disconnect();
    await observer.close();
    await fixture.stop();
  }
});

test("malformed presence capability fails closed and never reaches a terminal write", async () => {
  const fixture = await startFixture();
  const client = new HostClient(fixture.endpoint, fixture.token);
  const original = globalThis.fetch;
  let writes = 0;
  try {
    globalThis.fetch = async (input, init) => {
      const body: unknown =
        typeof init?.body === "string" ? JSON.parse(init.body) : undefined;
      if (body && typeof body === "object") {
        const message = object(body);
        if (message.method === "tools/call") {
          const params = object(message.params);
          if (params.name === "terminal_write") writes++;
          if (
            params.name === "client_instance" &&
            object(params.arguments).action === "register"
          ) {
            return new Response(
              JSON.stringify({
                jsonrpc: "2.0",
                id: message.id,
                result: {
                  content: [],
                  structuredContent: {
                    action: "register",
                    credential: { id: "invalid", secret: "invalid" },
                  },
                },
              }),
              { headers: { "Content-Type": "application/json" } },
            );
          }
        }
      }
      return original(input, init);
    };
    await assert.rejects(client.connect(), { code: "incompatible" });
    assert.equal(writes, 0);
  } finally {
    globalThis.fetch = original;
    await client.disconnect();
    await fixture.stop();
  }
});
