import test from "node:test";
import assert from "node:assert/strict";
import { mkdtemp, readFile, writeFile, mkdir } from "node:fs/promises";
import { join } from "node:path";
import { tmpdir } from "node:os";
import { debugBytes } from "../src/main/host/debug-send";
import { TerminalCodec } from "../src/main/host/terminal-codec";
import { HostClient } from "../src/main/host/client";
import { CommandSnippets } from "../src/main/snippets";
import { isDebugPayload, type DebugPayload } from "../src/shared/debug-send";
import { startFixture } from "./fixture";

const hex = (data: string): DebugPayload => ({
  mode: "hex",
  data,
  ending: "none",
});

test("debug byte contract rejects malformed, empty, odd, oversized and unencodable input", () => {
  const codec = new TerminalCodec("utf-8");
  for (const data of [
    "",
    " ",
    "1",
    "0x12",
    "00 zz",
    "00\u00a0ff",
    "aa".repeat(4097),
  ])
    assert.throws(() => debugBytes(hex(data), codec), {
      code: "invalid_input",
    });
  assert.equal(isDebugPayload({ ...hex("00"), command: "execute" }), false);
  assert.equal(
    isDebugPayload({ mode: "text", data: "\ud800", ending: "none" }),
    false,
  );
  assert.deepEqual(
    [...debugBytes({ ...hex("00 ff 80\n7f"), ending: "crlf" }, codec)],
    [0, 255, 128, 127, 13, 10],
  );
  assert.throws(
    () =>
      debugBytes(
        { mode: "text", data: "中", ending: "none" },
        new TerminalCodec("ascii"),
      ),
    { code: "invalid_input" },
  );
  assert.equal(
    debugBytes(
      { mode: "text", data: "中", ending: "lf" },
      new TerminalCodec("gbk"),
    ).toString("hex"),
    "d6d00a",
  );
  assert.equal(debugBytes(hex("ff".repeat(4096)), codec).length, 4096);
  assert.throws(
    () => debugBytes({ ...hex("ff".repeat(4096)), ending: "lf" }, codec),
    { code: "invalid_input" },
  );
});

test("confirmed binary writes use original attachment and Host authority, never replaying uncertain delivery", async () => {
  const fixture = await startFixture();
  const client = new HostClient(fixture.endpoint, fixture.token);
  const originalFetch = globalThis.fetch;
  try {
    await client.connect();
    const id = (await client.list())[0]!.session_id;
    const active = await client.attach(id, {
      encoding: "utf-8",
      view: "hex",
      description: "",
    });
    await client.read(active.id);
    const prepared = client.prepareDebugWrite(active.id, {
      ...hex("00 ff 80 7f"),
      ending: "crlf",
    });
    assert.equal(prepared.hex, "00 ff 80 7f 0d 0a");
    assert.equal(await prepared.send(), 6);
    const output = Buffer.from((await client.read(active.id)).data);
    assert.equal(
      output.includes(Buffer.from([0, 255, 128, 127, 13, 10])),
      true,
    );
    await assert.rejects(prepared.send(), { code: "cancelled" });
    await assert.rejects(client.write(active.id, "keyboard"), {
      code: "invalid_input",
    });
    const legacy = await client.attach(id, {
      encoding: "gbk",
      view: "text",
      description: "",
    });
    await client.read(legacy.id);
    await client
      .prepareDebugWrite(legacy.id, { mode: "text", data: "中", ending: "lf" })
      .send();
    assert.deepEqual((await client.read(legacy.id)).data, [0xd6, 0xd0, 0x0a]);
    await client.detach(legacy.id);
    const stale = client.prepareDebugWrite(active.id, hex("01"));
    client.discardQueuedInput();
    await assert.rejects(stale.send(), { code: "cancelled" });
    await fixture.command("lease");
    await assert.rejects(
      client.prepareDebugWrite(active.id, hex("02")).send(),
      { code: "busy" },
    );
    await fixture.command("release");
    const detached = client.prepareDebugWrite(active.id, hex("03"));
    await client.detach(active.id);
    await assert.rejects(detached.send(), { code: "cancelled" });
    const next = await client.attach(id);
    await client.read(next.id);
    let calls = 0;
    globalThis.fetch = async (input, init) => {
      const body =
        typeof init?.body === "string" ? JSON.parse(init.body) : undefined;
      if (
        body?.method === "tools/call" &&
        body.params?.name === "terminal_write"
      ) {
        calls++;
        await originalFetch(input, init);
        throw new Error("uncertain fixture failure after Host acceptance");
      }
      return originalFetch(input, init);
    };
    const uncertain = client.prepareDebugWrite(next.id, hex("04"));
    await assert.rejects(uncertain.send());
    await assert.rejects(uncertain.send(), { code: "cancelled" });
    assert.equal(calls, 1);
    assert.deepEqual((await client.read(next.id)).data, [4]);
  } finally {
    globalThis.fetch = originalFetch;
    await client.disconnect();
    await fixture.stop();
  }
});

test("snippet persistence requires explicit plaintext consent, remains bounded and preserves failed writes", async () => {
  const root = await mkdtemp(join(tmpdir(), "channelterm-snippets-"));
  const path = join(root, "snippets.json");
  const library = new CommandSnippets(path);
  assert.deepEqual(await library.list(), []);
  const change = {
    action: "save",
    snippet: { ...hex("00"), name: "probe" },
    confirmPlaintext: true,
  };
  await assert.rejects(library.change({ ...change, confirmPlaintext: false }), {
    code: "invalid_input",
  });
  await library.change(change);
  assert.deepEqual(await new CommandSnippets(path).list(), [change.snippet]);
  for (let i = 0; i < 31; i++)
    await library.change({
      ...change,
      snippet: { ...change.snippet, name: `n${i}` },
    });
  await assert.rejects(
    library.change({
      ...change,
      snippet: { ...change.snippet, name: "overflow" },
    }),
    { code: "invalid_input" },
  );
  assert.equal((await library.list()).length, 32);
  await library.change({ action: "remove", name: "probe" });
  assert.equal((await library.list()).length, 31);
  const before = "damaged-json";
  await writeFile(path, before);
  await assert.rejects(library.change(change), { code: "configuration" });
  assert.equal(await readFile(path, "utf8"), before);
  const directory = join(root, "not-a-file");
  await mkdir(directory);
  await assert.rejects(new CommandSnippets(directory).change(change), {
    code: "configuration",
  });
});
