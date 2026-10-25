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

test("SSH file actions allow dedicated transfers but never offer serial checkpoint resume", () => {
  const state = {
    ...initialMenuState,
    connected: true,
    currentOpen: true,
    currentSSH: true,
    leased: true,
    attachment: { id: "ssh-attachment", sessionId: "ssh-session" },
  };
  const entries = flatten(applicationMenu(state));
  assert.equal(
    entries.find((entry) => entry.id === "file.send")?.enabled,
    true,
  );
  assert.equal(
    entries.find((entry) => entry.id === "file.receive")?.enabled,
    true,
  );
  assert.equal(
    entries.find((entry) => entry.id === "file.resume")?.enabled,
    false,
  );
  const context = sessionMenu(state, "other-ssh", true, false, true);
  assert.equal(
    context.find((entry) => entry.id === "file.send")?.enabled,
    true,
  );
  assert.equal(
    context.find((entry) => entry.id === "file.resume")?.enabled,
    false,
  );
});

test("menu catalogs cover every implemented item in all nine languages and depth is bounded", () => {
  function depth(items: MenuEntry[]): number {
    return Math.max(0, ...items.map((item) => 1 + depth(item.children ?? [])));
  }
  assert.equal(depth(applicationMenu(initialMenuState)), 3);
  assert.deepEqual(
    applicationMenu(initialMenuState).map((entry) => entry.id),
    ["connection", "session", "terminal", "view", "settings", "help"],
  );
  const expected = {
    en: "Connection",
    de: "Verbindung",
    es: "Conexión",
    fr: "Connexion",
    ja: "接続",
    ko: "연결",
    ru: "Подключение",
    "zh-CN": "连接",
    "zh-TW": "連線",
  };
  assert.deepEqual(locales, Object.keys(expected));
  for (const locale of locales) {
    const i18n = setupI18n({ locale, messages: menuCatalogs });
    assert.equal(i18n._("menu.connection"), expected[locale]);
    assert.equal(isMenuState({ ...initialMenuState, locale }), true);
    assert.equal(isAction({ id: "language.change", locale }), true);
    for (const message of menuMessages) {
      assert.ok(
        Object.hasOwn(menuCatalogs[locale], message.id),
        `Extract and translate ${message.id} for ${locale}`,
      );
      if (locale === "en") assert.equal(i18n._(message.id), message.message);
      assert.doesNotMatch(
        i18n._(message.id),
        /\?{2,}|\uFFFD/,
        `${locale}: ${message.id} must preserve Unicode`,
      );
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
    actionEnabled({ id: "connection.serial" }, initialMenuState),
    false,
  );
  assert.equal(actionEnabled({ id: "connection.serial" }, live), true);
  assert.equal(actionEnabled({ id: "connection.ssh" }, live), false);
  assert.equal(
    actionEnabled({ id: "connection.ssh" }, { ...live, sshAvailable: true }),
    true,
  );
  assert.equal(
    actionEnabled(
      { id: "connection.ssh" },
      { ...live, sshAvailable: true, busy: true },
    ),
    false,
  );
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
    { ...initialMenuState, locale: "unsupported" },
    { ...initialMenuState, busy: "false" },
    { ...initialMenuState, attachment: { id: "x", sessionId: "x\n" } },
  ])
    assert.equal(isMenuState(value), false);
  for (const value of [
    { id: "session.close" },
    { id: "host.start" },
    { id: "shell.exec", command: "x" },
    { id: "app.quit", extra: true },
    { id: "language.change", locale: "unsupported" },
    { id: "session.attach", sessionId: "x".repeat(513) },
    { id: "tab.close", attachment: { id: "x" } },
  ])
    assert.equal(isAction(value), false);
  assert.equal(isAction({ id: "session.attach", sessionId: "opaque" }), true);
});
