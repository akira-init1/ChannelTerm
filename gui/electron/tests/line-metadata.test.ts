import test from "node:test";
import assert from "node:assert/strict";
import {
  LineMetadata,
  type LineMarker,
  type MetadataTerminal,
} from "../src/renderer/terminal/line-metadata";

class Marker implements LineMarker {
  isDisposed = false;
  constructor(
    readonly id: number,
    public line: number,
  ) {}
  dispose() {
    this.isDisposed = true;
    this.line = -1;
  }
}

class FakeTerminal implements MetadataTerminal {
  rows = [{ text: "", wrapped: false }];
  markers: Marker[] = [];
  normal = {
    type: "normal" as const,
    baseY: 0,
    cursorY: 0,
    cursorX: 0,
    length: 1,
    getLine: (index: number) => {
      const line = this.rows[index];
      return line
        ? { isWrapped: line.wrapped, translateToString: () => line.text }
        : undefined;
    },
  };
  buffer: MetadataTerminal["buffer"] = {
    normal: this.normal,
    active: this.normal,
  };
  registerMarker(offset = 0) {
    const line = this.normal.baseY + this.normal.cursorY + offset;
    if (line < 0 || line >= this.normal.length) return undefined;
    const marker = new Marker(this.markers.length, line);
    this.markers.push(marker);
    return marker;
  }
  output(rows: { text: string; wrapped: boolean }[], cursorX = 0) {
    this.rows = rows;
    this.normal.length = rows.length;
    this.normal.cursorY = rows.length - 1;
    this.normal.cursorX = cursorX;
  }
}
const lines = (...text: string[]) =>
  text.map((text) => ({ text, wrapped: false }));

test("hidden metadata collection distinguishes unknown history and first live receipt without storing terminal text", () => {
  const terminal = new FakeTerminal();
  const metadata = new LineMetadata(terminal);
  const history = metadata.begin();
  terminal.output(lines("old history", ""));
  metadata.finish(history);
  assert.deepEqual(metadata.viewport(0, 3), [
    { present: true, sequence: 1, receivedAt: undefined, continuation: false },
    {
      present: false,
      sequence: undefined,
      receivedAt: undefined,
      continuation: false,
    },
  ]);
  const live = metadata.begin(1234);
  terminal.output(lines("old history", "new 设备😀", ""));
  metadata.finish(live);
  assert.equal(metadata.viewport(1, 1)[0]?.receivedAt, 1234);
  assert.equal(metadata.viewport(1, 1)[0]?.sequence, 2);
  terminal.normal.cursorY = 0;
  const rewritten = metadata.begin(9999);
  terminal.rows[0]!.text = "rewritten history";
  terminal.normal.cursorX = 3;
  metadata.finish(rewritten);
  assert.equal(
    metadata.viewport(0, 1)[0]?.receivedAt,
    undefined,
    "rewriting cannot invent an old line's first receipt time",
  );
});

test("logical-line metadata follows public marker movement and wrapped continuations", () => {
  const terminal = new FakeTerminal();
  const metadata = new LineMetadata(terminal);
  const batch = metadata.begin(500);
  terminal.output(
    [
      { text: "long", wrapped: false },
      { text: "continued", wrapped: true },
      { text: "next", wrapped: false },
    ],
    4,
  );
  metadata.finish(batch);
  assert.equal(metadata.size, 2);
  assert.equal(metadata.viewport(1, 1)[0]?.sequence, 1);
  assert.equal(metadata.viewport(1, 1)[0]?.continuation, true);
  // Native marker probe separately verifies xterm's reflow movement. The model consumes only line.
  for (const marker of terminal.markers)
    if (!marker.isDisposed && marker.line === 2) marker.line = 1;
  terminal.output(lines("long continued", "next"), 4);
  assert.equal(metadata.viewport(1, 1)[0]?.sequence, 2);
  assert.equal(metadata.viewport(1, 1)[0]?.receivedAt, 500);
});

test("retention, alternate screens, explicit reset and late batches cannot revive stale metadata", () => {
  const terminal = new FakeTerminal();
  const metadata = new LineMetadata(terminal, 2);
  const first = metadata.begin(1);
  terminal.output(lines("a", "b", "c", ""));
  metadata.finish(first);
  assert.equal(metadata.size, 2);
  assert.equal(metadata.viewport(0, 1)[0]?.sequence, undefined);
  const alt = { ...terminal.normal, type: "alternate" as const };
  terminal.buffer = { normal: terminal.normal, active: alt };
  const alternate = metadata.begin(2);
  metadata.finish(alternate);
  assert.deepEqual(metadata.viewport(0, 5), []);
  assert.equal(metadata.size, 2);
  terminal.buffer = { normal: terminal.normal, active: terminal.normal };
  const stale = metadata.begin(3);
  metadata.clear();
  metadata.finish(stale);
  assert.equal(metadata.size, 0);
  assert.ok(terminal.markers.every((marker) => marker.isDisposed));
  metadata.dispose();
  metadata.dispose();
  metadata.finish(metadata.begin(4));
  assert.equal(metadata.size, 0);
  assert.deepEqual(metadata.viewport(0, 5), []);
});

test("metadata limits pending callbacks and reset rescans without retaining old marker ownership", () => {
  const terminal = new FakeTerminal();
  const metadata = new LineMetadata(terminal, 3);
  const batches = Array.from({ length: 9 }, (_, i) => metadata.begin(i));
  assert.equal(
    terminal.markers.filter((marker) => !marker.isDisposed).length,
    4,
  );
  terminal.output(lines("a", "b", ""));
  metadata.finish(batches[0]!);
  assert.equal(metadata.size, 0);
  metadata.clear();
  metadata.finish(metadata.begin(100, true));
  assert.equal(metadata.size, 2);
  assert.equal(metadata.viewport(0, 1)[0]?.receivedAt, 100);
  for (const batch of batches) metadata.finish(batch);
  assert.equal(metadata.size, 2);
  metadata.dispose();
  assert.ok(terminal.markers.every((marker) => marker.isDisposed));
  for (const maximum of [0, 6001, 1.5, NaN])
    assert.throws(() => new LineMetadata(terminal, maximum), RangeError);
});
