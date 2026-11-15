import { expect } from "@playwright/test";
import assert from "node:assert/strict";
import { dirname, join } from "node:path";
import { readFile, writeFile } from "node:fs/promises";
import { setupI18n } from "@lingui/core";
import { menuCatalogs } from "../src/locales/catalogs.ts";
import { locales } from "../src/shared/menus.ts";

/** Real renderer/Main/CLI operations use only disposable fixture targets and native picker seams. */
export async function checkSSHAdvanced(desktop, page, fixture, local, output) {
  const form = page.locator(".ssh-file-dialog");
  const job = page.locator(".transfer-job").first();
  await desktop.evaluate(({ dialog }) => {
    globalThis.advancedPreviousConfirm = dialog.showMessageBox;
    globalThis.advancedApproved = false;
    globalThis.advancedPreviews = [];
    dialog.showMessageBox = async (_window, options) => {
      globalThis.advancedPreviews.push(options);
      return { response: globalThis.advancedApproved ? 1 : 0 };
    };
  });
  async function open(receive = false) {
    await desktop.evaluate(
      ({ Menu }, receive) =>
        Menu.getApplicationMenu()
          .getMenuItemById(receive ? "file.receive" : "file.send")
          .click(),
      receive,
    );
    await expect(form).toBeVisible();
    await form.locator('[name="remotePath"]').fill("/gui-payload.bin");
    await form
      .locator('[name="identityFiles"]')
      .fill(join(dirname(fixture.knownHosts), "identity"));
    await form.locator('[name="knownHostsFile"]').fill(fixture.knownHosts);
    await form.locator('[name="password"]').fill("fixture-password");
    await desktop.evaluate(
      ({ dialog }, path) => {
        dialog.showOpenDialog = async () => ({
          canceled: false,
          filePaths: [path],
        });
      },
      receive ? output : local,
    );
    await form
      .getByRole("button", {
        name: receive ? "Choose save folder" : "Choose file",
        exact: true,
      })
      .click();
    await form.locator(".ssh-advanced summary").click();
    await expect(form.locator('[name="overwrite"]')).not.toBeChecked();
    await expect(form.locator('[name="safeReplace"]')).toBeDisabled();
    await expect(form.locator('[name="operationTimeoutSeconds"]')).toHaveValue(
      "0",
    );
  }
  async function start(receive = false) {
    const previous = await page.evaluate(
      async () => (await window.channelterm.sshTransfers()).value?.at(-1)?.id,
    );
    await form
      .getByRole("button", {
        name: receive ? "Receive from device" : "Send to device",
        exact: true,
      })
      .click();
    await expect(form).toHaveCount(0);
    await expect
      .poll(() =>
        page.evaluate(
          async () =>
            (await window.channelterm.sshTransfers()).value?.at(-1)?.id,
        ),
      )
      .not.toBe(previous);
    const current = await page.evaluate(
      async () => (await window.channelterm.sshTransfers()).value?.at(-1)?.id,
    );
    assert.ok(current);
    // A finished predecessor can remain in the renderer until its next status poll.
    await expect(job).toContainText(current);
  }
  try {
    console.log("SSH advanced: existing destination refusal");
    await open();
    await start();
    await expect(job).toContainText(
      "The server reported that the target exists.",
    );
    await expect(job).toContainText("Final destination state is uncertain");
    await open();
    const resume = form.getByRole("checkbox", {
      name: "Resume an existing partial file (SFTP)",
      exact: true,
    });
    await resume.check();
    await expect(form.locator('[name="overwrite"]')).toBeDisabled();
    await resume.uncheck();
    await form.locator('[name="overwrite"]').check();
    await expect(resume).toBeDisabled();
    await form.locator('[name="safeReplace"]').check();
    await form
      .getByRole("combobox", { name: "File protocol" })
      .selectOption("scp");
    await expect(form.locator('[name="safeReplace"]')).toBeDisabled();
    await expect(form.locator('[name="safeReplace"]')).not.toBeChecked();
    await form
      .getByRole("combobox", { name: "File protocol" })
      .selectOption("sftp");
    await form.locator('[name="safeReplace"]').check();
    await form.locator('[name="operationTimeoutSeconds"]').fill("-1");
    await form
      .getByRole("button", { name: "Send to device", exact: true })
      .click();
    await expect(form).toBeVisible();
    await form.locator('[name="operationTimeoutSeconds"]').fill("30");
    await page.screenshot({ path: "test-results/ssh-advanced-options.png" });
    console.log("SSH advanced: native overwrite cancellation");
    await start();
    await expect(job).toContainText("Transfer did not start");
    const preview = await desktop.evaluate(() =>
      globalThis.advancedPreviews.at(-1),
    );
    assert.equal(preview.defaultId, 0);
    assert.equal(preview.cancelId, 0);
    assert.ok(
      preview.detail.includes(local) &&
        preview.detail.includes("/gui-payload.bin"),
    );
    assert.ok(!preview.detail.includes("fixture-password"));
    await desktop.evaluate(() => {
      globalThis.advancedApproved = true;
    });
    console.log("SSH advanced: verified replacement roundtrip");
    const replacement = Buffer.from("native safe replacement\n");
    await writeFile(local, replacement);
    await open();
    await form.locator('[name="overwrite"]').check();
    await form.locator('[name="safeReplace"]').check();
    await start();
    await expect(job).toContainText("Transfer and verification confirmed");
    await expect(job).toContainText("Completed files: 1");
    await expect(job).toContainText("SHA-256 verified files: 1");
    await open(true);
    await form.locator('[name="overwrite"]').check();
    await form.locator('[name="safeReplace"]').check();
    await start(true);
    await expect(job).toContainText("Transfer and verification confirmed");
    assert.equal(
      (await readFile(join(output, "gui-payload.bin"))).equals(replacement),
      true,
      "download must match every replacement byte",
    );
    await expect(job.locator(".ssh-transfer-result")).toHaveCSS(
      "display",
      "grid",
    );
    await page.screenshot({ path: "test-results/ssh-advanced-results.png" });
    await open();
    console.log("SSH advanced: nine-locale compact rendering");
    const previousSize = await desktop.evaluate(({ BrowserWindow }) => {
      const window = BrowserWindow.getAllWindows()[0];
      const size = window.getSize();
      window.setSize(760, 500);
      return size;
    });
    try {
      for (const locale of locales) {
        const i18n = setupI18n({ locale, messages: menuCatalogs });
        await desktop.evaluate(
          ({ Menu }, locale) =>
            Menu.getApplicationMenu()
              .getMenuItemById(`language.${locale}`)
              .click(),
          locale,
        );
        await expect(form.locator("summary").first()).toHaveText(
          i18n._("file.advanced"),
        );
        await expect(
          form.locator('[name="operationTimeoutSeconds"]'),
        ).toHaveAccessibleName(i18n._("file.operationTimeout"));
        assert.equal(
          await page.evaluate(
            () => document.documentElement.scrollWidth <= innerWidth,
          ),
          true,
        );
        assert.equal(
          await job
            .locator(".ssh-transfer-result")
            .evaluate((el) => el.scrollWidth <= el.clientWidth),
          true,
        );
        assert.equal(
          await form.evaluate((el) => el.scrollWidth <= el.clientWidth),
          true,
        );
      }
      await form.locator(".ssh-advanced").scrollIntoViewIfNeeded();
      await page.screenshot({ path: "test-results/ssh-advanced-compact.png" });
    } finally {
      await desktop.evaluate(({ Menu, BrowserWindow }, size) => {
        Menu.getApplicationMenu().getMenuItemById("language.en").click();
        BrowserWindow.getAllWindows()[0].setSize(...size);
      }, previousSize);
    }
    await form.getByRole("button", { name: "Cancel", exact: true }).click();
  } catch (error) {
    console.error("SSH advanced failure:", error);
    throw error;
  } finally {
    await desktop.evaluate(({ dialog }) => {
      dialog.showMessageBox = globalThis.advancedPreviousConfirm;
    });
  }
}
