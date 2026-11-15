import test from "node:test";
import assert from "node:assert/strict";
import { resolve } from "node:path";
import { DesktopMCPSetup } from "../src/main/mcp-setup";
import {
  isMCPSetupRequest,
  type MCPSetupRequest,
} from "../src/shared/mcp-setup";

const request: MCPSetupRequest = {
  client: "codex",
  scope: "user",
  action: "configure",
  replace: false,
};
const preview = {
  plan: {
    path: resolve("private-profile/config.toml"),
    before: "null",
    after: '{"url":"http://127.0.0.1:37099/mcp","http_headers":"[redacted]"}',
    existing: false,
    fingerprint: "a".repeat(64),
  },
  client: { official: "needs_explicit_file_fallback" },
};
const endpoint = "http://127.0.0.1:37099/mcp";

test("setup IPC accepts only finite intent without paths, commands or credentials", () => {
  assert.equal(isMCPSetupRequest(request), true);
  for (const extra of [
    { path: "private" },
    { credential: "secret" },
    { client: {} },
    { scope: "project" },
    { action: "spawn" },
    { replace: 1 },
  ])
    assert.equal(isMCPSetupRequest({ ...request, ...extra }), false);
});

test("native preview cancellation never applies; exact path and diff stay out of renderer results", async () => {
  const calls: Record<string, unknown>[] = [];
  const setup = new DesktopMCPSetup("unused", async (raw) => {
    const value = raw as Record<string, unknown>;
    calls.push(value);
    return value.action === "recover"
      ? { ok: true, value: { state: "none" } }
      : { ok: true, value: preview };
  });
  const result = await setup.perform(
    request,
    endpoint,
    async () => undefined,
    async (value) => {
      assert.equal(value.path, preview.plan.path);
      assert.equal(value.after, preview.plan.after);
      return false;
    },
  );
  assert.deepEqual(result, { state: "cancelled" });
  assert.deepEqual(
    calls.map((call) => call.action),
    ["recover", "preview"],
  );
  assert.equal(JSON.stringify(result).includes("private-profile"), false);
});

test("only confirmed native preview authorizes exact fingerprint and explicit file fallback", async () => {
  const calls: Record<string, unknown>[] = [];
  const setup = new DesktopMCPSetup("unused", async (raw) => {
    const value = raw as Record<string, unknown>;
    calls.push(value);
    if (value.action === "recover")
      return { ok: true, value: { state: "none" } };
    if (value.action === "preview") return { ok: true, value: preview };
    return {
      ok: true,
      value: {
        state: "applied",
        client_id: "CL-" + "b".repeat(32),
        permissions: "read_only",
      },
    };
  });
  const result = await setup.perform(
    { ...request, replace: true },
    endpoint,
    async () => undefined,
    async () => true,
  );
  assert.equal(result.state, "applied");
  assert.equal(result.permissions, "read_only");
  assert.deepEqual(calls.at(-1), {
    action: "apply",
    target: { client: "codex", scope: "user" },
    endpoint,
    fingerprint: "a".repeat(64),
    replace: true,
    file_fallback: true,
  });
  assert.equal(JSON.stringify(result).includes("fingerprint"), false);
});

test("interrupted recovery never silently starts a second mutation and project selection is explicit", async () => {
  let calls = 0;
  const recovery = new DesktopMCPSetup("unused", async () => {
    calls++;
    return { ok: true, value: { state: "recovered_applied" } };
  });
  const result = await recovery.perform(
    request,
    endpoint,
    async () => {
      assert.fail("must not choose");
    },
    async () => {
      assert.fail("must not confirm");
    },
  );
  assert.equal(result.state, "recovered_applied");
  assert.equal(calls, 1);
  const cancelled = new DesktopMCPSetup("unused", async () => ({
    ok: true,
    value: { state: "none" },
  }));
  assert.equal(
    (
      await cancelled.perform(
        { ...request, client: "claude", scope: "project" },
        endpoint,
        async () => undefined,
        async () => {
          assert.fail("must not preview");
        },
      )
    ).state,
    "cancelled",
  );
});

test("malformed preview and concurrent work fail closed without applying", async () => {
  let release = () => {};
  const waiting = new Promise<void>((resolve) => {
    release = resolve;
  });
  const setup = new DesktopMCPSetup("unused", async (raw) => {
    if ((raw as { action: string }).action === "recover")
      return { ok: true, value: { state: "none" } };
    await waiting;
    return {
      ok: true,
      value: { ...preview, plan: { ...preview.plan, path: "relative-unsafe" } },
    };
  });
  const running = setup.perform(
    request,
    endpoint,
    async () => undefined,
    async () => true,
  );
  await assert.rejects(setup.discover(), { code: "busy" });
  release();
  await assert.rejects(running, { code: "incompatible" });
  assert.equal(setup.busy, false);
});
