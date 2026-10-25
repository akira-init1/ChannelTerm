import assert from "node:assert/strict";
import { expect } from "@playwright/test";

/** Real DOM-rendered colors and controls; the observer sees only original fixture bytes. */
export async function checkTerminalStyles(
  desktop,
  page,
  fixture,
  observer,
  attachment,
) {
  const rows = page.locator(".xterm-rows");
  const cells = () =>
    rows.evaluate((element) =>
      Array.from(element.querySelectorAll("span"), (span) => ({
        text: span.textContent,
        color: getComputedStyle(span).color,
        background: getComputedStyle(span).backgroundColor,
        cursor: span.classList.contains("xterm-cursor"),
      })),
    );
  const read = async () =>
    Buffer.from((await observer.read(attachment.id)).data).toString();
  const output = async (text) => {
    await fixture.command("output", text);
    assert.equal(
      await read(),
      text,
      "presentation must not alter shared output",
    );
  };
  const setStyle = async (style) => {
    await page.locator('[data-menu="settings"]').click();
    await page
      .getByRole("menuitem", { name: "Preferences...", exact: true })
      .click();
    const dialog = page.getByRole("dialog", { name: "Display", exact: true });
    await dialog
      .getByLabel("Terminal style", { exact: true })
      .selectOption(style);
    if (style === "monochrome")
      await expect(dialog.locator(".style-preview span").first()).toHaveCSS(
        "color",
        "rgb(255, 255, 255)",
      );
    await dialog.getByRole("button", { name: "Apply", exact: true }).click();
    await expect(page.locator(`.terminal-view.${style}`)).toBeVisible();
  };
  await read();
  await output("\x1bcERROR ready uart0 42\r\n");
  await expect(rows).toContainText("ERROR ready uart0 42");
  await expect
    .poll(
      async () => (await cells()).find((cell) => cell.text === "ERROR")?.color,
    )
    .toBe("rgb(255, 95, 95)");
  await page.screenshot({ path: "test-results/terminal-semantic-style.png" });
  await setStyle("monochrome");
  await expect(rows).toContainText("ERROR ready uart0 42");
  await output(
    "\x1b[31;44mANSI16\x1b[0m \x1b[38;5;196mANSI256\x1b[0m \x1b[38;2;7;8;9mTRUECOLOR\x1b[0m \x1b[7mINVERSE\x1b[0m\r\n\x1b]11;rgb:ff/00/00\x07",
  );
  await expect(rows).toContainText("ANSI16 ANSI256 TRUECOLOR INVERSE");
  await expect
    .poll(async () =>
      (await cells())
        .filter((cell) => !cell.cursor && cell.text?.trim())
        .every(
          (cell) =>
            cell.color === "rgb(255, 255, 255)" &&
            cell.background === "rgba(0, 0, 0, 0)",
        ),
    )
    .toBe(true);
  await expect(page.locator(".xterm-screen")).toHaveCSS(
    "background-color",
    "rgb(0, 0, 0)",
  );
  await expect(page.locator(".xterm-scrollable-element")).toHaveCSS(
    "background-color",
    "rgb(0, 0, 0)",
  );
  const screen = await page.locator(".xterm-screen").boundingBox();
  await page.mouse.dblclick(screen.x + 20, screen.y + 8);
  await expect(page.locator(".xterm-selection div")).not.toHaveCount(0);
  await page.screenshot({ path: "test-results/terminal-monochrome-style.png" });

  // Styles keep the live terminal parser, including alternate screens and CR overwrite.
  await output("\x1b[?1049h\x1b[2J\x1b[Hfirst\rSECOND");
  await expect(rows).toContainText("SECOND");
  await expect(rows).not.toContainText("ANSI16");
  await output("\x1b[?1049l");
  await expect(rows).toContainText("ANSI16");
  await setStyle("semantic");
  await expect
    .poll(
      async () =>
        (await cells()).find((cell) => cell.text === "TRUECOLOR")?.color,
    )
    .toBe("rgb(7, 8, 9)");
  await expect
    .poll(
      async () => (await cells()).find((cell) => cell.text === "ERROR")?.color,
    )
    .toBe("rgb(255, 95, 95)");
  assert.equal(
    await read(),
    "",
    "style, selection and preview actions send no device bytes",
  );
  await output("\x1b]111\x07\x1b[0m\r\nstyle-restored\r\n");
  await page.locator(".xterm-helper-textarea").focus();
}
