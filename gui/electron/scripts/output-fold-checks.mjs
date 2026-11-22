import assert from "node:assert/strict";
import { expect } from "@playwright/test";

/** Drive the fold margin itself and verify independent Host output over fake serial I/O. */
export async function checkCommandFolding(page, fixture, observer, attachment) {
  const read = async () =>
    Buffer.from((await observer.read(attachment.id)).data).toString();
  const terminal = page.locator(".xterm-helper-textarea");
  const query = page.getByRole("searchbox", { name: "Find retained text" });
  await read();
  const transcript =
    "\x1bc$ first\r\nNeedle needle NEEDLE\r\n$ second\r\n中文针 中文针\r\n$ ";
  await fixture.command("output", transcript);
  assert.equal(await read(), transcript);
  const firstCommand = page.getByRole("button", {
    name: /command output · \$ first/i,
  });
  const secondCommand = page.getByRole("button", {
    name: /command output · \$ second/i,
  });
  const history = page.locator(".fold-history");
  const metadata = page.locator(".line-sidebar .line-observation");
  await expect(firstCommand).toHaveText("−");
  await expect(metadata.nth(2)).toHaveAttribute("data-received-at", /^\d+$/);
  const secondHeader = await metadata.nth(2).textContent();
  assert.equal((await firstCommand.boundingBox()).width, 16);
  await expect(firstCommand).toHaveCSS("background-color", "rgba(0, 0, 0, 0)");
  await firstCommand.hover();
  assert.equal(
    await firstCommand.evaluate(
      (node) => getComputedStyle(node, "::after").borderLeftStyle,
    ),
    "solid",
  );
  await firstCommand.click();
  await expect(firstCommand).toHaveAttribute("aria-expanded", "false");
  await expect(firstCommand).toHaveText("+");
  assert.equal(
    await firstCommand.evaluate(
      (node) => getComputedStyle(node, "::after").content,
    ),
    "none",
  );
  await expect(secondCommand).toHaveAttribute("aria-expanded", "true");
  await secondCommand.click();
  await expect(history).toHaveJSProperty("textContent", "$ first\n$ second");
  await expect(metadata.nth(1)).toHaveJSProperty("textContent", secondHeader);
  await firstCommand.click();
  await expect(history).toHaveJSProperty(
    "textContent",
    "$ first\nNeedle needle NEEDLE\n$ second",
  );
  await expect(secondCommand).toHaveAttribute("aria-expanded", "false");
  await fixture.command("output", "live-after-fold\r\n");
  await expect(page.locator(".xterm-rows")).toContainText("live-after-fold");
  assert.equal(await read(), "live-after-fold\r\n");
  await terminal.focus();
  await page.keyboard.type("z");
  await expect.poll(read).toBe("z");
  await expect(secondCommand).toHaveAttribute("aria-expanded", "false");
  await page.keyboard.press("Control+Shift+f");
  await expect(query).toBeFocused();
  await expect(history).toBeHidden();
  await expect(secondCommand).toHaveAttribute("aria-expanded", "true");
  await query.fill("中文针");
  await expect(page.locator(".search-result")).toHaveText(/^[12]\/2$/);
  await query.press("Escape");
  await secondCommand.click();
  await fixture.command("output", "\x1b[?1049hfull-screen");
  await expect(page.locator(".fold-margin button")).toHaveCount(0);
  await fixture.command("output", "\x1b[?1049l");
  await expect(secondCommand).toHaveAttribute("aria-expanded", "true");
  await firstCommand.click();
  await fixture.command("output", "\x1bcraw serial log\r\nno shell prompt\r\n");
  await expect(page.locator(".xterm-rows")).toContainText("raw serial log");
  await expect(page.locator(".fold-margin button")).toHaveCount(0);
  await expect(history).toBeHidden();
  const marker = "\x1b]133;A\x07";
  await fixture.command(
    "output",
    "\x1bc" +
      marker +
      "λ demo\r\nvisible line\r\n" +
      marker +
      "λ status\r\nOK\r\n" +
      marker +
      "λ ",
  );
  const custom = page.getByRole("button", { name: /command output · λ demo/ });
  await custom.click();
  await expect(custom).toHaveAttribute("aria-expanded", "false");
  await page.locator('[data-menu="terminal"]').click();
  await page
    .getByRole("menuitem", { name: "Expand all command outputs", exact: true })
    .click();
  await expect(custom).toHaveAttribute("aria-expanded", "true");
  await expect(history).toBeHidden();
  await fixture.command(
    "output",
    "\x1bc$ verbose\r\n" +
      "row\r\n".repeat(96) +
      "$ next\r\nnext-visible\r\n$ ",
  );
  await expect(page.locator(".xterm-rows")).toContainText("next-visible");
  await terminal.focus();
  for (let step = 0; step < 12; step++)
    await page.keyboard.press("Shift+PageUp");
  const verbose = page.getByRole("button", {
    name: /command output · \$ verbose/,
  });
  await expect(verbose).toBeVisible();
  await verbose.click();
  await expect(verbose).toHaveAttribute("aria-expanded", "false");
  await expect(
    page.getByRole("button", { name: /command output · \$ next/ }),
  ).toBeVisible();
  await expect
    .poll(() =>
      page
        .locator(".xterm-rows > div")
        .filter({ hasText: "next-visible" })
        .evaluate((node) => {
          const box = node.getBoundingClientRect();
          return !!document
            .elementFromPoint(box.left + 5, box.top + box.height / 2)
            ?.closest(".xterm");
        }),
    )
    .toBe(true);
}
