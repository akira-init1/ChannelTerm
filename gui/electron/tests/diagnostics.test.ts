import test from "node:test";
import assert from "node:assert/strict";
import { createHash } from "node:crypto";
import { mkdtemp, readFile, readdir, rm } from "node:fs/promises";
import { tmpdir } from "node:os";
import { join } from "node:path";
import { downloadDiagnostic } from "../src/main/host/diagnostics";
import { DesktopDiagnostics } from "../src/main/diagnostics";
import { HostClient } from "../src/main/host/client";
import { isDiagnosticRequest } from "../src/shared/diagnostics";
import { startFixture } from "./fixture";
import { diagnosticZipFiles } from "./diagnostic-zip";

const id = "DIAG-" + "a".repeat(24),
  signal = new AbortController().signal;
const payload = Buffer.alloc(130000, 0x5a);
const sha256 = createHash("sha256").update(payload).digest("hex");
function chunk(offset: number): Record<string, unknown> {
  const end = Math.min(offset + 65536, payload.length),
    data = payload.subarray(offset, end);
  return {
    size: payload.length,
    sha256,
    encoding: "base64",
    data: data.toString("base64"),
    next: end,
    bytes_read: data.length,
    eof: end === payload.length,
  };
}

test("diagnostic downloads verify bounded sequential chunks and reject changed, malformed or corrupt archives", async () => {
  const output: Buffer[] = [];
  const offsets: number[] = [];
  assert.deepEqual(
    await downloadDiagnostic(
      id,
      async (offset) => {
        offsets.push(offset);
        return chunk(offset);
      },
      async (data) => {
        output.push(data);
      },
      signal,
    ),
    { id, size: payload.length, sha256 },
  );
  assert.deepEqual(offsets, [0, 65536]);
  assert.deepEqual(Buffer.concat(output), payload);
  for (const patch of [
    { size: 17 * 1024 * 1024 },
    { next: 0 },
    { bytes_read: 0 },
    { data: "invalid" },
    { eof: true },
    { sha256: "not-a-hash" },
    { bytes_read: 65537 },
  ])
    await assert.rejects(
      downloadDiagnostic(
        id,
        async () => ({ ...chunk(0), ...patch }),
        async () => {},
        signal,
      ),
      { code: "incompatible" },
    );
  await assert.rejects(
    downloadDiagnostic(
      id,
      async (offset) => ({ ...chunk(offset), sha256: "0".repeat(64) }),
      async () => {},
      signal,
    ),
    /SHA-256/,
  );
  await assert.rejects(
    downloadDiagnostic(
      id,
      async (offset) => ({
        ...chunk(offset),
        ...(offset ? { size: payload.length + 1 } : {}),
      }),
      async () => {},
      signal,
    ),
    { code: "incompatible" },
  );
  const cancel = new AbortController();
  let reads = 0;
  await assert.rejects(
    downloadDiagnostic(
      id,
      async (offset) => {
        reads++;
        return chunk(offset);
      },
      async () => {
        cancel.abort();
      },
      cancel.signal,
    ),
    { name: "AbortError" },
  );
  assert.equal(reads, 1);
});

test("diagnostic intent has no paths, implicit raw opt-in or arbitrary Host identifiers", () => {
  assert.ok(
    isDiagnosticRequest({
      kind: "snapshot",
      attachmentId: "view",
      includeRaw: false,
    }),
  );
  assert.ok(isDiagnosticRequest({ kind: "existing", diagnosticId: id }));
  for (const bad of [
    { kind: "snapshot", attachmentId: "view" },
    { kind: "snapshot", attachmentId: "view", includeRaw: "yes" },
    { kind: "existing", diagnosticId: id, includeRaw: true },
    { kind: "existing", diagnosticId: "../../file" },
    { kind: "existing", diagnosticId: id, path: "C:/secret" },
  ])
    assert.equal(isDiagnosticRequest(bad), false);
});

