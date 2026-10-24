import assert from "node:assert/strict";
import { expect } from "@playwright/test";

/** Exercise real pointer capture and xterm fitting without changing Host state. */
export async function checkCompactLayout(desktop, page) {
  const side = page.locator(".sidebar");
  const divider = page.getByRole("separator", {
    name: "Resize Session sidebar",
  });
  const width = async () => (await side.boundingBox()).width;
  const selected = await page.locator(".session.selected").textContent();
  await expect(
    page.locator(".terminal-input-hint, .terminal-target, .session-meta"),
  ).toHaveCount(0);
  await expect(page.locator(".terminal-toolbar")).toHaveCSS("height", "32px");
  await expect(page.locator(".statusbar")).toHaveCSS("height", "24px");
  assert.ok(
    (await page.locator(".session.selected").boundingBox()).height <= 48,
  );
  await expect.poll(width).toBe(260);
  const bounds = await divider.boundingBox();
  await page.mouse.move(bounds.x + bounds.width / 2, bounds.y + 60);
  await page.mouse.down();
  await page.mouse.move(bounds.x + bounds.width / 2 + 100, bounds.y + 60, {
    steps: 8,
  });
  await page.mouse.up();
  await expect.poll(width).toBe(360);
  await divider.focus();
  await page.keyboard.press("Home");
  await expect.poll(width).toBe(180);
  await page.keyboard.press("ArrowRight");
  await expect.poll(width).toBe(190);
  await divider.evaluate((element) =>
    element.dispatchEvent(
      new KeyboardEvent("keydown", {
        key: "ArrowRight",
        isComposing: true,
        bubbles: true,
      }),
    ),
  );
  await expect.poll(width).toBe(190);
  await page.keyboard.press("End");
  await expect.poll(width).toBe(480);
  await desktop.evaluate(({ BrowserWindow }) =>
    BrowserWindow.getAllWindows()[0].setSize(760, 500),
  );
  // Native window borders reduce the renderer viewport on Windows.
  await expect
    .poll(
      async () =>
        (await width()) === (await page.evaluate(() => innerWidth - 400)),
    )
    .toBe(true);
  await expect
    .poll(() =>
      page.evaluate(() => {
        const main = document.querySelector("main").getBoundingClientRect();
        const container = document
          .querySelector(".terminal-view")
          .getBoundingClientRect();
        const screen = document
          .querySelector(".xterm-screen")
          .getBoundingClientRect();
        return (
          main.width >= 360 &&
          screen.right <= container.right &&
          screen.bottom <= container.bottom &&
          document.documentElement.scrollWidth === innerWidth
        );
      }),
    )
    .toBe(true);
  await divider.dblclick();
  await expect.poll(width).toBe(210);
  await page.screenshot({ path: "test-results/workbench-compact-resized.png" });
  await desktop.evaluate(({ BrowserWindow }) =>
    BrowserWindow.getAllWindows()[0].setSize(1180, 780),
  );
  await expect.poll(width).toBe(210);
  await divider.dblclick();
  await expect.poll(width).toBe(260);
  // Capture is released on cancellation; a later pointer move must not resize.
  const reset = await divider.boundingBox();
  await page.mouse.move(reset.x + 3, reset.y + 60);
  await page.mouse.down();
  await divider.evaluate((element) =>
    element.dispatchEvent(new PointerEvent("pointercancel", { bubbles: true })),
  );
  await page.mouse.move(reset.x + 100, reset.y + 60);
  await page.mouse.up();
  await expect.poll(width).toBe(260);
  assert.equal(await page.locator(".session.selected").textContent(), selected);
  await expect(page.locator(".xterm-screen")).toBeVisible();
  await page.screenshot({ path: "test-results/workbench-compact-layout.png" });
}
