import test from "node:test";
import assert from "node:assert/strict";
import {
  mkdtemp,
  readdir,
  readFile,
  rm,
  stat,
  writeFile,
} from "node:fs/promises";
import { join } from "node:path";
import { tmpdir } from "node:os";
import { setTimeout } from "node:timers/promises";
import {
  DesktopRecordings,
  RecordingWriter,
  RecordingText,
} from "../src/main/logging";
import { capturedOutput } from "../src/main/host/capture";
import { HostClient } from "../src/main/host/client";
import {
  isRecordingRequest,
  type RecordingStatus,
  type RecordingRequest,
} from "../src/shared/logging";
import { startFixture } from "./fixture";

const request: RecordingRequest = {
  attachmentId: "view",
  format: "raw",
  segmentMiB: 1,
  segments: 2,
};
const status = (directory: string): RecordingStatus => ({
  id: "recording",
  sessionId: "session",
  directory,
  format: "raw",
  state: "recording",
  bytes: 0,
  gaps: 0,
  rotations: 0,
});
async function until(condition: () => boolean) {
  for (let count = 0; count < 200; count++) {
    if (condition()) return;
    await setTimeout(10);
  }
  assert.fail("Recording did not reach expected state");
}

test("recording contracts bound all input and reject discontinuous purported raw output", () => {
  assert.equal(isRecordingRequest(request), true);
  for (const patch of [
    { segments: 0 },
    { segments: 9 },
    { segmentMiB: 0.5 },
    { format: "hex" },
    { path: "arbitrary" },
    { attachmentId: "" },
  ])
    assert.equal(isRecordingRequest({ ...request, ...patch }), false);
  const value = {
    data: "YQ==",
    encoding: "base64",
    next: 5,
    bytes_read: 1,
    dropped: false,
  };
  assert.equal(capturedOutput(value, 4).data[0], 97);
  for (const patch of [
    { next: 6 },
    { data: "YQ" },
    { bytes_read: 2 },
    { encoding: "utf8" },
  ])
    assert.throws(() => capturedOutput({ ...value, ...patch }, 4));
});

test("text recording preserves split Unicode while escaping terminal controls and flushing incomplete bytes", () => {
  const text = new RecordingText();
  const value = Buffer.from("中😀\x1b[31m\r\n\t\0");
  const chunks = [...value].map((byte) => text.decode(new Uint8Array([byte])));
  chunks.push(text.decode(new Uint8Array([0xe4])));
  chunks.push(text.decode(undefined, true));
  assert.equal(
    Buffer.concat(chunks).toString(),
    "中😀\\x1b[31m\\x0d\n\t\\x00�",
  );
});

test("raw segments preserve byte ranges, rotate within the combined disk cap, record gaps and finish durably", async () => {
  const root = await mkdtemp(join(tmpdir(), "ct-recording-"));
  const directory = join(root, "capture");
  const current = status(directory);
  const writer = new RecordingWriter(current, request, 100);
  try {
    await writer.start();
    assert.equal(
      JSON.parse(await readFile(join(directory, "manifest.json"), "utf8"))
        .state,
      "recording",
    );
    let cursor = 100;
    for (let i = 0; i < 40; i++) {
      const data = Buffer.alloc(65536, i);
      await writer.append({
        data,
        start: cursor,
        next: cursor + data.length,
        dropped: false,
      });
      cursor += data.length;
    }
    await writer.append({
      data: Buffer.from([0, 255, 13, 10]),
      start: cursor + 20,
      next: cursor + 24,
      dropped: true,
    });
    await writer.finish("stopped");
    await writer.finish("stopped");
    assert.ok(current.rotations > 0);
    assert.equal(current.gaps, 1);
    const manifest = JSON.parse(
      await readFile(join(directory, "manifest.json"), "utf8"),
    );
    assert.equal(manifest.state, "stopped");
    assert.equal(manifest.segments.length, 2);
    assert.equal((await readdir(directory)).length, 5);
    for (const name of manifest.segments) {
      assert.ok(
        (await stat(join(directory, name))).size +
          (await stat(join(directory, name + ".jsonl"))).size <=
          1048576,
      );
      const bytes = await readFile(join(directory, name));
      const records = (await readFile(join(directory, name + ".jsonl"), "utf8"))
        .trim()
        .split("\n")
        .map((line) => JSON.parse(line));
      for (const entry of records.slice(1))
        assert.ok(entry.offset + entry.bytes <= bytes.length);
    }
    const last = await readFile(join(directory, manifest.segments.at(-1)));
    assert.deepEqual([...last.subarray(-4)], [0, 255, 13, 10]);
    await assert.rejects(
      writer.append({
        data: new Uint8Array(),
        start: 0,
        next: 0,
        dropped: false,
      }),
    );
  } finally {
    await writer.finish("failed").catch(() => {});
    await rm(root, { recursive: true, force: true });
  }
});

test("disk-full simulation stops capture and releases use without retry or unbounded read queues", async () => {
  let reads = 0,
    closes = 0,
    writes = 0;
  const recorder = new DesktopRecordings(() => ({
    start: async () => {},
    append: async () => {
      writes++;
      throw Object.assign(new Error("full"), { code: "ENOSPC" });
    },
    finish: async () => {},
  }));
  const source = {
    validateAttachment: () => {},
    captureOutput: async () => ({
      sessionId: "session",
      start: 0,
      read: async () => {
        reads++;
        return { data: Buffer.from("one"), start: 0, next: 3, dropped: false };
      },
      close: async () => {
        closes++;
      },
    }),
  };
  await recorder.start(source, request, async () => "unused-test-path");
  await until(() => recorder.list()[0]?.state === "failed");
  assert.equal(recorder.list()[0]?.error, "disk");
  assert.equal(reads, 1);
  assert.equal(writes, 1);
  assert.equal(closes, 1);
  await recorder.stopAll();
});