test("native save ownership rejects overlap and replacement, and cleans failed or cancelled output", async () => {
  const root = await mkdtemp(join(tmpdir(), "channelterm-diagnostics-"));
  const saver = new DesktopDiagnostics(),
    request = { kind: "existing", diagnosticId: id } as const;
  const host = {
    validateAttachment: () => {},
    downloadDiagnostic: async (
      _request: unknown,
      abort: AbortSignal,
      write: (bytes: Buffer) => Promise<void>,
    ) => downloadDiagnostic(id, async (offset) => chunk(offset), write, abort),
  };
  try {
    const path = join(root, "saved.zip");
    await saver.save(host, request, async () => path);
    assert.deepEqual(await readFile(path), payload);
    await assert.rejects(
      saver.save(host, request, async () => path),
      { code: "invalid_input" },
    );
    assert.deepEqual(await readFile(path), payload);
    let finish!: (path: string) => void;
    const pending = saver.save(
      host,
      request,
      () =>
        new Promise((resolve) => {
          finish = resolve;
        }),
    );
    await assert.rejects(
      saver.save(host, request, async () => undefined),
      { code: "busy" },
    );
    const rejected = assert.rejects(pending, { code: "cancelled" });
    saver.cancel();
    finish(join(root, "cancelled.zip"));
    await rejected;
    await assert.rejects(
      saver.save(
        {
          ...host,
          downloadDiagnostic: async (_request, _abort, write) => {
            await write(Buffer.from("partial"));
            throw new Error("private filesystem path");
          },
        },
        request,
        async () => join(root, "failed.zip"),
      ),
      { code: "invalid_input" },
    );
    assert.deepEqual(await readdir(root), ["saved.zip"]);
    assert.equal(
      await saver.save(host, request, async () => undefined),
      undefined,
    );
  } finally {
    await rm(root, { recursive: true, force: true });
  }
});

test("real Host exports metadata by default, raw only on request, and downloads existing IDs until eviction", async () => {
  const fixture = await startFixture(),
    client = new HostClient(fixture.endpoint, fixture.token),
    other = new HostClient(fixture.endpoint, fixture.token);
  const root = await mkdtemp(join(tmpdir(), "channelterm-diagnostic-host-"));
  try {
    await client.connect();
    await other.connect();
    const sessionId = (await client.list())[0]!.session_id;
    const active = await client.attach(sessionId),
      foreign = await other.attach(sessionId);
    await client.write(active.id, "diagnostic-private-input\r");
    await client.read(active.id);
    const saver = new DesktopDiagnostics();
    await assert.rejects(
      saver.save(
        client,
        { kind: "snapshot", attachmentId: foreign.id, includeRaw: false },
        async () => {
          assert.fail("foreign attachment opened picker");
        },
      ),
      { code: "cancelled" },
    );
    const meta = await saver.save(
      client,
      { kind: "snapshot", attachmentId: active.id, includeRaw: false },
      async () => join(root, "meta.zip"),
    );
    assert.ok(meta);
    const files = diagnosticZipFiles(await readFile(join(root, "meta.zip")));
    assert.equal(files.has("terminal.bin"), false);
    assert.doesNotMatch(
      [...files.values()].join(""),
      /diagnostic-private-input/,
    );
    await saver.save(
      client,
      { kind: "snapshot", attachmentId: active.id, includeRaw: true },
      async () => join(root, "raw.zip"),
    );
    assert.match(
      diagnosticZipFiles(await readFile(join(root, "raw.zip")))
        .get("terminal.bin")!
        .toString(),
      /diagnostic-private-input/,
    );
    await saver.save(
      client,
      { kind: "existing", diagnosticId: meta.id },
      async () => join(root, "again.zip"),
    );
    assert.deepEqual(
      await readFile(join(root, "again.zip")),
      await readFile(join(root, "meta.zip")),
    );
    for (let index = 0; index < 4; index++)
      await client.downloadDiagnostic(
        { kind: "snapshot", attachmentId: active.id, includeRaw: false },
        signal,
        async () => {},
      );
    await assert.rejects(
      saver.save(
        client,
        { kind: "existing", diagnosticId: meta.id },
        async () => join(root, "expired.zip"),
      ),
      /expired or been evicted/,
    );
    assert.equal((await readdir(root)).includes("expired.zip"), false);
    await assert.rejects(
      saver.save(
        {
          validateAttachment: (id) => client.validateAttachment(id),
          downloadDiagnostic: (request, abort, write) =>
            client.downloadDiagnostic(request, abort, async (bytes) => {
              await write(bytes);
              await client.detach(active.id);
            }),
        },
        { kind: "snapshot", attachmentId: active.id, includeRaw: false },
        async () => join(root, "detached.zip"),
      ),
      { code: "cancelled" },
    );
    assert.equal((await readdir(root)).includes("detached.zip"), false);
    await assert.rejects(
      client.downloadDiagnostic(
        { kind: "snapshot", attachmentId: active.id, includeRaw: false },
        signal,
        async () => {},
      ),
      { code: "cancelled" },
    );
    assert.equal((await other.list())[0]!.state, "open");
  } finally {
    await client.disconnect();
    await other.disconnect();
    await fixture.stop();
    await rm(root, { recursive: true, force: true });
  }
});
