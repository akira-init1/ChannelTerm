import assert from "node:assert/strict";
import { expect } from "@playwright/test";
import { mkdtemp, readFile, readdir, rm } from "node:fs/promises";
import { join } from "node:path";
import { tmpdir } from "node:os";
import { diagnosticZipFiles } from "../tests/diagnostic-zip.ts";

/** Production renderer/IPC/Host export; only the native destination picker is substituted. */
export async function checkDiagnostics(desktop, page, observer, attachment) {
  const root = await mkdtemp(join(tmpdir(), "channelterm-gui-diagnostic-"));
  await desktop.evaluate(({ dialog }) => {
    globalThis.originalDiagnosticSave = dialog.showSaveDialog;
  });
  async function destination(name) {
    await desktop.evaluate(
      ({ dialog }, path) => {
        dialog.showSaveDialog = async () => ({
          canceled: false,
          filePath: path,
        });
      },
      join(root, name),
    );
  }
  try {
    const invalid = await page.evaluate(() =>
      window.channelterm.saveDiagnostics({
        kind: "existing",
        diagnosticId: "../../secret",
      }),
    );
    assert.equal(invalid.ok, false);
    assert.equal(invalid.error.code, "invalid_input");
    const foreign = await page.evaluate(
      (id) =>
        window.channelterm.saveDiagnostics({
          kind: "snapshot",
          attachmentId: id,
          includeRaw: false,
        }),
      attachment.id,
    );
    assert.equal(foreign.ok, false);
    assert.equal(foreign.error.code, "cancelled");
    await observer.write(attachment.id, "diagnostic-sensitive-fixture\r");
    await observer.read(attachment.id);
    await page.locator('[data-menu="help"]').click();
    await page
      .getByRole("menuitem", { name: "Diagnostic ZIP…", exact: true })
      .click();
    const modal = page.getByRole("dialog", { name: "Diagnostic ZIP…" });
    const raw = modal.getByRole("checkbox", {
      name: "Include raw terminal output and write contents",
    });
    await expect(raw).not.toBeChecked();
    await destination("metadata.zip");
    await modal.getByRole("button", { name: "Save ZIP…", exact: true }).click();
    await expect(modal.getByRole("status")).toContainText(
      "ZIP saved and verified.",
    );
    const id = await modal
      .locator(".diagnostic-result code")
      .first()
      .textContent();
    assert.match(id, /^DIAG-[a-f0-9]{24}$/);
    const metadata = await readFile(join(root, "metadata.zip"));
    const files = diagnosticZipFiles(metadata);
    assert.equal(files.has("terminal.bin"), false);
    assert.doesNotMatch(
      [...files.values()].join(""),
      /diagnostic-sensitive-fixture/,
    );
    await raw.check();
    await destination("raw.zip");
    await modal.getByRole("button", { name: "Save ZIP…", exact: true }).click();
    await expect(modal.getByRole("status")).toContainText("raw.zip");
    assert.match(
      diagnosticZipFiles(await readFile(join(root, "raw.zip")))
        .get("terminal.bin")
        .toString(),
      /diagnostic-sensitive-fixture/,
    );
    await modal
      .getByRole("combobox", { name: "Archive source" })
      .selectOption("existing");
    await expect(raw).toHaveCount(0);
    await modal
      .getByRole("textbox", { name: "Diagnostic ID", exact: true })
      .fill(id);
    await destination("existing.zip");
    await modal.getByRole("button", { name: "Save ZIP…", exact: true }).click();
    await expect(modal.getByRole("status")).toContainText("existing.zip");
    assert.deepEqual(await readFile(join(root, "existing.zip")), metadata);
    await page.screenshot({ path: "test-results/diagnostic-export.png" });
    await destination("unavailable.zip");
    await modal
      .getByRole("textbox", { name: "Diagnostic ID", exact: true })
      .fill("DIAG-" + "0".repeat(24));
    await modal.getByRole("button", { name: "Save ZIP…", exact: true }).click();
    await expect(modal.getByRole("alert")).toContainText(
      "expired or been evicted",
    );
    await expect
      .poll(async () => (await readdir(root)).includes("unavailable.zip"))
      .toBe(false);
    await desktop.evaluate(({ dialog }) => {
      dialog.showSaveDialog = async () => ({ canceled: true });
    });
    await modal.getByRole("button", { name: "Save ZIP…", exact: true }).click();
    await expect(modal.getByRole("status")).toHaveText("Save cancelled.");
    await modal.getByRole("button", { name: "Close", exact: true }).click();
    assert.equal((await observer.list())[0].state, "open");
  } finally {
    await desktop.evaluate(({ dialog }) => {
      dialog.showSaveDialog = globalThis.originalDiagnosticSave;
    });
    await rm(root, { recursive: true, force: true });
  }
}
