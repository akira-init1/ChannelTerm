import assert from "node:assert/strict";
import { expect } from "@playwright/test";

/** Search uses native xterm buffers/decorations, never a copied Host history or device write. */
export async function checkTerminalSearch(
  desktop,
  page,
  fixture,
  observer,
  attachment,
) {
  const terminal = page.locator(".xterm-helper-textarea");
  const search = page.getByRole("search", {
    name: "Find in terminal",
    exact: true,
  });
  const query = search.getByRole("searchbox", { name: "Find retained text" });
  const status = search.getByRole("status");
  const markers = page.locator(".xterm-find-result-decoration");
  // Addon 0.16 styles active decorations but does not assign its active CSS class.
  // Assert the visible distinguishing border instead of relying on that class.
  const active = markers.locator(
    'xpath=self::*[contains(@style, "rgb(255, 230, 128)")]',
  );
  const read = async () =>
    Buffer.from((await observer.read(attachment.id)).data).toString();
  await read();
  await fixture.command(
    "output",
    "\x1bcNeedle needle NEEDLE\r\n中文针 中文针\r\n",
  );
  await read();
  await expect(page.locator(".xterm-rows")).toContainText(
    "Needle needle NEEDLE",
  );
  await terminal.focus();
  await page.keyboard.press("Control+Shift+f");
  await expect(query).toBeFocused();
  await query.fill("needle");
  await expect(status).toHaveText(/^[123]\/3$/);
  await expect.poll(() => markers.count()).toBe(4);
  await expect(active).toHaveCount(1);
  const first = await status.innerText();
  await query.press("Enter");
  await expect(status).not.toHaveText(first);
  await query.press("Shift+Enter");
  await expect(status).toHaveText(first);
  await search.getByRole("checkbox", { name: "Match case" }).check();
  await expect(status).toHaveText("1/1");
  await query.fill("中文针");
  await expect(status).toHaveText(/^[12]\/2$/);
  const chinese = await status.innerText();
  await query.evaluate((element) => {
    element.dispatchEvent(
      new KeyboardEvent("keydown", {
        key: "Enter",
        code: "Enter",
        isComposing: true,
        bubbles: true,
        cancelable: true,
      }),
    );
    element.dispatchEvent(
      new KeyboardEvent("keydown", {
        key: "Escape",
        code: "Escape",
        isComposing: true,
        bubbles: true,
        cancelable: true,
      }),
    );
  });
  await expect(search).toBeVisible();
  await expect(status).toHaveText(chinese);
  await query.fill("no-such-term");
  await expect(status).toHaveText("No matches");
  await expect(markers).toHaveCount(0);
  await query.fill("needle");
  await search.getByRole("checkbox", { name: "Match case" }).uncheck();
  await expect(status).toHaveText(/^[123]\/3$/);

  // Color suppression must not hide temporary match decorations.
  await page.locator('[data-menu="settings"]').click();
  await page
    .getByRole("menuitem", { name: "Preferences...", exact: true })
    .click();
  const display = page.getByRole("dialog", { name: "Display", exact: true });
  await display
    .getByLabel("Terminal style", { exact: true })
    .selectOption("monochrome");
  await display.getByRole("button", { name: "Apply", exact: true }).click();
  await expect(active).toBeVisible();
  // xterm draws decoration backgrounds into text cells, which monochrome masks.
  // Its overlay outlines remain distinct and visible in either display style.
  await expect(active).toHaveCSS("outline-color", "rgb(255, 230, 128)");
  await expect(
    markers
      .locator('xpath=self::*[contains(@style, "rgb(181, 155, 53)")]')
      .first(),
  ).toHaveCSS("outline-color", "rgb(181, 155, 53)");
  await desktop.evaluate(({ BrowserWindow }) =>
    BrowserWindow.getAllWindows()[0].setSize(760, 500),
  );
  await expect.poll(() => page.evaluate(() => window.outerWidth)).toBe(760);
  await expect(query).toBeInViewport();
  await expect(
    search.getByRole("button", { name: "Close search (Escape)" }),
  ).toBeInViewport();
  await page.screenshot({ path: "test-results/terminal-search-compact.png" });
  await search
    .getByRole("button", { name: "Select all retained text" })
    .click();
  await expect(terminal).toBeFocused();
  const previousClipboard = await desktop.evaluate(({ clipboard }) =>
    clipboard.readText(),
  );
  try {
    await page.keyboard.press("Control+Shift+c");
    await expect
      .poll(() => desktop.evaluate(({ clipboard }) => clipboard.readText()))
      .toContain("Needle needle NEEDLE");
  } finally {
    await desktop.evaluate(
      ({ clipboard }, text) => clipboard.writeText(text),
      previousClipboard,
    );
  }
  await page.keyboard.press("Control+Shift+f");
  await expect(query).toBeFocused();
  await query.press("Escape");
  await expect(search).toHaveCount(0);
  await expect(markers).toHaveCount(0);
  assert.equal(
    await read(),
    "",
    "search, selection and shortcuts must not send query text to the device",
  );

  await page.getByRole("searchbox", { name: "Search Sessions" }).focus();
  await page.keyboard.press("Control+Shift+f");
  await expect(search).toHaveCount(0);
  await terminal.focus();
  await page.keyboard.press("Control+Shift+f");
  await fixture.command("output", "\x1bc");
  // The fake Host intentionally retains only 1 KiB. Feed bounded batches and
  // observe rendering so this tests xterm's cap, not Host overwrite recovery.
  for (let batch = 0; batch < 22; batch++) {
    await fixture.command(
      "output",
      "capword\r\n".repeat(50) + `batch-${batch}\r\n`,
    );
    await expect(page.locator(".xterm-rows")).toContainText(`batch-${batch}`);
    await read();
  }
  await query.fill("capword");
  await expect(status).toContainText("≥1000");
  await expect(status).toContainText("Result limit reached");
  await desktop.evaluate(({ Menu }) =>
    Menu.getApplicationMenu().getMenuItemById("tab.close").click(),
  );
  await expect(search).toHaveCount(0);
  await page.getByRole("button", { name: /SER-1/ }).click();
  await expect(terminal).toBeVisible();
  await expect(search).toHaveCount(0);
  assert.equal(
    await read(),
    "",
    "detach must discard search state without sending input",
  );
  await page.locator('[data-menu="settings"]').click();
  await page
    .getByRole("menuitem", { name: "Preferences...", exact: true })
    .click();
  await display
    .getByLabel("Terminal style", { exact: true })
    .selectOption("semantic");
  await display.getByRole("button", { name: "Apply", exact: true }).click();
  await desktop.evaluate(({ BrowserWindow }) =>
    BrowserWindow.getAllWindows()[0].setSize(1500, 900),
  );
  await fixture.command("output", "\x1bcsearch-restored\r\n");
  await read();
  await terminal.focus();
}
