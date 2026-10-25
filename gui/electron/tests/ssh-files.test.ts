import test from "node:test";
import assert from "node:assert/strict";
import { mkdtemp, writeFile, readFile, rm } from "node:fs/promises";
import { tmpdir } from "node:os";
import { dirname, join, resolve } from "node:path";
import { setTimeout as delay } from "node:timers/promises";
import {
  DesktopSSHFiles,
  applySSHProgress,
  sshTransferRequest,
} from "../src/main/ssh-files";
import { HostClient } from "../src/main/host/client";
import type {
  SSHTransferRequest,
  TransferStatus,
} from "../src/shared/transfers";
import { startFixture } from "./fixture";
import { spawn } from "node:child_process";

test("SCP companion requires completion plus exit success and keeps credentials private", async () => {
  const root = await mkdtemp(join(tmpdir(), "channelterm-ssh-child-test-"));
  const local = join(root, "file.bin");
  await writeFile(local, "payload");
  const progress = JSON.stringify({
    type: "progress",
    name: "file.bin",
    transferred: 7,
    total: 7,
  });
  const completed = JSON.stringify({
    type: "completed",
    files: 1,
    bytes: 7,
    verified_files: 1,
  });
  try {
    for (const [records, code, success] of [
      [progress + "\n" + completed + "\n", 0, true],
      [progress + "\n", 0, false],
      [completed + "\n", 7, false],
      ["not json\n", 0, false],
      ["x".repeat(70000), 0, false],
    ] as const) {
      let launched = false;
      const files = new DesktopSSHFiles("fixed-file-companion", (exe, args) => {
        launched = true;
        assert.equal(exe, "fixed-file-companion");
        assert.equal(args[0], "scp");
        assert.ok(
          args.includes("--json") && args.includes("--credentials-stdin"),
        );
        assert.ok(
          !args.includes("--resume") &&
            !args.join(" ").includes("fixture-secret"),
        );
        const script = `let input = "";
          process.stdin.setEncoding("utf8");
          process.stdin.on("data", chunk => input += chunk);
          process.stdin.on("end", () => {
            if (JSON.parse(input).password !== "fixture-secret") process.exit(9);
            process.stderr.write("fixture-secret must never reach renderer");
            process.stdout.write(${JSON.stringify(records)}, () => process.exit(${code}));
          });`;
        return spawn(process.execPath, ["-e", script], {
          shell: false,
          windowsHide: true,
          stdio: ["pipe", "pipe", "pipe"],
        });
      });
      const r = request();
      r.protocol = "scp";
      r.resume = false;
      r.connection.password = "fixture-secret";
      const host = {
        validateFileSession: async () => ({
          session_id: r.sessionId,
          session_ref: "SSH-1",
          label: "fixture",
          transport: "ssh",
          endpoint: "fixture-user@127.0.0.1:22",
          state: "open",
        }),
      };
      const operation = files.transfer(host, r, () => local);
      if (success) await operation;
      else
        await assert.rejects(
          operation,
          (error: Error) => !error.message.includes("fixture-secret"),
        );
      assert.ok(launched);
      assert.equal(files.busy, false);
      assert.equal(files.snapshots()[0]?.released, true);
      assert.equal(
        files.snapshots()[0]?.state,
        success ? "completed" : "failed",
      );
      assert.ok(!JSON.stringify(files.snapshots()).includes("fixture-secret"));
    }
  } finally {
    await rm(root, { recursive: true, force: true });
  }
});

function request(): SSHTransferRequest {
  return {
    sessionId: "fixture-session",
    direction: "send",
    selectionId: "fixture-selection",
    remotePath: "/payload.bin",
    protocol: "sftp",
    resume: true,
    recursive: false,
    connection: {
      kind: "ssh",
      host: "127.0.0.1",
      user: "fixture-user",
      port: 22,
      label: "",
      knownHostsFile: "",
      identityFiles: [],
    },
  };
}

test("desktop disconnect cancels its SSH child and clears display history", async () => {
  const root = await mkdtemp(
    join(tmpdir(), "channelterm-ssh-disconnect-test-"),
  );
  const local = join(root, "file");
  await writeFile(local, "payload");
  const files = new DesktopSSHFiles("fixed-file-companion", () =>
    spawn(
      process.execPath,
      [
        "-e",
        `
    process.stdin.resume();
    process.stdin.on("end", () => process.stdout.write(JSON.stringify({type:"progress", name:"file", transferred:1, total:7}) + "\\n"));
    setInterval(() => {}, 1000);
  `,
      ],
      { shell: false, windowsHide: true, stdio: ["pipe", "pipe", "pipe"] },
    ),
  );
  try {
    const r = request();
    const host = {
      validateFileSession: async () => ({
        session_id: r.sessionId,
        session_ref: "SSH-1",
        label: "fixture",
        transport: "ssh",
        endpoint: "fixture-user@127.0.0.1:22",
        state: "open",
      }),
    };
    const operation = files.transfer(host, r, () => local);
    const deadline = Date.now() + 5000;
    while (files.snapshots()[0]?.transferred !== 1 && Date.now() < deadline)
      await delay(5);
    assert.equal(files.snapshots()[0]?.transferred, 1);
    files.disconnect();
    files.disconnect();
    await operation;
    assert.equal(files.busy, false);
    assert.deepEqual(files.snapshots(), []);
  } finally {
    files.disconnect();
    await rm(root, { recursive: true, force: true });
  }
});

