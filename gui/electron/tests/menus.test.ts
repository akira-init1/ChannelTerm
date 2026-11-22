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
  systemMenuLocale,
  windowActions,
  tabMenu,
  type MenuEntry,
} from "../src/shared/menus";
import { menuCatalogs } from "../src/locales/catalogs";
import { menuMessages } from "../src/shared/menu-messages";

function flatten(items: MenuEntry[]): MenuEntry[] {
  return items.flatMap((item) => [item, ...flatten(item.children ?? [])]);
}

test("the serial size command targets a writable serial text attachment", () => {
  const attachment = { id: "pane", sessionId: "board" };
  const state = {
    ...initialMenuState,
    attachment,
    connected: true,
    currentOpen: true,
    currentSerial: true,
  };
  const action = { id: "terminal.serialSizeInsert", attachment } as const;
  const entry = (menu: MenuEntry[]) =>
    flatten(menu).find((item) => item.id === action.id);
  assert.deepEqual(entry(applicationMenu(state))?.action, action);
  assert.equal(entry(terminalMenu(state))?.enabled, true);
  assert.equal(isAction(action), true);
  assert.equal(isAction({ ...action, command: "arbitrary" }), false);
  for (const change of [
    { currentSerial: false },
    { hexView: true },
    { attachment: undefined },
  ]) {
    assert.equal(entry(terminalMenu({ ...state, ...change })), undefined);
    assert.equal(actionEnabled(action, { ...state, ...change }), false);
  }
  for (const change of [
    { currentOpen: false },
    { connected: false },
    { busy: true },
    { leased: true },
    { inputPaused: true },
  ]) {
    assert.equal(entry(terminalMenu({ ...state, ...change }))?.enabled, false);
    assert.equal(actionEnabled(action, { ...state, ...change }), false);
  }
  assert.equal(
    actionEnabled(action, {
      ...state,
      attachment: { ...attachment, id: "other" },
    }),
    false,
  );
});

test("U-Boot is a finite serial-only action requiring advertised cancellation and the original attachment", () => {
  const state = {
    ...initialMenuState,
    connected: true,
    currentOpen: true,
    currentSerial: true,
    ubootAvailable: true,
    attachment: { id: "view", sessionId: "opaque-session" },
  };
  const entry = flatten(terminalMenu(state)).find(
    (row) => row.id === "uboot.open",
  )!;
  assert.equal(entry.enabled, true);
  assert.equal(isAction(entry.action), true);
  assert.equal(actionEnabled(entry.action!, state), true);
  for (const change of [
    { ubootAvailable: false },
    { currentSerial: false },
    { currentOpen: false },
    { connected: false },
    { busy: true },
    { leased: true },
    { inputPaused: true },
  ]) {
    assert.equal(
      flatten(terminalMenu({ ...state, ...change })).find(
        (row) => row.id === "uboot.open",
      )!.enabled,
      false,
    );
    assert.equal(actionEnabled(entry.action!, { ...state, ...change }), false);
  }
  assert.equal(
    actionEnabled(entry.action!, {
      ...state,
      attachment: { id: "replacement", sessionId: "opaque-session" },
    }),
    false,
  );
  assert.equal(isAction({ ...entry.action, command: "reboot" }), false);
  assert.equal(isMenuState({ ...state, ubootAvailable: "yes" }), false);
});

test("split actions bind an exact live attachment, respect view capacity and never imply write authority", () => {
  const state = {
    ...initialMenuState,
    connected: true,
    currentOpen: true,
    tabCount: 2,
    attachment: { id: "observer", sessionId: "shared" },
    leased: true,
    inputPaused: true,
  };
  for (const id of ["tab.splitRight", "tab.splitDown"] as const) {
    const action = { id, attachment: { ...state.attachment } };
    assert.equal(isAction(action), true);
    assert.equal(
      actionEnabled(action, state),
      true,
      "read-only observers can open independent read views",
    );
    for (const changed of [
      { connected: false },
      { currentOpen: false },
      { busy: true },
      { tabCount: 32 },
      { attachment: { id: "replacement", sessionId: "shared" } },
    ])
      assert.equal(actionEnabled(action, { ...state, ...changed }), false);
    assert.equal(
      actionEnabled(action, { ...state, tabCount: 32, split: true }),
      true,
      "orientation changes need no extra attachment",
    );
    assert.equal(isAction({ ...action, command: "unexpected" }), false);
    assert.equal(isAction({ id, sessionId: "shared" }), false);
  }
  assert.equal(actionEnabled({ id: "view.single" }, state), false);
  assert.equal(
    actionEnabled({ id: "view.single" }, { ...state, split: true }),
    true,
  );
  assert.equal(isMenuState({ ...state, split: "true" }), false);
});

