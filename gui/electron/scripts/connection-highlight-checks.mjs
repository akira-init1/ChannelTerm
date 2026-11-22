import assert from "node:assert/strict";
import { mkdtemp, writeFile } from "node:fs/promises";
import { resolve } from "node:path";
import { _electron as electron, expect } from "@playwright/test";
import { setupI18n } from "@lingui/core";
import { menuCatalogs } from "../src/locales/catalogs.ts";
import { locales } from "../src/shared/menus.ts";
import { startFixture } from "../tests/fixture.ts";
import { HostClient } from "../src/main/host/client.ts";

/** Saved-connection styling over real IPC and fake serial I/O, without terminal-byte mutation. */
export async function checkConnectionHighlights(environment) {
  const fixture = await startFixture();
  const observer = new HostClient(fixture.endpoint, fixture.token);
  const profile = await mkdtemp(resolve("test-results/highlight-profile-"));
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
    await expect(page.locator('[data-menu="session"]')).toBeVisible();
    // Native menu actions target the focused BrowserWindow, including after a relaunch.
    const window = await desktop.browserWindow(page);
    try {
      await window.evaluate((window) => window.focus());
      await expect
        .poll(() => window.evaluate((window) => window.isFocused()))
        .toBe(true);
    } finally {
      await window.dispose();
    }
    await native("host.connect");
    await expect(page.locator(".host-panel")).toContainText("Host connected");
  };
  const form = () => page.locator('dialog[aria-labelledby="connection-title"]');
  const editor = () => page.locator(".highlight-dialog");
  const rules = () => editor().locator(".highlight-rule");
  const records = () =>
    page.evaluate(() =>
      JSON.parse(localStorage.getItem("channelterm.connections.v1")),
    );
  const edit = async () => {
    await native("connection.new");
    await form().locator(".connection-history summary").click();
    await form()
      .getByRole("button", { name: "Keyword highlights…", exact: true })
      .first()
      .click();
    await expect(editor()).toBeVisible();
  };
  const add = async (text, color) => {
    await editor()
      .getByRole("button", { name: "Add rule", exact: true })
      .click();
    await rules().last().getByLabel("Literal text", { exact: true }).fill(text);
    await rules().last().locator('input[type="color"]').fill(color);
  };
  const closeForm = async () => {
    await form().getByRole("button", { name: "Cancel", exact: true }).click();
    await expect(form()).toHaveCount(0);
  };
  try {
    await observer.connect();
    await launch();
    await native("connection.new");
    await form().locator('[name="port"]').fill("COM-FIXTURE");
    await form().locator('[name="label"]').fill("Highlight board");
    await form().getByRole("button", { name: "Connect", exact: true }).click();
    await expect(form()).toHaveCount(0);
    await expect(page.locator(".terminal-tab.active")).toContainText(
      "Highlight board",
    );
    const saved = (await records())[0];
    assert.match(saved.id, /^[a-f0-9-]{36}$/);
    const session = (await observer.list()).find(
      (entry) => entry.label === "Highlight board",
    );
    assert.ok(session);
    const watching = await observer.attach(session.session_id);
    await observer.read(watching.id);
    await edit();
    await add("ERROR设备😀", "#123456");
    await add("设备😀 ready", "#abcdef");
    await rules()
      .last()
      .getByRole("button", { name: "Move rule up", exact: true })
      .click();
    await expect(
      rules().first().getByLabel("Literal text", { exact: true }),
    ).toHaveValue("设备😀 ready");
    await rules()
      .first()
      .getByRole("button", { name: "Move rule down", exact: true })
      .click();
    await rules()
      .last()
      .getByRole("checkbox", { name: "Enabled", exact: true })
      .uncheck();
    await rules()
      .last()
      .getByRole("checkbox", { name: "Enabled", exact: true })
      .check();
    await add("remove-me", "#ffffff");
    await rules()
      .last()
      .getByRole("button", { name: "Remove rule", exact: true })
      .click();
    await editor().locator(".highlight-templates summary").click();
    await editor()
      .getByLabel("Template name", { exact: true })
      .fill("Reusable board");
    await editor()
      .getByRole("button", { name: "Save as new template", exact: true })
      .click();
    await editor()
      .getByRole("button", { name: "Clear this list", exact: true })
      .click();
    await expect(rules()).toHaveCount(0);
    await editor()
      .getByRole("button", { name: "Import copies", exact: true })
      .click();
    await expect(rules()).toHaveCount(2);
    await editor()
      .getByRole("button", { name: "Save connection rules", exact: true })
      .click();
    await expect(editor()).toHaveCount(0);
    await closeForm();
    assert.equal((await records())[0].id, saved.id);
    assert.equal((await records())[0].highlights.length, 2);
    const text = "\r\nERROR设备😀 ready\r\n";
    await observer.write(watching.id, text);
    let raw = "";
    await expect
      .poll(async () => {
        raw += Buffer.from((await observer.read(watching.id)).data).toString();
        return raw;
      })
      .toBe(text);
    const colored = () =>
      page
        .locator(".terminal-pane:not([hidden]) .xterm-rows span")
        .evaluateAll((nodes) =>
          nodes
            .filter(
              (node) => getComputedStyle(node).color === "rgb(18, 52, 86)",
            )
            .map((node) => node.textContent)
            .join("")
            .includes("ERROR设备😀"),
        );
    await expect
      .poll(colored)
      .toBe(true)
      .catch(async (error) => {
        console.log(
          await page.evaluate(() => ({
            workspace: localStorage.getItem("channelterm.workspace.v1.0"),
            profiles: localStorage.getItem("channelterm.connections.v1"),
            cells: Array.from(
              document.querySelectorAll(
                ".terminal-pane:not([hidden]) .xterm-rows span",
              ),
              (node) => ({
                text: node.textContent,
                color: getComputedStyle(node).color,
              }),
            )
              .filter((node) => node.text?.trim())
              .slice(-50),
          })),
        );
        await page.screenshot({
          path: "test-results/connection-highlights-failure.png",
        });
        throw error;
      });
    await page.locator(".xterm-helper-textarea:visible").focus();
    await page.keyboard.press("Control+Shift+f");
    const search = page.getByRole("search", {
      name: "Find in terminal",
      exact: true,
    });
    await search.getByRole("searchbox").fill("ERROR设备");
    await expect(search.getByRole("status")).toHaveText("1/1");
    await search.getByRole("searchbox").press("Escape");
    await expect.poll(colored).toBe(true);

    await edit();
    await editor().locator(".highlight-templates summary").click();
    await editor()
      .getByRole("combobox", { name: "Template", exact: true })
      .selectOption({ label: "Reusable board" });
    await rules()
      .first()
      .getByLabel("Literal text", { exact: true })
      .fill("template-only-change");
    await editor()
      .getByRole("button", { name: "Replace selected template", exact: true })
      .click();
    await editor()
      .getByRole("button", { name: "Delete selected template", exact: true })
      .click();
    assert.deepEqual(
      await page.evaluate(() =>
        JSON.parse(localStorage.getItem("channelterm.highlight-templates.v1")),
      ),
      [],
    );
    await editor().getByRole("button", { name: "Cancel", exact: true }).click();
    await closeForm();
    assert.equal(
      (await records())[0].highlights[0].text,
      "ERROR设备😀",
      "editing a template cannot mutate an imported connection",
    );

    await edit();
    const beforeFailure = JSON.stringify(await records());
    await page.evaluate(() => {
      globalThis.originalStorageWrite = Storage.prototype.setItem;
      Storage.prototype.setItem = function (key, value) {
        if (key === "channelterm.connections.v1")
          throw new DOMException("Test quota", "QuotaExceededError");
        return globalThis.originalStorageWrite.call(this, key, value);
      };
    });
    await rules()
      .first()
      .getByRole("checkbox", { name: "Enabled", exact: true })
      .uncheck();
    await editor()
      .getByRole("button", { name: "Save connection rules", exact: true })
      .click();
    await expect(editor().getByRole("alert")).toContainText(
      "Cannot save or load",
    );
    assert.equal(JSON.stringify(await records()), beforeFailure);
    await page.evaluate(() => {
      Storage.prototype.setItem = globalThis.originalStorageWrite;
    });
    await rules()
      .first()
      .getByRole("checkbox", { name: "Enabled", exact: true })
      .check();
    await desktop.evaluate(({ BrowserWindow }) =>
      BrowserWindow.getAllWindows()[0].setSize(760, 700),
    );
    for (const locale of locales) {
      const i18n = setupI18n({ locale, messages: menuCatalogs });
      await native(`language.${locale}`);
      await expect(editor().locator("h2")).toContainText(
        i18n._("highlight.title"),
      );
      assert.equal(
        await editor().evaluate((node) => node.scrollWidth <= node.clientWidth),
        true,
        `${locale}: editor overflow`,
      );
    }
    await page.screenshot({
      path: "test-results/connection-highlights-compact.png",
    });
    await native("language.en");
    await editor()
      .getByRole("button", { name: "Save connection rules", exact: true })
      .click();
    await closeForm();
    await desktop.close();
    desktop = undefined;
    await launch();
    await expect(page.locator(".terminal-tab.active")).toContainText(
      "Highlight board",
    );
    await expect.poll(colored).toBe(true);
    assert.equal((await records())[0].id, saved.id);
    assert.equal((await records())[0].highlights[0].text, "ERROR设备😀");
    // The same physical endpoint under another saved preset has a different local identity.
    await native("connection.new");
    await form().locator('[name="port"]').fill("COM-FIXTURE");
    await form().locator('[name="label"]').fill("Second saved preset");
    await form().getByRole("button", { name: "Connect", exact: true }).click();
    await expect(form()).toHaveCount(0);
    const other = (await records()).find(
      (entry) => entry.request.label === "Second saved preset",
    );
    assert.ok(other);
    assert.notEqual(other.id, saved.id);
    assert.equal(other.highlights, undefined);
    await expect(page.locator(".xterm-rows:visible")).toContainText(
      "ERROR设备",
    );
    await expect.poll(colored).toBe(false);
    await native("connection.new");
    await form().locator(".connection-history summary").click();
    await form()
      .getByRole("button", { name: "Highlight board", exact: true })
      .click();
    await form().getByRole("button", { name: "Connect", exact: true }).click();
    await expect(form()).toHaveCount(0);
    await expect.poll(colored).toBe(true);
    assert.equal(
      (await records()).find(
        (entry) => entry.request.label === "Highlight board",
      ).id,
      saved.id,
    );
    await native("tab.splitRight");
    await expect(page.locator(".terminal-pane:not([hidden])")).toHaveCount(2);
    await expect
      .poll(() =>
        page
          .locator(".terminal-pane:not([hidden]) .xterm-rows")
          .evaluateAll((rows) =>
            rows.every((row) =>
              Array.from(row.querySelectorAll("span"))
                .filter(
                  (node) => getComputedStyle(node).color === "rgb(18, 52, 86)",
                )
                .map((node) => node.textContent)
                .join("")
                .includes("ERROR设备😀"),
            ),
          ),
      )
      .toBe(true);
    const movedWindow = desktop.waitForEvent("window");
    await native("tab.move");
    const moved = await movedWindow;
    moved.on("pageerror", (error) => errors.push(error.message));
    await expect(moved.locator(".terminal-tab.active")).toContainText(
      "Highlight board",
    );
    await expect
      .poll(() =>
        moved.locator(".xterm-rows span").evaluateAll((nodes) =>
          nodes
            .filter(
              (node) => getComputedStyle(node).color === "rgb(18, 52, 86)",
            )
            .map((node) => node.textContent)
            .join("")
            .includes("ERROR设备😀"),
        ),
      )
      .toBe(true);
    await moved.close();
    await expect(page.locator(".terminal-pane:not([hidden])")).toHaveCount(1);
    assert.ok(
      (await observer.list()).some(
        (entry) =>
          entry.session_id === session.session_id && entry.state === "open",
      ),
    );
    assert.deepEqual(errors, []);
    console.log(
      "PASS connection highlights: native editor/order/enable/remove/clear, independent templates, raw-byte preservation, search, storage failure recovery, nine locales, restart, split and moved-window association",
    );
  } finally {
    await desktop?.close();
    await observer.disconnect();
    await fixture.stop();
  }
}
