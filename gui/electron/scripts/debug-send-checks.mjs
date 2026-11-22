import { chooseSession } from "./session-picker.mjs";
import assert from "node:assert/strict";
import { mkdtemp, writeFile, readFile } from "node:fs/promises";
import { resolve, join } from "node:path";
import { _electron as electron, expect } from "@playwright/test";
import { setupI18n } from "@lingui/core";
import { menuCatalogs } from "../src/locales/catalogs.ts";
import { locales } from "../src/shared/menus.ts";
import { startFixture } from "../tests/fixture.ts";
import { HostClient } from "../src/main/host/client.ts";

/** Explicit native confirmation and exact real Host bytes, using only fake device I/O. */
export async function checkDebugSend(environment) {
  const fixture = await startFixture();
  const observer = new HostClient(fixture.endpoint, fixture.token);
  const profile = await mkdtemp(resolve("test-results/debug-send-"));
  const preload = join(profile, "profile.cjs");
  await writeFile(
    preload,
    `const {app}=require('electron');app.setPath('userData',${JSON.stringify(profile)});app.setPath('sessionData',${JSON.stringify(profile)});`,
  );
  const env = {
    ...environment,
    CHANNELTERM_GUI_ENDPOINT: fixture.endpoint,
    CHANNELTERM_HTTP_AUTH_TOKEN: fixture.token,
  };
  delete env.ELECTRON_RUN_AS_NODE;
  let desktop;
  async function action(page, group, id) {
    await page.locator(`[data-menu="${group}"]`).click();
    await page.locator(`[role=menu] [data-action="${id}"]`).click();
  }
  try {
    await observer.connect();
    const id = (await observer.list())[0].session_id;
    const watched = await observer.attach(id);
    desktop = await electron.launch({
      args: ["-r", preload, "--lang=en-US", "."],
      env,
    });
    let page = await desktop.firstWindow();
    await page
      .getByRole("button", { name: "Connect to Host...", exact: true })
      .click();
    await chooseSession(page, "Isolated echo");
    await expect(page.locator(".xterm-screen")).toBeVisible();
    await observer.read(watched.id);
    await desktop.evaluate(({ dialog }) => {
      globalThis.debugConfirm = false;
      globalThis.debugPreviews = [];
      dialog.showMessageBox = async (_window, options) => {
        globalThis.debugPreviews.push(options.detail);
        return { response: globalThis.debugConfirm ? 1 : 0 };
      };
    });
    assert.equal(
      (
        await page.evaluate(
          (id) =>
            window.channelterm.debugSend(id, {
              mode: "hex",
              data: "ff",
              ending: "none",
            }),
          watched.id,
        )
      ).ok,
      false,
    );
    await action(page, "terminal", "send.open");
    let modal = page.locator(".debug-send-dialog");
    await expect(modal).toBeVisible();
    await modal.locator('[name="debug-format"]').selectOption("hex");
    await modal.locator('[name="debug-content"]').fill("00 ff 80 7f");
    await modal.locator('[name="debug-ending"]').selectOption("crlf");
    await modal
      .getByRole("button", { name: "Preview and send", exact: true })
      .click();
    await expect(modal).toContainText("Cancelled before sending.");
    assert.equal((await observer.read(watched.id)).data.length, 0);
    const previews = await desktop.evaluate(() => globalThis.debugPreviews);
    assert.ok(previews[0].includes(id));
    assert.ok(previews[0].includes("00 ff 80 7f 0d 0a"));
    await desktop.evaluate(() => {
      globalThis.debugConfirm = true;
    });
    await modal
      .getByRole("button", { name: "Preview and send", exact: true })
      .click();
    await expect(modal).toContainText("Host accepted bytes: 6 B");
    assert.deepEqual(
      (await observer.read(watched.id)).data,
      [0, 255, 128, 127, 13, 10],
    );
    await modal.locator('[name="debug-content"]').fill("0x00");
    await modal
      .getByRole("button", { name: "Preview and send", exact: true })
      .click();
    await expect(modal.getByRole("alert")).toBeVisible();
    assert.equal(
      (await desktop.evaluate(() => globalThis.debugPreviews)).length,
      2,
    );
    await modal.locator('[name="debug-content"]').fill("00 ff");
    await modal
      .getByLabel("Snippet name", { exact: true })
      .fill("fixture snippet");
    await expect(
      modal.getByRole("button", { name: "Save or replace snippet" }),
    ).toBeDisabled();
    await modal.getByRole("checkbox").check();
    await modal
      .getByRole("button", { name: "Save or replace snippet" })
      .click();
    await expect(modal).toContainText("Snippet saved. Nothing was sent.");
    assert.equal(
      JSON.parse(
        await readFile(join(profile, "command-snippets.json"), "utf8"),
      )[0].data,
      "00 ff",
    );
    assert.equal((await observer.read(watched.id)).data.length, 0);
    await modal.getByRole("button", { name: "Close", exact: true }).click();
    // A preview bound to a detached view must never use another attachment.
    const extra = (
      await page.evaluate((id) => window.channelterm.attach(id), id)
    ).value;
    await desktop.evaluate(({ dialog }) => {
      globalThis.debugPending = false;
      dialog.showMessageBox = () =>
        new Promise((resolve) => {
          globalThis.resolveDebug = resolve;
          globalThis.debugPending = true;
        });
    });
    const pending = page.evaluate(
      (id) =>
        window.channelterm.debugSend(id, {
          mode: "hex",
          data: "42",
          ending: "none",
        }),
      extra.id,
    );
    await expect
      .poll(() => desktop.evaluate(() => globalThis.debugPending))
      .toBe(true);
    assert.equal(
      (await page.evaluate((id) => window.channelterm.detach(id), extra.id)).ok,
      true,
    );
    await desktop.evaluate(() => globalThis.resolveDebug({ response: 1 }));
    assert.equal((await pending).ok, false);
    assert.equal((await observer.read(watched.id)).data.length, 0);
    for (const locale of locales) {
      await page.bringToFront();
      const window = await desktop.browserWindow(page);
      await expect.poll(() => window.evaluate((w) => w.isFocused())).toBe(true);
      await desktop.evaluate(
        ({ Menu }, locale) =>
          Menu.getApplicationMenu()
            .getMenuItemById(`language.${locale}`)
            .click(),
        locale,
      );
      const i18n = setupI18n({ locale, messages: menuCatalogs });
      await expect(page.locator('[data-menu="terminal"]')).toHaveText(
        i18n._("menu.terminal"),
      );
      await action(page, "terminal", "send.open");
      await expect(modal.locator("h2")).toHaveText(i18n._("send.open"));
      await expect(modal).toContainText(i18n._("send.plaintext"));
      if (locale === "zh-CN") {
        const window = await desktop.browserWindow(page);
        await window.evaluate((window) => window.setSize(760, 500));
        await expect(modal.locator("footer button")).toBeInViewport({
          ratio: 1,
        });
        assert.equal(
          await modal.evaluate((node) => node.scrollWidth <= node.clientWidth),
          true,
        );
        await page.screenshot({ path: "test-results/debug-send-compact.png" });
      }
      await modal
        .getByRole("button", { name: i18n._("diagnostics.close"), exact: true })
        .click();
    }
    await desktop.close();
    desktop = undefined;
    assert.ok((await observer.list()).some((row) => row.session_id === id));
    desktop = await electron.launch({
      args: ["-r", preload, "--lang=en-US", "."],
      env,
    });
    page = await desktop.firstWindow();
    await expect
      .poll(
        async () =>
          (await page.evaluate(() => window.channelterm.snippets())).value
            ?.length,
      )
      .toBe(1);
    const stored = (await page.evaluate(() => window.channelterm.snippets()))
      .value;
    assert.equal(stored[0].name, "fixture snippet");
    assert.equal((await observer.read(watched.id)).data.length, 0);
    assert.equal(
      (
        await page.evaluate(() =>
          window.channelterm.changeSnippet({
            action: "remove",
            name: "fixture snippet",
          }),
        )
      ).ok,
      true,
    );
    assert.deepEqual(
      (await page.evaluate(() => window.channelterm.snippets())).value,
      [],
    );
    console.log(
      "PASS debug send: exact binary/line-ending bytes, native confirmation/cancel, malformed input, detached target, explicit plaintext snippets, reload without execution, nine locales, compact layout and shared Session survival",
    );
  } finally {
    await desktop?.close().catch(() => {});
    await observer.disconnect();
    await fixture.stop();
  }
}
