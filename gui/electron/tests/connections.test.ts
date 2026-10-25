import test from "node:test";
import assert from "node:assert/strict";
import {
  isConnectionRequest,
  defaultSSHAdvanced,
  isSSHAdvanced,
  type ConnectionRequest,
} from "../src/shared/connections";
import { HostClient } from "../src/main/host/client";
import { startFixture } from "./fixture";
import { commonBaudRates } from "../src/shared/serial-settings";

const serial: ConnectionRequest = {
  kind: "serial",
  port: "COM-FIXTURE",
  baud: 115200,
  dataBits: 8,
  parity: "none",
  stopBits: "1",
  flowControl: "none",
  label: "GUI serial",
};

test("advanced SSH settings are bounded and require explicit Host support", async () => {
  assert.equal(isSSHAdvanced(defaultSSHAdvanced), true);
  for (const change of [
    { timeoutSeconds: 0 },
    { timeoutSeconds: 121 },
    { keepAliveSeconds: -1 },
    { keepAliveSeconds: 3601 },
    { keepAliveCount: 11 },
    { keepAliveCount: 0 },
    { writeTimeoutSeconds: -1 },
    { writeTimeoutSeconds: 3601 },
    { term: "bad\nterm" },
    { term: "" },
    { columns: 0 },
    { rows: 1001 },
    { rows: 1.5 },
    { resizeOwner: true },
  ])
    assert.equal(isSSHAdvanced({ ...defaultSSHAdvanced, ...change }), false);
  const fixture = await startFixture();
  const client = new HostClient(fixture.endpoint, fixture.token);
  const original = globalThis.fetch;
  let posts = 0;
  try {
    globalThis.fetch = async (input, init) => {
      if (new URL(input.toString()).pathname === "/gui/v1/ssh") {
        if (init?.method === "GET") return Response.json({ available: true });
        posts++;
      }
      return original(input, init);
    };
    assert.equal((await client.connect()).sshAdvanced, false);
    const request = {
      kind: "ssh",
      host: "127.0.0.1",
      port: fixture.sshPort,
      user: "fixture-user",
      password: "fixture-password",
      identityFiles: [],
      knownHostsFile: fixture.knownHosts,
      label: "",
    } as const;
    await assert.rejects(
      client.openConnection({ ...request, advanced: defaultSSHAdvanced }),
      { code: "incompatible" },
    );
    assert.equal(posts, 0);
    assert.ok((await client.openConnection(request)).sessionId);
    assert.equal(posts, 1);
  } finally {
    globalThis.fetch = original;
    await client.disconnect();
    await fixture.stop();
  }
});

test("baud presets and custom integer rates retain the bounded serial contract", () => {
  for (const baud of [...commonBaudRates, 123456, 1])
    assert.equal(isConnectionRequest({ ...serial, baud }), true);
  for (const baud of [0, -1, 4000001, 115200.5, "9600", NaN, Infinity])
    assert.equal(isConnectionRequest({ ...serial, baud }), false);
});

test("an older Host retains attachment support but cannot be authorized for SSH by a menu hint", async () => {
  const fixture = await startFixture();
  const client = new HostClient(fixture.endpoint, fixture.token);
  const original = globalThis.fetch;
  try {
    globalThis.fetch = async (input, init) => {
      if (new URL(input.toString()).pathname === "/gui/v1/ssh")
        return new Response("Not found", { status: 404 });
      return original(input, init);
    };
    assert.equal((await client.connect()).sshAvailable, false);
    const current = await client.attach((await client.list())[0]!.session_id);
    await assert.rejects(
      client.openConnection({
        kind: "ssh",
        host: "127.0.0.1",
        port: fixture.sshPort,
        user: "fixture-user",
        password: "fixture-password",
        identityFiles: [],
        knownHostsFile: fixture.knownHosts,
        label: "",
      }),
      { code: "incompatible" },
    );
    await client.write(current.id, "existing-session-still-works\n");
  } finally {
    globalThis.fetch = original;
    await client.disconnect();
    await fixture.stop();
  }
});

