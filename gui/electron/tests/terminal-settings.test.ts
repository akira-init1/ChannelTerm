import test from "node:test";
import assert from "node:assert/strict";
import {
  defaultTerminalSettings,
  isTerminalSettings,
} from "../src/shared/terminal-settings";
import { TerminalPresentation } from "../src/renderer/terminal/presentation";
import { HostClient } from "../src/main/host/client";
import { startFixture } from "./fixture";

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

test("renderer decodes split GBK/GB18030 characters and discards state across missing output", () => {
  for (const encoding of ["gbk", "gb18030"] as const) {
    const wire =
      encoding === "gbk" ? [0xd6, 0xd0, 0xce, 0xc4] : [0x94, 0x39, 0xfc, 0x36];
    const expected = encoding === "gbk" ? "中文" : "😀";
    for (let split = 0; split <= wire.length; split++) {
      const p = new TerminalPresentation({
        ...defaultTerminalSettings,
        encoding,
      });
      assert.equal(
        String(p.output(Uint8Array.from(wire.slice(0, split)))) +
          String(p.output(Uint8Array.from(wire.slice(split)))),
        expected,
      );
      p.output(Uint8Array.from([0xd6]));
      p.reset();
      assert.equal(p.output(Uint8Array.from([65])), "A");
    }
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
      await assert.rejects(client.write(attachment.id, "stale"), {
        code: "cancelled",
      });
      assert.deepEqual((await observer.read(second.id)).data, []);
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
