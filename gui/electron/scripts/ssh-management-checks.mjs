import { expect } from "@playwright/test";
import assert from "node:assert/strict";
import { dirname, join } from "node:path";
import { setupI18n } from "@lingui/core";
import { menuCatalogs } from "../src/locales/catalogs.ts";
import { locales } from "../src/shared/menus.ts";

/** Mutations are confined to the disposable in-memory SFTP fixture. */
export async function checkSSHManagement(desktop, page, fixture) {
  await desktop.evaluate(({ Menu, dialog }) => {
    globalThis.managementPreviousConfirm = dialog.showMessageBox;
    globalThis.managementConfirm = false;
    globalThis.managementPreviews = [];
    dialog.showMessageBox = async (_window, options) => {
      globalThis.managementPreviews.push(options);
      return { response: globalThis.managementConfirm ? 1 : 0 };
    };
    Menu.getApplicationMenu().getMenuItemById("file.receive").click();
  });
  try {
    assert.equal(
      (
        await page.evaluate(() =>
          window.channelterm.manageSSH({ operation: "exec" }),
        )
      ).ok,
      false,
    );
    const form = page.getByRole("dialog", { name: "Receive from device" });
    await form
      .locator('[name="identityFiles"]')
      .fill(join(dirname(fixture.knownHosts), "identity"));
    await form.locator('[name="knownHostsFile"]').fill(fixture.knownHosts);
    await form.locator('[name="password"]').fill("fixture-password");
    await form
      .getByRole("button", { name: "Browse SFTP", exact: true })
      .click();
    const browser = form.getByRole("region", { name: "Browse SFTP" });
    const status = browser.getByRole("status");
    const path = browser.locator("input").first();
    const list = async (target = "/") => {
      await path.fill(target);
      await browser
        .getByRole("button", { name: "List directory", exact: true })
        .click();
      await expect(
        browser.getByRole("button", { name: "List directory", exact: true }),
      ).toBeEnabled();
    };
    const row = (name) =>
      browser
        .getByRole("listitem")
        .filter({ has: page.getByText(name, { exact: true }) });
    const review = () =>
      browser
        .getByRole("button", { name: "Review change", exact: true })
        .click();
    const mkdir = async (target) => {
      await browser
        .getByRole("button", { name: "Create directory", exact: true })
        .click();
      await browser.locator('[name="sshManagePath"]').fill(target);
      const before = await desktop.evaluate(
        () => globalThis.managementPreviews.length,
      );
      await browser.locator('[name="sshManagePath"]').press("Enter");
      await expect(browser.locator('[name="sshManagePath"]')).toBeVisible();
      assert.equal(
        await desktop.evaluate(() => globalThis.managementPreviews.length),
        before,
      );
      await review();
    };
    await mkdir("/gui-management");
    await expect(status).toContainText("No SFTP mutation was started");
    await list();
    await expect(row("gui-management")).toHaveCount(0);
    await desktop.evaluate(() => {
      globalThis.managementConfirm = true;
    });
    await mkdir("/gui-management");
    await expect(status).toContainText("Server confirmed");
    await mkdir("/gui-management/child");
    await expect(status).toContainText("Server confirmed");
    await list();
    await row("gui-management")
      .getByRole("button", { name: "Remove empty directory", exact: true })
      .click();
    await review();
    await expect(status).toContainText("Final state is unknown");
    await list();
    await expect(row("gui-management")).toBeVisible();
    await row("gui-management")
      .getByRole("button", { name: "Rename entry", exact: true })
      .click();
    await browser.locator('[name="sshManageTarget"]').fill("/gui-renamed");
    await review();
    await expect(status).toContainText("Server confirmed");
    await list("/gui-renamed");
    await row("child")
      .getByRole("button", { name: "Remove empty directory", exact: true })
      .click();
    await review();
    await expect(status).toContainText("Server confirmed");
    await list();
    await row("gui-renamed")
      .getByRole("button", { name: "Remove empty directory", exact: true })
      .click();
    await review();
    await expect(status).toContainText("Server confirmed");
    // The preceding transfer test has already verified this fixture-only payload.
    await list();
    await row("gui-payload.bin")
      .getByRole("button", { name: "Remove file or symlink", exact: true })
      .click();
    await review();
    await expect(status).toContainText("Server confirmed");
    await list();
    await expect(row("gui-payload.bin")).toHaveCount(0);
    const previousSize = await desktop.evaluate(({ BrowserWindow }) => {
      globalThis.managementConfirm = false;
      const window = BrowserWindow.getAllWindows()[0];
      const size = window.getSize();
      window.setSize(760, 500);
      return size;
    });
    for (const locale of locales) {
      const i18n = setupI18n({ locale, messages: menuCatalogs });
      await desktop.evaluate(
        ({ Menu }, locale) =>
          Menu.getApplicationMenu()
            .getMenuItemById(`language.${locale}`)
            .click(),
        locale,
      );
      const localized = page.locator(".ssh-browser");
      await localized
        .getByRole("button", { name: i18n._("sshManage.mkdir"), exact: true })
        .click();
      await localized
        .locator('[name="sshManagePath"]')
        .fill("/locale-not-created");
      await localized
        .getByRole("button", { name: i18n._("sshManage.review"), exact: true })
        .click();
      await expect(localized.getByRole("status")).toContainText(
        i18n._("sshManage.not_started"),
      );
      const preview = await desktop.evaluate(() =>
        globalThis.managementPreviews.at(-1),
      );
      assert.equal(preview.message, i18n._("sshManage.confirm"));
      assert.ok(preview.detail.includes(i18n._("sshManage.boundary")));
      assert.equal(
        await page.evaluate(
          () => document.documentElement.scrollWidth <= innerWidth,
        ),
        true,
      );
    }
    await page.screenshot({ path: "test-results/ssh-management-compact.png" });
    await desktop.evaluate(({ Menu, BrowserWindow }, size) => {
      Menu.getApplicationMenu().getMenuItemById("language.en").click();
      BrowserWindow.getAllWindows()[0].setSize(...size);
    }, previousSize);
    const previews = await desktop.evaluate(
      () => globalThis.managementPreviews,
    );
    assert.ok(previews.length >= 7);
    assert.ok(
      previews.every(
        (p) =>
          p.defaultId === 0 &&
          p.cancelId === 0 &&
          !p.detail.includes("fixture-password"),
      ),
    );
    assert.ok(
      previews.some((p) =>
        p.detail.includes("/gui-management\n→ /gui-renamed"),
      ),
    );
    await page.screenshot({ path: "test-results/ssh-management.png" });
    await form.getByRole("button", { name: "Cancel", exact: true }).click();
    await expect(form).toHaveCount(0);
    await expect(
      page.locator(".terminal-pane:not([hidden]) .xterm-screen"),
    ).toBeVisible();
  } finally {
    await desktop.evaluate(({ dialog }) => {
      dialog.showMessageBox = globalThis.managementPreviousConfirm;
    });
  }
}
