import { expect } from "@playwright/test";
import assert from "node:assert/strict";

/** Native rendering uses test-only directory snapshots; transfer events use the real Host. */
export async function checkFiles(desktop, page, fixture) {
  await fixture.command("transfer-start");
  await expect(page.locator(".transfer-panel")).toBeVisible();
  await expect(page.locator(".transfer-job progress")).toHaveAttribute(
    "value",
    "512",
  );
  await expect(page.locator(".xterm-rows")).not.toContainText(
    "BINARY-PROTOCOL",
  );
  await page.screenshot({ path: "test-results/file-transfer-progress.png" });
  const cancelButton = page
    .locator(".transfer-job")
    .getByRole("button", { name: "Cancel", exact: true });
  await cancelButton.click();
  const confirmation = page.getByRole("dialog", {
    name: "Cancel this file transfer?",
  });
  await expect(confirmation).toBeVisible();
  await expect(
    confirmation.getByRole("button", { name: "Continue", exact: true }),
  ).toBeFocused();
  await expect(confirmation).toHaveCSS("background-color", "rgb(23, 28, 36)");
  await page.screenshot({
    path: "test-results/transfer-cancel-confirmation.png",
  });
  await page.keyboard.press("Escape");
  await expect(confirmation).not.toBeVisible();
  await expect(cancelButton).toBeEnabled();
  await cancelButton.click();
  await confirmation
    .getByRole("button", { name: "Continue", exact: true })
    .click();
  await expect(confirmation).not.toBeVisible();
  await expect(cancelButton).toBeEnabled();
  await fixture.command("transfer-finish");
  await expect(page.locator(".transfer-job")).toContainText("Completed");
  await expect(page.locator(".xterm-rows")).toContainText("transfer-finished");

  // Test snapshots are installed only in this disposable Electron process.
  await desktop.evaluate(({ ipcMain }) => {
    globalThis.fileRequests = [];
    ipcMain.removeHandler("files:browse");
    ipcMain.handle("files:browse", (_event, id, path) => {
      globalThis.fileRequests.push({ id, path });
      return {
        ok: true,
        value: {
          path: path === "." ? "/home/root" : path,
          entries: [
            { name: "logs", kind: "directory" },
            { name: "firmware.bin", kind: "file" },
            { name: "link", kind: "other" },
          ],
          truncated: false,
        },
      };
    });
    ipcMain.removeHandler("files:choose");
    ipcMain.handle("files:choose", () => ({
      ok: true,
      value: { id: "test-selection", name: "downloads" },
    }));
    ipcMain.removeHandler("files:transfer");
    ipcMain.handle("files:transfer", (_event, request) => {
      globalThis.fileRequest = request;
      return { ok: true };
    });
  });
  await expect
    .poll(() =>
      desktop.evaluate(
        ({ Menu }) =>
          Menu.getApplicationMenu().getMenuItemById("file.receive").enabled,
      ),
    )
    .toBe(true);
  await desktop.evaluate(({ Menu }) =>
    Menu.getApplicationMenu().getMenuItemById("file.receive").click(),
  );
  const dialog = page.getByRole("dialog", { name: "Receive from device" });
  await expect(dialog).toBeVisible();
  await expect(dialog.getByRole("textbox")).toHaveValue("/home/root");
  await dialog.getByRole("option", { name: /logs/ }).dblclick();
  await expect(dialog.getByRole("textbox")).toHaveValue("/home/root/logs");
  await dialog.getByRole("button", { name: "Up", exact: true }).click();
  await expect(dialog.getByRole("textbox")).toHaveValue("/home/root");
  await expect(dialog.getByRole("option", { name: /link/ })).toBeDisabled();
  await dialog.getByRole("option", { name: /firmware.bin/ }).click();
  await dialog.getByRole("button", { name: "Choose save folder" }).click();
  await expect(
    dialog.getByRole("button", { name: "Receive from device", exact: true }),
  ).toBeEnabled();
  await expect(dialog.getByText("downloads", { exact: true })).toBeVisible();
  await page.screenshot({ path: "test-results/file-receive-browser.png" });
  await dialog
    .getByRole("button", { name: "Receive from device", exact: true })
    .click();
  await expect(dialog).not.toBeVisible();
  const request = await desktop.evaluate(() => globalThis.fileRequest);
  assert.equal(request.direction, "receive");
  assert.equal(request.remotePath, "/home/root/firmware.bin");
  assert.equal(request.selectionId, "test-selection");
  // No path navigation is typed through the terminal input channel.
  await expect(page.locator(".xterm-rows")).not.toContainText("cd /home");
  await fixture.command("transfer-start");
  await cancelButton.click();
  await confirmation
    .getByRole("button", { name: "Cancel transfer", exact: true })
    .click();
  await fixture.command("transfer-cancel");
  await expect(page.locator(".transfer-job").first()).toContainText(
    "Cancelled",
  );
  await page.locator(".transfer-heading").click();
}
