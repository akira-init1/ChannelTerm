import test from "node:test";
import assert from "node:assert/strict";
import { setupI18n } from "@lingui/core";
import {
  actionEnabled,
  applicationMenu,
  initialMenuState,
  isAction,
  isMenuState,
  locales,
  sessionMenu,
  terminalMenu,
  type MenuEntry,
} from "../src/shared/menus";
import { menuCatalogs } from "../src/locales/catalogs";
import { menuMessages } from "../src/shared/menu-messages";

function flatten(items: MenuEntry[]): MenuEntry[] {
  return items.flatMap((item) => [item, ...flatten(item.children ?? [])]);
}

test("menu catalogs cover every implemented item in all three languages and depth is bounded", () => {
  function depth(items: MenuEntry[]): number {
    return Math.max(0, ...items.map((item) => 1 + depth(item.children ?? [])));
  }
  assert.equal(depth(applicationMenu(initialMenuState)), 3);
  assert.deepEqual(
    applicationMenu(initialMenuState).map((entry) => entry.id),
    ["connection", "session", "terminal", "view", "settings", "help"],
  );
  const expected = ["Connection", "连接", "接続"];
  for (const [index, locale] of locales.entries()) {
    const i18n = setupI18n({ locale, messages: menuCatalogs });
    assert.equal(i18n._("menu.connection"), expected[index]);
    for (const message of menuMessages) {
      assert.ok(
        Object.hasOwn(menuCatalogs[locale], message.id),
        `Extract and translate ${message.id} for ${locale}`,
      );
      if (locale === "en") assert.equal(i18n._(message.id), message.message);
    }
    const entries = [
      ...flatten(applicationMenu({ ...initialMenuState, locale })),
      ...sessionMenu(initialMenuState, "opaque", false),
      ...terminalMenu({ ...initialMenuState, inputPaused: true }),
    ];
    for (const item of entries) assert.notEqual(i18n._(item.label), item.label);
    assert.equal(
      entries.filter((item) => item.id.startsWith("language.") && item.checked)
        .length,
      1,
    );
  }
});

test("offline, closed, busy and leased menus are display hints with explicit attachment identity", () => {
  const old = { id: "attachment-old", sessionId: "opaque-a" };
  const live = {
    ...initialMenuState,
    connected: true,
    currentOpen: true,
    attachment: old,
  };
  assert.equal(actionEnabled({ id: "host.connect" }, initialMenuState), true);
  assert.equal(
    actionEnabled({ id: "session.refresh" }, initialMenuState),
    false,
  );
  assert.equal(sessionMenu(live, "opaque-b", false)[0]?.enabled, false);
  const other = sessionMenu(live, "opaque-b", true);
  assert.deepEqual(other[0]?.action, {
    id: "session.attach",
    sessionId: "opaque-b",
  });
  assert.equal(other[1]?.enabled, false);
  assert.equal(other[1]?.action, undefined);
  const close = { id: "tab.close", attachment: old } as const;
  assert.equal(actionEnabled(close, { ...live, currentOpen: false }), true);
  assert.equal(
    actionEnabled(close, { ...live, attachment: { ...old, id: "new" } }),
    false,
  );
  assert.equal(actionEnabled(close, { ...live, busy: true }), false);
  assert.equal(
    actionEnabled(
      { id: "terminal.resume", attachment: old },
      { ...live, inputPaused: true, leased: true },
    ),
    false,
  );
  assert.equal(
    actionEnabled(
      { id: "terminal.clear", attachment: old },
      { ...live, leased: true },
    ),
    true,
  );
});

test("menu IPC rejects unknown actions, language, arbitrary templates and oversized targets", () => {
  assert.equal(isMenuState(initialMenuState), true);
  for (const value of [
    null,
    [],
    {},
    { ...initialMenuState, token: "secret" },
    { ...initialMenuState, locale: "fr" },
    { ...initialMenuState, busy: "false" },
    { ...initialMenuState, attachment: { id: "x", sessionId: "x\n" } },
  ])
    assert.equal(isMenuState(value), false);
  for (const value of [
    { id: "session.close" },
    { id: "host.start" },
    { id: "shell.exec", command: "x" },
    { id: "app.quit", extra: true },
    { id: "language.change", locale: "fr" },
    { id: "session.attach", sessionId: "x".repeat(513) },
    { id: "tab.close", attachment: { id: "x" } },
  ])
    assert.equal(isAction(value), false);
  assert.equal(isAction({ id: "session.attach", sessionId: "opaque" }), true);
});
