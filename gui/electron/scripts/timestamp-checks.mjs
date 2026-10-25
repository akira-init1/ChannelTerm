import assert from "node:assert/strict";
import { expect } from "@playwright/test";

/** Native view stamps live prompts while an independent client retains original bytes. */
export async function checkPromptTimestamps(
  desktop,
  page,
  fixture,
  observer,
  attachment,
) {
  const rows = page.locator(".xterm-rows");
  const terminal = page.locator(".xterm-helper-textarea");
  const stamp = /\[\d{2}:\d{2}:\d{2}\] root@/;
  const read = async () =>
    Buffer.from((await observer.read(attachment.id)).data).toString();
  async function settings(enabled, style = "semantic") {
    await page.locator('[data-menu="settings"]').click();
    await page
      .getByRole("menuitem", { name: "Preferences...", exact: true })
      .click();
    const dialog = page.getByRole("dialog", { name: "Display", exact: true });
    await dialog
      .getByRole("checkbox", { name: "Local prompt timestamps" })
      .setChecked(enabled);
    await dialog
      .getByLabel("Terminal style", { exact: true })
      .selectOption(style);
    await dialog.getByRole("button", { name: "Apply", exact: true }).click();
  }
  await read();
  await fixture.command("output", "\x1bcroot@old:~# ");
  await expect(rows).toContainText("root@old:~#");
  await expect(rows).not.toContainText(stamp);
  await read();
  await settings(true);
  await fixture.command("output", "\r\nroot@live:~# ");
  await expect(rows).toContainText(/\[\d{2}:\d{2}:\d{2}\] root@live:~#/);
  assert.equal(await read(), "\r\nroot@live:~# ");
  await page
    .locator(".terminal-tab.active")
    .getByRole("button", { name: "Close tab (detach)", exact: true })
    .click();
  await expect(terminal).toHaveCount(0);
  await page.getByRole("button", { name: /SER-1/ }).click();
  await expect(rows).toContainText("root@live:~#");
  await expect(rows).not.toContainText(stamp);
  assert.equal(
    await read(),
    "",
    "view restore cannot send input or modify Host history",
  );
  await settings(true, "monochrome");
  await fixture.command("output", "\r\nroot@mono:~# ");
  await expect(rows).toContainText(/\[\d{2}:\d{2}:\d{2}\] root@mono:~#/);
  assert.equal(await read(), "\r\nroot@mono:~# ");
  await page.screenshot({
    path: "test-results/terminal-prompt-timestamps.png",
  });
  const alternate = "\x1b[?1049h\x1b[2J\x1b[Hroot@full:~# ";
  await fixture.command("output", alternate);
  await expect(rows).toContainText("root@full:~#");
  await expect(rows).not.toContainText(stamp);
  assert.equal(await read(), alternate);
  await fixture.command("output", "\x1b[?1049l\r\n");
  await expect(rows).toContainText("root@mono:~#");
  await read();
  await settings(false);
  await fixture.command("output", "root@off:~# ");
  await expect(rows).toContainText("root@off:~#");
  await expect(rows).not.toContainText(/\[\d{2}:\d{2}:\d{2}\] root@off/);
  assert.equal(await read(), "root@off:~# ");
  await terminal.focus();
}
