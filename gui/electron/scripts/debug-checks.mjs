import assert from "node:assert/strict";
import { expect } from "@playwright/test";

/** Exercise native navigation using only registered structured fixture evidence. */
export async function checkDebugOverview(desktop, page, fixture) {
  const before = await page.locator(".terminal-pane").count();
  await fixture.command("debug-overview");
  await page
    .getByRole("button", { name: "Debug", exact: true })
    .first()
    .click();
  const sidebar = page.getByRole("region", { name: "Debug", exact: true });
  await expect(sidebar).toContainText("Network fixture");
  await sidebar.getByRole("button", { name: /Network fixture/ }).click();
  await expect(sidebar).toContainText("Client report; not Host verification");
  await expect(sidebar).toContainText("Stopped");
  await expect(sidebar).toContainText("127.0.0.1:43210");
  await expect(sidebar).toContainText("192.0.2.10:2222");
  await expect(sidebar).toContainText("electron-fixture");
  await desktop.evaluate(({ Menu }) =>
    Menu.getApplicationMenu().getMenuItemById("view.sidebar").click(),
  );
  await expect(sidebar).not.toBeVisible();
  await desktop.evaluate(({ Menu }) =>
    Menu.getApplicationMenu().getMenuItemById("view.sidebar").click(),
  );
  await expect(sidebar).toBeVisible();
  assert.equal(await page.locator(".terminal-pane").count(), before);
  await page.screenshot({ path: "test-results/debug-overview.png" });
  await desktop.evaluate(({ Menu }) =>
    Menu.getApplicationMenu().getMenuItemById("language.zh-CN").click(),
  );
  const translated = page.getByRole("region", { name: "调试", exact: true });
  await expect(translated).toContainText("客户端报告，未经 Host 验证");
  await expect(translated).toContainText("本机转发端点");
  await desktop.evaluate(({ BrowserWindow }) =>
    BrowserWindow.getAllWindows()[0].setSize(760, 500),
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
  await page.screenshot({ path: "test-results/debug-overview-compact.png" });
  await desktop.evaluate(({ BrowserWindow }) =>
    BrowserWindow.getAllWindows()[0].setSize(1180, 780),
  );
  await desktop.evaluate(({ Menu }) =>
    Menu.getApplicationMenu().getMenuItemById("language.en").click(),
  );
  await expect(sidebar).toContainText("Client report; not Host verification");
  await sidebar
    .getByRole("button", { name: "Locate Session", exact: true })
    .click();
  await expect(
    page.getByRole("navigation", { name: "Sessions", exact: true }),
  ).toBeVisible();
  assert.equal(await page.locator(".terminal-pane").count(), before);
}
