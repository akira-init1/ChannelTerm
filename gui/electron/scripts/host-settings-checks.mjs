import assert from "node:assert/strict";
import { mkdtemp, writeFile, readFile } from "node:fs/promises";
import { resolve } from "node:path";
import { createServer } from "node:net";
import { once } from "node:events";
import { _electron as electron, expect } from "@playwright/test";
import { setupI18n } from "@lingui/core";
import { menuCatalogs } from "../src/locales/catalogs.ts";
import { locales } from "../src/shared/menus.ts";
import { HostClient } from "../src/main/host/client.ts";
import { startFixture } from "../tests/fixture.ts";

/** Real Main/Preload/window switching with isolated storage and fake device I/O. */
export async function checkHostSettings(environment) {
  const fixture = await startFixture();
  const observer = new HostClient(fixture.endpoint, fixture.token);
  const profile = await mkdtemp(resolve("test-results/host-settings-"));
  const preload = resolve(profile, "profile.cjs");
  await writeFile(
    preload,
    `const { app } = require("electron"); app.setPath("userData", ${JSON.stringify(profile)}); app.setPath("sessionData", ${JSON.stringify(profile)});`,
  );
  const saved = resolve(profile, "host-settings.json");
  await writeFile(
    saved,
    JSON.stringify({ version: 1, endpoint: fixture.endpoint }),
  );
  const env = { ...environment, CHANNELTERM_HTTP_AUTH_TOKEN: fixture.token };
  delete env.CHANNELTERM_GUI_ENDPOINT;
  delete env.ELECTRON_RUN_AS_NODE;
  const options = { args: ["-r", preload, "--lang=en-US", "."], env };
  let desktop;
  async function action(page, group, id) {
    await page.bringToFront();
    await page.locator(`[data-menu="${group}"]`).click();
    await page.locator(`[role="menu"] [data-action="${id}"]`).click();
  }
  async function settings(page) {
    await action(page, "host", "host.settings");
    await expect(page.locator('[name="host-endpoint"]')).toHaveValue(
      (await page.evaluate(() => window.channelterm.info())).value.endpoint,
    );
  }
  async function save(page, endpoint) {
    await settings(page);
    await page.locator('[name="host-endpoint"]').fill(endpoint);
    await page
      .getByRole("button", {
        name: "Save and disconnect this window",
        exact: true,
      })
      .click();
    await expect(page.locator(".host-settings-dialog")).toHaveCount(0);
  }
  async function connect(page, success = true) {
    await action(page, "host", "host.connect");
    await expect
      .poll(
        async () =>
          (await page.evaluate(() => window.channelterm.hostSettings())).value
            .connected,
      )
      .toBe(success);
    if (success)
      await expect(page.locator(".host-panel")).toContainText("Host connected");
    else await expect(page.getByRole("alert").first()).toBeVisible();
  }
  try {
    await observer.connect();
    const id = (await observer.list())[0].session_id;
    const observed = await observer.attach(id);
    desktop = await electron.launch(options);
    let page = await desktop.firstWindow();
    await connect(page);
    await page.locator(".session").filter({ hasText: "Isolated echo" }).click();
    // The UI attachment must finish before creating this separate stale-handle probe.
    // Main deliberately rejects overlapping attachment changes; do not race or retry them.
    await expect(page.locator(".xterm-screen")).toBeVisible();
    const extraAttachment = await page.evaluate(
      (id) => window.channelterm.attach(id),
      id,
    );
    assert.equal(extraAttachment.ok, true, JSON.stringify(extraAttachment));
    const stale = extraAttachment.value.id;
    const nextWindow = desktop.waitForEvent("window");
    await action(page, "window", "window.new");
    const second = await nextWindow;
    await expect(second.locator(".host-panel")).toContainText("Host connected");
    await settings(page);
    const info = (await page.evaluate(() => window.channelterm.hostSettings()))
      .value;
    assert.ok(info.host.hostVersion);
    assert.ok(info.electronVersion);
    assert.equal(info.environmentOverride, false);
    assert.equal(JSON.stringify(info).includes(fixture.token), false);
    await page
      .locator('[name="host-endpoint"]')
      .fill("http://user:secret@192.0.2.1/mcp");
    await page
      .getByRole("button", {
        name: "Save and disconnect this window",
        exact: true,
      })
      .click();
    await expect(
      page.locator(".host-settings-dialog [role=alert]"),
    ).toBeVisible();
    assert.equal(
      (await page.evaluate(() => window.channelterm.hostSettings())).value
        .connected,
      true,
    );
    assert.equal(
      JSON.parse(await readFile(saved, "utf8")).endpoint,
      fixture.endpoint,
    );
    await page.getByRole("button", { name: "Close", exact: true }).click();
    for (const value of [
      null,
      {},
      "https://127.0.0.1/mcp",
      "http://localhost/mcp",
    ]) {
      assert.equal(
        (
          await page.evaluate(
            (value) => window.channelterm.saveHostEndpoint(value),
            value,
          )
        ).ok,
        false,
      );
    }
    const socket = createServer();
    socket.listen(0, "127.0.0.1");
    await once(socket, "listening");
    const unavailable = `http://127.0.0.1:${socket.address().port}/mcp`;
    await new Promise((done) => socket.close(done));
    await save(page, unavailable);
    assert.equal(
      (
        await page.evaluate(
          (id) => window.channelterm.write(id, "stale-input-must-not-send"),
          stale,
        )
      ).ok,
      false,
    );
    assert.equal(
      (await second.evaluate(() => window.channelterm.info())).value.endpoint,
      fixture.endpoint,
    );
    assert.equal(
      (await second.evaluate(() => window.channelterm.list())).ok,
      true,
    );
    await observer.write(observed.id, "observer-survives-switch");
    await connect(page, false);
    await save(page, fixture.endpoint);
    await connect(page);
    for (const locale of locales) {
      await page.bringToFront();
      await desktop.evaluate(
        ({ Menu }, locale) =>
          Menu.getApplicationMenu()
            .getMenuItemById(`language.${locale}`)
            .click(),
        locale,
      );
      const i18n = setupI18n({ locale, messages: menuCatalogs });
      await settings(page);
      await expect(page.locator("#host-settings-title")).toHaveText(
        i18n._("host.settings"),
      );
      await expect(page.locator(".host-settings-dialog")).toContainText(
        i18n._("host.settings.versions"),
      );
      if (locale === "zh-CN") {
        const window = await desktop.browserWindow(page);
        await window.evaluate((window) => window.setSize(760, 500));
        await expect(
          page.locator(".host-settings-dialog button[type=submit]"),
        ).toBeInViewport({ ratio: 1 });
        await page.screenshot({
          path: "test-results/host-settings-compact.png",
        });
      }
      await page
        .getByRole("button", { name: i18n._("diagnostics.close"), exact: true })
        .click();
    }
    await desktop.close();
    desktop = undefined;
    desktop = await electron.launch(options);
    page = await desktop.firstWindow();
    await expect
      .poll(
        async () =>
          (await page.evaluate(() => window.channelterm.info())).value.endpoint,
      )
      .toBe(fixture.endpoint);
    assert.equal(
      (await page.evaluate(() => window.channelterm.hostSettings())).value
        .connected,
      false,
    );
    await desktop.close();
    desktop = undefined;
    // Override is immutable in both UI and IPC; a rejected credential never starts a replacement.
    desktop = await electron.launch({
      ...options,
      env: {
        ...env,
        CHANNELTERM_GUI_ENDPOINT: fixture.endpoint,
        CHANNELTERM_HTTP_AUTH_TOKEN: "intentionally-invalid-fixture-token",
      },
    });
    page = await desktop.firstWindow();
    await expect
      .poll(
        async () =>
          (await page.evaluate(() => window.channelterm.hostSettings())).value
            .environmentOverride,
      )
      .toBe(true);
    assert.equal(
      (
        await page.evaluate(
          (endpoint) => window.channelterm.saveHostEndpoint(endpoint),
          unavailable,
        )
      ).ok,
      false,
    );
    const rejected = await page.evaluate(() => window.channelterm.connect());
    assert.equal(rejected.ok, false);
    assert.equal(rejected.error.code, "unauthorized");
    assert.ok(
      (await observer.list()).some((session) => session.session_id === id),
    );
    console.log(
      "PASS Host settings: nine locales, compact native window, Main versions, invalid/unreachable endpoints, persistence/override, authentication rejection, stale input and independent window/observer survival",
    );
  } finally {
    await desktop?.close();
    await observer.disconnect();
    await fixture.stop();
  }
}
