import assert from "node:assert/strict";
import { mkdtemp, writeFile } from "node:fs/promises";
import { resolve } from "node:path";
import { _electron as electron, expect } from "@playwright/test";
import { startFixture } from "../tests/fixture.ts";
import { HostClient } from "../src/main/host/client.ts";
import { setupI18n } from "@lingui/core";
import { menuCatalogs } from "../src/locales/catalogs.ts";

/** Restart the real process with one disposable profile and a surviving authoritative Host. */
export async function checkWorkspace(environment) {
  const fixture = await startFixture();
  const observer = new HostClient(fixture.endpoint, fixture.token);
  const profile = await mkdtemp(resolve("test-results/workspace-profile-"));
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
  let desktop;
  let page;
  const native = async (id) => {
    await page.bringToFront();
    await expect
      .poll(() =>
        desktop.evaluate(
          ({ Menu }, id) =>
            Menu.getApplicationMenu()?.getMenuItemById(id)?.enabled,
          id,
        ),
      )
      .toBe(true);
    await desktop.evaluate(
      ({ Menu }, id) => Menu.getApplicationMenu().getMenuItemById(id).click(),
      id,
    );
  };
  const launch = async () => {
    desktop = await electron.launch(options);
    page = await desktop.firstWindow();
    await expect(page.locator('[data-menu="settings"]')).toBeVisible();
  };
  try {
    await observer.connect();
    const live = (await observer.list())[0];
    const attached = await observer.attach(live.session_id);
    const closed = await observer.openConnection({
      kind: "serial",
      port: "COM-FIXTURE",
      baud: 115200,
      dataBits: 8,
      parity: "none",
      stopBits: "1",
      flowControl: "none",
      label: "Workspace closed",
    });
    await launch();
    await native("host.connect");
    await page.locator(`[data-session-id="${live.session_id}"]`).click();
    await expect(page.locator(".xterm-screen")).toHaveCount(1);
    await page.locator(`[data-session-id="${closed.sessionId}"]`).click();
    await expect(page.locator(".terminal-tab")).toHaveCount(2);
    await page.locator('.terminal-tab [role="tab"]').first().click();
    await native("settings.display");
    const dialog = page.getByRole("dialog");
    await dialog.locator('[name="encoding"]').selectOption("gbk");
    await dialog.locator('[name="view"]').selectOption("hex");
    await dialog.locator('[name="description"]').fill("New view default");
    await dialog.getByRole("button", { name: "Apply", exact: true }).click();
    await expect(dialog).toHaveCount(0);
    await native("view.auxiliary");
    await native("view.panel");
    const sidebarDivider = page.getByRole("separator", {
      name: "Resize Session sidebar",
    });
    await sidebarDivider.focus();
    await page.keyboard.press("Home");
    await page.keyboard.press("ArrowRight");
    await expect(page.locator(".sidebar")).toHaveCSS("width", "190px");
    const divider = page.getByRole("separator", {
      name: "Resize bottom panel",
    });
    await divider.focus();
    await page.keyboard.press("Home");
    await page.keyboard.press("ArrowUp");
    await native("language.zh-CN");
    await fixture.command("output", "\r\nworkspace-retained\r\n\x1b[6n");
    await observer.read(attached.id);
    // A pending composition/DOM draft is deliberately not delivered to the terminal.
    await page
      .locator(".terminal-pane:not([hidden]) .xterm-helper-textarea")
      .evaluate((node) => {
        node.value = "UNSENT-WORKSPACE-DRAFT";
      });
    await expect
      .poll(() =>
        page.evaluate(
          () =>
            JSON.parse(localStorage.getItem("channelterm.workspace.v1.0"))?.tabs
              .length,
        ),
      )
      .toBe(2);
    const saved = await page.evaluate(() => ({
      workspace: JSON.parse(localStorage.getItem("channelterm.workspace.v1.0")),
      preferences: JSON.parse(
        localStorage.getItem("channelterm.preferences.v1"),
      ),
    }));
    assert.equal(saved.workspace.selected, 0);
    assert.deepEqual(
      saved.workspace.tabs.map((tab) => tab.target.sessionId),
      [live.session_id, closed.sessionId],
    );
    assert.equal(saved.preferences.language, "zh-CN");
    assert.equal(saved.preferences.panelHeight, 110);
    assert.equal(saved.preferences.sidebarWidth, 190);
    assert.equal(saved.preferences.terminal.encoding, "gbk");
    assert.ok(!JSON.stringify(saved).includes("UNSENT-WORKSPACE-DRAFT"));
    assert.ok(!JSON.stringify(saved).includes("workspace-retained"));
    await desktop.close();
    desktop = undefined;
    const closedAttachment = await observer.attach(closed.sessionId);
    await observer.closeSession(closedAttachment.id);
    await observer.read(attached.id);
    await launch();
    const zh = setupI18n({ locale: "zh-CN", messages: menuCatalogs });
    await expect(page.locator('[data-menu="settings"]')).toHaveText(
      zh._("menu.settings"),
    );
    await expect(page.locator(".terminal-tab")).toHaveCount(2);
    await expect(page.locator(".xterm-screen")).toHaveCount(0);
    await expect(page.locator(".auxiliary-sidebar")).toBeVisible();
    await expect(page.locator(".sidebar")).toHaveCSS("width", "190px");
    await expect(page.locator(".bottom-panel")).toHaveCSS("height", "110px");
    await native("host.connect");
    await expect(page.locator(".xterm-screen")).toHaveCount(1);
    await expect(
      page.locator('.terminal-tab [role="tab"]').first(),
    ).toHaveAttribute("aria-selected", "true");
    await expect(page.locator(".notices")).toContainText(
      zh._("workspace.hint"),
    );
    assert.equal((await observer.list()).length, 1);
    assert.equal(
      Buffer.from((await observer.read(attached.id)).data).toString(),
      "",
    );
    await page.locator('.terminal-tab [role="tab"]').nth(1).click();
    await expect(page.locator(".notices")).toContainText(
      zh._("workspace.missing"),
    );
    await native("language.en");
    await native("connection.new");
    await expect(
      page.getByRole("dialog").locator('[name="encoding"]'),
    ).toHaveValue("gbk");
    await expect(page.getByRole("dialog").locator('[name="view"]')).toHaveValue(
      "hex",
    );
    await page.getByRole("button", { name: "Cancel", exact: true }).click();
    await page.evaluate(() => {
      Storage.prototype.setItem = () => {
        throw new Error("fixture storage denied");
      };
    });
    await native("view.auxiliary");
    await expect(page.locator(".notices")).toContainText(
      "Could not save or load local preferences/workspace",
    );
    assert.equal((await observer.list()).length, 1);
    await page.screenshot({ path: "test-results/workspace-restored.png" });
    console.log(
      "PASS: process restart restores language/layout/defaults and ordered tabs, rechecks live/closed Sessions, retains only Host output and never replays drafts or historical terminal queries; storage failure leaves views usable.",
    );
  } finally {
    await desktop?.close();
    await observer.disconnect();
    await fixture.stop();
  }
}
