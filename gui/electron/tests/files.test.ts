import test from "node:test";
import assert from "node:assert/strict";
import { TransferEvents } from "../src/main/host/transfer-events";
import {
  DesktopFiles,
  TransferConfirmation,
  directoryResult,
  remotePath,
  downloadName,
} from "../src/main/files";
import { HostClient } from "../src/main/host/client";
import { startFixture } from "./fixture";

test("custom cancellation confirmation is single-use and retains its original Host target", async () => {
  const confirmation = new TransferConfirmation();
  const answers: unknown[][] = [];
  const host = {
    beginTransferCancel: async (session: string, transfer: string) =>
      `${session}-${transfer}`,
    resolveTransferCancel: async (...args: [string, string, boolean]) => {
      answers.push(args);
    },
  };
  const id = await confirmation.begin(
    host,
    "clicked-session",
    "original-transfer",
  );
  await assert.rejects(
    confirmation.begin(host, "other-session", "other-transfer"),
    { code: "busy" },
  );
  await assert.rejects(confirmation.resolve("foreign-id", true), {
    code: "closed",
  });
  await assert.rejects(confirmation.resolve(id, "yes"), {
    code: "invalid_input",
  });
  await confirmation.resolve(id, false);
  await assert.rejects(confirmation.resolve(id, true), { code: "closed" });
  assert.deepEqual(answers, [
    ["clicked-session", "clicked-session-original-transfer", false],
  ]);
  const second = await confirmation.begin(
    host,
    "clicked-session",
    "next-transfer",
  );
  await confirmation.resolve(second, true);
  assert.deepEqual(answers.at(-1), [
    "clicked-session",
    "clicked-session-next-transfer",
    true,
  ]);
  const third = await confirmation.begin(
    host,
    "clicked-session",
    "last-transfer",
  );
  confirmation.dismiss();
  await assert.rejects(confirmation.resolve(third, true), { code: "closed" });
  assert.deepEqual(answers.at(-1), [
    "clicked-session",
    "clicked-session-last-transfer",
    false,
  ]);
});

test("disconnect during confirmation startup withdraws the late Host request", async () => {
  const confirmation = new TransferConfirmation();
  let ready!: (id: string) => void;
  const answers: boolean[] = [];
  const pending = confirmation.begin(
    {
      beginTransferCancel: () =>
        new Promise<string>((resolve) => {
          ready = resolve;
        }),
      resolveTransferCancel: async (_session, _request, confirm) => {
        answers.push(confirm);
      },
    },
    "s",
    "transfer",
  );
  confirmation.dismiss();
  ready("host-request");
  await assert.rejects(pending, { code: "cancelled" });
  assert.deepEqual(answers, [false]);
});

test("transfer presentation suppresses exact cursor ranges and waits for release", () => {
  const state = new TransferEvents("s");
  const event = (
    id: number,
    type: string,
    metadata: Record<string, unknown>,
  ) => ({ id, type, session_id: "s", metadata });
  state.apply({
    next: 3,
    dropped: false,
    events: [
      event(0, "LEASE_ACQUIRED", {
        type: "file-transfer",
        output_cursor: 4,
        transfer_id: "ft",
      }),
      event(1, "FILE_TRANSFER_STARTED", {
        transfer_id: "ft",
        source_path: "a",
        requested_path: "/tmp/a",
        total: 3,
      }),
      event(2, "FILE_TRANSFER_COMPLETED", {
        transfer_id: "ft",
        total: 3,
        sent: 3,
        sha256: "a".repeat(64),
        resolved_path: "/tmp/a_1",
      }),
    ],
  });
  assert.equal(state.snapshots()[0]?.state, "cleaning");
  assert.equal(
    Buffer.from(state.filter(Buffer.from("textBINARY"), 10).data).toString(),
    "text",
  );
  state.apply({
    next: 4,
    dropped: false,
    events: [
      event(3, "LEASE_RELEASED", {
        type: "file-transfer",
        output_cursor: 10,
        transfer_id: "ft",
      }),
    ],
  });
  assert.equal(state.snapshots()[0]?.state, "completed");
  assert.equal(state.snapshots()[0]?.destination, "/tmp/a_1");
  assert.equal(
    Buffer.from(
      state.filter(Buffer.from("textBINARYprompt"), 16).data,
    ).toString(),
    "textprompt",
  );
  state.apply({ next: 8, dropped: true, events: [] });
  assert.deepEqual(state.filter(Buffer.from("uncertain"), 25), {
    data: [],
    dropped: true,
  });
  assert.throws(() => state.apply({ next: 7, dropped: false, events: [] }));
});

