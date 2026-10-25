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
  const button = page.locator(`[data-menu="${id}"]`);
  await button.hover();
  if ((await button.getAttribute("aria-expanded")) !== "true")
    await button.click();
}

/** Exercise the real renderer bridge and native menu, without registering global keys. */
export async function checkOfflineMenus(desktop, page) {
  await expect
    .poll(async () => (await nativeItem(desktop, "language.system")).checked)
    .toBe(true);
  // Simulate a system language notification; an explicit preference must resist later changes.
  await page.evaluate(() => {
    Object.defineProperty(navigator, "languages", {
      configurable: true,
      value: ["ja-JP"],
    });
    window.dispatchEvent(new Event("languagechange"));
  });
  await expect(page.locator('[data-menu="window"]')).toHaveText("ウィンドウ");
  await nativeClick(desktop, "language.en");
  await page.evaluate(() => {
    Object.defineProperty(navigator, "languages", {
      configurable: true,
      value: ["zh-CN"],
    });
    window.dispatchEvent(new Event("languagechange"));
  });
  await expect(page.locator('[data-menu="window"]')).toHaveText("Window");
  await nativeClick(desktop, "language.system");
  await expect(page.locator('[data-menu="window"]')).toHaveText("窗口");
  await page.evaluate(() => {
    delete navigator.languages;
    window.dispatchEvent(new Event("languagechange"));
  });
  await expect(page.locator(".app-menu > button")).toHaveText([
    "Session",
    "Host",
    "Terminal",
    "View",
    "Window",
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
  await open(page, "window");
  for (const id of ["window.new", "window.close", "tab.new"]) {
    assert.equal((await nativeItem(desktop, id)).enabled, true);
    await expect(popup(page).locator(`[data-action="${id}"]`)).toBeEnabled();
  }
  for (const id of [
    "tab.next",
    "tab.previous",
    "window.next",
    "window.previous",
  ]) {
    assert.equal((await nativeItem(desktop, id)).enabled, false);
    await expect(popup(page).locator(`[data-action="${id}"]`)).toBeDisabled();
  }
  await page.keyboard.press("Escape");
  await page.locator('[data-menu="settings"]').hover();
  await expect(popup(page)).not.toBeVisible();
  await open(page, "settings");
  for (const id of [
    "host",
    "session",
    "terminal",
    "view",
    "help",
    "settings",
  ]) {
    await page.locator(`[data-menu="${id}"]`).hover();
    await expect(page.locator(`[data-menu="${id}"]`)).toHaveAttribute(
      "aria-expanded",
      "true",
    );
    await expect(popup(page)).toBeVisible();
  }
  await page.locator(".statusbar").hover();
  await expect(popup(page)).toBeVisible();
  await page.locator('[data-menu="settings"]').click();
  await expect(popup(page)).not.toBeVisible();
  await page.locator('[data-menu="host"]').hover();
  await expect(popup(page)).not.toBeVisible();
  await open(page, "host");
  await page.locator(".statusbar").click();
  await page.locator('[data-menu="settings"]').hover();
  await expect(popup(page)).not.toBeVisible();
  await open(page, "settings");
  await page.keyboard.press("Escape");
  await page.locator('[data-menu="host"]').hover();
  await expect(popup(page)).not.toBeVisible();
  await open(page, "terminal");
  await page.keyboard.press("Escape");
  await expect(popup(page)).not.toBeVisible();
  await open(page, "settings");
  await popup(page)
    .getByRole("menuitem", { name: "Language", exact: true })
    .hover();
  const flyout = popup(page).getByRole("menu", {
    name: "Language",
    exact: true,
  });
  await expect(flyout).toBeVisible();
  await popup(page)
    .getByRole("menuitem", { name: "Language", exact: true })
    .focus();
  await page.keyboard.press("ArrowRight");
  await expect(
    flyout.getByRole("menuitemradio", { name: "Follow System", exact: true }),
  ).toBeFocused();
  await page.keyboard.press("End");
  await expect(
    flyout.getByRole("menuitemradio", { name: "繁體中文", exact: true }),
  ).toBeFocused();
  await page.keyboard.press("ArrowLeft");
  await expect(flyout).not.toBeVisible();
  await expect(
    popup(page).getByRole("menuitem", { name: "Language", exact: true }),
  ).toBeFocused();
  await page.keyboard.press("ArrowRight");
  await expect(flyout).toBeVisible();
  const parentBounds = await popup(page).boundingBox();
  const childBounds = await flyout.boundingBox();
  assert.ok(
    childBounds.x >= parentBounds.x + parentBounds.width - 2,
    "hover expands right",
  );
  await page.screenshot({ path: "test-results/workbench-hover-menu.png" });
  await popup(page).getByRole("menuitemradio", { name: "简体中文" }).click();
  await expect(page.locator(".app-menu > button")).toHaveText([
    "会话",
    "Host",
    "终端",
    "视图",
    "窗口",
    "设置",
    "帮助",
  ]);
  await expect
    .poll(async () => (await nativeItem(desktop, "host.disconnect")).label)
    .toBe("断开与 Host 的连接");
  assert.equal((await nativeItem(desktop, "language.zh-CN")).checked, true);
  assert.equal((await nativeItem(desktop, "language.zh-CN")).type, "radio");
  await nativeClick(desktop, "language.ja");
  await expect(page.locator(".app-menu > button")).toHaveText([
    "セッション",
    "ホスト",
    "ターミナル",
    "表示",
    "ウィンドウ",
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
    .poll(async () => (await nativeItem(desktop, "host")).label)
    .toBe("Host");

  // Switch through every supported catalog via the renderer. The native menu
  // must acknowledge the same label and exactly one checked language each time.
  const languages = [
    {
      locale: "de",
      name: "Deutsch",
      connection: "Host",
      language: "Sprache",
    },
    {
      locale: "es",
      name: "Español",
      connection: "Host",
      language: "Idioma",
    },
    {
      locale: "fr",
      name: "Français",
      connection: "Hôte",
      language: "Langue",
    },
    { locale: "ja", name: "日本語", connection: "ホスト", language: "言語" },
    { locale: "ko", name: "한국어", connection: "호스트", language: "언어" },
    {
      locale: "ru",
      name: "Русский",
      connection: "Хост",
      language: "Язык",
    },
    { locale: "zh-CN", name: "简体中文", connection: "Host", language: "语言" },
    { locale: "zh-TW", name: "繁體中文", connection: "Host", language: "語言" },
    {
      locale: "en",
      name: "English",
      connection: "Host",
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
    await expect(popup(page).getByRole("menuitemradio")).toHaveCount(10);
    const bounds = await popup(page)
      .getByRole("menu", { name: languageLabel, exact: true })
      .boundingBox();
    const viewport = await page.evaluate(() => ({
      width: innerWidth,
      height: innerHeight,
    }));
    assert.ok(
      bounds.x >= 0 &&
        bounds.y >= 0 &&
        bounds.x + bounds.width <= viewport.width &&
        bounds.y + bounds.height <= viewport.height,
      "flyout stays inside the minimum window",
    );
    await popup(page)
      .getByRole("menuitemradio", { name: language.name, exact: true })
      .click();
    await expect(page.locator('[data-menu="host"]')).toHaveText(
      language.connection,
    );
    await expect
      .poll(async () => (await nativeItem(desktop, "host")).label)
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
  await open(page, "host");
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
  await nativeClick(desktop, "view.sidebar");
  await expect(page.locator(".sidebar")).not.toBeVisible();
  await open(page, "session");
  await popup(page)
    .getByRole("menuitem", { name: "Attach Session...", exact: true })
    .click();
  await expect(
    page.getByRole("searchbox", { name: "Search Sessions" }),
  ).toBeFocused();
  await nativeClick(desktop, "session.info");
  await expect(page.locator(".session-details")).toContainText(
    observerAttachment.sessionId,
  );
  await nativeClick(desktop, "view.auxiliary");
  await open(page, "session");
  await popup(page)
    .getByRole("menuitem", { name: "File Transfer", exact: true })
    .hover();
  await expect(
    popup(page).getByRole("menu", { name: "File Transfer", exact: true }),
  ).toBeVisible();
  await expect(
    popup(page).getByRole("menuitem", { name: "Send to device", exact: true }),
  ).toBeEnabled();
  await page.keyboard.press("Escape");
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
  await page.locator('[data-menu="settings"]').hover();
  await expect(page.locator('[data-menu="settings"]')).toHaveAttribute(
    "aria-expanded",
    "false",
  );
  await expect(
    popup(page).getByRole("menuitem", { name: "Clear local display" }),
  ).toBeVisible();
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
  await open(page, "window");
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
