import assert from "node:assert/strict";
import test from "node:test";
import xterm from "@xterm/xterm";
import { SemanticHighlighter } from "../src/renderer/terminal/highlight";

const bytes = (text: string) => new TextEncoder().encode(text);
const plain = (data: Uint8Array) =>
  new TextDecoder().decode(data).replace(/\x1b\[[\d;]+m/g, "");
const error = "\x1b[38;2;255;95;95mERROR\x1b[0m";

test("serial prompt colors, echo and erase-to-end agree at every read boundary", async (t) => {
  const prompt = "root@ATK-ZYNQ:~# ";
  const input = bytes(prompt + "\x1b[Kcd /");
  for (let split = 0; split <= input.length; split++) {
    await t.test("split " + split, async () => {
      const renderer = new SemanticHighlighter();
      const terminal = new xterm.Terminal();
      try {
        const write = (data: string | Uint8Array) =>
          new Promise<void>((done) => terminal.write(data, done));
        // Old cells beyond the echo make a missing or reordered EL observable.
        await write("x".repeat(40) + "\r");
        await write(renderer.write(input.subarray(0, split)));
        await write(renderer.write(input.subarray(split)));
        assert.equal(renderer.hasPending, false);
        const line = terminal.buffer.active.getLine(0)!;
        assert.equal(line.translateToString(true), prompt + "cd /");
        let column = 0;
        for (const [text, color] of [
          ["root", 1],
          ["@", undefined],
          ["ATK-ZYNQ", 6],
          [":", undefined],
          ["~", 12],
          ["#", 1],
          [" cd /", undefined],
        ] as const) {
          for (const character of text) {
            const cell = line.getCell(column++)!;
            assert.equal(cell.getChars(), character);
            if (color === undefined) assert.ok(cell.isFgDefault(), character);
            else {
              assert.ok(cell.isFgPalette(), character);
              assert.equal(cell.getFgColor(), color, character);
            }
          }
        }
      } finally {
        terminal.dispose();
      }
    });
  }
});

test("bracketed user and host receive semantic colors while the command echo stays plain", () => {
  for (const [user, path, marker] of [
    ["root", "~", "#"],
    ["alice", "/tmp", "$"],
  ] as const) {
    const prompt = `[${user}@ATK-ZYNQ]:${path}${marker} `;
    const expected = `[\x1b[31m${user}\x1b[0m@\x1b[36mATK-ZYNQ\x1b[0m]:\x1b[94m${path}\x1b[0m\x1b[31m${marker}\x1b[0m `;
    for (const echo of ["", "echo ERROR 42", "echo ERROR 42\r\n"]) {
      const input = bytes(prompt + echo);
      for (let split = 0; split <= input.length; split++) {
        const renderer = new SemanticHighlighter();
        assert.equal(
          Buffer.concat([
            renderer.write(input.subarray(0, split)),
            renderer.write(input.subarray(split)),
          ]).toString(),
          expected + echo,
          "split " + split,
        );
      }
    }
  }
  const minimal = bytes("[root@ATK-ZYNQ ~]# ");
  const renderer = new SemanticHighlighter();
  assert.deepEqual(renderer.write(minimal), minimal);
  assert.deepEqual(renderer.write(bytes("ERROR")), bytes("ERROR"));
});

test("connection literal rules override default overlaps, preserve Unicode across chunks and never interpret patterns", () => {
  const rules = [
    { text: "ERROR设备😀", color: "#123456", enabled: true },
    { text: "设备😀 ready", color: "#abcdef", enabled: true },
    { text: "[literal].*", color: "#0000ff", enabled: true },
    { text: "ignored", color: "#ffffff", enabled: false },
  ];
  const input = bytes("ERROR设备😀 ready [literal].* ignored\n");
  const expected =
    "\x1b[38;2;18;52;86mERROR设备😀\x1b[0m\x1b[38;2;171;205;239m ready\x1b[0m \x1b[38;2;0;0;255m[literal].*\x1b[0m ignored\n";
  for (let split = 0; split <= input.length; split++) {
    const renderer = new SemanticHighlighter();
    assert.equal(renderer.setRules(rules).length, 0);
    const actual = Buffer.concat([
      renderer.write(input.slice(0, split)),
      renderer.write(input.slice(split)),
    ]);
    assert.equal(actual.toString(), expected, `split ${split}`);
    assert.equal(plain(actual), new TextDecoder().decode(input));
  }
});

test("rule changes flush incomplete text without resetting ANSI safety or replaying retained output", () => {
  const renderer = new SemanticHighlighter();
  const rule = { text: "ERROR", color: "#123456", enabled: true };
  renderer.write(bytes("partial "));
  assert.equal(new TextDecoder().decode(renderer.setRules([rule])), "partial ");
  assert.equal(
    new TextDecoder().decode(renderer.write(bytes("ERROR\n"))),
    "ERROR\n",
  );
  const remote = "\x1b[31mERROR\n";
  assert.equal(new TextDecoder().decode(renderer.write(bytes(remote))), remote);
  renderer.setRules([{ ...rule, color: "#abcdef" }]);
  assert.equal(
    new TextDecoder().decode(renderer.write(bytes("ERROR\n"))),
    "ERROR\n",
  );
  assert.equal(
    new TextDecoder().decode(renderer.write(bytes("\x1b[0m\nERROR\n"))),
    "\x1b[0m\n\x1b[38;2;171;205;239mERROR\x1b[0m\n",
  );
  const alternate = "\x1b[?1049hERROR\n\x1b[2JERROR\n\x1b[?1049l";
  assert.equal(
    new TextDecoder().decode(renderer.write(bytes(alternate))),
    alternate,
  );
  assert.throws(
    () => renderer.setRules([{ ...rule, text: "\x1b" }]),
    RangeError,
  );
  const overloaded = bytes("a".repeat(3000) + "\n");
  renderer.setRules([{ ...rule, text: "a" }]);
  assert.deepEqual(
    renderer.write(overloaded),
    overloaded,
    "custom match overflow stays raw and bounded",
  );
});

test("semantic colors retain text, priority fragments, Unicode and prompts across byte splits", () => {
  const input = bytes(
    '设备😀 mmcblk0: ERROR "ready 42" 0x10\r\nroot@board:/tmp# ',
  );
  const expected = new SemanticHighlighter().write(input);
  assert.equal(plain(expected), new TextDecoder().decode(input));
  assert.ok(new TextDecoder().decode(expected).includes(error));
  assert.ok(
    new TextDecoder()
      .decode(expected)
      .includes('\x1b[38;2;203;169;75m"ready 42"\x1b[0m'),
  );
  for (let split = 0; split <= input.length; split++) {
    const renderer = new SemanticHighlighter();
    assert.deepEqual(
      Buffer.concat([
        renderer.write(input.slice(0, split)),
        renderer.write(input.slice(split)),
      ]),
      Buffer.from(expected),
      `split ${split}`,
    );
  }
});

test("remote controls and persistent colors remain raw until a safe boundary", () => {
  for (const remote of [
    "\x1b[31mERROR\nERROR\x1b[0m\n",
    "\x1b[31m[root@ATK-ZYNQ]:~# \x1b[Kcd /\x1b[0m\r\n",
    "\x1b[?1049hroot@ATK-ZYNQ:~# \x1b[Kcd /\x1b[?1049l\r\n",
    "\x1b[10Groot@ATK-ZYNQ:~# \x1b[Kcd /\r\n",
    "\x1b[38;5;196mERROR\n\x1b[39m\n",
    "\x1b[38:2::10:20:30mERROR\n\x1b[0m\n",
    "\x1b[48;2;1;2;3mERROR\n\x1b[49m\n",
    "\x1b[?1049hERROR\n\x1b[2JERROR\n\x1b[?1049l",
    "\x1b]0;ERROR\x07\n",
    "\x1bPERROR\x1b\\\n",
    "\u009b31mERROR\n\u009b0m\n",
    `\x1b[${"0;".repeat(80)}31mERROR\n\x1b[0m\n`,
    "\x1b[999mERROR\n\x1b[998mERROR\n\x1b[0m\n",
  ]) {
    const input = bytes(remote + "ERROR\n");
    for (let split = 0; split <= input.length; split++) {
      const renderer = new SemanticHighlighter();
      const actual = Buffer.concat([
        renderer.write(input.slice(0, split)),
        renderer.write(input.slice(split)),
      ]);
      assert.equal(
        actual.toString(),
        remote + error + "\n",
        `split ${split}: ${JSON.stringify(remote)}`,
      );
    }
  }
});

test("unsafe text, invalid UTF-8, long lines and excessive matches pass through unchanged", () => {
  for (const input of [
    bytes("ERROR\rrewrite\n"),
    bytes("ERROR\b\n"),
    new Uint8Array([0xff, 69, 82, 82, 79, 82, 10]),
    bytes("ERROR ".repeat(2000) + "\n"),
    bytes("()".repeat(3000) + "\n"),
  ])
    assert.deepEqual(new SemanticHighlighter().write(input), input);
});

test("latency release, style changes and loss discard stale styling state without replay", () => {
  const renderer = new SemanticHighlighter();
  assert.equal(renderer.write(bytes("unknown prompt: ")).length, 0);
  assert.equal(renderer.hasPending, true);
  assert.equal(
    new TextDecoder().decode(renderer.release()),
    "unknown prompt: ",
  );
  assert.equal(
    new TextDecoder().decode(renderer.write(bytes("ERROR\n"))),
    "ERROR\n",
  );
  assert.equal(
    new TextDecoder().decode(renderer.write(bytes("ERROR\n"))),
    error + "\n",
  );
  renderer.write(bytes("discard"));
  renderer.reset();
  assert.equal(renderer.release().length, 0);
  const remote = bytes("\x1b[31mERROR\n");
  assert.deepEqual(renderer.write(remote, false), remote);
  assert.equal(
    new TextDecoder().decode(renderer.write(bytes("ERROR\n"))),
    "ERROR\n",
  );
  assert.equal(
    new TextDecoder().decode(renderer.write(bytes("\x1b[0m\nERROR\n"))),
    "\x1b[0m\n" + error + "\n",
  );
});

test("a delayed serial prompt keeps its colors across every split and releases complete text immediately", (t) => {
  t.mock.timers.enable({ apis: ["setTimeout"] });
  const command = "cd /\r\n";
  for (const prompt of ["root@ATK-ZYNQ:~# ", "[root@ATK-ZYNQ]:~# "]) {
    const expected = new SemanticHighlighter().write(bytes(prompt + command));
    assert.ok(expected.includes(27));
    for (let split = 1; split < prompt.length; split++) {
      const output: Uint8Array[] = [];
      const renderer = new SemanticHighlighter((tail) => output.push(tail));
      output.push(renderer.write(bytes(prompt.slice(0, split))));
      t.mock.timers.tick(50);
      t.mock.timers.tick(100);
      output.push(renderer.write(bytes(prompt.slice(split) + command)));
      assert.deepEqual(
        Buffer.concat(output),
        Buffer.from(expected),
        `split ${split}`,
      );
      assert.equal(renderer.hasPending, false);
    }
  }
});

test("latency deadlines belong to each tail and ambiguous prompt prefixes still flush", (t) => {
  t.mock.timers.enable({ apis: ["setTimeout"] });
  const flushed: string[] = [];
  const renderer = new SemanticHighlighter((tail) => flushed.push(plain(tail)));
  renderer.write(bytes("ordinary text "));
  t.mock.timers.tick(49);
  renderer.write(bytes("\r\nroot@"));
  t.mock.timers.tick(50);
  renderer.write(bytes("ATK-"));
  t.mock.timers.tick(199);
  assert.deepEqual(flushed, []);
  t.mock.timers.tick(1);
  assert.deepEqual(flushed, ["root@ATK-"]);
  assert.deepEqual(renderer.write(bytes("rest\r\n")), bytes("rest\r\n"));
  renderer.write(bytes("Password: "));
  t.mock.timers.tick(50);
  assert.deepEqual(flushed, ["root@ATK-", "Password: "]);
  renderer.write(bytes("\r\nroot@"));
  t.mock.timers.tick(50);
  renderer.reset();
  t.mock.timers.tick(250);
  assert.equal(flushed.length, 2);
});