test("shared close is explicit, bound to the captured attachment, and unavailable offline or closed", () => {
  const state = {
    ...initialMenuState,
    connected: true,
    currentOpen: true,
    attachment: { id: "view", sessionId: "session" },
  };
  const close = flatten(applicationMenu(state)).find(
    (entry) => entry.id === "session.close",
  )!;
  assert.equal(close.enabled, true);
  assert.equal(isAction(close.action), true);
  assert.equal(actionEnabled(close.action!, state), true);
  for (const patch of [
    { connected: false },
    { currentOpen: false },
    { busy: true },
    { attachment: { id: "replacement", sessionId: "session" } },
  ])
    assert.equal(actionEnabled(close.action!, { ...state, ...patch }), false);
  assert.equal(
    sessionMenu(state, "another-session", true).some(
      (entry) => entry.id === "session.close",
    ),
    false,
  );
  assert.equal(actionEnabled(close.action!, { ...state, leased: true }), true);
});

test("tab navigation, empty-tab close and move actions use bounded display state and exact targets", () => {
  const state = {
    ...initialMenuState,
    connected: true,
    currentOpen: true,
    tabId: "tab-a",
    tabCount: 2,
    windowCount: 2,
    attachment: { id: "attachment-a", sessionId: "session-a" },
  };
  for (const id of windowActions)
    assert.equal(actionEnabled({ id }, state), true);
  assert.equal(
    actionEnabled({ id: "tab.new" }, { ...state, tabCount: 32 }),
    false,
  );
  assert.equal(
    actionEnabled({ id: "connection.new" }, { ...state, tabCount: 32 }),
    false,
  );
  assert.equal(
    actionEnabled(
      { id: "connection.new" },
      { ...state, tabCount: 32, attachment: undefined },
    ),
    true,
  );
  assert.equal(
    actionEnabled({ id: "window.new" }, { ...state, windowCount: 16 }),
    false,
  );
  assert.equal(isMenuState({ ...state, tabCount: 33 }), false);
  assert.equal(isMenuState({ ...state, windowCount: 17 }), false);
  assert.equal(isAction({ id: "tab.closeEmpty", tabId: "tab-a" }), true);
  assert.equal(
    actionEnabled({ id: "tab.closeEmpty", tabId: "tab-a" }, state),
    false,
  );
  assert.equal(
    actionEnabled(
      { id: "tab.closeEmpty", tabId: "tab-a" },
      { ...state, attachment: undefined },
    ),
    true,
  );
  assert.equal(
    actionEnabled(
      { id: "tab.closeEmpty", tabId: "other" },
      { ...state, attachment: undefined },
    ),
    false,
  );
  const move = tabMenu(state).find((entry) => entry.id === "tab.move")!.action!;
  assert.equal(actionEnabled(move, state), true);
  assert.equal(
    actionEnabled(move, {
      ...state,
      attachment: { id: "replacement", sessionId: "session-a" },
    }),
    false,
  );
  assert.equal(actionEnabled(move, { ...state, connected: false }), false);
  assert.equal(actionEnabled(move, { ...state, currentOpen: false }), false);
  assert.equal(actionEnabled(move, { ...state, busy: true }), false);
});

