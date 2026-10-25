import assert from "node:assert/strict";
import { expect } from "@playwright/test";
import { setupI18n } from "@lingui/core";
import { locales } from "../src/shared/menus.ts";
import { menuCatalogs } from "../src/locales/catalogs.ts";

/** Exercise the metadata-only clipboard action through the trusted Main boundary in every locale. */
export async function checkTargetCopy(desktop, page, fixture, sessionId) {
  const previous = await desktop.evaluate(({ clipboard }) =>
    clipboard.readText(),
  );
  try {
    const stale = await page.evaluate(() =>
      window.channelterm.copyAITarget("expired", "en"),
    );
    assert.equal(stale.ok, false);
    assert.equal(stale.error.code, "cancelled");
    await expect(page.locator(".target-reference")).toHaveText("SER-1");
    await expect(page.locator(".target-id")).toHaveAttribute(
      "title",
      sessionId,
    );
    for (const locale of locales) {
      const i18n = setupI18n({ locale, messages: menuCatalogs });
      await desktop.evaluate(
        ({ Menu }, locale) =>
          Menu.getApplicationMenu()
            .getMenuItemById(`language.${locale}`)
            .click(),
        locale,
      );
      const button = page.getByRole("button", {
        name: i18n._("target.copy"),
        exact: true,
      });
      await expect(button).toBeEnabled();
      await expect(button).toHaveAttribute("title", i18n._("target.hint"));
      await button.click();
      await expect(page.locator(".target-feedback")).toHaveText(
        i18n._("target.copied"),
      );
      const copied = await desktop.evaluate(({ clipboard }) =>
        clipboard.readText(),
      );
      assert.equal(
        copied,
        i18n._("target.instructions", {
          name: "Isolated echo",
          host: fixture.endpoint,
          sessionId,
          reference: "SER-1",
          transport: "serial",
          endpoint: "in-memory",
        }),
      );
      assert.ok(!copied.includes(fixture.token));
      assert.ok(!copied.includes("Host ready"));
      await desktop.evaluate(({ BrowserWindow }) =>
        BrowserWindow.getAllWindows()[0].setSize(760, 500),
      );
      await expect(button).toBeInViewport();
      await expect(page.locator(".target-reference")).toBeVisible();
      await expect
        .poll(() =>
          page
            .locator(".statusbar")
            .evaluate((element) => element.scrollWidth <= element.clientWidth),
        )
        .toBe(true);
    }
    await page.screenshot({ path: "test-results/copy-ai-target-compact.png" });
  } finally {
    await desktop.evaluate(
      ({ clipboard }, text) => clipboard.writeText(text),
      previous,
    );
    await desktop.evaluate(({ Menu, BrowserWindow }) => {
      Menu.getApplicationMenu().getMenuItemById("language.en").click();
      BrowserWindow.getAllWindows()[0].setSize(1180, 780);
    });
  }
}
