import assert from "node:assert/strict";
import test from "node:test";
import {
  moveDirectoryRow,
  orderedKeys,
  readDirectoryOrder,
} from "../src/renderer/sessions/directory-order";

test("manual order survives renamed or filtered rows, eviction and new records", () => {
  const keys = ["COM8", "hidden", "COM9", "SSH"];
  const order = moveDirectoryRow(keys, ["COM8", "COM9", "SSH"], "SSH", "COM8");
  assert.deepEqual(order, ["SSH", "hidden", "COM8", "COM9"]);
  assert.deepEqual(
    orderedKeys(
      ["COM9", "new", "COM8", "SSH"],
      readDirectoryOrder(JSON.stringify(order)),
    ),
    ["SSH", "COM8", "COM9", "new"],
  );
  assert.equal(
    moveDirectoryRow(keys, ["COM8", "COM9"], "COM8", "missing"),
    keys,
  );
});
test("invalid or oversized directory preferences fall back to default ordering", () => {
  for (const raw of [
    null,
    "{",
    "{}",
    "[1]",
    JSON.stringify(Array(513).fill("x")),
    JSON.stringify(["x".repeat(2049)]),
  ])
    assert.deepEqual(readDirectoryOrder(raw), []);
  assert.deepEqual(readDirectoryOrder('["a","a","b"]'), ["a", "b"]);
});
