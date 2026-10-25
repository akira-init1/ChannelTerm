import assert from "node:assert/strict";
import { test } from "node:test";
import { defaultHighlights } from "../src/shared/default-highlights";
import examples from "../../../internal/cli/highlight/testdata/defaults-cases.json";

test("desktop and CLI load one versioned palette and match the shared rule corpus", () => {
  assert.equal(defaultHighlights.version, 1);
  const seen = new Set<number>();
  for (const example of examples.rules) {
    const rule = defaultHighlights.rules[example.rule];
    assert.ok(rule);
    seen.add(example.rule);
    assert.ok(rule.kind in defaultHighlights.palette);
    assert.ok(Number.isInteger(rule.priority) && rule.priority > 0);
    const pattern = new RegExp(rule.pattern, `${rule.flags}gd`);
    const actual = Array.from(example.input.matchAll(pattern), (match) => {
      const range = match.indices?.[rule.group];
      assert.ok(range);
      return example.input.slice(...range);
    });
    assert.deepEqual(actual, example.matches, `rule ${example.rule}`);
    pattern.lastIndex = 0;
    assert.equal(pattern.test("plain"), false);
  }
  assert.equal(seen.size, defaultHighlights.rules.length);
  for (const example of examples.prompts) {
    assert.ok(example.pattern in defaultHighlights.prompts);
    const source =
      defaultHighlights.prompts[
        example.pattern as keyof typeof defaultHighlights.prompts
      ];
    const pattern = new RegExp(source);
    assert.deepEqual(pattern.exec(example.input)?.slice(1), example.captures);
    assert.equal(pattern.test(`log: ${example.input}`), false);
  }
});
