import assert from "node:assert/strict";
import { mkdtemp, writeFile } from "node:fs/promises";
import { resolve } from "node:path";
import { _electron as electron, expect } from "@playwright/test";
import { startFixture } from "../tests/fixture.ts";
import { HostClient } from "../src/main/host/client.ts";
import { setupI18n } from "@lingui/core";
import { menuCatalogs } from "../src/locales/catalogs.ts";
import { locales } from "../src/shared/menus.ts";

async function language(desktop, page, locale) {
  await page.bringToFront();
  await desktop.evaluate(
    ({ Menu }, locale) =>
      Menu.getApplicationMenu().getMenuItemById(`language.${locale}`).click(),
    locale,
  );
  const i18n = setupI18n({ locale, messages: menuCatalogs });
  await expect(page.locator('[data-menu="window"]')).toHaveText(
    i18n._("menu.window"),
  );
  return i18n;
}

const activeTerminal = (page) =>
  page.locator(".terminal-pane:not([hidden]) .xterm-helper-textarea");
async function menu(page, id) {
  await page.bringToFront();
  const opener = page.locator('[data-menu="window"]');
  if ((await opener.getAttribute("aria-expanded")) !== "true")
    await opener.click();
  try {
    await page.locator(`[role="menu"] [data-action="${id}"]`).click();
  } catch (error) {
    // A successful close can dispose the page before Playwright's click reply.
    if (id !== "window.close" || !page.isClosed()) throw error;
  }
}
async function connect(page) {
  const button = page.getByRole("button", {
    name: "Connect to Host",
    exact: true,
  });
  await button.click();
  await expect(page.locator(".host-panel")).toContainText("Host connected");
}
async function select(page, label) {
  await page.locator(".session").filter({ hasText: label }).click();
  await expect(page.locator(".terminal-tab.active")).toContainText(label);
  await expect(activeTerminal(page)).toHaveCount(1);
}
async function attachment(desktop, page) {
  await page.bringToFront();
  await page.evaluate(() => {
    globalThis.capturedMenuAction = undefined;
    const stop = window.channelterm.onMenuAction((action) => {
      if (action.id === "terminal.focus") {
        globalThis.capturedMenuAction = action;
        stop();
      }
    });
  });
  await expect
    .poll(() =>
      desktop.evaluate(
        ({ Menu }) =>
          Menu.getApplicationMenu().getMenuItemById("terminal.focus").enabled,
      ),
    )
    .toBe(true);
  await desktop.evaluate(({ Menu }) =>
    Menu.getApplicationMenu().getMenuItemById("terminal.focus").click(),
  );
  await page.waitForFunction(() => globalThis.capturedMenuAction);
  return page.evaluate(() => globalThis.capturedMenuAction.attachment);
}

