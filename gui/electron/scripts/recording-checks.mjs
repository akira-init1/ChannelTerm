import { chooseSession } from "./session-picker.mjs";
import assert from "node:assert/strict";
import { mkdtemp, writeFile, readFile, readdir } from "node:fs/promises";
import { resolve, join } from "node:path";
import { _electron as electron, expect } from "@playwright/test";
import { setupI18n } from "@lingui/core";
import { menuCatalogs } from "../src/locales/catalogs.ts";
import { locales } from "../src/shared/menus.ts";
import { startFixture } from "../tests/fixture.ts";
import { HostClient } from "../src/main/host/client.ts";

/** Native capture lifecycle with isolated destinations; picker/trash are substituted, never user files. */
export async function checkRecordings(environment) {
  const fixture = await startFixture();
  const observer = new HostClient(fixture.endpoint, fixture.token);
  const profile = await mkdtemp(resolve("test-results/recording-profile-"));
  const preload = join(profile, "profile.cjs");
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
  const records = (page) =>
    page.evaluate(async () => (await window.channelterm.recordings()).value);
  async function action(page, group, id) {
    await page.bringToFront();
    await page.locator(`[data-menu="${group}"]`).click();
    await page.locator(`[role=menu] [data-action="${id}"]`).click();
  }
  try {
    await observer.connect();
    const id = (await observer.list())[0].session_id;
    const watched = await observer.attach(id);
    desktop = await electron.launch({
      args: ["-r", preload, "--lang=en-US", "."],
      env,
    });
    const page = await desktop.firstWindow();
    await page
      .getByRole("button", { name: "Connect to Host...", exact: true })
      .click();
    await chooseSession(page, "Isolated echo");
    await expect(page.locator(".xterm-screen")).toBeVisible();
    await observer.write(watched.id, "before-recording-must-not-be-exported");
    await desktop.evaluate(({ dialog }, directory) => {
      dialog.showOpenDialog = async () => ({
        canceled: false,
        filePaths: [directory],
      });
    }, profile);
    const bad = await page.evaluate(
      (id) =>
        window.channelterm.startRecording({
          attachmentId: id,
          format: "raw",
          segmentMiB: 1,
          segments: 2,
        }),
      watched.id,
    );
    assert.equal(bad.ok, false);
    await action(page, "terminal", "logs.open");
    const modal = page.locator(".recording-dialog");
    await modal
      .getByRole("button", { name: "Choose folder and start recording" })
      .click();
    await expect
      .poll(async () => (await records(page))[0]?.state)
      .toBe("recording");
    const raw = (await records(page))[0];
    const dataPath = join(raw.directory, "00000000.bin");
    await observer.write(watched.id, "recorded-中文\r\n");
    await expect
      .poll(async () => (await readFile(dataPath)).toString())
      .toContain("recorded-中文");
    assert.doesNotMatch(
      (await readFile(dataPath)).toString(),
      /before-recording/,
    );
    await modal.getByRole("button", { name: "Close", exact: true }).click();
    await action(page, "window", "tab.close");
    await expect(page.locator(".xterm-screen")).toHaveCount(0);
    await observer.write(watched.id, "after-tab-close\n");
    await expect
      .poll(async () => (await readFile(dataPath)).toString())
      .toContain("after-tab-close");
    await action(page, "terminal", "logs.open");
    await expect(
      modal.getByRole("button", { name: "Choose folder and start recording" }),
    ).toBeDisabled();
    await modal
      .getByRole("button", { name: "Stop recording", exact: true })
      .click();
    await expect
      .poll(async () => (await records(page))[0].state)
      .toBe("stopped");
    const stopped = await readFile(dataPath);
    await observer.write(watched.id, "after-stop-must-not-be-recorded");
    assert.deepEqual(await readFile(dataPath), stopped);
    assert.equal(
      JSON.parse(await readFile(join(raw.directory, "manifest.json"), "utf8"))
        .state,
      "stopped",
    );
    await desktop.evaluate(({ dialog, shell }, path) => {
      dialog.showOpenDialog = async () => ({
        canceled: false,
        filePaths: [path],
      });
      dialog.showMessageBox = async () => ({ response: 0 });
      globalThis.trashedLog = undefined;
      shell.trashItem = async (value) => {
        globalThis.trashedLog = value;
      };
    }, dataPath);
    await modal
      .getByRole("button", { name: "Choose a local log file to delete…" })
      .click();
    assert.equal(
      await desktop.evaluate(() => globalThis.trashedLog),
      undefined,
    );
    await desktop.evaluate(({ dialog }) => {
      dialog.showMessageBox = async () => ({ response: 1 });
    });
    await modal
      .getByRole("button", { name: "Choose a local log file to delete…" })
      .click();
    await expect
      .poll(() => desktop.evaluate(() => globalThis.trashedLog))
      .toBe(dataPath);
    await modal.getByRole("button", { name: "Close", exact: true }).click();
    await chooseSession(page, "Isolated echo");
    await desktop.evaluate(({ dialog }, directory) => {
      dialog.showOpenDialog = async () => ({
        canceled: false,
        filePaths: [directory],
      });
    }, profile);
    await action(page, "terminal", "logs.open");
    await modal
      .getByRole("combobox", { name: "Recording format" })
      .selectOption("text");
    await modal
      .getByRole("button", { name: "Choose folder and start recording" })
      .click();
    await expect
      .poll(async () => (await records(page))[1]?.state)
      .toBe("recording");
    const text = (await records(page))[1];
    const textPath = join(text.directory, "00000000.txt");
    await observer.write(watched.id, "\u001b[31mtext-😀\n");
    await expect
      .poll(async () => (await readFile(textPath)).toString())
      .toContain("\\x1b[31mtext-😀");
    await desktop.evaluate(
      ({ dialog }, path) => {
        dialog.showOpenDialog = async () => ({
          canceled: false,
          filePaths: [path],
        });
      },
      process.platform === "win32" ? textPath.toUpperCase() : textPath,
    );
    const activeDelete = await page.evaluate(() =>
      window.channelterm.deleteRecordingFile(),
    );
    assert.equal(activeDelete.ok, false);
    assert.equal(activeDelete.error.code, "busy");
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
      await action(page, "terminal", "logs.open");
      await expect(modal.locator("h2")).toHaveText(i18n._("logs.open"));
      await expect(modal).toContainText(i18n._("logs.warning"));
      if (locale === "zh-CN") {
        const window = await desktop.browserWindow(page);
        await window.evaluate((window) => window.setSize(760, 500));
        await expect(modal.locator("footer button").last()).toBeInViewport({
          ratio: 1,
        });
        await page.screenshot({ path: "test-results/recording-compact.png" });
      }
      await modal
        .getByRole("button", { name: i18n._("diagnostics.close"), exact: true })
        .click();
    }
    await desktop.close();
    desktop = undefined;
    assert.equal(
      JSON.parse(await readFile(join(text.directory, "manifest.json"), "utf8"))
        .state,
      "stopped",
    );
    assert.ok(
      (await observer.list()).some((session) => session.session_id === id),
    );
    assert.ok(
      (await readdir(profile)).some((name) =>
        name.startsWith("channelterm-log-"),
      ),
    );
    // Abruptly exit only this disposable Electron process, bypassing normal cleanup.
    // This checks recovery markers, not power-loss durability on another filesystem.
    desktop = await electron.launch({
      args: ["-r", preload, "--lang=en-US", "."],
      env,
    });
    const abruptPage = await desktop.firstWindow();
    await expect
      .poll(
        async () =>
          (await abruptPage.evaluate(() => window.channelterm.info())).ok,
      )
      .toBe(true);
    assert.equal(
      (await abruptPage.evaluate(() => window.channelterm.connect())).ok,
      true,
    );
    const abruptView = (
      await abruptPage.evaluate((id) => window.channelterm.attach(id), id)
    ).value;
    await desktop.evaluate(({ dialog }, directory) => {
      dialog.showOpenDialog = async () => ({
        canceled: false,
        filePaths: [directory],
      });
    }, profile);
    assert.equal(
      (
        await abruptPage.evaluate(
          (attachmentId) =>
            window.channelterm.startRecording({
              attachmentId,
              format: "raw",
              segmentMiB: 1,
              segments: 1,
            }),
          abruptView.id,
        )
      ).ok,
      true,
    );
    const abrupt = (await records(abruptPage))[0];
    await observer.write(watched.id, "synced-before-abrupt-exit");
    await expect
      .poll(
        async () =>
          await readFile(join(abrupt.directory, "00000000.bin.jsonl"), "utf8"),
      )
      .toContain('"type":"output"');
    await desktop.evaluate(({ app }) => app.exit(42)).catch(() => {});
    desktop = undefined;
    const crashManifest = JSON.parse(
      await readFile(join(abrupt.directory, "manifest.json"), "utf8"),
    );
    assert.equal(crashManifest.state, "recording");
    const crashData = await readFile(join(abrupt.directory, "00000000.bin"));
    const crashIndex = (
      await readFile(join(abrupt.directory, "00000000.bin.jsonl"), "utf8")
    )
      .trim()
      .split("\n")
      .map((line) => JSON.parse(line));
    for (const entry of crashIndex.slice(1))
      assert.ok(entry.offset + entry.bytes <= crashData.length);
    assert.ok(
      (await observer.list()).some((session) => session.session_id === id),
    );
    console.log(
      "PASS continuous raw/text recording, independent closed-tab capture, durable stop, stale/foreign target rejection, explicit selected-file deletion, active-file protection, nine locales and native exit/shared Session survival",
    );
  } finally {
    await desktop?.close();
    await observer.disconnect();
    await fixture.stop();
  }
}
