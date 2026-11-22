import { chooseSession } from "./session-picker.mjs";
import assert from "node:assert/strict";
import { mkdtemp, writeFile, readFile } from "node:fs/promises";
import { resolve, join } from "node:path";
import { _electron as electron, expect } from "@playwright/test";
import { setupI18n } from "@lingui/core";
import { menuCatalogs } from "../src/locales/catalogs.ts";
import { locales } from "../src/shared/menus.ts";
import { startFixture } from "../tests/fixture.ts";
import { HostClient } from "../src/main/host/client.ts";

/** One real Host operation through production IPC, with fake I/O and substituted native confirmation only. */
export async function checkUBoot(environment) {
  const fixture = await startFixture(),
    observer = new HostClient(fixture.endpoint, fixture.token);
  const profile = await mkdtemp(resolve("test-results/uboot-")),
    preload = join(profile, "profile.cjs");
  await writeFile(
    preload,
    `const {app}=require('electron');app.setPath('userData',${JSON.stringify(profile)});app.setPath('sessionData',${JSON.stringify(profile)});`,
  );
  const env = {
    ...environment,
    CHANNELTERM_GUI_ENDPOINT: fixture.endpoint,
    CHANNELTERM_HTTP_AUTH_TOKEN: fixture.token,
  };
  delete env.ELECTRON_RUN_AS_NODE;
  let desktop;
  const payload = {
    mode: "wait_reset",
    rebootCommand: "",
    verifiedIdleBash: false,
    countdown: "Autoboot in ",
    prompt: "Board> ",
    bootStarted: ["Starting kernel"],
    interruptHex: "21",
    timeoutMS: 4000,
    capture: true,
  };
  try {
    await observer.connect();
    const id = (await observer.list())[0].session_id,
      watched = await observer.attach(id);
    desktop = await electron.launch({
      args: ["-r", preload, "--lang=en-US", "."],
      env,
    });
    const page = await desktop.firstWindow();
    const errors = [];
    page.on("pageerror", (error) => errors.push(error.message));
    await page
      .getByRole("button", { name: "Connect to Host...", exact: true })
      .click();
    await chooseSession(page, undefined, id);
    await expect(page.locator(".xterm-screen")).toBeVisible();
    await observer.read(watched.id);
    await desktop.evaluate(({ dialog }) => {
      globalThis.ubootConfirm = false;
      globalThis.ubootPreviews = [];
      dialog.showMessageBox = async (_window, options) => {
        globalThis.ubootPreviews.push(options);
        return { response: globalThis.ubootConfirm ? 1 : 0 };
      };
    });
    const invalid = await page.evaluate(() =>
      window.channelterm.startUBoot("invalid", {}),
    );
    assert.equal(invalid.ok, false);
    assert.equal(invalid.error.code, "invalid_input");
    const foreign = await page.evaluate(
      ({ id, payload }) => window.channelterm.startUBoot(id, payload),
      { id: watched.id, payload },
    );
    assert.equal(foreign.ok, false);
    assert.equal(foreign.error.code, "cancelled");
    const open = async () => {
      await page.locator('[data-menu="terminal"]').click();
      await page.locator('[role="menu"] [data-action="uboot.open"]').click();
      await expect(page.locator(".uboot-dialog")).toBeVisible();
    };
    await open();
    let modal = page.locator(".uboot-dialog");
    await expect(modal.locator('[name="uboot-mode"]')).toHaveValue(
      "wait_reset",
    );
    for (const name of ["countdown", "prompt", "interrupt"])
      await expect(modal.locator(`[name="uboot-${name}"]`)).toHaveValue("");
    await expect(
      modal.getByRole("button", { name: "Review and start", exact: true }),
    ).toBeDisabled();
    await modal.locator('[name="uboot-countdown"]').fill(payload.countdown);
    await modal.locator('[name="uboot-prompt"]').fill(payload.prompt);
    await modal
      .locator('[name="uboot-boot-started"]')
      .fill(payload.bootStarted[0]);
    await modal.locator('[name="uboot-interrupt"]').fill("2");
    await expect(
      modal.getByRole("button", { name: "Review and start", exact: true }),
    ).toBeDisabled();
    await modal.locator('[name="uboot-interrupt"]').fill("21");
    await modal.locator('[name="uboot-timeout"]').fill("4000");
    await modal.locator('[name="uboot-capture"]').check();
    await modal
      .getByRole("button", { name: "Review and start", exact: true })
      .click();
    await expect(
      modal.getByRole("button", { name: "Review and start", exact: true }),
    ).toBeEnabled();
    assert.deepEqual((await observer.read(watched.id)).data, []);
    assert.equal(
      (await page.evaluate(() => window.channelterm.ubootStatus())).value,
      undefined,
    );
    const previews = await desktop.evaluate(() => globalThis.ubootPreviews);
    assert.equal(previews[0].defaultId, 0);
    assert.equal(previews[0].cancelId, 0);
    assert.ok(previews[0].detail.includes(id));
    // Hold the renderer snapshot still. A second explicit operation cannot rely
    // on background polling to refresh the Main-owned control version.
    const idle = await page.evaluate(() => window.channelterm.list());
    assert.equal(idle.ok, true);
    await desktop.evaluate(({ ipcMain }, snapshot) => {
      ipcMain.removeHandler("host:list");
      ipcMain.handle("host:list", () => snapshot);
    }, idle);
    assert.ok(previews[0].detail.includes('"Board> "'));
    await desktop.evaluate(() => {
      globalThis.ubootConfirm = true;
    });
    await modal
      .getByRole("button", { name: "Review and start", exact: true })
      .click();
    await expect(modal.getByRole("status")).toContainText(
      "You may perform the authorized manual reset",
    );
    const running = (
      await page.evaluate(() => window.channelterm.ubootStatus())
    ).value;
    assert.equal(running.state, "ready");
    assert.match(running.operationId, /^BOOT-/);
    const stale = await page.evaluate(() =>
      window.channelterm.cancelUBoot("foreign-job"),
    );
    assert.equal(stale.ok, false);
    await fixture.command("output", "\r\nAutoboot in 3\r\n");
    let received = "";
    await expect
      .poll(async () => {
        received += Buffer.from(
          (await observer.read(watched.id)).data,
        ).toString("utf8");
        return received.includes("!");
      })
      .toBe(true);
    await fixture.command("output", "\r\nBoard> ");
    await expect(modal.getByRole("status")).toHaveText(
      "Fresh U-Boot prompt confirmed.",
    );
    const completed = (
      await page.evaluate(() => window.channelterm.ubootStatus())
    ).value;
    assert.equal(completed.outcome.bytesWritten, 1);
    assert.equal(completed.outcome.promptMatched, true);
    const archive = join(profile, "boot.zip");
    await desktop.evaluate(({ dialog }, path) => {
      dialog.showSaveDialog = async () => ({ canceled: false, filePath: path });
    }, archive);
    await modal
      .getByRole("button", { name: "Save diagnostic archive…", exact: true })
      .click();
    await expect
      .poll(async () => {
        try {
          return (await readFile(archive)).subarray(0, 2).toString();
        } catch {
          return "";
        }
      })
      .toBe("PK");
    await expect
      .poll(() =>
        modal
          .locator(".recording-content")
          .evaluate((node) => node.scrollWidth <= node.clientWidth + 1),
      )
      .toBe(true);
    await page.screenshot({ path: join(profile, "uboot-success.png") });
    await modal.getByRole("button", { name: "Close", exact: true }).click();
    await open();
    modal = page.locator(".uboot-dialog");
    await expect(modal.locator('[name="uboot-interrupt"]')).toHaveValue("");
    const fillBoard = async () => {
      await modal.locator('[name="uboot-countdown"]').fill(payload.countdown);
      await modal.locator('[name="uboot-prompt"]').fill(payload.prompt);
      await modal
        .locator('[name="uboot-boot-started"]')
        .fill(payload.bootStarted[0]);
      await modal
        .locator('[name="uboot-interrupt"]')
        .fill(payload.interruptHex);
      await modal.locator('[name="uboot-timeout"]').fill("15000");
    };
    await fillBoard();
    await modal
      .getByRole("button", { name: "Review and start", exact: true })
      .click();
    await expect(modal.getByRole("status")).toContainText(
      "You may perform the authorized manual reset",
    );
    await expect(
      modal.getByRole("button", { name: "Close", exact: true }),
    ).toBeDisabled();
    await modal
      .getByRole("button", { name: "Stop attempt", exact: true })
      .click();
    await expect
      .poll(
        async () =>
          (await page.evaluate(() => window.channelterm.ubootStatus())).value
            ?.state,
      )
      .toBe("finished");
    await expect
      .poll(async () => !!(await observer.list())[0].lease)
      .toBe(false);
    await observer.read(watched.id);
    assert.deepEqual((await observer.read(watched.id)).data, []);
    await expect(
      modal.getByRole("button", { name: "Close", exact: true }),
    ).toBeEnabled();
    // Reboot is separately confirmed; rejecting its preview sends no command or interrupt.
    await modal.locator('[name="uboot-mode"]').selectOption("reboot");
    await expect(modal.locator('[name="uboot-command"]')).toHaveValue("");
    await expect(modal.locator('[name="uboot-idle"]')).not.toBeChecked();
    await modal
      .locator('[name="uboot-command"]')
      .fill("fixture-reboot-never-executed");
    await expect(
      modal.getByRole("button", { name: "Review and start", exact: true }),
    ).toBeDisabled();
    await modal.locator('[name="uboot-idle"]').check();
    await desktop.evaluate(() => {
      globalThis.ubootConfirm = false;
    });
    await modal
      .getByRole("button", { name: "Review and start", exact: true })
      .click();
    await expect(
      modal.getByRole("button", { name: "Review and start", exact: true }),
    ).toBeEnabled();
    const rebootPreview = await desktop.evaluate(() =>
      globalThis.ubootPreviews.at(-1),
    );
    assert.ok(rebootPreview.detail.includes("fixture-reboot-never-executed"));
    assert.equal(rebootPreview.defaultId, 0);
    assert.deepEqual((await observer.read(watched.id)).data, []);
    await modal.getByRole("button", { name: "Close", exact: true }).click();
    for (const locale of locales) {
      await desktop.evaluate(
        ({ Menu }, locale) =>
          Menu.getApplicationMenu()
            .getMenuItemById(`language.${locale}`)
            .click(),
        locale,
      );
      const i18n = setupI18n({ locale, messages: menuCatalogs });
      await page.locator('[data-menu="terminal"]').click();
      await page.locator('[role="menu"] [data-action="uboot.open"]').click();
      modal = page.locator(".uboot-dialog");
      await expect(modal).toContainText(i18n._("uboot.help"));
      await desktop.evaluate(({ BrowserWindow }) =>
        BrowserWindow.getAllWindows()[0].setSize(760, 500),
      );
      await modal.locator('[name="uboot-mode"]').selectOption("reboot");
      await expect
        .poll(() =>
          modal.evaluate((node) => {
            const bounds = node.getBoundingClientRect();
            return (
              bounds.left >= 0 &&
              bounds.right <= innerWidth &&
              bounds.top >= 0 &&
              bounds.bottom <= innerHeight &&
              node.scrollWidth <= node.clientWidth + 1
            );
          }),
        )
        .toBe(true);
      await expect
        .poll(() =>
          modal
            .locator(".recording-content")
            .evaluate((node) => node.scrollWidth <= node.clientWidth + 1),
        )
        .toBe(true);
      await expect
        .poll(() =>
          modal
            .locator('[name="uboot-capture"]')
            .evaluate((node) => node.getBoundingClientRect().width),
        )
        .toBe(16);
      if (locale === "zh-CN")
        await page.screenshot({ path: join(profile, "uboot-compact.png") });
      await modal
        .getByRole("button", { name: i18n._("diagnostics.close"), exact: true })
        .click();
    }
    // Closing the owning native window during an armed wait releases only this operation.
    await desktop.evaluate(({ Menu }) =>
      Menu.getApplicationMenu().getMenuItemById("language.en").click(),
    );
    await open();
    modal = page.locator(".uboot-dialog");
    await fillBoard();
    await desktop.evaluate(() => {
      globalThis.ubootConfirm = true;
    });
    await modal
      .getByRole("button", { name: "Review and start", exact: true })
      .click();
    await expect(modal.getByRole("status")).toContainText(
      "You may perform the authorized manual reset",
    );
    assert.deepEqual(errors, []);
    await desktop.close();
    desktop = undefined;
    await expect
      .poll(async () => !!(await observer.list())[0].lease)
      .toBe(false);
    assert.equal(
      (await observer.list()).find((row) => row.session_id === id)?.state,
      "open",
    );
    assert.deepEqual((await observer.read(watched.id)).data, []);
    console.log(
      `PASS U-Boot IPC, exact input, cancellation, provenance receipt, diagnostics and nine-locale layout: ${profile}`,
    );
  } finally {
    await desktop?.close();
    await observer.disconnect();
    await fixture.stop();
  }
}