test("Session owns setup and file actions; window actions follow window and tab availability", () => {
  const live = {
    ...initialMenuState,
    connected: true,
    currentOpen: true,
    currentSerial: true,
    attachment: { id: "attachment", sessionId: "session" },
  };
  const menus = applicationMenu(live);
  const session = menus.find((entry) => entry.id === "session")!.children!;
  assert.deepEqual(
    session.map((entry) => entry.id),
    [
      "connection.new",
      "session.choose",
      "session.refresh",
      "session.files",
      "tab.close",
      "session.close",
      "session.info",
    ],
  );
  assert.ok(session[0]);
  assert.ok(session[3]);
  assert.equal(session[0].children, undefined);
  assert.deepEqual(session[0].action, { id: "connection.new" });
  assert.deepEqual(
    session[3].children?.map((entry) => entry.id),
    ["file.send", "file.receive", "file.resume"],
  );
  assert.deepEqual(
    menus
      .find((entry) => entry.id === "host")
      ?.children?.map((entry) => entry.id),
    ["setup.open", "host.settings", "host.connect", "host.disconnect"],
  );
  for (const state of [initialMenuState, live, { ...live, busy: true }]) {
    const window = applicationMenu(state).find(
      (entry) => entry.id === "window",
    )!.children!;
    for (const id of windowActions) {
      const action = { id };
      assert.equal(isAction(action), true);
      const expected =
        id === "window.new" ||
        id === "window.close" ||
        (id === "tab.new" && !state.busy);
      assert.equal(actionEnabled(action, state), expected);
      assert.equal(window.find((entry) => entry.id === id)?.enabled, expected);
    }
    const close = window.find((entry) => entry.id === "window.tab.close")!;
    assert.equal(close.enabled, !!state.attachment && !state.busy);
    if (state.attachment)
      assert.deepEqual(close.action, {
        id: "tab.close",
        attachment: state.attachment,
      });
    for (const id of ["connection.new", "session.choose"] as const)
      assert.equal(
        actionEnabled({ id }, state),
        state.connected && !state.busy,
      );
  }
});

test("system language resolves locale variants and keeps exactly one preference checked", () => {
  for (const [languages, expected] of [
    [["en-US"], "en"],
    [["ja-JP"], "ja"],
    [["zh-Hans-CN"], "zh-CN"],
    [["zh-Hant-HK"], "zh-TW"],
    [["unknown", "de-DE"], "de"],
    [[], "en"],
  ] as const)
    assert.equal(systemMenuLocale(languages), expected);
  assert.equal(isAction({ id: "language.change", locale: "system" }), true);
  assert.equal(
    isMenuState({ ...initialMenuState, languagePreference: "system" }),
    true,
  );
  assert.equal(
    isMenuState({ ...initialMenuState, languagePreference: "bad" }),
    false,
  );
  for (const locale of locales) {
    const checked = flatten(
      applicationMenu({
        ...initialMenuState,
        locale,
        languagePreference: "system",
      }),
    ).filter((entry) => entry.id.startsWith("language.") && entry.checked);
    assert.deepEqual(
      checked.map((entry) => entry.id),
      ["language.system"],
    );
  }
});

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
    ["session", "host", "terminal", "view", "window", "settings", "help"],
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
      if (locale === "en" && message.id !== "workbench.sessionCount")
        assert.equal(
          i18n._(message.id, {
            name: "{name}",
            host: "{host}",
            sessionId: "{sessionId}",
            reference: "{reference}",
            transport: "{transport}",
            endpoint: "{endpoint}",
            seconds: "{seconds}",
            exitSeconds: "{exitSeconds}",
          }),
          message.message,
        );
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
  for (const entry of sessionMenu(live, "opaque-b", true, true).slice(2))
    assert.deepEqual(entry.action, { id: entry.id, sessionId: "opaque-b" });
  for (const state of [
    { ...live, currentSerial: true, leased: true },
    { ...live, currentSerial: true, currentOpen: false },
  ])
    for (const entry of flatten(applicationMenu(state)).filter((entry) =>
      ["file.send", "file.receive", "file.resume"].includes(entry.id),
    ))
      assert.equal(entry.enabled, false);
  const close = { id: "tab.close", attachment: old } as const;
  const paste = { id: "terminal.paste", attachment: old } as const;
  const copy = { id: "terminal.copy", attachment: old } as const;
  const find = { id: "terminal.find", attachment: old } as const;
  assert.equal(isAction(find), true);
  assert.equal(isAction({ id: "terminal.find" }), false);
  assert.equal(
    actionEnabled(find, {
      ...live,
      leased: true,
      hexView: true,
      currentOpen: false,
    }),
    true,
  );
  assert.equal(
    actionEnabled(find, { ...live, attachment: { ...old, id: "new" } }),
    false,
  );
  assert.equal(actionEnabled(paste, live), true);
  assert.equal(actionEnabled(paste, { ...live, leased: true }), false);
  assert.equal(actionEnabled(paste, { ...live, inputPaused: true }), false);
  assert.equal(actionEnabled(paste, { ...live, hexView: true }), false);
  assert.equal(
    terminalMenu({ ...live, hexView: true }).find(
      (item) => item.id === "terminal.paste",
    )?.enabled,
    false,
  );
  assert.equal(actionEnabled(paste, { ...live, currentOpen: false }), false);
  assert.equal(
    actionEnabled(paste, { ...live, attachment: { ...old, id: "new" } }),
    false,
  );
  assert.equal(actionEnabled(copy, { ...live, leased: true }), true);
  assert.equal(isAction({ id: "terminal.paste" }), false);
  assert.equal(actionEnabled(close, { ...live, currentOpen: false }), true);
  assert.equal(
    actionEnabled(close, { ...live, attachment: { ...old, id: "new" } }),
    false,
  );
  assert.equal(actionEnabled(close, { ...live, busy: true }), false);
  assert.equal(
    actionEnabled(
      { id: "session.info", attachment: old },
      { ...live, attachment: { ...old, id: "replacement" } },
    ),
    false,
  );
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

