import test from "node:test";
import assert from "node:assert/strict";
import {
  defaultShortcuts,
  isShortcutBindings,
  shortcutFromEvent,
  shortcutLabel,
  restoreShortcuts,
  type ShortcutKeyEvent,
} from "../src/shared/shortcuts";
import {
  applicationMenu,
  initialMenuState,
  isAction,
  isMenuState,
} from "../src/shared/menus";

const key: ShortcutKeyEvent = {
  code: "KeyB",
  ctrlKey: true,
  metaKey: false,
  altKey: false,
  shiftKey: false,
  isComposing: false,
  keyCode: 66,
  getModifierState: () => false,
};

test("platform defaults and physical Option keys match the layout commands", () => {
  assert.deepEqual(defaultShortcuts(false), {
    "view.sidebar": "Ctrl+B",
    "view.auxiliary": "Ctrl+Alt+B",
    "view.panel": "Ctrl+J",
    "terminal.copy": "Ctrl+Shift+C",
    "terminal.paste": "Ctrl+Shift+V",
    "terminal.find": "Ctrl+Shift+F",
  });
  assert.deepEqual(defaultShortcuts(true), {
    "view.sidebar": "Meta+B",
    "view.auxiliary": "Meta+Alt+B",
    "view.panel": "Meta+J",
    "terminal.copy": "Ctrl+Shift+C",
    "terminal.paste": "Ctrl+Shift+V",
    "terminal.find": "Ctrl+Shift+F",
  });
  assert.equal(shortcutFromEvent(key), "Ctrl+B");
  assert.equal(
    shortcutFromEvent({ ...key, ctrlKey: false, metaKey: true, altKey: true }),
    "Meta+Alt+B",
  );
  assert.equal(shortcutLabel("Meta+Alt+B"), "⌘⌥B");
  assert.equal(
    shortcutFromEvent({ ...key, code: "Digit1", shiftKey: true }),
    "Ctrl+Shift+1",
  );
});

test("terminal clipboard chords and migration preserve custom layout bindings", () => {
  assert.equal(
    shortcutFromEvent({ ...key, code: "KeyC", shiftKey: true }),
    "Ctrl+Shift+C",
  );
  assert.equal(
    shortcutFromEvent({ ...key, code: "KeyV", shiftKey: true }),
    "Ctrl+Shift+V",
  );
  const old = {
    "view.sidebar": "Ctrl+K",
    "view.auxiliary": "",
    "view.panel": "Ctrl+Shift+J",
  };
  assert.deepEqual(restoreShortcuts(old, false), {
    ...defaultShortcuts(false),
    ...old,
  });
  assert.deepEqual(
    restoreShortcuts({ ...old, unexpected: "Ctrl+F" }, false),
    defaultShortcuts(false),
  );
  assert.equal(
    isShortcutBindings({
      ...defaultShortcuts(false),
      "terminal.copy": "Ctrl+J",
    }),
    false,
  );
});

test("Find migration preserves existing chords and leaves conflicts unassigned", () => {
  const previous = {
    "view.sidebar": "Ctrl+Shift+F",
    "view.auxiliary": "",
    "view.panel": "Ctrl+K",
    "terminal.copy": "Ctrl+Shift+C",
    "terminal.paste": "Ctrl+Shift+V",
  };
  assert.deepEqual(restoreShortcuts(previous, false), {
    ...previous,
    "terminal.find": "",
  });
  assert.deepEqual(
    restoreShortcuts({ ...previous, "view.sidebar": "Ctrl+B" }, false),
    {
      ...previous,
      "view.sidebar": "Ctrl+B",
      "terminal.find": "Ctrl+Shift+F",
    },
  );
  assert.equal(
    shortcutFromEvent({ ...key, code: "KeyF", shiftKey: true }),
    "Ctrl+Shift+F",
  );
  assert.deepEqual(
    restoreShortcuts(
      {
        "view.sidebar": "Ctrl+Shift+F",
        "view.auxiliary": "",
        "terminal.copy": "Ctrl+K",
      },
      false,
    ),
    defaultShortcuts(false),
  );
});

test("IME, AltGr, ordinary typing and reserved keys cannot become layout shortcuts", () => {
  for (const change of [
    { isComposing: true },
    { keyCode: 229 },
    { getModifierState: () => true },
    { ctrlKey: false },
    { metaKey: true },
    { code: "KeyC" },
    { code: "KeyW" },
    { code: "Escape" },
    { code: "F4", altKey: true },
  ])
    assert.equal(shortcutFromEvent({ ...key, ...change }), undefined);
});

test("stored bindings and IPC hints reject conflicts, malformed and extra capabilities", () => {
  const defaults = defaultShortcuts(false);
  for (const value of [
    null,
    [],
    {},
    { ...defaults, "view.panel": "Ctrl+B" },
    { ...defaults, "view.panel": "Ctrl+C" },
    { ...defaults, "view.panel": "Alt+F4" },
    { ...defaults, "shell.exec": "Ctrl+E" },
    { ...defaults, "view.panel": "x".repeat(512) },
  ]) {
    assert.equal(isShortcutBindings(value), false);
    assert.equal(isMenuState({ ...initialMenuState, shortcuts: value }), false);
  }
  assert.equal(
    isShortcutBindings({ ...defaults, "view.sidebar": "", "view.panel": "" }),
    true,
  );
  for (const id of [
    "view.sidebar",
    "view.auxiliary",
    "view.panel",
    "view.transfers",
    "view.status",
    "settings.shortcuts",
  ]) {
    assert.equal(isAction({ id }), true);
    assert.equal(isAction({ id, command: "bad" }), false);
  }
  assert.equal(
    isMenuState({ ...initialMenuState, auxiliaryVisible: "yes" }),
    false,
  );
  const menu = applicationMenu({
    ...initialMenuState,
    auxiliaryVisible: true,
    panelVisible: true,
    shortcuts: defaultShortcuts(true),
  });
  const view = menu.find((item) => item.id === "view")!.children!;
  assert.equal(
    view.find((item) => item.id === "view.auxiliary")!.checked,
    true,
  );
  assert.equal(view.find((item) => item.id === "view.panel")!.shortcut, "⌘J");
});
