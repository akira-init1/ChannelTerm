import test from "node:test";
import assert from "node:assert/strict";
import {
  focusSplit,
  reorderTabs,
  retainSplit,
  type SplitLayout,
} from "../src/shared/tab-layout";

test("local tab ordering preserves attachment identity and rejects stale or foreign targets", () => {
  const tabs = ["a", "b", "c"].map((id) => ({
    id,
    attachment: { id: `view-${id}`, sessionId: "shared-session" },
  }));
  const moved = reorderTabs(tabs, "a", "c", "after");
  assert.deepEqual(
    moved.map((tab) => tab.id),
    ["b", "c", "a"],
  );
  assert.equal(moved[2], tabs[0]);
  assert.equal(moved[2]!.attachment, tabs[0]!.attachment);
  assert.deepEqual(
    tabs.map((tab) => tab.id),
    ["a", "b", "c"],
  );
  assert.deepEqual(reorderTabs(moved, "a", "b", "before"), tabs);
  for (const [source, target] of [
    ["other-window", "b"],
    ["a", "closed"],
    ["a", "a"],
  ])
    assert.equal(reorderTabs(tabs, source!, target!, "before"), tabs);
});

test("split focus never swaps pane positions and hidden tabs replace only the focused pane", () => {
  const split: SplitLayout = { axis: "columns", first: "a", second: "b" };
  assert.equal(focusSplit(split, "a", "b"), split);
  assert.deepEqual(focusSplit(split, "b", "c"), { ...split, second: "c" });
  assert.deepEqual(focusSplit(split, "a", "c"), { ...split, first: "c" });
  assert.equal(focusSplit(split, "a", undefined), undefined);
  const tabs = [{ id: "a" }, { id: "b" }, { id: "c" }];
  assert.equal(retainSplit(split, tabs), split);
  assert.equal(retainSplit(split, tabs.slice(1)), undefined);
  assert.equal(retainSplit({ ...split, second: "a" }, tabs), undefined);
  assert.equal(tabs.length, 3, "collapsing layout never deletes a tab");
});
