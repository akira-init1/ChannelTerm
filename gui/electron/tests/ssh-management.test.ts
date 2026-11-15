import test from "node:test";
import assert from "node:assert/strict";
import { spawn } from "node:child_process";
import { dirname, join, resolve } from "node:path";
import {
  sshManageRequest,
  SSHManagementReceipt,
} from "../src/main/ssh-management";
import { DesktopSSHBrowser } from "../src/main/ssh-browser";
import type {
  SSHManageRequest,
  SSHManageOperation,
} from "../src/shared/ssh-browser";
import { HostClient } from "../src/main/host/client";
import { startFixture } from "./fixture";

function request(operation: SSHManageOperation = "mkdir"): SSHManageRequest {
  return {
    sessionId: "fixture",
    operation,
    path: "/explicit",
    connection: {
      kind: "ssh",
      host: "127.0.0.1",
      user: "fixture-user",
      port: 22,
      label: "",
      identityFiles: [],
      knownHostsFile: "",
      password: "fixture-secret",
    },
  };
}
const session = {
  session_id: "fixture",
  session_ref: "SSH-1",
  label: "fixture",
  transport: "ssh",
  endpoint: "fixture-user@127.0.0.1:22",
  state: "open",
};
const host = { validateFileSession: async () => ({ ...session }) };
const complete = {
  type: "completed",
  operation: "mkdir",
  outcome: "confirmed",
  error_code: "",
};
function child(output: unknown[], code = 0) {
  const text = output.map((value) => JSON.stringify(value)).join("\n") + "\n";
  return spawn(
    process.execPath,
    [
      "-e",
      `process.stdin.resume();process.stdin.on('end',()=>{process.stderr.write('fixture-secret');process.stdout.write(${JSON.stringify(text)},()=>process.exit(${code}));});`,
    ],
    { shell: false, windowsHide: true, stdio: ["pipe", "pipe", "pipe"] },
  );
}

test("SFTP management accepts only explicit bounded paths and finite nonrecursive operations", () => {
  assert.equal(sshManageRequest(request()).operation, "mkdir");
  for (const patch of [
    { operation: "exec" },
    { path: "/" },
    { path: "/a/.." },
    { path: "relative" },
    { path: "/a/" },
    { path: "/bad\nname" },
    { path: "/" + "a".repeat(4096) },
    { target: "/unexpected" },
    { overwrite: true },
    { recursive: true },
    { operation: "rename" },
    { operation: "rename", target: "/explicit" },
  ])
    assert.throws(() => sshManageRequest({ ...request(), ...patch }));
  assert.equal(
    sshManageRequest({ ...request("rename"), target: "/renamed" }).target,
    "/renamed",
  );
});

test("SFTP management requires final receipt and exit agreement without inventing success", () => {
  for (const terminal of [
    complete,
    {
      type: "failed",
      operation: "mkdir",
      outcome: "unknown",
      error_code: "permission_denied",
    },
  ]) {
    const parser = new SSHManagementReceipt(request());
    parser.accept({ type: "started" });
    parser.accept(terminal);
    assert.equal(
      parser.finish(terminal.type === "completed" ? 0 : 1).outcome,
      terminal.outcome,
    );
    assert.throws(() => parser.finish(terminal.type === "completed" ? 1 : 0));
    assert.throws(() => parser.accept(terminal));
  }
  for (const patch of [
    { operation: "rm" },
    { outcome: "unknown" },
    { error_code: "secret remote text" },
    { type: "entry" },
  ]) {
    const parser = new SSHManagementReceipt(request());
    parser.accept({ type: "started" });
    assert.throws(() => parser.accept({ ...complete, ...patch }));
  }
  assert.throws(() => new SSHManagementReceipt(request()).finish(0));
});

test("SFTP native confirmation binds exact intent and stale/cancelled requests never launch", async () => {
  const browser = new DesktopSSHBrowser("fixed", () =>
    assert.fail("must not spawn"),
  );
  const r = request();
  const cancelled = await browser.manage(host, r, async (intent) => {
    assert.deepEqual(intent, {
      sessionId: "fixture",
      endpoint: session.endpoint,
      operation: "mkdir",
      path: "/explicit",
    });
    return false;
  });
  assert.equal(cancelled.outcome, "not_started");
  assert.equal(r.connection.password, undefined);
  let checks = 0;
  const stale = await browser.manage(
    {
      validateFileSession: async () => ({
        ...session,
        session_ref: ++checks > 1 ? "SSH-2" : "SSH-1",
      }),
    },
    request(),
    async () => true,
  );
  assert.equal(stale.reason, "target_changed");
  assert.equal(stale.outcome, "not_started");
  const stopped = await browser.manage(host, request(), async () => {
    browser.cancel();
    return true;
  });
  assert.equal(stopped.reason, "cancelled");
  assert.equal(browser.busy, false);
  const missing = new DesktopSSHBrowser(
    join("nonexistent-directory", "companion"),
  );
  assert.equal(
    (await missing.manage(host, request(), async () => true)).outcome,
    "not_started",
  );
});