test("SSH file intent rejects extra capabilities, SCP resume, directories and malformed credentials", () => {
  assert.deepEqual(sshTransferRequest(request()), request());
  for (const patch of [
    { command: "bad" },
    { protocol: "ftp" },
    { protocol: "scp" },
    { recursive: true },
    { remotePath: "bad\npath" },
    { connection: { ...request().connection, host: "host/other" } },
  ])
    assert.throws(() => sshTransferRequest({ ...request(), ...patch }));
});

test("machine progress never marks payload completion as successful and rejects invalid counters", () => {
  const job: TransferStatus = {
    id: "job",
    sessionId: "session",
    protocol: "scp",
    direction: "send",
    source: "file",
    destination: "target",
    state: "transferring",
    transferred: 0,
    total: 10,
    speed: 0,
    released: false,
  };
  assert.equal(
    applySSHProgress(job, {
      type: "progress",
      name: "file",
      transferred: 10,
      total: 10,
    }),
    false,
  );
  assert.equal(job.state, "transferring");
  assert.equal(job.released, false);
  assert.equal(
    applySSHProgress(job, {
      type: "completed",
      files: 1,
      bytes: 10,
      verified_files: 1,
    }),
    true,
  );
  assert.notEqual(job.state, "completed"); // The process exit still has to succeed.
  for (const transferred of [-1, 11, Number.MAX_SAFE_INTEGER + 1, "10"])
    assert.throws(() =>
      applySSHProgress(job, {
        type: "progress",
        name: "file",
        transferred,
        total: 10,
      }),
    );
});

test(
  "Main uses real SFTP CLI progress, cancellation and resume without closing the SSH terminal",
  { timeout: 60000 },
  async () => {
    const fixture = await startFixture();
    const root = await mkdtemp(join(tmpdir(), "channelterm-ssh-files-test-"));
    const host = new HostClient(fixture.endpoint, fixture.token);
    let diagnostics = "";
    const files = new DesktopSSHFiles(
      resolve(
        "test-results",
        process.platform === "win32" ? "files.exe" : "files",
      ),
      (exe, args) => {
        const child = spawn(exe, args, {
          shell: false,
          windowsHide: true,
          stdio: ["pipe", "pipe", "pipe"],
        });
        child.stderr.on("data", (data: Buffer) => {
          diagnostics += data.toString();
        });
        return child;
      },
    );
    try {
      await host.connect();
      const r = request();
      r.connection.port = fixture.sshPort;
      r.connection.knownHostsFile = fixture.knownHosts;
      r.connection.identityFiles = [
        join(dirname(fixture.knownHosts), "identity"),
      ];
      r.connection.password = "fixture-password";
      const opened = await host.openConnection(r.connection);
      r.sessionId = opened.sessionId;
      const attachment = await host.attach(r.sessionId);
      const payload = Buffer.alloc(4 * 1024 * 1024);
      for (let i = 0; i < payload.length; i++) payload[i] = i % 251;
      const local = join(root, "payload.bin");
      await writeFile(local, payload);
      const transfer = files.transfer(host, r, () => local);
      let failure: unknown;
      void transfer.catch((error: unknown) => {
        failure = error;
      });
      let active: TransferStatus | undefined;
      const deadline = Date.now() + 15000;
      while (Date.now() < deadline) {
        if (failure) assert.fail(`fixture transfer: ${diagnostics}`);
        active = files
          .snapshots()
          .find(
            (j) => !j.released && j.transferred > 0 && j.transferred < j.total,
          );
        if (active) break;
        await delay(5);
      }
      assert.ok(active, "missing intermediate SFTP progress");
      await assert.rejects(
        files.transfer(host, r, () => local),
        /already running/,
      );
      files.cancel(active.id);
      await transfer;
      assert.equal(files.snapshots().at(-1)?.state, "cancelled");
      assert.equal(files.busy, false);
      const continuation = files.transfer(host, r, () => local);
      void continuation.catch((error: unknown) => {
        failure = error;
      });
      let resumed = false;
      while (files.busy) {
        const current = files.snapshots().at(-1);
        if (
          current?.id !== active.id &&
          current?.transferred &&
          current.transferred < current.total
        )
          resumed = true;
        await delay(5);
      }
      if (failure) assert.fail(`fixture resume: ${diagnostics}`);
      await continuation;
      assert.equal(resumed, true);
      assert.equal(files.snapshots().at(-1)?.state, "completed");
      const output = join(root, "download");
      const { mkdir } = await import("node:fs/promises");
      await mkdir(output);
      await files.transfer(
        host,
        { ...r, direction: "receive", resume: false },
        () => output,
      );
      assert.deepEqual(await readFile(join(output, "payload.bin")), payload);
      assert.equal(files.snapshots().at(-1)?.state, "completed");
      await host.write(attachment.id, "terminal still alive\n");
      assert.equal(
        (await host.list()).find((s) => s.session_id === r.sessionId)?.state,
        "open",
      );
      const retained = JSON.stringify(files.snapshots());
      assert.ok(
        !retained.includes("fixture-password") &&
          !retained.includes(fixture.knownHosts),
      );
      await assert.rejects(
        files.transfer(
          host,
          { ...r, connection: { ...r.connection, port: fixture.sshPort + 1 } },
          () => {
            throw new Error("selection consumed too early");
          },
        ),
        /differs from the selected Session/,
      );
    } finally {
      const active = files.snapshots().find((j) => !j.released);
      if (active) files.cancel(active.id);
      await host.disconnect();
      await fixture.stop();
      await rm(root, { recursive: true, force: true });
    }
  },
);
