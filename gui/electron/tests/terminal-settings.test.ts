import test from "node:test";
import assert from "node:assert/strict";
import {
  defaultTerminalSettings,
  isTerminalSettings,
} from "../src/shared/terminal-settings";
import { TerminalPresentation } from "../src/renderer/terminal/presentation";
import { HostClient } from "../src/main/host/client";
import { startFixture } from "./fixture";
import { TerminalCodec } from "../src/main/host/terminal-codec";
import {
  terminalEncodings,
  normalizeTerminalEncoding,
  type TerminalEncoding,
} from "../src/shared/terminal-encodings";

test("local settings reject control characters, excess bytes, unknown fields and malformed Unicode", () => {
  assert.ok(isTerminalSettings(defaultTerminalSettings));
  assert.ok(
    isTerminalSettings({ ...defaultTerminalSettings, description: "板子😀" }),
  );
  for (const patch of [
    { description: "\x1b[2J" },
    { description: "中".repeat(171) },
    { description: "\ud800" },
    { encoding: "utf7" },
    { view: "binary" },
    { command: "anything" },
  ])
    assert.equal(
      isTerminalSettings({ ...defaultTerminalSettings, ...patch }),
      false,
    );
});

test("Main decodes split GBK/GB18030 characters and discards state across missing output", () => {
  for (const encoding of ["gbk", "gb18030"] as const) {
    const wire =
      encoding === "gbk" ? [0xd6, 0xd0, 0xce, 0xc4] : [0x94, 0x39, 0xfc, 0x36];
    const expected = encoding === "gbk" ? "中文" : "😀";
    for (let split = 0; split <= wire.length; split++) {
      const p = new TerminalCodec(encoding);
      assert.equal(
        p.decode(wire.slice(0, split)) + p.decode(wire.slice(split)),
        expected,
      );
      p.decode([0xd6]);
      p.reset();
      assert.equal(p.decode([65]), "A");
    }
  }
});

test("all registered terminal encodings preserve controls and normalize aliases", () => {
  for (const entry of terminalEncodings) {
    assert.ok(
      isTerminalSettings({ ...defaultTerminalSettings, encoding: entry.name }),
    );
    for (const alias of [entry.name, ...entry.aliases])
      assert.equal(
        normalizeTerminalEncoding(` ${alias.toUpperCase()} `),
        entry.name,
      );
    const codec = new TerminalCodec(entry.name);
    const sample = "abc\r\n\x1b[31m\t";
    assert.equal(codec.encode(sample).toString("ascii"), sample);
    assert.equal(codec.decode([...Buffer.from(sample)]), sample);
  }
  assert.equal(normalizeTerminalEncoding("utf-16"), undefined);
  assert.throws(() => new TerminalCodec("ascii").encode("é"), {
    code: "invalid_input",
  });
  assert.equal(new TerminalCodec("ascii").decode([0x80]), "\ufffd");
});

test("legacy text uses actual codepages instead of browser aliases", () => {
  const samples: [TerminalEncoding, string, number[]][] = [
    ["big5", "中", [0xa4, 0xa4]],
    ["big5", "𠕇", [0xfa, 0x40]],
    ["shift_jis", "あ", [0x82, 0xa0]],
    ["euc-jp", "あ", [0xa4, 0xa2]],
    ["euc-kr", "가", [0xb0, 0xa1]],
    ["windows-1251", "Я", [0xdf]],
    ["cp437", "é", [0x82]],
    ["cp850", "é", [0x82]],
    ["cp866", "Я", [0x9f]],
    ["koi8-r", "Я", [0xf1]],
    ["koi8-u", "Я", [0xf1]],
    ["iso-8859-1", "\u0080", [0x80]],
    ["windows-1252", "€", [0x80]],
  ];
  for (const [name, text, wire] of samples) {
    const codec = new TerminalCodec(name);
    assert.deepEqual([...codec.encode(text)], wire, name);
    assert.equal(codec.decode(wire), text, name);
    const fragmented = new TerminalCodec(name);
    assert.equal(
      wire.map((byte) => fragmented.decode([byte])).join(""),
      text,
      name,
    );
    const view = new TerminalPresentation({
      ...defaultTerminalSettings,
      encoding: name,
    });
    assert.equal(view.output(Uint8Array.from(wire), text), text);
  }
});

test("hex view renders control bytes as data, with local offsets and bounded rows", () => {
  const p = new TerminalPresentation({
    ...defaultTerminalSettings,
    view: "hex",
  });
  const out = String(p.output(Uint8Array.from([27, 91, 50, 74, 0, 255])));
  assert.match(out, /00000000  1b 5b 32 4a 00 ff/);
  assert.match(out, /\|\.\[2J\.\.\|/);
  assert.ok(!out.includes("\x1b"));
  assert.match(String(p.output(Uint8Array.of(65))), /^00000006/);
  p.reset();
  assert.match(String(p.output(Uint8Array.of(65))), /^00000000/);
});

test("real Host keeps raw bytes while Main encodes client input and rejects hex/lossy/stale writes", async () => {
  const fixture = await startFixture();
  const client = new HostClient(fixture.endpoint, fixture.token);
  const observer = new HostClient(fixture.endpoint, fixture.token);
  try {
    await client.connect();
    await observer.connect();
    const id = (await client.list())[0]!.session_id;
    const second = await observer.attach(id);
    await observer.read(second.id);
    for (const encoding of ["gbk", "gb18030"] as const) {
      const attachment = await client.attach(id, {
        ...defaultTerminalSettings,
        encoding,
        description: "local only",
      });
      await client.read(attachment.id);
      const input = encoding === "gbk" ? "中文" : "😀";
      const expected =
        encoding === "gbk"
          ? [0xd6, 0xd0, 0xce, 0xc4]
          : [0x94, 0x39, 0xfc, 0x36];
      await client.write(attachment.id, input);
      const rendered = await client.read(attachment.id);
      assert.equal(rendered.text, input);
      assert.deepEqual(rendered.data, expected);
      assert.deepEqual((await observer.read(second.id)).data, expected);
      if (encoding === "gbk") {
        await assert.rejects(client.write(attachment.id, "prefix😀"), {
          code: "invalid_input",
        });
        assert.deepEqual((await observer.read(second.id)).data, []);
      }
      await assert.rejects(
        client.attach(id, { ...defaultTerminalSettings, encoding: "invalid" }),
        { code: "invalid_input" },
      );
      // Failed validation must retain the previous valid attachment.
      await client.write(attachment.id, "ok");
      assert.equal(
        Buffer.from((await observer.read(second.id)).data).toString(),
        "ok",
      );
      const hex = await client.attach(id, {
        ...defaultTerminalSettings,
        view: "hex",
      });
      await assert.rejects(client.write(hex.id, "unsafe"), {
        code: "invalid_input",
      });
      // Replacing a view is now explicit; opening another tab retains the old cursor/codec.
      client.validateAttachment(attachment.id);
      await client.detach(attachment.id);
      await assert.rejects(client.write(attachment.id, "stale"), {
        code: "cancelled",
      });
      assert.deepEqual((await observer.read(second.id)).data, []);
      await client.detach(hex.id);
    }
    assert.equal((await observer.list())[0]!.label, "Isolated echo");
    await client.disconnect();
    assert.equal((await observer.list())[0]!.state, "open");
  } finally {
    await client.disconnect();
    await observer.disconnect();
    await fixture.stop();
  }
});