/** Native windows and independent client handles, using the real Host and disposable I/O. */
export async function checkWindows(environment) {
  const fixture = await startFixture();
  const observer = new HostClient(fixture.endpoint, fixture.token);
  const profile = await mkdtemp(resolve("test-results/window-profile-"));
  const preload = resolve(profile, "profile.cjs");
  await writeFile(
    preload,
    `const { app } = require("electron"); app.setPath("userData", ${JSON.stringify(profile)}); app.setPath("sessionData", ${JSON.stringify(profile)});`,
  );
  const options = {
    args: ["-r", preload, "--lang=en-US", "."],
    env: {
      ...environment,
      CHANNELTERM_GUI_ENDPOINT: fixture.endpoint,
      CHANNELTERM_HTTP_AUTH_TOKEN: fixture.token,
    },
  };
  delete options.env.ELECTRON_RUN_AS_NODE;
  let desktop;
  try {
    await observer.connect();
    const firstId = (await observer.list())[0].session_id;
    const opened = await observer.openConnection({
      kind: "serial",
      port: "COM-FIXTURE",
      baud: 115200,
      dataBits: 8,
      parity: "none",
      stopBits: "1",
      flowControl: "none",
      label: "Second endpoint",
    });
    const firstObserver = await observer.attach(firstId);
    const secondObserver = await observer.attach(opened.sessionId);
    desktop = await electron.launch(options);
    const source = await desktop.firstWindow();
    const errors = [];
    source.on("pageerror", (error) => errors.push(error.message));
    await connect(source);
    await select(source, "Isolated echo");
    const firstAttachment = await attachment(desktop, source);
    await menu(source, "tab.new");
    await expect(source.locator('.terminal-tabs [role="tab"]')).toHaveCount(2);
    await expect(source.locator(".terminal-tab.active")).toContainText(
      "New Tab",
    );
    await select(source, "Second endpoint");
    await expect(source.locator(".terminal-pane[hidden]")).toHaveCount(1);
    await menu(source, "tab.previous");
    await expect(source.locator(".terminal-tab.active")).toContainText(
      "Isolated echo",
    );
    await menu(source, "tab.next");
    await expect(source.locator(".terminal-tab.active")).toContainText(
      "Second endpoint",
    );

    await language(desktop, source, "ja");
    const nextWindow = desktop.waitForEvent("window");
    await menu(source, "window.new");
    const second = await nextWindow;
    const japanese = setupI18n({ locale: "ja", messages: menuCatalogs });
    await expect(second.locator('[data-menu="window"]')).toHaveText(
      japanese._("menu.window"),
    );
    await language(desktop, source, "en");
    await expect(second.locator('[data-menu="window"]')).toHaveText(
      japanese._("menu.window"),
    );
    await language(desktop, second, "en");
    second.on("pageerror", (error) => errors.push(error.message));
    await expect(second.locator(".host-panel")).toContainText("Host connected");
    await select(second, "Isolated echo");
    const secondAttachment = await attachment(desktop, second);
    assert.equal(secondAttachment.sessionId, firstAttachment.sessionId);
    assert.notEqual(secondAttachment.id, firstAttachment.id);
    for (const operation of ["read", "write", "detach", "readClipboard"]) {
      const result = await source.evaluate(
        async ({ operation, id }) => {
          if (operation === "write")
            return window.channelterm.write(id, "must-not-cross-windows");
          return window.channelterm[operation](id);
        },
        { operation, id: secondAttachment.id },
      );
      assert.equal(
        result.ok,
        false,
        `${operation} rejects another window's attachment`,
      );
    }
    assert.equal(
      (await source.evaluate(() => window.channelterm.focusWindow("arbitrary")))
        .ok,
      false,
    );
    assert.equal(
      (
        await source.evaluate(() =>
          window.channelterm.acceptMovedTab("foreign", "foreign"),
        )
      ).ok,
      false,
    );
    const sourceWindow = await desktop.browserWindow(source);
    const secondWindow = await desktop.browserWindow(second);
    await menu(second, "window.previous");
    await expect
      .poll(() => sourceWindow.evaluate((window) => window.isFocused()))
      .toBe(true);
    await menu(source, "window.next");
    await expect
      .poll(() => secondWindow.evaluate((window) => window.isFocused()))
      .toBe(true);

    await Promise.all([
      observer.read(firstObserver.id),
      observer.read(secondObserver.id),
    ]);
    await activeTerminal(source).focus();
    await source.keyboard.type("only-endpoint-two");
    await activeTerminal(second).focus();
    await second.keyboard.type("only-endpoint-one");
    assert.equal(
      Buffer.from((await observer.read(secondObserver.id)).data).toString(),
      "only-endpoint-two",
    );
    assert.equal(
      Buffer.from((await observer.read(firstObserver.id)).data).toString(),
      "only-endpoint-one",
    );
    await expect(second.locator(".xterm-rows:visible")).toContainText(
      "only-endpoint-one",
    );
    await fixture.command("output", "\r\nmove-replay-sentinel\r\n");
    const identities = (await observer.list())
      .map((session) => session.session_id)
      .sort();
    // Move the background tab, not the active Second endpoint tab.
    await source.bringToFront();
    await source
      .locator(".terminal-tab")
      .filter({ hasText: "Isolated echo" })
      .click({ button: "right" });
    const movedWindow = desktop.waitForEvent("window");
    await source
      .getByRole("menuitem", { name: "Move Tab to New Window", exact: true })
      .click();
    const moved = await movedWindow;
    moved.on("pageerror", (error) => errors.push(error.message));
    await expect(moved.locator(".terminal-tab.active")).toContainText(
      "Isolated echo",
      { timeout: 25000 },
    );
    await expect(moved.locator(".notices")).toContainText("up to 64 KiB");
    await expect(moved.locator(".xterm-rows:visible")).toContainText(
      "move-replay-sentinel",
    );
    await expect(source.locator('.terminal-tabs [role="tab"]')).toHaveCount(1);
    await expect(source.locator(".terminal-tab.active")).toContainText(
      "Second endpoint",
    );
    assert.deepEqual(
      (await observer.list()).map((session) => session.session_id).sort(),
      identities,
    );
    assert.equal(
      (
        await source.evaluate(
          (id) => window.channelterm.write(id, "stale-moved-input"),
          firstAttachment.id,
        )
      ).ok,
      false,
    );
    await moved.screenshot({ path: "test-results/window-moved-tab.png" });
    // Labels and restore notices come from the same catalogs in all nine languages.
    for (const locale of locales) {
      const translated = await language(desktop, moved, locale);
      await expect(moved.locator(".notices")).toContainText(
        translated._("tab.restoreHint"),
      );
      await moved.locator('[data-menu="window"]').click();
      for (const id of ["window.new", "tab.new", "window.close", "tab.move"])
        await expect(
          moved.locator(`[role="menu"] [data-action="${id}"]`),
        ).toHaveText(translated._(id));
      await moved.keyboard.press("Escape");
      await moved.locator(".terminal-tab").click({ button: "right" });
      await expect(
        moved.getByRole("menuitem", {
          name: translated._("tab.move"),
          exact: true,
        }),
      ).toBeVisible();
      await moved.keyboard.press("Escape");
      await expect(second.locator('[data-menu="window"]')).toHaveText("Window");
    }
    await language(desktop, moved, "en");
    await fixture.command("lease");
    await expect(moved.locator(".notices")).toContainText("Lease busy");
    await expect(second.locator(".notices")).toContainText("Lease busy");
    await fixture.command("release");
    await expect(moved.locator(".notices")).not.toContainText("Lease busy");

    // A hidden cursor may be overwritten; it reports loss when selected instead of inventing history.
    await menu(second, "tab.new");
    await select(second, "Second endpoint");
    await fixture.command("flood");
    await menu(second, "tab.previous");
    await expect(second.locator(".notices")).toContainText(
      "Output overwritten",
    );
    await expect(second.locator(".xterm-rows:visible")).toContainText(
      "retained-after-overwrite",
    );
    await menu(second, "tab.new");
    await select(second, "Isolated echo");
    await expect(second.locator('.terminal-tabs [role="tab"]')).toHaveCount(3);
    // Two tabs for the same Session have distinct handles and independent closing.
    const duplicate = await attachment(desktop, second);
    assert.notEqual(duplicate.id, secondAttachment.id);
    await menu(second, "tab.close");
    await expect(second.locator('.terminal-tabs [role="tab"]')).toHaveCount(2);
    await menu(second, "tab.previous");
    await expect(second.locator(".terminal-tab.active")).toContainText(
      "Isolated echo",
    );

    const sourceClosed = source.waitForEvent("close");
    await menu(source, "window.close");
    await sourceClosed;
    await expect(second.locator(".xterm-screen:visible")).toHaveCount(1);
    await expect(moved.locator(".xterm-screen:visible")).toHaveCount(1);
    await menu(moved, "tab.close");
    await expect(moved.locator('.terminal-tabs [role="tab"]')).toHaveCount(0);
    assert.ok(
      (await observer.list()).every((session) => session.state === "open"),
    );
    // Closing a tab in moved leaves that native window active; DOM focus alone is insufficient on Windows.
    await second.bringToFront();
    await expect
      .poll(() => secondWindow.evaluate((window) => window.isFocused()))
      .toBe(true);
    await expect(second.locator(".input-status")).toHaveText(
      "Attached · Input enabled",
    );
    await activeTerminal(second).focus();
    await expect(activeTerminal(second)).toBeFocused();
    await second.keyboard.type("surviving-window-input");
    let output = "";
    await expect
      .poll(async () => {
        output += Buffer.from(
          (await observer.read(firstObserver.id)).data,
        ).toString();
        return output;
      })
      .toContain("surviving-window-input");
    assert.deepEqual(errors, []);
    await desktop.close();
    desktop = undefined;
    assert.ok(
      (await observer.list()).every((session) => session.state === "open"),
    );

    // Fail before the destination acknowledges: only that provisional window is destroyed.
    desktop = await electron.launch(options);
    const original = await desktop.firstWindow();
    await connect(original);
    await select(original, "Isolated echo");
    const originalAttachment = await attachment(desktop, original);
    await desktop.evaluate(({ ipcMain, BrowserWindow }) => {
      ipcMain.removeHandler("window:accept-tab");
      ipcMain.handle("window:accept-tab", (event) => {
        BrowserWindow.fromWebContents(event.sender).destroy();
        return {
          ok: false,
          error: { code: "cancelled", message: "Test destination failure" },
        };
      });
    });
    await menu(original, "tab.move");
    await expect(original.locator(".notices")).toContainText(
      "The original tab was retained",
      { timeout: 25000 },
    );
    await expect
      .poll(() =>
        desktop.evaluate(
          ({ BrowserWindow }) => BrowserWindow.getAllWindows().length,
        ),
      )
      .toBe(1);
    assert.deepEqual(await attachment(desktop, original), originalAttachment);
    await activeTerminal(original).focus();
    await original.keyboard.type("after-failed-move");
    output = "";
    await expect
      .poll(async () => {
        output += Buffer.from(
          (await observer.read(firstObserver.id)).data,
        ).toString();
        return output;
      })
      .toContain("after-failed-move");
    assert.deepEqual(
      (await observer.list()).map((session) => session.session_id).sort(),
      identities,
    );
    // A source closed before commitment must tear down its provisional destination only.
    const spareWindow = desktop.waitForEvent("window");
    await menu(original, "window.new");
    const spare = await spareWindow;
    await expect(spare.locator(".host-panel")).toContainText("Host connected");
    await desktop.evaluate(({ ipcMain }) => {
      ipcMain.removeHandler("window:accept-tab");
      ipcMain.handle("window:accept-tab", () => ({
        ok: false,
        error: { code: "cancelled", message: "Test defers handoff commitment" },
      }));
    });
    const provisionalWindow = desktop.waitForEvent("window");
    await menu(original, "tab.move");
    const provisional = await provisionalWindow;
    await expect(provisional.locator(".xterm-screen")).toHaveCount(1);
    const originalClosed = original.waitForEvent("close");
    const provisionalClosed = provisional.waitForEvent("close");
    await (
      await desktop.browserWindow(original)
    ).evaluate((window) => window.close());
    await Promise.all([originalClosed, provisionalClosed]);
    await expect
      .poll(() =>
        desktop.evaluate(
          ({ BrowserWindow }) => BrowserWindow.getAllWindows().length,
        ),
      )
      .toBe(1);
    await select(spare, "Isolated echo");
    await expect(spare.locator(".xterm-screen")).toBeVisible();
    assert.ok(
      (await observer.list()).every((session) => session.state === "open"),
    );
    console.log(
      "PASS: multiple windows/tabs, independent cursors/input, hidden-tab overwrite, cross-window IPC rejection, background-tab move, destination-failure rollback, source-close cleanup, nine locales and shared Session survival.",
    );
  } finally {
    await desktop?.close();
    await observer.disconnect();
    await fixture.stop();
  }
}
