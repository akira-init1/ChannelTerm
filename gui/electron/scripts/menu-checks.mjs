import assert from "node:assert/strict";
import { expect } from "@playwright/test";
import { HostClient } from "../src/main/host/client.ts";

async function nativeItem(desktop, id) {
  return desktop.evaluate(({ Menu }, id) => {
    const item = Menu.getApplicationMenu().getMenuItemById(id);
    return {
      label: item.label,
      enabled: item.enabled,
      checked: item.checked,
      accelerator: item.accelerator,
      type: item.type,
    };
  }, id);
}
async function nativeClick(desktop, id) {
  await desktop.evaluate(({ Menu }, id) => {
    const item = Menu.getApplicationMenu().getMenuItemById(id);
    item.click();
  }, id);
}
const popup = (page) =>
  page.getByRole("menu", { name: "Workbench menu", exact: true });
async function open(page, id) {
  await page.locator(`[data-menu="${id}"]`).click();
}

/** Exercise the real renderer bridge and native menu, without registering global keys. */
export async function checkOfflineMenus(desktop, page) {
  await expect(page.locator(".app-menu > button")).toHaveText([
    "Connection",
    "Session",
    "Terminal",
    "View",
    "Settings",
    "Help",
  ]);
  for (const id of [
    "host.disconnect",
    "tab.close",
    "terminal.clear",
    "session.refresh",
  ]) {
    assert.equal((await nativeItem(desktop, id)).enabled, false);
  }
  assert.equal((await nativeItem(desktop, "host.connect")).enabled, true);
  await open(page, "settings");
  await popup(page)
    .getByRole("menuitem", { name: "Language", exact: true })
    .click();
  await popup(page).getByRole("menuitemradio", { name: "简体中文" }).click();
  await expect(page.locator(".app-menu > button")).toHaveText([
    "连接",
    "会话",
    "终端",
    "视图",
    "设置",
    "帮助",
  ]);
  await expect
    .poll(async () => (await nativeItem(desktop, "host.disconnect")).label)
    .toBe("断开 GUI 与 Host");
  assert.equal((await nativeItem(desktop, "language.zh-CN")).checked, true);
  assert.equal((await nativeItem(desktop, "language.zh-CN")).type, "radio");
  await nativeClick(desktop, "language.ja");
  await expect(page.locator(".app-menu > button")).toHaveText([
    "接続",
    "セッション",
    "ターミナル",
    "表示",
    "設定",
    "ヘルプ",
  ]);
  await expect
    .poll(async () => (await nativeItem(desktop, "language.ja")).checked)
    .toBe(true);
  await open(page, "settings");
  await popup(page)
    .getByRole("menuitem", { name: "言語", exact: true })
    .click();
  await expect(
    popup(page).getByRole("menuitemradio", { name: "日本語" }),
  ).toHaveAttribute("aria-checked", "true");
  await page.screenshot({ path: "test-results/workbench-menu-ja.png" });
  await popup(page).getByRole("menuitemradio", { name: "English" }).click();
  await expect
    .poll(async () => (await nativeItem(desktop, "connection")).label)
    .toBe("Connection");

  // Switch through every supported catalog via the renderer. The native menu
  // must acknowledge the same label and exactly one checked language each time.
  const languages = [
    {
      locale: "de",
      name: "Deutsch",
      connection: "Verbindung",
      language: "Sprache",
    },
    {
      locale: "es",
      name: "Español",
      connection: "Conexión",
      language: "Idioma",
    },
    {
      locale: "fr",
      name: "Français",
      connection: "Connexion",
      language: "Langue",
    },
    { locale: "ja", name: "日本語", connection: "接続", language: "言語" },
    { locale: "ko", name: "한국어", connection: "연결", language: "언어" },
    {
      locale: "ru",
      name: "Русский",
      connection: "Подключение",
      language: "Язык",
    },
    { locale: "zh-CN", name: "简体中文", connection: "连接", language: "语言" },
    { locale: "zh-TW", name: "繁體中文", connection: "連線", language: "語言" },
    {
      locale: "en",
      name: "English",
      connection: "Connection",
      language: "Language",
    },
  ];
  const originalSize = await desktop.evaluate(({ BrowserWindow }) => {
    const window = BrowserWindow.getAllWindows()[0];
    const size = window.getSize();
    window.setSize(760, 500);
    return size;
  });
  let languageLabel = "Language";
  for (const language of languages) {
    await open(page, "settings");
    await popup(page)
      .getByRole("menuitem", { name: languageLabel, exact: true })
      .click();
    await expect(popup(page).getByRole("menuitemradio")).toHaveCount(9);
    await popup(page)
      .getByRole("menuitemradio", { name: language.name, exact: true })
      .click();
    await expect(page.locator('[data-menu="connection"]')).toHaveText(
      language.connection,
    );
    await expect
      .poll(async () => (await nativeItem(desktop, "connection")).label)
      .toBe(language.connection);
    await expect
      .poll(
        async () =>
          (await nativeItem(desktop, `language.${language.locale}`)).checked,
      )
      .toBe(true);
    const checked = await desktop.evaluate(({ Menu }) => {
      return Menu.getApplicationMenu()
        .getMenuItemById("language")
        .submenu.items.filter((item) => item.checked)
        .map((item) => item.id);
    });
    assert.deepEqual(checked, [`language.${language.locale}`]);
    const menuFits = await page.locator(".app-menu").evaluate((menu) => {
      const bounds = menu.getBoundingClientRect();
      return menu.scrollWidth <= menu.clientWidth && bounds.right <= innerWidth;
    });
    assert.equal(
      menuFits,
      true,
      `${language.locale}: menus fit the minimum window`,
    );
    languageLabel = language.language;
  }
  await desktop.evaluate(({ BrowserWindow }, size) => {
    BrowserWindow.getAllWindows()[0].setSize(...size);
  }, originalSize);

  await open(page, "view");
  await popup(page)
    .getByRole("menuitemcheckbox", { name: "Session sidebar" })
    .click();
  await expect(page.locator(".sidebar")).not.toBeVisible();
  await expect
    .poll(async () => (await nativeItem(desktop, "view.sidebar")).checked)
    .toBe(false);
  await nativeClick(desktop, "view.sidebar");
  await expect(page.locator(".sidebar")).toBeVisible();

  await page.locator('[data-menu="help"]').focus();
  await page
    .locator('[data-menu="help"]')
    .dispatchEvent("keydown", { key: "ArrowDown", isComposing: true });
  await expect(popup(page)).not.toBeVisible();
  await page.keyboard.press("ArrowDown");
  await expect(
    popup(page).getByRole("menuitem", { name: "Quick help" }),
  ).toBeFocused();
  await page.keyboard.press("Escape");
  await expect(popup(page)).not.toBeVisible();
  await expect(page.locator('[data-menu="help"]')).toBeFocused();
  await open(page, "help");
  await page.keyboard.press("Enter");
  await expect(page.getByRole("dialog")).toBeVisible();
  await page.keyboard.press("Escape");
  await expect(page.getByRole("dialog")).not.toBeVisible();
  await open(page, "connection");
  await page.screenshot({ path: "test-results/workbench-menu.png" });
  await page.locator(".terminal-toolbar").click();
  await expect(popup(page)).not.toBeVisible();
  assert.equal(
    await page.evaluate(
      async () =>
        (
          await window.channelterm.syncMenu({
            locale: "en",
            arbitraryCommand: "bad",
          })
        ).error.code,
    ),
    "invalid_input",
  );
  await desktop.evaluate(({ Menu }) => {
    const check = (menu) => {
      for (const item of menu.items) {
        if (item.accelerator)
          throw new Error("Menu must not intercept terminal keys");
        if (item.submenu) check(item.submenu);
      }
    };
    check(Menu.getApplicationMenu());
  });
}

