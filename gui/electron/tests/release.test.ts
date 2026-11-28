import test from "node:test";
import assert from "node:assert/strict";
import { execFile } from "node:child_process";
import { resolve } from "node:path";
import { promisify } from "node:util";
import { HostClient } from "../src/main/host/client";
import { HostReleases } from "../src/main/host/release";
import { startFixture } from "./fixture";

test("CLI port release and desktop snapshot refer only to original registrations", async () => {
  const fixture = await startFixture();
  const desktop = new HostClient(fixture.endpoint, fixture.token);
  const executable = resolve(
    "test-results",
    process.platform === "win32" ? "files.exe" : "files",
  );
  const cli = (target: string, token = fixture.token) =>
    promisify(execFile)(
      executable,
      ["release", target, "--endpoint", fixture.endpoint, "--yes"],
      {
        env: { ...process.env, CHANNELTERM_HTTP_AUTH_TOKEN: token },
        windowsHide: true,
      },
    );
  const serial = (port: string) =>
    desktop.openConnection({
      kind: "serial",
      port,
      label: port,
      baud: 115200,
      dataBits: 8,
      parity: "none",
      stopBits: "1",
      flowControl: "none",
    });
  try {
    assert.equal((await desktop.connect()).releaseAvailable, true);
    const original = await serial("COM8");
    const abandoned = await desktop.releases.preview(original.sessionId);
    desktop.releases.discard(abandoned.ticket);
    await assert.rejects(desktop.releases.confirm(abandoned.ticket));
    const batch = await desktop.releases.preview(null);
    const later = await serial("COM9");
    await assert.rejects(cli("COM8", fixture.agentToken), /403/);
    assert.match((await cli("COM8")).stdout, /COM8.*released/);
    const result = await desktop.releases.confirm(batch.ticket);
    assert.deepEqual(
      result,
      batch.targets.map((row) =>
        row.id === original.sessionId ? "ended" : "released",
      ),
    );
    await assert.rejects(desktop.releases.confirm(batch.ticket));
    assert.deepEqual(
      (await desktop.list()).map((row) => row.session_id),
      [later.sessionId],
    );
    assert.match((await cli("--all")).stdout, /COM9.*released/);
    assert.match((await cli("--all")).stdout, /no open connections/);
    const ssh = {
      kind: "ssh" as const,
      host: "127.0.0.1",
      port: fixture.sshPort,
      user: "fixture-user",
      password: "fixture-password",
      identityFiles: [],
      knownHostsFile: fixture.knownHosts,
      label: "loopback",
    };
    const shell = await desktop.openConnection(ssh);
    const sibling = await desktop.openConnection(ssh);
    assert.notEqual(shell.sessionId, sibling.sessionId);
    const selected = await desktop.releases.preview(shell.sessionId);
    assert.match(
      (await cli(selected.targets[0]!.reference)).stdout,
      /released/,
    );
    assert.deepEqual(
      (await desktop.list()).map((row) => row.session_id),
      [sibling.sessionId],
    );
  } finally {
    await desktop.disconnect();
    await fixture.stop();
  }
});

test("release continues after a denied or uncertain result without replaying a request", async () => {
  const original = globalThis.fetch;
  const calls: string[] = [];
  const ids = ["first", "denied", "lost", "last"];
  try {
    globalThis.fetch = async (_input, init) => {
      if (init?.method === "HEAD") return new Response(null, { status: 204 });
      if (init?.method === "GET")
        return Response.json({
          host: "fixture",
          targets: ids.map((id) => ({
            id,
            reference: id,
            endpoint: id,
            transport: "ssh",
            reason: "keep",
          })),
        });
      const id = JSON.parse(String(init?.body)).id;
      calls.push(id);
      if (id === "lost") throw new Error("response lost");
      return new Response(null, { status: id === "denied" ? 403 : 204 });
    };
    const api = new HostReleases(
      new URL("http://127.0.0.1:1/mcp"),
      "fixture",
      () => new AbortController().signal,
    );
    await api.probe();
    const plan = await api.preview(null);
    assert.deepEqual(await api.confirm(plan.ticket), [
      "released",
      "rejected",
      "unknown",
      "released",
    ]);
    assert.deepEqual(calls, ids);
  } finally {
    globalThis.fetch = original;
  }
});