test("directory and local selection boundaries reject unsafe or stale targets", async () => {
  assert.equal(remotePath("-name"), "./-name");
  for (const name of [
    "/tmp/..",
    "/tmp/CON",
    "/tmp/a:b",
    "/tmp/a\\b",
    "/tmp/end.",
  ])
    assert.throws(() => downloadName(name, "win32"));
  assert.throws(() => remotePath("x\nrm -rf"));
  assert.throws(() =>
    directoryResult({
      path: "/tmp",
      entries: [{ name: "../x", kind: "file" }],
      truncated: false,
    }),
  );
  assert.throws(() =>
    directoryResult({
      path: "/tmp",
      entries: [
        { name: "a", kind: "file" },
        { name: "a", kind: "file" },
      ],
      truncated: false,
    }),
  );
  assert.deepEqual(
    directoryResult({
      path: "/tmp",
      entries: [{ name: "a ' file", kind: "file" }],
      truncated: false,
    }).entries[0]?.name,
    "a ' file",
  );
  const files = new DesktopFiles("must-never-execute");
  const host = new HostClient("http://127.0.0.1:1/mcp", "fake");
  const selection = files.remember("fake-file", "original", "send");
  await assert.rejects(
    files.transfer(host, {
      sessionId: "original",
      direction: "send",
      selectionId: selection.id,
      remotePath: "/tmp/a",
      wire: "shell-command",
    }),
    { code: "invalid_input" },
  );
  await assert.rejects(
    files.transfer(host, {
      sessionId: "replacement",
      direction: "send",
      selectionId: selection.id,
      remotePath: "/tmp/a",
    }),
    { code: "invalid_input" },
  );
  await assert.rejects(
    files.transfer(host, {
      sessionId: "original",
      direction: "receive",
      selectionId: selection.id,
      remotePath: "/tmp/a",
    }),
    { code: "invalid_input" },
  );
  await assert.rejects(
    files.transfer(host, {
      sessionId: "original",
      direction: "send",
      selectionId: selection.id,
      remotePath: "/tmp/a",
      command: "bad",
    }),
    { code: "invalid_input" },
  );
  files.clearSelections();
  await assert.rejects(
    files.transfer(host, {
      sessionId: "original",
      direction: "send",
      selectionId: selection.id,
      remotePath: "/tmp/a",
    }),
    { code: "invalid_input" },
  );
});

test("bounded hidden history never replays evicted protocol bytes", () => {
  const state = new TransferEvents("s");
  for (let i = 0; i < 1025; i++) {
    state.apply({
      next: i * 2 + 2,
      dropped: false,
      events: [
        {
          id: i * 2,
          type: "LEASE_ACQUIRED",
          session_id: "s",
          metadata: { type: "file-transfer", output_cursor: i * 4 + 1 },
        },
        {
          id: i * 2 + 1,
          type: "LEASE_RELEASED",
          session_id: "s",
          metadata: { type: "file-transfer", output_cursor: i * 4 + 3 },
        },
      ],
    });
  }
  assert.deepEqual(state.filter(Buffer.from("old!"), 4), {
    data: [],
    dropped: true,
  });
  assert.equal(
    Buffer.from(state.filter(Buffer.from("aXXb"), 4100).data).toString(),
    "ab",
  );
});

test("file operation reserves before awaits and releases after validation failure", async () => {
  const files = new DesktopFiles("must-never-execute");
  const host = new HostClient("http://127.0.0.1:1/mcp", "fake");
  let release!: () => void;
  host.validateSerial = () =>
    new Promise<void>((_resolve, reject) => {
      release = () => reject(new Error("closed before spawn"));
    });
  const first = files.browse(host, "s", ".");
  assert.equal(files.busy, true);
  await assert.rejects(files.browse(host, "s", "."), { code: "busy" });
  release();
  await assert.rejects(first, /closed before spawn/);
  assert.equal(files.busy, false);
});

test("another client's transfer reaches GUI events without exposing payload in xterm", async () => {
  const fixture = await startFixture();
  const client = new HostClient(fixture.endpoint, fixture.token);
  try {
    await client.connect();
    const id = (await client.list())[0]!.session_id;
    const attachment = await client.attach(id);
    await client.read(attachment.id);
    await fixture.command("transfer-start");
    assert.equal(
      Buffer.from((await client.read(attachment.id)).data).toString(),
      "",
    );
    const jobs = await client.transfers(id);
    assert.equal(jobs[0]?.transferred, 512);
    const cancellation = await client.beginTransferCancel(id, jobs[0]!.id);
    await client.resolveTransferCancel(id, cancellation, false);
    assert.equal((await client.transfers(id))[0]?.released, false);
    await assert.rejects(client.write(attachment.id, "do-not-replay"), {
      code: "busy",
    });
    await fixture.command("transfer-finish");
    assert.equal(
      Buffer.from((await client.read(attachment.id)).data).toString(),
      "transfer-finished\r\n",
    );
    assert.equal((await client.transfers(id))[0]?.state, "completed");
    await assert.rejects(client.beginTransferCancel(id, jobs[0]!.id), {
      code: "closed",
    });
    await fixture.command("transfer-start");
    const replacement = (await client.transfers(id)).at(-1)!;
    const request = await client.beginTransferCancel(id, replacement.id);
    const resolved = client.resolveTransferCancel(id, request, true);
    await fixture.command("transfer-cancel");
    await resolved;
    assert.equal((await client.transfers(id)).at(-1)?.state, "cancelled");
    assert.equal((await client.transfers(id)).at(-1)?.released, true);
  } finally {
    await client.disconnect();
    await fixture.stop();
  }
});
