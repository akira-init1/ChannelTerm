import assert from "node:assert/strict";
import { expect } from "@playwright/test";

/** Layout shortcuts must consume their own keys without writing to the shared Session. */
export async function checkWorkbenchPanels(
  desktop,
  page,
  observer,
  attachment,
) {
  const left = page.locator(".sidebar");
  const right = page.locator(".auxiliary-sidebar");
  const bottom = page.locator(".bottom-panel");
  const terminal = page.locator(".xterm-helper-textarea");
  const modifier = process.platform === "darwin" ? "Meta" : "Control";
  await observer.read(attachment.id);
  await terminal.focus();
  await page.keyboard.press(`${modifier}+b`);
  await expect(left).toBeHidden();
  await page.keyboard.press(`${modifier}+b`);
  await expect(left).toBeVisible();
  await page.keyboard.press(`${modifier}+Alt+b`);
  await expect(right).toBeVisible();
  await expect(right.locator("dd")).toContainText([
    /./,
    /./,
    /./,
    "open",
    attachment.sessionId,
  ]);
  await page.keyboard.press(`${modifier}+j`);
  await expect(bottom).toBeVisible();
  await expect(
    bottom.getByRole("tab", { name: "File transfers" }),
  ).toHaveAttribute("aria-selected", "true");
  await bottom.getByRole("tab", { name: "Session status" }).click();
  await expect(bottom.getByRole("tabpanel")).toContainText("Host connected");
  await page.keyboard.press("ArrowLeft");
  await expect(
    bottom.getByRole("tab", { name: "File transfers" }),
  ).toBeFocused();
  await expect(bottom.getByRole("tabpanel")).toContainText(
    "No file transfers yet",
  );
  const divider = page.getByRole("separator", {
    name: "Resize secondary sidebar",
  });
  const before = await right.boundingBox();
  const handle = await divider.boundingBox();
  await page.mouse.move(handle.x + 3, handle.y + 70);
  await page.mouse.down();
  await expect(divider).toHaveClass(/dragging/);
  await page.mouse.move(handle.x - 57, handle.y + 70, { steps: 6 });
  await expect
    .poll(async () => (await right.boundingBox()).width)
    .toBe(before.width + 60);
  await page.mouse.up();
  const horizontal = page.getByRole("separator", {
    name: "Resize bottom panel",
  });
  await horizontal.focus();
  await page.keyboard.press("Home");
  await expect(bottom).toHaveCSS("height", "100px");
  await page.keyboard.press("ArrowUp");
  await expect(bottom).toHaveCSS("height", "110px");
  await horizontal.dblclick();
  await desktop.evaluate(({ BrowserWindow }) =>
    BrowserWindow.getAllWindows()[0].setSize(760, 500),
  );
  await expect
    .poll(() =>
      page.evaluate(() => {
        const surface = document
          .querySelector(".terminal-view")
          .getBoundingClientRect();
        const screen = document
          .querySelector(".xterm-screen")
          .getBoundingClientRect();
        return (
          document.querySelector("main").getBoundingClientRect().width >= 320 &&
          surface.width >= 304 &&
          screen.right <= surface.right &&
          screen.bottom <= surface.bottom &&
          document.documentElement.scrollWidth === innerWidth
        );
      }),
    )
    .toBe(true);
  await page.screenshot({ path: "test-results/workbench-panels-compact.png" });
  await desktop.evaluate(({ BrowserWindow }) =>
    BrowserWindow.getAllWindows()[0].setSize(1180, 780),
  );
  await terminal.focus();
  await page.mouse.move(20, 15);
  await page.screenshot({ path: "test-results/workbench-panels.png" });
  await terminal.focus();
  await terminal.dispatchEvent("keydown", {
    code: "KeyB",
    key: "b",
    ctrlKey: true,
    isComposing: true,
  });
  await expect(left).toBeVisible();
  assert.equal(
    Buffer.from((await observer.read(attachment.id)).data).toString(),
    "",
  );

  await page.locator('[data-menu="settings"]').click();
  await page.getByRole("menuitem", { name: "Keyboard shortcuts" }).click();
  const dialog = page.getByRole("dialog", { name: "Keyboard shortcuts" });
  const field = dialog.getByRole("textbox", {
    name: "Bottom panel",
    exact: true,
  });
  await field.focus();
  await page.keyboard.press(`${modifier}+b`);
  await expect(dialog.getByRole("alert")).toContainText("unique");
  await expect(
    dialog.getByRole("button", { name: "Apply", exact: true }),
  ).toBeDisabled();
  await page.keyboard.press(`${modifier}+c`);
  await expect(dialog.getByRole("alert")).toContainText("reserved");
  await page.keyboard.press(`${modifier}+Shift+j`);
  await page.screenshot({ path: "test-results/workbench-shortcuts.png" });
  await page.evaluate(() => {
    const original = Storage.prototype.setItem;
    Storage.prototype.setItem = function (key, value) {
      if (key === "channelterm.layout-shortcuts.v1")
        throw new Error("Storage unavailable");
      original.call(this, key, value);
    };
    globalThis.restoreShortcutStorage = () => {
      Storage.prototype.setItem = original;
    };
  });
  try {
    await dialog.getByRole("button", { name: "Apply", exact: true }).click();
    await expect(dialog.getByRole("alert")).toContainText(
      "Could not save shortcuts",
    );
    await expect(dialog).toBeVisible();
  } finally {
    await page.evaluate(() => globalThis.restoreShortcutStorage());
  }
  await dialog.getByRole("button", { name: "Apply", exact: true }).click();
  await expect(dialog).toBeHidden();
  assert.equal(
    await page.evaluate(
      () =>
        JSON.parse(localStorage.getItem("channelterm.layout-shortcuts.v1"))[
          "view.panel"
        ],
    ),
    `${modifier === "Meta" ? "Meta" : "Ctrl"}+Shift+J`,
  );
  await terminal.focus();
  await page.keyboard.press(`${modifier}+Shift+j`);
  await expect(bottom).toBeHidden();
  await page.locator('[data-menu="view"]').click();
  await expect(page.locator('[data-action="view.panel"]')).toContainText(
    modifier === "Meta" ? "⌘⇧J" : "Ctrl+Shift+J",
  );
  await page.keyboard.press("Escape");
  // Editors retain their keys even when a matching layout binding exists.
  await page.getByRole("searchbox", { name: "Search Sessions" }).focus();
  await page.keyboard.press(`${modifier}+b`);
  await expect(left).toBeVisible();
  await desktop.evaluate(({ Menu }) =>
    Menu.getApplicationMenu().getMenuItemById("settings.shortcuts").click(),
  );
  await dialog.getByRole("button", { name: "Restore defaults" }).click();
  await dialog.getByRole("button", { name: "Apply", exact: true }).click();
  await page.locator('[data-layout="view.auxiliary"]').click();
  await expect(right).toBeHidden();
  await terminal.focus();
  assert.equal(
    Buffer.from((await observer.read(attachment.id)).data).toString(),
    "",
  );
}

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
