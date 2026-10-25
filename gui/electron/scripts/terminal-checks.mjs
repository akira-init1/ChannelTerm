import assert from "node:assert/strict";
import { expect } from "@playwright/test";

/** Real xterm/IPC against fake serial I/O: replay queries, clipboard and local display. */
export async function checkTerminalControls(
  desktop,
  page,
  fixture,
  observer,
  attachment,
) {
  const terminal = page.locator(".xterm-helper-textarea");
  const read = async () =>
    Buffer.from((await observer.read(attachment.id)).data).toString();
  await page
    .getByRole("button", { name: "Close tab (detach)", exact: true })
    .click();
  await fixture.command(
    "output",
    "\r\nhistorical-query\x1b[6n\x1b[?6n\x1b[c\x1b[>c",
  );
  await read();
  await page.getByRole("button", { name: /SER-1/ }).click();
  await expect(terminal).toBeVisible();
  await terminal.focus();
  await page.keyboard.type("replay-complete");
  let received = "";
  await expect
    .poll(async () => {
      received += await read();
      return received;
    })
    .toContain("replay-complete");
  assert.equal(
    received,
    "replay-complete",
    "historical queries must not write responses",
  );
  // Live, fragmented queries must still receive xterm's answer.
  await fixture.command("output", "\x1b[");
  await read();
  await fixture.command("output", "6n");
  received = "";
  await expect
    .poll(async () => {
      received += await read();
      return received;
    })
    .toMatch(/\x1b\[\d+;\d+R/);

  const previousClipboard = await desktop.evaluate(({ clipboard }) =>
    clipboard.readText(),
  );
  try {
    await fixture.command("output", "\x1b[2J\x1b[Hclipboard\r\n");
    await read();
    // Double-click selects a word through xterm's real selection service.
    const screen = await page.locator(".xterm-screen").boundingBox();
    await expect
      .poll(async () => {
        await page.mouse.dblclick(screen.x + 20, screen.y + 8);
        await page.keyboard.press("Control+Shift+c");
        return desktop.evaluate(({ clipboard }) => clipboard.readText());
      })
      .toBe("clipboard");
    assert.equal(await read(), "", "copy must send no terminal bytes");
    await desktop.evaluate(({ clipboard }) =>
      clipboard.writeText("paste-test"),
    );
    await terminal.focus();
    await page.keyboard.press("Control+Shift+v");
    received = "";
    await expect
      .poll(async () => {
        received += await read();
        return received;
      })
      .toBe("paste-test");
    const preview = page.getByRole("dialog", {
      name: "Review multiline paste",
    });
    const showPreview = async (text) => {
      await desktop.evaluate(
        ({ clipboard }, text) => clipboard.writeText(text),
        text,
      );
      await terminal.focus();
      await page.keyboard.press("Control+Shift+v");
      await expect(preview).toBeVisible();
      await expect(preview).toContainText(attachment.sessionId);
    };
    await showPreview("first\nsecond\n");
    assert.equal(await read(), "", "preview sends nothing before confirmation");
    await preview.getByRole("button", { name: "Cancel", exact: true }).click();
    assert.equal(await read(), "", "cancel discards the paste");
    // A native DOM paste must enter the same confirmation path, before xterm consumes it.
    await terminal.evaluate((element) =>
      element.dispatchEvent(
        new ClipboardEvent("paste", { bubbles: true, cancelable: true }),
      ),
    );
    await expect(preview).toBeVisible();
    await page.screenshot({ path: "test-results/multiline-paste-preview.png" });
    await preview
      .getByRole("button", { name: "Paste into this target" })
      .evaluate((button) => {
        button.click();
        button.click();
      });
    received = "";
    await expect
      .poll(async () => {
        received += await read();
        return received;
      })
      .toBe("first\rsecond\r");
    assert.equal(
      await read(),
      "",
      "repeated confirmation cannot duplicate the paste",
    );

    await fixture.command("output", "\x1b[?2004hbracketed-ready\r\n");
    await read();
    await expect(page.locator(".xterm-rows")).toContainText("bracketed-ready");
    await showPreview("bracketed\npaste");
    await preview
      .getByRole("button", { name: "Paste into this target" })
      .click();
    received = "";
    await expect
      .poll(async () => {
        received += await read();
        return received;
      })
      .toBe("\x1b[200~bracketed\rpaste\x1b[201~");
    await fixture.command("output", "\x1b[?2004lbracketed-disabled\r\n");
    await read();
    await expect(page.locator(".xterm-rows")).toContainText(
      "bracketed-disabled",
    );

    await showPreview("must-not\nsend");
    await fixture.command("lease");
    await expect(
      preview.getByRole("button", { name: "Paste into this target" }),
    ).toBeDisabled();
    await fixture.command("release");
    await expect(page.getByText(/Lease busy/)).toBeHidden();
    await expect(
      preview.getByRole("button", { name: "Paste into this target" }),
    ).toBeDisabled();
    await page.keyboard.press("Escape");
    await expect(preview).toHaveCount(0);
    assert.equal(
      await read(),
      "",
      "a lease invalidates the draft permanently; release cannot replay it",
    );

    await showPreview("hidden\ndraft");
    await desktop.evaluate(({ Menu }) =>
      Menu.getApplicationMenu().getMenuItemById("tab.new").click(),
    );
    await expect(page.locator(".terminal-empty")).toBeVisible();
    await desktop.evaluate(({ Menu }) =>
      Menu.getApplicationMenu().getMenuItemById("tab.previous").click(),
    );
    await expect(preview).toBeVisible();
    await expect(
      preview.getByRole("button", { name: "Paste into this target" }),
    ).toBeDisabled();
    await page.keyboard.press("Escape");
    await expect(preview).toHaveCount(0);
    assert.equal(await read(), "", "returning to a tab cannot revive a draft");
    await desktop.evaluate(({ Menu }) =>
      Menu.getApplicationMenu().getMenuItemById("tab.next").click(),
    );
    await expect(page.locator(".terminal-empty")).toBeVisible();
    await desktop.evaluate(({ Menu }) =>
      Menu.getApplicationMenu().getMenuItemById("tab.close").click(),
    );
    await expect(terminal).toBeVisible();

    await showPreview("detached\ndraft");
    await desktop.evaluate(({ Menu }) =>
      Menu.getApplicationMenu().getMenuItemById("tab.close").click(),
    );
    await expect(preview).toHaveCount(0);
    await page.getByRole("button", { name: /SER-1/ }).click();
    await expect(terminal).toBeVisible();
    assert.equal(
      await read(),
      "",
      "detach discards the draft instead of sending it to a replacement view",
    );
    const stale = await page.evaluate(() =>
      window.channelterm.readClipboard("expired"),
    );
    assert.equal(stale.ok, false);
    assert.equal(stale.error.code, "cancelled");
    const oversized = await page.evaluate(() =>
      window.channelterm.writeClipboard("expired", "x".repeat(65537)),
    );
    assert.equal(oversized.ok, false);
    assert.equal(oversized.error.code, "invalid_input");
    await fixture.command("lease");
    await expect(page.getByText(/Lease busy/)).toBeVisible();
    await terminal.focus();
    await page.keyboard.press("Control+Shift+v");
    assert.equal(
      await read(),
      "",
      "paste under a lease must not write or queue input",
    );
    await fixture.command("release");
    await expect(page.getByText(/Lease busy/)).toBeHidden();
    assert.equal(await read(), "", "release must not replay rejected paste");
  } finally {
    await desktop.evaluate(
      ({ clipboard }, text) => clipboard.writeText(text),
      previousClipboard,
    );
  }

  await page.locator('[data-menu="settings"]').click();
  await page
    .getByRole("menuitem", { name: "Keyboard shortcuts", exact: true })
    .click();
  const shortcuts = page.getByRole("dialog", { name: "Keyboard shortcuts" });
  const search = shortcuts.getByRole("searchbox", { name: "Search shortcuts" });
  await search.fill("paste");
  await expect(
    shortcuts.getByRole("textbox", { name: "Paste into terminal" }),
  ).toBeVisible();
  await expect(
    shortcuts.getByRole("textbox", { name: "Bottom panel", exact: true }),
  ).toHaveCount(0);
  await search.fill("nothing-matches");
  await expect(shortcuts.getByRole("status")).toHaveText(
    "No matching shortcuts",
  );
  await search.fill("ctrl+shift+c");
  await expect(
    shortcuts.getByRole("textbox", { name: "Copy terminal selection" }),
  ).toBeVisible();
  await shortcuts.getByRole("button", { name: "Cancel", exact: true }).click();

  await fixture.command("output", "\r\nfont-preserved\r\n");
  await read();
  await expect(page.locator(".xterm-rows")).toContainText("font-preserved");
  await page.locator('[data-menu="settings"]').click();
  await page
    .getByRole("menuitem", { name: "Preferences...", exact: true })
    .click();
  const display = page.getByRole("dialog", { name: "Display", exact: true });
  await display.getByRole("spinbutton", { name: "Font size" }).fill("20");
  await display
    .getByRole("textbox", { name: "Font family" })
    .fill("Courier New");
  await display.getByRole("button", { name: "Apply", exact: true }).click();
  await expect(page.locator(".xterm-rows")).toHaveCSS("font-size", "20px");
  await expect(page.locator(".xterm-rows")).toContainText("font-preserved");
  await expect(page.locator(".xterm-rows")).toHaveCSS(
    "font-family",
    /Courier New/,
  );
  await terminal.focus();
  const screen = await page.locator(".xterm-screen").boundingBox();
  await page.mouse.move(screen.x + 40, screen.y + 40);
  await page.keyboard.down("Control");
  await page.mouse.wheel(0, -100);
  await page.keyboard.up("Control");
  await expect(page.locator(".xterm-rows")).toHaveCSS("font-size", "21px");
  assert.equal(
    await desktop.evaluate(({ BrowserWindow }) =>
      BrowserWindow.getAllWindows()[0].webContents.getZoomFactor(),
    ),
    1,
  );
  assert.equal(await read(), "", "font changes must send no device bytes");
  assert.equal(
    await page.evaluate(
      () => JSON.parse(localStorage.getItem("channelterm.display.v1")).fontSize,
    ),
    21,
  );
  await page.locator('[data-menu="settings"]').click();
  await page
    .getByRole("menuitem", { name: "Preferences...", exact: true })
    .click();
  await expect(
    display.getByRole("spinbutton", { name: "Font size" }),
  ).toHaveValue("21");
  await page.screenshot({ path: "test-results/terminal-display-settings.png" });
  await display.getByRole("button", { name: "Restore defaults" }).click();
  await display.getByRole("button", { name: "Apply", exact: true }).click();
  await expect(page.locator(".xterm-rows")).toHaveCSS("font-size", "14px");
  // A failed persistence write leaves usable local zoom and correctly placed notices.
  await page.evaluate(() => {
    const original = Storage.prototype.setItem;
    Storage.prototype.setItem = function (key, value) {
      if (key === "channelterm.display.v1")
        throw new Error("Storage unavailable");
      original.call(this, key, value);
    };
    globalThis.restoreDisplayStorage = () => {
      Storage.prototype.setItem = original;
    };
  });
  try {
    await terminal.focus();
    await page.locator(".xterm-screen").hover();
    await page.keyboard.down("Control");
    await page.mouse.wheel(0, -100);
    await page.keyboard.up("Control");
    await expect(page.locator(".xterm-rows")).toHaveCSS("font-size", "15px");
    await expect(page.locator(".notices").getByRole("alert")).toContainText(
      "Could not save display settings",
    );
    await expect(page.locator(".statusbar")).toBeInViewport();
    await page.locator('[data-menu="settings"]').click();
    await page
      .getByRole("menuitem", { name: "Preferences...", exact: true })
      .click();
    await display.getByRole("button", { name: "Apply", exact: true }).click();
    await expect(display.getByRole("alert")).toContainText(
      "Could not save display settings",
    );
  } finally {
    await page.evaluate(() => globalThis.restoreDisplayStorage());
  }
  await display.getByRole("button", { name: "Restore defaults" }).click();
  await display.getByRole("button", { name: "Apply", exact: true }).click();
  await expect(page.locator(".notices").getByRole("alert")).toHaveCount(0);
  await terminal.focus();
}