test("capture ignores terminal visibility and transfer suppression, retains real idle use, and stops without closing the Session", async () => {
  const fixture = await startFixture({ idle: true });
  const client = new HostClient(fixture.endpoint, fixture.token);
  let capture;
  try {
    await client.connect();
    const id = (await client.list())[0]!.session_id;
    const view = await client.attach(id);
    capture = await client.captureOutput(view.id, new AbortController().signal);
    await client.detach(view.id);
    await setTimeout(1100);
    await fixture.command("sweep-idle");
    assert.ok((await client.list()).some((entry) => entry.session_id === id));
    await fixture.command("transfer-start");
    await fixture.command("output", "raw-transfer-range\u001b[31m中");
    const output = await capture.read();
    assert.ok(
      Buffer.from(output!.data).toString().includes("raw-transfer-range"),
    );
    await fixture.command("transfer-finish");
    await fixture.command("flood");
    const overwritten = await capture.read();
    assert.equal(overwritten?.dropped, true);
    // Keep a view attached so idle release cannot mask an explicit Session close.
    const remainingView = await client.attach(id);
    const pending = capture.read().catch(() => undefined);
    await capture.close();
    await capture.close();
    await pending;
    assert.ok((await client.list()).some((entry) => entry.session_id === id));
    await client.detach(remainingView.id);
  } finally {
    await capture?.close();
    await client.disconnect();
    await fixture.stop();
  }
});

test("canceling destination selection creates no files or monitor and failed destination stays explicit", async () => {
  const recorder = new DesktopRecordings();
  let starts = 0;
  const source = {
    validateAttachment: () => {},
    captureOutput: async () => {
      starts++;
      throw new Error("must not capture");
    },
  };
  await recorder.start(source, request, async () => undefined);
  assert.equal(starts, 0);
  assert.deepEqual(recorder.list(), []);
  const root = await mkdtemp(join(tmpdir(), "ct-recording-invalid-"));
  try {
    const path = join(root, "not-directory");
    await writeFile(path, "untouched");
    const writer = new RecordingWriter(
      status(join(path, "capture")),
      request,
      0,
    );
    await assert.rejects(writer.start());
    await writer.finish("failed");
    assert.equal(await readFile(path, "utf8"), "untouched");
  } finally {
    await rm(root, { recursive: true, force: true });
  }
});

test("stop waits for the accepted disk batch, never reads another batch, and cannot hide a write failure", async () => {
  for (const failure of [false, true]) {
    let release = () => {};
    const blocked = new Promise<void>((resolve) => {
      release = resolve;
    });
    let entered = false,
      reads = 0,
      finished = false,
      stopped = false;
    const recorder = new DesktopRecordings(() => ({
      start: async () => {},
      append: async () => {
        entered = true;
        await blocked;
        if (failure) throw new Error("disk failure during stop");
      },
      finish: async () => {
        finished = true;
      },
    }));
    await recorder.start(
      {
        validateAttachment: () => {},
        captureOutput: async () => ({
          sessionId: "session",
          start: 0,
          read: async () => {
            reads++;
            return {
              data: Buffer.from("one"),
              start: 0,
              next: 3,
              dropped: false,
            };
          },
          close: async () => {},
        }),
      },
      request,
      async () => "unused-test-path",
    );
    await until(() => entered);
    const stopping = recorder.stopAll().then(() => {
      stopped = true;
    });
    await setTimeout(10);
    assert.equal(stopped, false);
    assert.equal(finished, false);
    assert.equal(recorder.list()[0]?.state, "stopping");
    release();
    await stopping;
    assert.equal(reads, 1);
    assert.equal(finished, true);
    assert.equal(recorder.list()[0]?.state, failure ? "failed" : "stopped");
    if (failure) assert.equal(recorder.list()[0]?.error, "disk");
  }
});

test("cancel during destination initialization preserves permission errors and waits for cleanup", async () => {
  let release = () => {};
  const gate = new Promise<void>((resolve) => {
    release = resolve;
  });
  let entered = false,
    closed = false,
    outcome = "";
  const recorder = new DesktopRecordings(() => ({
    start: async () => {
      entered = true;
      await gate;
      throw Object.assign(new Error("permission denied"), { code: "EACCES" });
    },
    append: async () => {
      assert.fail("must not read after failed initialization");
    },
    finish: async (state) => {
      outcome = state;
    },
  }));
  const starting = recorder
    .start(
      {
        validateAttachment: () => {},
        captureOutput: async () => ({
          sessionId: "session",
          start: 0,
          read: async () => {
            assert.fail("must not read");
          },
          close: async () => {
            closed = true;
          },
        }),
      },
      request,
      async () => "unused-test-path",
    )
    .catch(() => {});
  await until(() => entered);
  const stopping = recorder.stopAll();
  release();
  await Promise.all([starting, stopping]);
  assert.equal(closed, true);
  assert.equal(outcome, "failed");
  assert.equal(recorder.list()[0]?.state, "failed");
  assert.equal(recorder.list()[0]?.error, "disk");
});
