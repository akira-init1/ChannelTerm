import assert from "node:assert/strict";
import test from "node:test";
import { PromptTimestamp } from "../src/renderer/terminal/prompt-timestamp";

const bytes = (text: string) => new TextEncoder().encode(text);
const text = (data: Uint8Array) => new TextDecoder().decode(data);
const renderer = () =>
  new PromptTimestamp(() => new Date(2026, 0, 1, 14, 5, 1));

test("local timestamps share CLI shell recognition and preserve split UTF-8/SGR bytes", () => {
  for (const prompt of [
    "root@board:~# ",
    "user@host:/路径$ ",
    "sh-5.1# ls",
    "/ # ",
    "[root@board ~]# ",
    "\x1b[32mroot@board\x1b[0m:~# ",
  ]) {
    const input = bytes(prompt);
    for (let split = 0; split <= input.length; split++) {
      const view = renderer();
      const result = Buffer.concat([
        view.write(input.slice(0, split), true),
        view.write(input.slice(split), true),
        view.release(),
      ]);
      assert.equal(
        result.toString(),
        "[14:05:01] " + prompt,
        `split ${split}: ${prompt}`,
      );
    }
  }
});

test("replay and preference transitions never assign current time to retained or partial prompts", () => {
  const view = renderer();
  assert.equal(text(view.write(bytes("root@old:~# "), false)), "root@old:~# ");
  assert.equal(
    text(view.write(bytes("echo\r\nroot@new:~# "), true)),
    "echo\r\n[14:05:01] root@new:~# ",
  );
  assert.equal(text(view.write(bytes("\r\nroot@partial"), true)), "\r\n");
  assert.equal(text(view.write(bytes(":~# "), false)), "root@partial:~# ");
  assert.equal(
    text(view.write(bytes("\r\nroot@again:~# "), true)),
    "\r\n[14:05:01] root@again:~# ",
  );
  view.reset();
  view.write(bytes("root@discarded"), true);
  view.reset();
  assert.equal(
    text(view.write(bytes("root@replay:~# "), false)),
    "root@replay:~# ",
  );
});

test("ordinary logs, interactive prompts, screen controls and bounded latency remain raw", () => {
  for (const input of [
    "CPU ready\r\nroot@board:~# echo\r\n",
    "(gdb) ",
    "=> ",
    "barebox@board:/ # ",
    "\x1b[2J\x1b[Hroot@board:~# ",
    "\x1b[?1049h\nroot@board:~# \x1b[?1049l",
    "\u009b?1049h\nroot@board:~# \u009b?1049l",
    "x".repeat(9000),
  ]) {
    const view = renderer();
    assert.equal(
      Buffer.concat([
        view.write(bytes(input), true),
        view.release(),
      ]).toString(),
      input,
    );
  }
  const view = renderer();
  view.write(bytes("root@partial"), true);
  assert.equal(text(view.release()), "root@partial");
  assert.equal(text(view.write(bytes(":~# "), true)), ":~# ");
  const invalid = new Uint8Array([0xff, 0xfe]);
  assert.deepEqual(
    Buffer.concat([view.write(invalid, true), view.release()]),
    Buffer.from(invalid),
  );
});
