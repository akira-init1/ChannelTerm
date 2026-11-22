import assert from "node:assert/strict";
import { expect } from "@playwright/test";
import { setupI18n } from "@lingui/core";
import { menuCatalogs } from "../src/locales/catalogs.ts";
import { locales } from "../src/shared/menus.ts";

/** Switch actual native and renderer catalogs with a retained loss notice. */
export async function checkWorkbenchLocales(desktop, page, dropped = false) {
  const previousSize = await desktop.evaluate(({ BrowserWindow }) => {
    const window = BrowserWindow.getAllWindows()[0];
    const size = window.getSize();
    if (!window.isDestroyed()) window.setSize(760, 500);
    return size;
  });
  await expect.poll(() => page.evaluate(() => window.outerWidth)).toBe(760);
  // Resize before capturing output: reflow itself is not a localization change.
  await page.waitForTimeout(150);

  for (const locale of locales) {
    const i18n = setupI18n({ locale, messages: menuCatalogs });
    const t = (id) => i18n._(id);
    await desktop.evaluate(({ Menu }, locale) => {
      Menu.getApplicationMenu().getMenuItemById(`language.${locale}`).click();
    }, locale);
    await expect(page.locator(".host-panel .connection")).toHaveText(
      t(dropped ? "workbench.connected" : "workbench.disconnected"),
    );
    await expect(page.locator(".session-search:visible input")).toHaveAttribute(
      "aria-label",
      t("workbench.search"),
    );
    await expect(page.locator(".statusbar")).toHaveAttribute(
      "aria-label",
      t("workbench.status"),
    );
    await expect
      .poll(() =>
        page.evaluate(
          () =>
            document.documentElement.scrollWidth <= innerWidth &&
            document.documentElement.scrollHeight <= innerHeight,
        ),
      )
      .toBe(true);
    if (dropped) {
      await expect(
        page.locator('.session.selected[aria-pressed="true"]'),
      ).toBeVisible();
      await expect(page.locator(".notices")).toContainText(
        t("workbench.dropped"),
      );
      // Translated notices change terminal height and reflow visible rows. The
      // retained raw sentinel stays unchanged; visible flood-prefix length may vary.
      assert.match(
        (await page.locator(".xterm-rows").allTextContents()).join(""),
        /^x*retained-after-overwrite\s*$/,
      );
    } else {
      await expect(page.locator(".terminal-empty h3")).toHaveText(
        t("workbench.noHost"),
      );
      await expect(page.locator(".input-status")).toHaveText(
        t("workbench.unattached"),
      );
    }
  }
  await desktop.evaluate(
    ({ BrowserWindow }, size) =>
      BrowserWindow.getAllWindows()[0].setSize(...size),
    previousSize,
  );
  await desktop.evaluate(({ Menu }) =>
    Menu.getApplicationMenu().getMenuItemById("language.en").click(),
  );
  await expect(page.locator(".host-panel .connection")).toHaveText(
    dropped ? "Host connected" : "Host disconnected",
  );
}