test("SFTP management preserves uncertainty on invalid receipts, disconnect and cancellation without retry", async () => {
  for (const [records, code, outcome] of [
    [[{ type: "started" }, complete], 0, "confirmed"],
    [[{ type: "started" }, complete], 1, "unknown"],
    [[{ type: "started" }], 0, "unknown"],
  ] as const) {
    let launches = 0;
    const browser = new DesktopSSHBrowser("fixed", (_exe, args) => {
      launches++;
      assert.ok(!args.includes("--overwrite") && !args.includes("--recursive"));
      assert.ok(!args.join(" ").includes("fixture-secret"));
      return child([...records], code);
    });
    const result = await browser.manage(host, request(), async () => true);
    assert.equal(result.outcome, outcome);
    assert.equal(launches, 1);
    assert.equal(browser.busy, false);
  }
  let launched!: () => void;
  const ready = new Promise<void>((resolve) => {
    launched = resolve;
  });
  const browser = new DesktopSSHBrowser("fixed", () => {
    const processChild = spawn(
      process.execPath,
      ["-e", "process.stdin.resume();setInterval(()=>{},1000)"],
      { shell: false, windowsHide: true, stdio: ["pipe", "pipe", "pipe"] },
    );
    launched();
    return processChild;
  });
  const pending = browser.manage(host, request(), async () => true);
  await ready;
  await assert.rejects(
    browser.manage(host, request(), async () => true),
    { code: "busy" },
  );
  browser.cancel();
  browser.cancel();
  const result = await pending;
  assert.equal(result.outcome, "unknown");
  assert.equal(result.reason, "cancelled");
});

test("SFTP acknowledgement cannot be published against a replacement Session", async () => {
  let checks = 0,
    launches = 0;
  const browser = new DesktopSSHBrowser("fixed", () => {
    launches++;
    return child([{ type: "started" }, complete]);
  });
  const result = await browser.manage(
    {
      validateFileSession: async () => ({
        ...session,
        session_ref: ++checks >= 4 ? "SSH-2" : "SSH-1",
      }),
    },
    request(),
    async () => true,
  );
  assert.equal(result.outcome, "unknown");
  assert.equal(result.reason, "target_changed");
  assert.equal(launches, 1);
  assert.equal(browser.busy, false);
});

test("real isolated SFTP management changes only explicit entries and preserves the shared PTY", async () => {
  const fixture = await startFixture();
  const client = new HostClient(fixture.endpoint, fixture.token);
  const browser = new DesktopSSHBrowser(
    resolve(
      "test-results",
      process.platform === "win32" ? "files.exe" : "files",
    ),
  );
  try {
    await client.connect();
    const base = request();
    base.connection.port = fixture.sshPort;
    base.connection.password = "fixture-password";
    base.connection.identityFiles = [
      join(dirname(fixture.knownHosts), "identity"),
    ];
    base.connection.knownHostsFile = fixture.knownHosts;
    base.sessionId = (await client.openConnection(base.connection)).sessionId;
    const attachment = await client.attach(base.sessionId);
    const run = (
      operation: SSHManageOperation,
      path: string,
      target?: string,
    ) =>
      browser.manage(
        client,
        {
          ...base,
          operation,
          path,
          ...(target ? { target } : {}),
          connection: { ...base.connection, password: "fixture-password" },
        },
        async () => true,
      );
    assert.equal((await run("mkdir", "/management")).outcome, "confirmed");
    assert.equal((await run("mkdir", "/management")).state, "failed");
    assert.equal(
      (await run("mkdir", "/management/child")).outcome,
      "confirmed",
    );
    assert.equal((await run("rmdir", "/management")).state, "failed");
    assert.equal((await run("rm", "/management/child")).state, "failed");
    assert.equal((await run("mkdir", "/existing")).outcome, "confirmed");
    assert.equal(
      (await run("rename", "/management/child", "/existing")).state,
      "failed",
    );
    assert.equal((await run("rmdir", "/existing")).outcome, "confirmed");
    assert.equal(
      (await run("rename", "/management/child", "/renamed")).outcome,
      "confirmed",
    );
    assert.equal((await run("rmdir", "/renamed")).outcome, "confirmed");
    assert.equal(
      (await run("rename", "/management/child", "/gone-source")).reason,
      "not_found",
    );
    assert.equal((await run("rmdir", "/management")).outcome, "confirmed");
    const missing = await run("rm", "/management/missing");
    assert.equal(missing.state, "failed");
    assert.equal(missing.reason, "not_found");
    await client.write(attachment.id, "management-keeps-pty-alive\n");
    assert.equal(
      (await client.list()).find((item) => item.session_id === base.sessionId)
        ?.state,
      "open",
    );
  } finally {
    browser.cancel();
    await client.disconnect();
    await fixture.stop();
  }
});
