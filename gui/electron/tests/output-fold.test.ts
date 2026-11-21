import assert from "node:assert/strict";
import test from "node:test";
import { foldedRows } from "../src/renderer/terminal/output-fold";
import {
  actionEnabled,
  initialMenuState,
  isAction,
  terminalMenu,
} from "../src/shared/menus";

test("two independently folded commands keep headers, wrapped header rows and subsequent output", () => {
  const first = { header: 1, start: 3, end: 6 },
    second = { header: 6, start: 7, end: 10 };
  assert.deepEqual(foldedRows(11, 12, [first, second]), [0, 1, 2, 6, 10, 11]);
  assert.deepEqual(foldedRows(11, 5, [second]), [4, 5, 6, 10, 11]);
  assert.deepEqual(foldedRows(8, 3, [first, second]), [1, 2, 6]);
  assert.deepEqual(foldedRows(1, 4, [first, second]), [0, 1]);
});

test("folding requires the same attachment and text view, including read-only observers", () => {
  const attachment = { id: "view", sessionId: "session" };
  const state = { ...initialMenuState, attachment, leased: true };
  const action = { id: "terminal.fold", attachment } as const;
  assert.ok(isAction(action));
  assert.ok(!isAction({ ...action, command: "clear" }));
  assert.ok(actionEnabled(action, state));
  for (const patch of [
    { busy: true },
    { hexView: true },
    { attachment: undefined },
  ]) {
    assert.equal(
      terminalMenu({ ...state, ...patch }).find((item) => item.id === action.id)
        ?.enabled,
      false,
    );
    assert.equal(actionEnabled(action, { ...state, ...patch }), false);
  }
  assert.equal(
    actionEnabled(action, {
      ...state,
      attachment: { ...attachment, id: "other" },
    }),
    false,
  );
});
