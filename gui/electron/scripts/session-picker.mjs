import { expect } from "@playwright/test";

// Exercise the same Session menu and picker used to attach to a Host-owned terminal.
export async function openPicker(page) {
  const picker = page.locator(".session-picker");
  if (!(await picker.isVisible())) {
    await page.locator('[data-menu="session"]').click();
    await page.locator('[role="menu"] [data-action="session.choose"]').click();
  }
  if (!(await picker.evaluate((el) => el.open)))
    await picker.locator("summary").first().click();
  return picker;
}
export async function chooseSession(page, text, id) {
  const picker = await openPicker(page);
  const row = id
    ? picker.locator('[data-session-id="' + id + '"]')
    : picker.locator("button.session").filter({ hasText: text });
  await row.click();
  await expect(picker).toHaveCount(0);
}