test("size display hints preserve menu actions but never admit private capabilities", () => {
  const attachment = {
    id: "view",
    sessionId: "session",
    remoteSize: true,
    ownsRemoteSize: true,
  };
  assert.equal(isMenuState({ ...initialMenuState, attachment }), true);
  assert.equal(isAction({ id: "terminal.copy", attachment }), true);
  assert.equal(
    isMenuState({
      ...initialMenuState,
      attachment: { ...attachment, owner: "private" },
    }),
    false,
  );
  assert.equal(
    isMenuState({
      ...initialMenuState,
      attachment: { ...attachment, ownsRemoteSize: "true" },
    }),
    false,
  );
});

test("line display actions are independent, default visible, local and attachment-fenced without shortcuts", () => {
  const attachment = { id: "view", sessionId: "shared" };
  const state = {
    ...initialMenuState,
    attachment,
    lineDisplay: { timestamps: true, sequence: false, foldMarkers: false },
  };
  const auxiliary = terminalMenu(state).find(
    (entry) => entry.id === "terminal.auxiliary",
  )!;
  assert.equal(auxiliary.children?.length, 3);
  const defaults = terminalMenu({ ...initialMenuState, attachment }).find(
    (entry) => entry.id === "terminal.auxiliary",
  );
  assert.deepEqual(
    defaults?.children?.map((entry) => entry.checked),
    [true, true, true],
  );
  for (const [index, id] of (
    [
      "terminal.timestamps",
      "terminal.sequence",
      "terminal.foldMarkers",
    ] as const
  ).entries()) {
    const entry: MenuEntry = auxiliary.children![index]!;
    assert.equal(entry.checked, index === 0);
    assert.equal(entry.shortcut, undefined);
    assert.deepEqual(entry.action, { id, attachment });
    assert.equal(
      actionEnabled(entry.action!, state),
      true,
      "retained closed/offline output remains locally configurable",
    );
    assert.equal(
      actionEnabled(entry.action!, { ...state, attachment: undefined }),
      false,
    );
    assert.equal(
      actionEnabled(entry.action!, {
        ...state,
        attachment: { ...attachment, id: "new" },
      }),
      false,
    );
    assert.equal(isAction({ id, attachment, command: "unexpected" }), false);
  }
  for (const lineDisplay of [
    null,
    { timestamps: true },
    { timestamps: true, sequence: 1 },
    { timestamps: true, sequence: false, foldMarkers: "no" },
    { timestamps: false, sequence: false, extra: true },
  ])
    assert.equal(isMenuState({ ...state, lineDisplay }), false);
  assert.ok(
    terminalMenu(initialMenuState)
      .find((entry) => entry.id === "terminal.auxiliary")!
      .children!.every((entry) => !entry.enabled && !entry.checked),
  );
});
