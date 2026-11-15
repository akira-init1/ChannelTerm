import test from "node:test";
import assert from "node:assert/strict";
import {
  readHighlightRules,
  restoreHighlightTemplates,
} from "../src/shared/highlight-rules";
import {
  recordConnection,
  restoreConnections,
} from "../src/shared/connection-history";
import type { ConnectionRequest } from "../src/shared/connections";

const id = "11111111-1111-4111-8111-111111111111";
const nextId = "22222222-2222-4222-8222-222222222222";
const rule = { text: "设备😀[literal]", color: "#AABBCC", enabled: true };
const request: ConnectionRequest = {
  kind: "serial",
  port: "COM-FAKE",
  baud: 115200,
  dataBits: 8,
  parity: "none",
  stopBits: "1",
  flowControl: "none",
  label: "Saved board",
};

test("literal rules have bounded Unicode text, safe colors and copied values", () => {
  const source = [{ ...rule, command: "not executable" }];
  const parsed = readHighlightRules(source)!;
  assert.deepEqual(parsed, [{ ...rule, color: "#aabbcc" }]);
  source[0]!.text = "changed";
  assert.equal(parsed[0]!.text, rule.text);
  for (const value of [
    null,
    {},
    Array(33).fill(rule),
    [{ ...rule, text: "" }],
    [{ ...rule, text: "x".repeat(129) }],
    [{ ...rule, text: "\ud800" }],
    [{ ...rule, text: "escape\x1b" }],
    [{ ...rule, text: "line\n" }],
    [{ ...rule, color: "red\x1b[31m" }],
    [{ ...rule, enabled: "true" }],
  ])
    assert.equal(readHighlightRules(value), undefined);
  assert.equal(
    readHighlightRules([{ ...rule, text: "😀".repeat(64) }])?.length,
    1,
  );
});

test("templates import independent copies and reject duplicate identity or excessive storage", () => {
  const source = [{ id, name: "Board template", rules: [rule] }];
  const templates = restoreHighlightTemplates(JSON.stringify(source));
  const imported = readHighlightRules(templates[0]!.rules)!;
  templates[0]!.rules[0]!.text = "edited template";
  assert.equal(imported[0]!.text, rule.text);
  for (const raw of [
    "{",
    "x".repeat(65537),
    JSON.stringify(Array(11).fill(source[0])),
    JSON.stringify([source[0], source[0]]),
    JSON.stringify([{ ...source[0], name: "bad\nname" }]),
  ])
    assert.deepEqual(restoreHighlightTemplates(raw), []);
});

test("saved connection identity survives reuse while another preset never inherits its rules", () => {
  const legacy = recordConnection([], request, false, 1);
  assert.equal(legacy[0]!.id, undefined);
  const migrated = recordConnection(legacy, request, false, 2, id);
  migrated[0]!.highlights = readHighlightRules([rule]);
  const reused = recordConnection(migrated, request, false, 3, nextId);
  assert.equal(reused[0]!.id, id);
  assert.deepEqual(reused[0]!.highlights, migrated[0]!.highlights);
  reused[0]!.highlights![0]!.enabled = false;
  assert.equal(migrated[0]!.highlights![0]!.enabled, true);
  const other = recordConnection(
    migrated,
    { ...request, baud: 9600 },
    false,
    4,
    nextId,
  );
  assert.equal(other[0]!.id, nextId);
  assert.equal(other[0]!.highlights, undefined);
  assert.equal(other[1]!.id, id);
  assert.deepEqual(restoreConnections(JSON.stringify(other)), other);
  const duplicated = restoreConnections(
    JSON.stringify([
      migrated[0],
      { ...migrated[0], request: { ...request, baud: 9600 } },
    ]),
  );
  assert.equal(
    duplicated.length,
    2,
    "damaged presentation identity does not erase valid connection settings",
  );
  assert.ok(
    duplicated.every(
      (entry) => entry.id === undefined && entry.highlights === undefined,
    ),
  );
});
