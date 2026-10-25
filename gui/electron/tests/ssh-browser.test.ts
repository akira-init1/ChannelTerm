import test from "node:test";
import assert from "node:assert/strict";
import { spawn } from "node:child_process";
import { dirname, join, resolve } from "node:path";
import {
  DesktopSSHBrowser,
  SSHListing,
  sshBrowseRequest,
} from "../src/main/ssh-browser";
import type { SSHBrowseRequest } from "../src/shared/ssh-browser";
import { HostClient } from "../src/main/host/client";
import { startFixture } from "./fixture";

function request(): SSHBrowseRequest {
  return {
    sessionId: "fixture-session",
    operation: "ls",
    path: "/",
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
  session_id: "fixture-session",
  session_ref: "SSH-1",
  label: "fixture",
  transport: "ssh",
  endpoint: "fixture-user@127.0.0.1:22",
  state: "open",
};
const host = { validateFileSession: async () => ({ ...session }) };
const entry = {
  type: "entry",
  name: "file.bin",
  kind: "file",
  size: 42,
  mode: "-rw-r--r--",
  modified: "1970-01-01T00:00:00Z",
};
const records = [{ type: "started" }, entry, { type: "completed", entries: 1 }];
const stream = (values: unknown[]) =>
  values.map((v) => JSON.stringify(v)).join("\n") + "\n";

test("SFTP browse accepts only read-only bounded intent and matching SSH targets", async () => {
  assert.deepEqual(sshBrowseRequest(request()), request());
  for (const patch of [
    { operation: "rm" },
    { command: "ls" },
    { path: "" },
    { path: "bad\npath" },
    { path: "a".repeat(4097) },
    { sessionId: "" },
    { connection: { ...request().connection, host: "host/other" } },
  ])
    assert.throws(() => sshBrowseRequest({ ...request(), ...patch }));
  const browser = new DesktopSSHBrowser("fixed", () =>
    assert.fail("must not spawn"),
  );
  for (const patch of [
    { transport: "serial" },
    { endpoint: "other@host:22" },
  ]) {
    const r = request();
    await assert.rejects(
      browser.browse(
        { validateFileSession: async () => ({ ...session, ...patch }) },
        r,
      ),
      { code: "invalid_input" },
    );
    assert.equal(r.connection.password, undefined);
    assert.equal(browser.busy, false);
  }
});

test("SFTP snapshots require complete finite metadata and reject unsafe navigation names", () => {
  const listing = new SSHListing("ls");
  for (const r of records) listing.accept(r);
  assert.deepEqual(listing.finish(), [
    {
      name: entry.name,
      kind: entry.kind,
      size: entry.size,
      mode: entry.mode,
      modified: entry.modified,
    },
  ]);
  assert.throws(() => listing.accept(entry));
  for (const patch of [
    { name: "../file" },
    { name: ".." },
    { name: "bad\\file" },
    { name: "bad\nfile" },
    { name: "a".repeat(4097) },
    { size: -1 },
    { size: Number.MAX_SAFE_INTEGER + 1 },
    { kind: "unknown" },
    { modified: "not a time" },
    { mode: "<script>" },
  ]) {
    const invalid = new SSHListing("ls");
    invalid.accept(records[0]);
    assert.throws(() => invalid.accept({ ...entry, ...patch }));
  }
  for (const values of [
    [entry],
    [records[0], entry, entry],
    [records[0], entry, { type: "completed", entries: 0 }],
    [records[0], { type: "failed", error_code: "listing_limit" }],
    [records[0], { type: "cancelled" }],
  ])
    assert.throws(() => {
      const invalid = new SSHListing("ls");
      for (const value of values) invalid.accept(value);
      invalid.finish();
    });
  for (const name of ["/", "."]) {
    const stat = new SSHListing("stat");
    for (const r of [
      records[0],
      { ...entry, name, kind: "directory" },
      records[2],
    ])
      stat.accept(r);
    assert.equal(stat.finish()[0]?.name, name);
  }
  const emptyStat = new SSHListing("stat");
  emptyStat.accept(records[0]);
  emptyStat.accept({ type: "completed", entries: 0 });
  assert.throws(() => emptyStat.finish());
  for (const [count, length] of [
    [1025, 8],
    [257, 4096],
  ]) {
    const large = new SSHListing("ls");
    large.accept(records[0]);
    assert.throws(() => {
      for (let i = 0; i < count!; i++)
        large.accept({ ...entry, name: String(i).padEnd(length!, "x") });
    });
  }
});

test("SFTP companion requires final exit success, rejects malformed streams and keeps credentials private", async () => {
  for (const [output, code, success] of [
    [stream(records), 0, true],
    [stream(records), 1, false],
    [stream(records.slice(0, 2)), 0, false],
    ["not-json\n", 0, false],
    ["x".repeat(32769), 0, false],
    [stream([{ type: "started", extra: "中".repeat(12000) }]), 0, false],
    [stream(records) + "trailing", 0, false],
  ] as const) {
    const r = request();
    const browser = new DesktopSSHBrowser("fixed-companion", (exe, args) => {
      assert.equal(exe, "fixed-companion");
      assert.equal(args[0], "sftp");
      assert.equal(args[2], "ls");
      assert.ok(
        args.includes("--credentials-stdin") && args.includes("--json"),
      );
      assert.deepEqual(args.slice(-2), ["--", "/"]);
      assert.ok(!args.join(" ").includes("fixture-secret"));
      return spawn(
        process.execPath,
        [
          "-e",
          `
        let input = "";
        process.stdin.on("data", b => input += b);
        process.stdin.on("end", () => {
          if (JSON.parse(input).password !== "fixture-secret") process.exit(9);
          process.stderr.write("fixture-secret");
          process.stdout.write(${JSON.stringify(output)}, () => process.exit(${code}));
        });`,
        ],
        { shell: false, windowsHide: true, stdio: ["pipe", "pipe", "pipe"] },
      );
    });
    const operation = browser.browse(host, r);
    if (success) assert.equal((await operation).entries[0]?.name, "file.bin");
    else
      await assert.rejects(
        operation,
        (error: Error) => !error.message.includes("fixture-secret"),
      );
    assert.equal(r.connection.password, undefined);
    assert.equal(browser.busy, false);
  }
});

test("SFTP cancellation is idempotent before launch and during I/O; overlap and stale completion are rejected", async () => {
  let validate!: (value: typeof session) => void;
  const browser = new DesktopSSHBrowser("fixed", () =>
    assert.fail("cancelled before launch"),
  );
  const pending = browser.browse(
    {
      validateFileSession: () =>
        new Promise((resolve) => {
          validate = resolve;
        }),
    },
    request(),
  );
  await assert.rejects(browser.browse(host, request()), { code: "busy" });
  const rejected = assert.rejects(pending, { code: "cancelled" });
  browser.cancel();
  browser.cancel();
  validate(session);
  await rejected;
  assert.equal(browser.busy, false);

  let launched!: () => void;
  const ready = new Promise<void>((resolve) => {
    launched = resolve;
  });
  const active = new DesktopSSHBrowser("fixed", () => {
    const child = spawn(
      process.execPath,
      ["-e", "process.stdin.resume(); setInterval(() => {}, 1000)"],
      { shell: false, windowsHide: true, stdio: ["pipe", "pipe", "pipe"] },
    );
    launched();
    return child;
  });
  const running = active.browse(host, request());
  const cancelled = assert.rejects(running, { code: "cancelled" });
  await ready;
  active.cancel();
  active.cancel();
  await cancelled;
  assert.equal(active.busy, false);

  let checks = 0;
  const stale = new DesktopSSHBrowser("fixed", () =>
    spawn(
      process.execPath,
      [
        "-e",
        `process.stdin.resume(); process.stdin.on("end", () => process.stdout.write(${JSON.stringify(stream(records))}));`,
      ],
      { shell: false, windowsHide: true, stdio: ["pipe", "pipe", "pipe"] },
    ),
  );
  await assert.rejects(
    stale.browse(
      {
        validateFileSession: async () => ({
          ...session,
          session_ref: ++checks === 1 ? "SSH-1" : "SSH-2",
        }),
      },
      request(),
    ),
    { code: "cancelled" },
  );
  assert.equal(stale.busy, false);
  const missing = new DesktopSSHBrowser(
    join("nonexistent-directory", "companion"),
  );
  await assert.rejects(missing.browse(host, request()), {
    code: "configuration",
  });
  assert.equal(missing.busy, false);
});

test(
  "SFTP companion deadline releases a stalled child without returning partial rows",
  { timeout: 40000 },
  async () => {
    const browser = new DesktopSSHBrowser("fixed", () =>
      spawn(
        process.execPath,
        [
          "-e",
          `process.stdin.resume(); process.stdout.write(${JSON.stringify(stream(records.slice(0, 2)))}); setInterval(() => {}, 1000);`,
        ],
        { shell: false, windowsHide: true, stdio: ["pipe", "pipe", "pipe"] },
      ),
    );
    try {
      await assert.rejects(browser.browse(host, request()), {
        code: "cancelled",
      });
      assert.equal(browser.busy, false);
    } finally {
      browser.cancel();
    }
  },
);

test(
  "real SFTP CLI lists and stats without writing to or closing the shared terminal",
  { timeout: 30000 },
  async () => {
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
      const r = request();
      r.connection.port = fixture.sshPort;
      r.connection.password = "fixture-password";
      r.connection.identityFiles = [
        join(dirname(fixture.knownHosts), "identity"),
      ];
      r.connection.knownHostsFile = fixture.knownHosts;
      r.sessionId = (await client.openConnection(r.connection)).sessionId;
      const attachment = await client.attach(r.sessionId);
      const fresh = (): SSHBrowseRequest => ({
        ...r,
        connection: { ...r.connection, password: "fixture-password" },
      });
      const listing = await browser.browse(client, fresh());
      assert.ok(Array.isArray(listing.entries));
      const stat = await browser.browse(client, {
        ...fresh(),
        operation: "stat",
      });
      assert.equal(stat.entries[0]?.kind, "directory");
      await assert.rejects(
        browser.browse(client, {
          ...fresh(),
          operation: "stat",
          path: "/missing-entry",
        }),
        { code: "tool_error" },
      );
      await client.write(attachment.id, "browse-keeps-terminal-alive\n");
      assert.equal(
        (await client.list()).find((s) => s.session_id === r.sessionId)?.state,
        "open",
      );
    } finally {
      browser.cancel();
      await client.disconnect();
      await fixture.stop();
    }
  },
);