test("connection IPC accepts bounded settings and rejects extra capabilities before Host calls", async () => {
  const client = new HostClient("http://127.0.0.1:1/mcp", "fixture-token");
  assert.equal(isConnectionRequest(serial), true);
  assert.equal(isConnectionRequest({ ...serial, wake: true }), true);
  for (const invalid of [
    null,
    {},
    { ...serial, baud: 0 },
    { ...serial, baud: 115200.5 },
    { ...serial, port: "COM\n1" },
    { ...serial, stopBits: 1 },
    { ...serial, wake: "true" },
    { ...serial, config_path: "private" },
    { ...serial, save: "profile" },
    { ...serial, command: "bad" },
    { ...serial, kind: "ssh", skipHostKey: true },
  ]) {
    assert.equal(isConnectionRequest(invalid), false);
    await assert.rejects(client.openConnection(invalid), {
      code: "invalid_input",
    });
  }
});

test("GUI opens serial and SSH through real Host authority, preserves failed targets and shares lifetime", async () => {
  const fixture = await startFixture();
  const client = new HostClient(fixture.endpoint, fixture.token);
  const observer = new HostClient(fixture.endpoint, fixture.token);
  try {
    assert.equal((await client.connect()).sshAvailable, true);
    await observer.connect();
    assert.deepEqual(await client.serialPorts(), ["COM-FIXTURE"]);
    const opening = client.openConnection(serial);
    await assert.rejects(client.openConnection(serial), { code: "busy" });
    const opened = await opening;
    assert.equal(opened.reused, false);
    assert.equal((await client.openConnection(serial)).reused, true);
    const active = await client.attach(opened.sessionId);
    await assert.rejects(
      client.openConnection({ ...serial, port: "MISSING" }),
      { code: "tool_error" },
    );
    await client.write(active.id, "after-failed-open\n");
    assert.match(
      Buffer.from((await client.read(active.id)).data).toString(),
      /Serial fixture ready|after-failed-open/,
    );
    const ssh: ConnectionRequest = {
      kind: "ssh",
      host: "127.0.0.1",
      port: fixture.sshPort,
      user: "fixture-user",
      password: "fixture-password",
      identityFiles: [],
      knownHostsFile: fixture.knownHosts,
      label: "GUI SSH",
      advanced: {
        ...defaultSSHAdvanced,
        term: "vt100",
        columns: 132,
        rows: 43,
        keepAliveSeconds: 10,
        writeTimeoutSeconds: 5,
      },
    };
    await assert.rejects(
      client.openConnection({ ...ssh, password: "wrong-fixture-password" }),
      { code: "tool_error" },
    );
    const remote = await client.openConnection(ssh);
    assert.equal(
      (await client.list()).find((s) => s.session_id === remote.sessionId)
        ?.transport,
      "ssh",
    );
    const first = await client.attach(remote.sessionId);
    const second = await observer.attach(remote.sessionId);
    await client.write(first.id, "shared-ssh\n");
    await client.disconnect();
    await observer.write(second.id, "survives-gui-close\n");
    let output = "";
    for (
      let attempt = 0;
      attempt < 4 && !output.includes("survives-gui-close");
      attempt++
    )
      output += Buffer.from((await observer.read(second.id)).data).toString();
    assert.match(output, /survives-gui-close/);
    const metadata = JSON.stringify(await observer.list());
    assert.doesNotMatch(metadata, /fixture-password|known_hosts/);
    assert.equal(
      (await fetch(new URL("/gui/v1/ssh", fixture.endpoint))).status,
      401,
    );
  } finally {
    await client.disconnect();
    await observer.disconnect();
    await fixture.stop();
  }
});

test("uncertain SSH creation is sent once without detaching the existing view", async () => {
  const fixture = await startFixture();
  const client = new HostClient(fixture.endpoint, fixture.token);
  const original = globalThis.fetch;
  let calls = 0;
  try {
    await client.connect();
    const current = await client.attach((await client.list())[0]!.session_id);
    globalThis.fetch = async (input, init) => {
      if (
        new URL(input.toString()).pathname === "/gui/v1/ssh" &&
        init?.method === "POST"
      ) {
        calls++;
        throw new Error("fixture-SECRET network failure");
      }
      return original(input, init);
    };
    await assert.rejects(
      client.openConnection({
        kind: "ssh",
        host: "127.0.0.1",
        port: fixture.sshPort,
        user: "fixture-user",
        password: "fixture-password",
        identityFiles: [],
        knownHostsFile: fixture.knownHosts,
        label: "",
      }),
      { code: "tool_error", message: /uncertain/ },
    );
    assert.equal(calls, 1);
    await client.write(current.id, "still-attached\n");
  } finally {
    globalThis.fetch = original;
    await client.disconnect();
    await fixture.stop();
  }
});