export async function checkAttachedMenus(
  desktop,
  page,
  fixture,
  observer,
  observerAttachment,
) {
  await expect
    .poll(async () => (await nativeItem(desktop, "tab.close")).enabled)
    .toBe(true);
  await page.locator(".xterm-helper-textarea").focus();
  for (const key of ["Control+c", "Control+l", "Control+w", "Alt+x", "F1"])
    await page.keyboard.press(key);
  let keys = "";
  await expect
    .poll(async () => {
      keys += Buffer.from(
        (await observer.read(observerAttachment.id)).data,
      ).toString();
      return keys;
    })
    .toContain("\x03\x0c\x17\x1bx");
  assert.match(keys, /\x1b(OP|\[11~)/);
  await expect(page.locator(".xterm-screen")).toBeVisible();
  await expect(popup(page)).not.toBeVisible();
  // Chromium composition events exercise xterm integration; physical OS IME
  // candidate-window behavior still needs a manual platform check.
  const cdp = await page.context().newCDPSession(page);
  try {
    await cdp.send("Input.imeSetComposition", {
      text: "测试",
      selectionStart: 2,
      selectionEnd: 2,
    });
    await cdp.send("Input.insertText", { text: "测试" });
  } finally {
    await cdp.detach();
  }
  let composed = "";
  await expect
    .poll(async () => {
      composed += Buffer.from(
        (await observer.read(observerAttachment.id)).data,
      ).toString();
      return composed;
    })
    .toContain("测试");
  assert.equal(composed.match(/测试/g)?.length, 1);
  await expect(popup(page)).not.toBeVisible();

  await fixture.command(
    "output",
    "\r\nlocal-clear-sentinel\r\nanother-line\r\nprompt>",
  );
  await expect(page.locator(".xterm-rows")).toContainText(
    "local-clear-sentinel",
  );
  await observer.read(observerAttachment.id);
  await page.locator(".terminal-view").click({ button: "right" });
  await nativeClick(desktop, "language.ja");
  await expect(
    popup(page).getByRole("menuitem", {
      name: "ローカル表示をクリア",
      exact: true,
    }),
  ).toBeVisible();
  await nativeClick(desktop, "language.en");
  await popup(page)
    .getByRole("menuitem", { name: "Clear local display" })
    .click();
  await expect(page.locator(".xterm-rows")).not.toContainText(
    "local-clear-sentinel",
  );
  // No clear/reset bytes were echoed to the other client, and retained Host
  // history remains available to a fresh observer after the local clear.
  assert.equal(
    Buffer.from((await observer.read(observerAttachment.id)).data).toString(),
    "",
  );
  const retained = new HostClient(fixture.endpoint, fixture.token);
  try {
    await retained.connect();
    const handle = await retained.attach(observerAttachment.sessionId);
    assert.match(
      Buffer.from((await retained.read(handle.id)).data).toString(),
      /local-clear-sentinel/,
    );
  } finally {
    await retained.disconnect();
  }
}

export async function checkContextTargets(desktop, page, observer) {
  // A fake second row exercises target binding against a real Host that rejects
  // that missing ID. It must not accidentally detach/re-attach the current row.
  const realRows = await observer.list();
  await desktop.evaluate(({ ipcMain }, rows) => {
    globalThis.menuTestRows = [
      ...rows,
      {
        ...rows[0],
        session_id: "missing-context-target",
        session_ref: "SER-2",
        label: "Other context target",
      },
    ];
    ipcMain.removeHandler("host:list");
    ipcMain.handle("host:list", () => ({
      ok: true,
      value: globalThis.menuTestRows,
    }));
  }, realRows);
  await page.getByRole("button", { name: "Refresh", exact: true }).click();
  await page.getByRole("button", { name: /SER-2/ }).click({ button: "right" });
  await expect(
    popup(page).getByRole("menuitem", {
      name: "Close tab (detach)",
      exact: true,
    }),
  ).toBeDisabled();
  await popup(page)
    .getByRole("menuitem", { name: "Attach Session", exact: true })
    .click();
  await expect(page.getByRole("alert")).toContainText(
    "Session is no longer open",
  );
  await expect(page.getByRole("button", { name: /SER-1/ })).toHaveAttribute(
    "aria-pressed",
    "true",
  );
  await expect(page.locator(".xterm-screen")).toBeVisible();
  await page.getByRole("button", { name: "Resume input", exact: true }).click();
  await page.getByRole("button", { name: /SER-2/ }).click({ button: "right" });
  await desktop.evaluate(() => {
    globalThis.menuTestRows = globalThis.menuTestRows.slice(0, 1);
  });
  await expect(
    popup(page).getByRole("menuitem", { name: "Attach Session", exact: true }),
  ).toBeDisabled();
  await page.keyboard.press("Escape");
  // A later fresh process restores production list IPC. Remaining checks in
  // this process only need the real-row snapshot, not live lease changes.
  await expect(page.locator(".session")).toHaveCount(1);
}

export async function checkMenuAvailability(desktop, connected) {
  for (const id of ["host.disconnect", "session.refresh"])
    await expect
      .poll(async () => (await nativeItem(desktop, id)).enabled)
      .toBe(connected);
  for (const id of ["tab.close", "terminal.clear"])
    assert.equal((await nativeItem(desktop, id)).enabled, false);
}

export async function checkClosedMenus(desktop, page) {
  await expect
    .poll(async () => (await nativeItem(desktop, "tab.close")).enabled)
    .toBe(true);
  await page
    .locator(".session")
    .first()
    .locator("..")
    .click({ button: "right" });
  await expect(
    popup(page).getByRole("menuitem", { name: "Attach Session", exact: true }),
  ).toBeDisabled();
  await expect(
    popup(page).getByRole("menuitem", {
      name: "Close tab (detach)",
      exact: true,
    }),
  ).toBeEnabled();
  await page.keyboard.press("Escape");
}

export async function exitFromMenu(desktop, page) {
  await open(page, "connection");
  const exited = desktop.waitForEvent("close");
  await popup(page)
    .getByRole("menuitem", { name: "Exit GUI", exact: true })
    .click();
  await exited;
}

export async function exitFromNativeMenu(desktop) {
  const exited = desktop.waitForEvent("close");
  await nativeClick(desktop, "app.quit");
  await exited;
}
