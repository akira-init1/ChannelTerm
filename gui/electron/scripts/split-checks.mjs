import { chooseSession } from "./session-picker.mjs";
import assert from "node:assert/strict";
import { mkdtemp, writeFile } from "node:fs/promises";
import { resolve } from "node:path";
import { _electron as electron, expect } from "@playwright/test";
import { startFixture } from "../tests/fixture.ts";
import { HostClient } from "../src/main/host/client.ts";
import { setupI18n } from "@lingui/core";
import { menuCatalogs } from "../src/locales/catalogs.ts";
import { locales } from "../src/shared/menus.ts";

/** Real local tab/pane lifecycle over disposable Host Sessions; no physical I/O or input replay. */
export async function checkSplits(environment) {
  const fixture = await startFixture();
  const observer = new HostClient(fixture.endpoint, fixture.token);
  const profile = await mkdtemp(resolve("test-results/split-profile-"));
  const preload = resolve(profile, "profile.cjs");
  await writeFile(
    preload,
    `const {app}=require('electron');app.setPath('userData',${JSON.stringify(profile)});app.setPath('sessionData',${JSON.stringify(profile)});`,
  );
  const options = {
    args: ["-r", preload, "--lang=en-US", "."],
    env: {
      ...environment,
      CHANNELTERM_GUI_ENDPOINT: fixture.endpoint,
      CHANNELTERM_HTTP_AUTH_TOKEN: fixture.token,
    },
  };
  delete options.env.ELECTRON_RUN_AS_NODE;
  let desktop, page;
  const errors = [];
  const native = async (id) => {
    await page.bringToFront();
    // CDP page activation alone does not guarantee OS focus after a window move.
    // Native application-menu callbacks require their owning window to be focused.
    const window = await desktop.browserWindow(page);
    try {
      await window.evaluate((window) => window.focus());
      await expect
        .poll(() => window.evaluate((window) => window.isFocused()))
        .toBe(true);
    } finally {
      await window.dispose();
    }
    await expect
      .poll(() =>
        desktop.evaluate(
          ({ Menu }, id) =>
            Menu.getApplicationMenu()?.getMenuItemById(id)?.enabled,
          id,
        ),
      )
      .toBe(true);
    await desktop.evaluate(
      ({ Menu }, id) => Menu.getApplicationMenu().getMenuItemById(id).click(),
      id,
    );
  };
  const launch = async () => {
    desktop = await electron.launch(options);
    page = await desktop.firstWindow();
    page.on("pageerror", (error) => errors.push(error.message));
    await expect(page.locator('[data-menu="window"]')).toBeVisible();
  };
  const focusedAttachment = async (previousId) => {
    let attachment;
    await expect
      .poll(async () => {
        await page.evaluate(() => {
          globalThis.splitAction = undefined;
          const stop = window.channelterm.onMenuAction((action) => {
            if (action.id === "terminal.focus") {
              globalThis.splitAction = action;
              stop();
            }
          });
        });
        await native("terminal.focus");
        await page.waitForFunction(() => globalThis.splitAction);
        attachment = await page.evaluate(
          () => globalThis.splitAction.attachment,
        );
        // Native menu snapshots cross asynchronous IPC; enabled alone is not a new-target barrier.
        return !!attachment && attachment.id !== previousId;
      })
      .toBe(true);
    return attachment;
  };
  const panes = () => page.locator(".terminal-pane:not([hidden])");
  const textarea = (pane) => pane.locator(".xterm-helper-textarea");
  try {
    await observer.connect();
    const first = (await observer.list())[0];
    const firstObserver = await observer.attach(first.session_id);
    const second = await observer.openConnection({
      kind: "serial",
      port: "COM-FIXTURE",
      baud: 115200,
      dataBits: 8,
      parity: "none",
      stopBits: "1",
      flowControl: "none",
      label: "Split second",
    });
    const secondObserver = await observer.attach(second.sessionId);
    await launch();
    await native("host.connect");
    await chooseSession(page, "Isolated echo");
    const original = await focusedAttachment();
    await native("tab.splitRight");
    await expect(panes()).toHaveCount(2);
    const duplicate = await focusedAttachment(original.id);
    assert.notEqual(duplicate.id, original.id);
    assert.equal(duplicate.sessionId, original.sessionId);
    await fixture.command("output", "\r\nindependent-split-cursors\r\n");
    for (let i = 0; i < 2; i++)
      await expect(panes().nth(i).locator(".xterm-rows")).toContainText(
        "independent-split-cursors",
      );
    await fixture.command("lease");
    await expect(page.locator(".notice-lease")).toBeVisible();
    for (let i = 0; i < 2; i++) {
      await textarea(panes().nth(i)).focus();
      await page.keyboard.type(`blocked-pane-${i}`);
    }
    await fixture.command("release");
    await expect(page.locator(".notice-lease")).toHaveCount(0);
    assert.doesNotMatch(
      Buffer.from((await observer.read(firstObserver.id)).data).toString(),
      /blocked-pane/,
    );
    await native("tab.splitDown");
    await expect(page.locator(".terminal-layout")).toHaveClass(/split-rows/);
    assert.equal(
      await page.locator(".terminal-tab").count(),
      2,
      "orientation reuses views",
    );
    await native("view.single");
    await expect(panes()).toHaveCount(1);
    assert.equal(
      await page.locator(".terminal-tab").count(),
      2,
      "single view retains the other attachment",
    );
    await native("tab.splitRight");
    await expect(panes()).toHaveCount(2);
    // Splitting again explicitly creates a new observer; the former hidden observer remains a tab.
    await chooseSession(page, "Split second");
    await expect(page.locator(".terminal-tab.active")).toContainText(
      "Split second",
    );
    await expect(panes()).toHaveCount(2);
    const left = panes().filter({
      has: page.locator(".split-pane-heading", { hasText: "Isolated echo" }),
    });
    const right = panes().filter({
      has: page.locator(".split-pane-heading", { hasText: "Split second" }),
    });
    await Promise.all([
      observer.read(firstObserver.id),
      observer.read(secondObserver.id),
    ]);
    await textarea(left).focus();
    await page.keyboard.type("only-first-pane");
    await textarea(left).evaluate((node) => {
      node.value = "UNSENT-SPLIT-DRAFT";
    });
    await textarea(right).focus();
    await page.keyboard.type("only-second-pane");
    // Keystrokes are individual asynchronous writes. One Host read can contain
    // a partial payload while the remaining keystrokes are still in flight.
    for (const [handle, expected] of [
      [firstObserver, "only-first-pane"],
      [secondObserver, "only-second-pane"],
    ]) {
      let received = "";
      await expect
        .poll(async () => {
          received += Buffer.from(
            (await observer.read(handle.id)).data,
          ).toString();
          return received;
        })
        .toBe(expected);
    }
    const before = await page
      .locator(".terminal-tab")
      .evaluateAll((nodes) => nodes.map((node) => node.dataset.tabId));
    const last = page.locator('.terminal-tab [role="tab"]').last();
    await last.focus();
    await page.keyboard.press("Shift+ArrowLeft");
    const keyboardOrder = await page
      .locator(".terminal-tab")
      .evaluateAll((nodes) => nodes.map((node) => node.dataset.tabId));
    assert.equal(keyboardOrder.at(-2), before.at(-1));
    assert.equal(
      await last.evaluate((node) => node === document.activeElement),
      false,
      "index-based locator now identifies the neighbour",
    );
    const selected = page.locator('.terminal-tab.active [role="tab"]');
    await expect(selected).toBeFocused();
    await selected.evaluate((node) =>
      node.dispatchEvent(
        new KeyboardEvent("keydown", {
          key: "ArrowLeft",
          shiftKey: true,
          isComposing: true,
          bubbles: true,
        }),
      ),
    );
    assert.deepEqual(
      await page
        .locator(".terminal-tab")
        .evaluateAll((nodes) => nodes.map((node) => node.dataset.tabId)),
      keyboardOrder,
    );
    await page
      .locator(".terminal-tab")
      .last()
      .dragTo(page.locator(".terminal-tab").first(), {
        sourcePosition: { x: 4, y: 8 },
        targetPosition: { x: 4, y: 8 },
      });
    const dragged = await page
      .locator(".terminal-tab")
      .evaluateAll((nodes) => nodes.map((node) => node.dataset.tabId));
    assert.equal(dragged[0], keyboardOrder.at(-1));
    await native("tab.splitDown");
    const size = await desktop.evaluate(({ BrowserWindow }) => {
      const w = BrowserWindow.getAllWindows()[0];
      const size = w.getSize();
      w.setSize(760, 700);
      return size;
    });
    for (const locale of locales) {
      const i18n = setupI18n({ locale, messages: menuCatalogs });
      await native(`language.${locale}`);
      await expect(
        page.locator(".focused-pane .split-pane-heading"),
      ).toContainText(i18n._("split.focused"));
      assert.equal(
        await page.evaluate(
          () => document.documentElement.scrollWidth <= innerWidth,
        ),
        true,
        `${locale} split overflows`,
      );
    }
    await page.screenshot({ path: "test-results/split-compact.png" });
    await native("language.en");
    await desktop.evaluate(
      ({ BrowserWindow }, size) =>
        BrowserWindow.getAllWindows()[0].setSize(...size),
      size,
    );
    await expect
      .poll(() =>
        page.evaluate(
          () =>
            JSON.parse(localStorage.getItem("channelterm.workspace.v1.0"))
              ?.split?.axis,
        ),
      )
      .toBe("rows");
    const saved = await page.evaluate(() =>
      JSON.parse(localStorage.getItem("channelterm.workspace.v1.0")),
    );
    assert.ok(!JSON.stringify(saved).includes("UNSENT-SPLIT-DRAFT"));
    await page.screenshot({ path: "test-results/split-views.png" });
    await desktop.close();
    desktop = undefined;
    await launch();
    await native("host.connect");
    await expect(panes()).toHaveCount(2);
    await expect(page.locator(".terminal-layout")).toHaveClass(/split-rows/);
    await expect
      .poll(() =>
        page.evaluate(
          () =>
            JSON.parse(localStorage.getItem("channelterm.workspace.v1.0"))?.tabs
              ?.length,
        ),
      )
      .toBe(saved.tabs.length);
    assert.deepEqual(
      (
        await page.evaluate(() =>
          JSON.parse(localStorage.getItem("channelterm.workspace.v1.0")),
        )
      ).tabs.map((tab) => tab.target.sessionId),
      saved.tabs.map((tab) => tab.target.sessionId),
    );
    assert.doesNotMatch(
      Buffer.from((await observer.read(firstObserver.id)).data).toString(),
      /UNSENT-SPLIT-DRAFT/,
    );
    await page
      .locator(".focused-pane .split-pane-heading")
      .getByRole("button", { name: "Close tab (detach)", exact: true })
      .click();
    await expect(panes()).toHaveCount(1);
    assert.equal(
      (await observer.list()).length,
      2,
      "closing one pane preserves both shared Sessions",
    );
    const ssh = await observer.openConnection({
      kind: "ssh",
      host: "127.0.0.1",
      port: fixture.sshPort,
      user: "fixture-user",
      password: "fixture-password",
      identityFiles: [],
      knownHostsFile: fixture.knownHosts,
      label: "Split SSH",
    });
    const owner = await observer.attach(ssh.sessionId);
    assert.equal(owner.ownsRemoteSize, true);
    await native("session.refresh");
    await chooseSession(page, "Split SSH");
    const observing = await focusedAttachment();
    assert.equal(observing.ownsRemoteSize, false);
    const originalTabId = await page
      .locator(".terminal-tab.active")
      .getAttribute("data-tab-id");
    await native("tab.splitRight");
    const sshDuplicate = await focusedAttachment(observing.id);
    const duplicateTabId = await page
      .locator(".terminal-tab.active")
      .getAttribute("data-tab-id");
    assert.notEqual(duplicateTabId, originalTabId);
    // CSS pane order deliberately survives tab reordering; DOM index is not pane identity.
    const originalPane = page.locator(
      `.terminal-pane[data-pane-tab="${originalTabId}"]`,
    );
    const duplicatePane = page.locator(
      `.terminal-pane[data-pane-tab="${duplicateTabId}"]`,
    );
    assert.equal(sshDuplicate.ownsRemoteSize, false);
    await expect(panes()).toHaveCount(2);
    await desktop.evaluate(({ BrowserWindow }) =>
      BrowserWindow.getAllWindows()[0].setSize(980, 620),
    );
    for (let i = 0; i < 2; i++)
      await expect(
        panes().nth(i).locator(".remote-size-controls"),
      ).toContainText("Local fitting only");
    assert.equal(
      await observer.resizeControl(owner.id, "status"),
      true,
      "split observers cannot seize the existing PTY size owner",
    );
    await originalPane
      .getByRole("button", {
        name: "Use this view for remote size",
        exact: true,
      })
      .click();
    await expect(originalPane.locator(".remote-size-controls")).toContainText(
      "Remote size follows this view",
    );
    // Taking control schedules the pane's initial resize. Host can reject a concurrent
    // observation as busy; wait only on this read-only status, never replay take/resize.
    await expect
      .poll(async () => {
        try {
          return await observer.resizeControl(owner.id, "status");
        } catch (error) {
          if (error.code === "busy") return "busy";
          throw error;
        }
      })
      .toBe(false);
    await expect(duplicatePane.locator(".remote-size-controls")).toContainText(
      "Local fitting only",
    );
    const resized = await page.evaluate(
      (id) => window.channelterm.resize(id, { columns: 80, rows: 24 }),
      sshDuplicate.id,
    );
    assert.equal(resized.ok, true);
    assert.equal(
      resized.value,
      false,
      "a duplicate remains an observer after another pane takes size ownership",
    );
    // Moving a visible pane removes only that view from this split; its sibling and shared Session survive.
    await duplicatePane.locator(".split-pane-heading button").first().click();
    const nextWindow = desktop.waitForEvent("window");
    await native("tab.move");
    const destination = await nextWindow;
    destination.on("pageerror", (error) => errors.push(error.message));
    await expect(destination.locator(".terminal-tab.active")).toContainText(
      "Split SSH",
    );
    await expect(panes()).toHaveCount(1);
    await expect(panes().locator(".remote-size-controls")).toContainText(
      "Remote size follows this view",
    );
    await destination.close();
    assert.ok(
      (await observer.list()).some(
        (session) =>
          session.session_id === ssh.sessionId && session.state === "open",
      ),
    );
    // Replace only the test process's attach handler after all successful integration checks.
    // A failed duplicate must leave the original view, focus and remote-size capability intact.
    const retained = await focusedAttachment();
    const retainedTabs = await page.locator(".terminal-tab").count();
    await desktop.evaluate(({ ipcMain }) => {
      ipcMain.removeHandler("host:attach");
      ipcMain.handle("host:attach", () => ({
        ok: false,
        error: {
          code: "unavailable",
          message: "Test split attachment failure",
        },
      }));
    });
    await native("tab.splitRight");
    await expect(page.locator(".notices")).toContainText(
      "The operation failed. Check the target and Host diagnostics",
    );
    await expect(panes()).toHaveCount(1);
    await expect(page.locator(".terminal-tab")).toHaveCount(retainedTabs);
    assert.deepEqual(await focusedAttachment(), retained);
    await expect(panes().locator(".remote-size-controls")).toContainText(
      "Remote size follows this view",
    );
    await page
      .getByRole("button", { name: "Resume input", exact: true })
      .click();
    await textarea(panes()).focus();
    await page.keyboard.type("after-failed-split");
    let survivingOutput = "";
    await expect
      .poll(async () => {
        survivingOutput += Buffer.from(
          (await observer.read(owner.id)).data,
        ).toString();
        return survivingOutput;
      })
      .toContain("after-failed-split");
    assert.deepEqual(errors, []);
    console.log(
      "PASS split views: local drag/keyboard order, independent cursors/input, lease blocking, layouts, nine locales, restart, view close/move, failed split rollback and explicit SSH size ownership",
    );
  } finally {
    await desktop?.close();
    await observer.disconnect();
    await fixture.stop();
  }
}
