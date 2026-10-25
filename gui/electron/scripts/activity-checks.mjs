import assert from "node:assert/strict";
import { expect } from "@playwright/test";

/** Existing Host observations cross the production IPC without raw input or cursor interference. */
export async function checkActivity(
  desktop,
  page,
  fixture,
  observer,
  attachment,
) {
  const invalid = await page.evaluate(() =>
    window.channelterm.activity("expired"),
  );
  assert.equal(invalid.ok, false);
  assert.equal(invalid.error.code, "cancelled");
  await observer.read(attachment.id);
  await fixture.command("agent-input", "activity-private-input");
  await desktop.evaluate(({ Menu }) =>
    Menu.getApplicationMenu().getMenuItemById("view.activity").click(),
  );
  const panel = page.getByRole("tabpanel", { name: "Session activity" });
  await expect(panel).toBeVisible();
  await expect(panel).toContainText("not verified client identities");
  await expect(
    panel.getByRole("row").filter({ hasText: "Agent" }).last(),
  ).toContainText("22 B");
  await expect(panel).toContainText("Initial history");
  await expect(panel).not.toContainText("activity-private-input");
  assert.match(
    Buffer.from((await observer.read(attachment.id)).data).toString(),
    /activity-private-input/,
  );
  await fixture.command("lease");
  await expect(panel).toContainText("LEASE_ACQUIRED");
  await expect(panel).toContainText("Observed later");
  await fixture.command("release");
  await expect(panel).toContainText("LEASE_RELEASED");
  await fixture.command("debug-report", "Client claims a stopped target.");
  await expect(panel).toContainText(
    "Client debug report (not a complete audit)",
  );
  await expect(panel).toContainText("Client claims a stopped target.");
  await desktop.evaluate(({ BrowserWindow }) =>
    BrowserWindow.getAllWindows()[0].setSize(760, 500),
  );
  await expect
    .poll(() =>
      page.evaluate(() => document.documentElement.scrollWidth <= innerWidth),
    )
    .toBe(true);
  await page.screenshot({ path: "test-results/session-activity.png" });
  await page.getByRole("tab", { name: "Session activity" }).focus();
  await page.keyboard.press("Home");
  await expect(page.getByRole("tab", { name: "File transfers" })).toBeFocused();
  await expect(page.locator(".session-activity")).toHaveCount(0);
  await page.keyboard.press("End");
  await expect(
    page.getByRole("tab", { name: "Session activity" }),
  ).toBeFocused();
  await expect(panel).toContainText("Client claims a stopped target.");
  await page.getByRole("button", { name: "Hide panel" }).click();
  await expect(page.locator(".session-activity")).toHaveCount(0);
  // Leave the original layout for later transfer and keyboard tests.
  await page.locator('[data-menu="view"]').click();
  await page
    .getByRole("menuitem", { name: "File transfers", exact: true })
    .click();
  await page.getByRole("button", { name: "Hide panel" }).click();
  await desktop.evaluate(({ BrowserWindow }) =>
    BrowserWindow.getAllWindows()[0].setSize(1180, 780),
  );
  assert.equal((await observer.list())[0].state, "open");
  // Complete the fixture input line before later transfer tests acquire a lease.
  await observer.write(attachment.id, "\r");
  await observer.read(attachment.id);
}
