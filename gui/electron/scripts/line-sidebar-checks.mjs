import assert from "node:assert/strict";
import { mkdtemp, writeFile } from "node:fs/promises";
import { resolve } from "node:path";
import { _electron as electron, expect } from "@playwright/test";
import { setupI18n } from "@lingui/core";
import { menuCatalogs } from "../src/locales/catalogs.ts";
import { locales } from "../src/shared/menus.ts";
import { startFixture } from "../tests/fixture.ts";
import { HostClient } from "../src/main/host/client.ts";

/** Real public xterm geometry and metadata over isolated Host output; never hardware I/O. */
export async function checkLineSidebar(environment) {
  const fixture = await startFixture();
  const observer = new HostClient(fixture.endpoint, fixture.token);
  const profile = await mkdtemp(resolve("test-results/line-profile-"));
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
    await expect(page.locator('[data-menu="terminal"]')).toBeVisible();
    await native("host.connect");
    await expect(page.locator(".host-panel")).toContainText("Host connected");
  };
  const pane = () => page.locator(".terminal-pane:not([hidden])");
  const rows = () => pane().locator(".xterm-rows");
  const gutter = () => pane().locator(".line-sidebar");
  const snapshot = () =>
    pane().evaluate((node) => {
      const textRows = [...node.querySelectorAll(".xterm-rows > div")];
      return [...node.querySelectorAll(".line-observation")].map(
        (entry, index) => ({
          text: textRows[index]?.textContent,
          sequence: entry.dataset.sequence,
          time: entry.dataset.receivedAt,
          continuation: entry.dataset.continuation,
          rowTop: textRows[index]?.getBoundingClientRect().top,
          gutterTop: entry.getBoundingClientRect().top,
          label: entry.textContent,
        }),
      );
    });
  const find = async (text) =>
    (await snapshot()).find((row) => row.text?.includes(text));
  try {
    await observer.connect();
    const session = (await observer.list())[0];
    const attached = await observer.attach(session.session_id);
    await fixture.command("output", "history-unknown\r\n");
    await launch();
    await page.locator(`[data-session-id="${session.session_id}"]`).click();
    await expect(rows()).toContainText("history-unknown");
    await expect(gutter()).toBeHidden();
    await observer.read(attached.id);
    const before = Date.now();
    const raw = "first-live-设备😀\r\n";
    await fixture.command("output", raw);
    await expect(rows()).toContainText("first-live-设备😀");
    assert.equal(
      Buffer.from((await observer.read(attached.id)).data).toString(),
      raw,
    );
    const after = Date.now();
    await native("terminal.timestamps");
    await expect(gutter()).toBeVisible();
    await expect(gutter().locator(".line-sequence")).toHaveCount(0);
    await native("terminal.sequence");
    await expect
      .poll(async () => !!(await find("first-live"))?.time)
      .toBe(true);
    const old = await find("history-unknown"),
      live = await find("first-live");
    assert.equal(old.time, undefined);
    assert.match(old.label, /—/);
    assert.ok(Number(live.time) >= before && Number(live.time) <= after);
    assert.ok(Number(live.sequence) > Number(old.sequence));
    assert.ok(
      (await snapshot()).every(
        (row) => Math.abs(row.rowTop - row.gutterTop) < 1,
      ),
      JSON.stringify(await snapshot()),
    );

    // Local selection/copy excludes both independent columns and does not mutate raw output.
    await pane().locator(".xterm-helper-textarea").focus();
    await page.keyboard.press("Control+Shift+F");
    await page
      .locator(".terminal-search")
      .getByRole("button", { name: "Select all retained text", exact: true })
      .click();
    await native("terminal.copy");
    const copied = await desktop.evaluate(({ clipboard }) =>
      clipboard.readText(),
    );
    assert.match(copied, /first-live-设备😀/);
    assert.ok(!copied.includes(live.label));
    await page
      .locator(".terminal-search")
      .getByRole("button", { name: "Close search (Escape)", exact: true })
      .click();
    await expect(page.locator(".terminal-search")).toHaveCount(0);

    await native("terminal.timestamps");
    await expect(gutter().locator(".line-time")).toHaveCount(0);
    assert.equal((await find("first-live")).sequence, live.sequence);
    await native("terminal.timestamps");

    const wrap = "wrap-" + "设备😀".repeat(40);
    await fixture.command("output", wrap + "\r\nend-wrap\r\n");
    await expect(rows()).toContainText("end-wrap");
    await expect(rows()).toContainText("wrap-");
    await expect
      .poll(async () =>
        (await snapshot()).some(
          (row) => row.continuation === "true" && row.sequence,
        ),
      )
      .toBe(true);
    const wrappedRows = await snapshot();
    for (let index = 1; index < wrappedRows.length; index++) {
      if (wrappedRows[index].continuation === "true") {
        assert.equal(
          wrappedRows[index].sequence,
          wrappedRows[index - 1].sequence,
        );
        assert.equal(wrappedRows[index].time, wrappedRows[index - 1].time);
      }
    }
    const anchored = (await find("end-wrap")).sequence;
    for (const width of [760, 1500, 1100]) {
      await desktop.evaluate(
        ({ BrowserWindow }, width) =>
          BrowserWindow.getAllWindows()[0].setSize(width, 780),
        width,
      );
      await expect
        .poll(async () => (await find("end-wrap"))?.sequence)
        .toBe(anchored);
      await expect
        .poll(async () =>
          (await snapshot()).every(
            (row) => Math.abs(row.rowTop - row.gutterTop) < 1,
          ),
        )
        .toBe(true);
    }
    await pane()
      .locator(".terminal-view")
      .dispatchEvent("wheel", { ctrlKey: true, deltaY: -100 });
    await expect
      .poll(async () =>
        (await snapshot()).every(
          (row) => Math.abs(row.rowTop - row.gutterTop) < 1,
        ),
      )
      .toBe(true);
    for (const locale of locales) {
      await native(`language.${locale}`);
      const i18n = setupI18n({ locale, messages: menuCatalogs });
      await expect(gutter()).toHaveAttribute("title", i18n._("line.hint"));
      assert.equal(
        await desktop.evaluate(
          ({ Menu }) =>
            Menu.getApplicationMenu().getMenuItemById("terminal.timestamps")
              .accelerator,
        ),
        null,
      );
      assert.equal(
        await desktop.evaluate(
          ({ Menu }) =>
            Menu.getApplicationMenu().getMenuItemById("terminal.timestamps")
              .checked,
        ),
        true,
      );
    }
    await native("language.en");
    await page.screenshot({ path: "test-results/line-sidebar.png" });

    // A hidden tab keeps reading while its gutter stays undrawn. Receipt precedes reselection.
    await native("tab.new");
    const backgroundBefore = Date.now();
    await fixture.command("output", "background-observed\r\n");
    // Wait on an independent raw cursor, then allow the view's bounded one-second read to complete.
    await observer.read(attached.id);
    await new Promise((resolve) => setTimeout(resolve, 1200));
    const selectedAt = Date.now();
    await native("tab.previous");
    await expect
      .poll(async () => !!(await find("background-observed"))?.time)
      .toBe(true);
    const hidden = await find("background-observed");
    assert.ok(
      Number(hidden.time) >= backgroundBefore &&
        Number(hidden.time) < selectedAt,
    );
    await fixture.command("output", "\x1b[?1049h\x1b[Halternate");
    await expect(gutter()).toHaveClass(/unsupported/);
    await expect(gutter()).toHaveAttribute("title", /paused/);
    await fixture.command("output", "\x1b[?1049l");
    await expect
      .poll(async () => (await find("background-observed"))?.sequence)
      .toBe(hidden.sequence);

    for (let batch = 0; batch < 120; batch++) {
      await fixture.command(
        "output",
        Array.from({ length: 50 }, (_, i) => `bounded-${batch}-${i}\r\n`).join(
          "",
        ),
      );
      await expect(rows()).toContainText(`bounded-${batch}-49`);
    }
    await pane().locator(".xterm-helper-textarea").focus();
    for (let step = 0; step < 250; step++)
      await page.keyboard.press("Shift+PageUp");
    await expect
      .poll(async () => {
        const first = (await snapshot())[0];
        return Number(first?.sequence) > 950 && Number(first?.sequence) < 1100;
      })
      .toBe(true);
    assert.ok(
      (await snapshot()).every(
        (row) => Math.abs(row.rowTop - row.gutterTop) < 1,
      ),
    );
    await fixture.command("flood");
    await expect(page.locator(".notices")).toContainText("Output overwritten");
    await expect(rows()).toContainText("retained-after-overwrite");
    assert.ok(
      (await snapshot())
        .filter((row) => row.text?.trim())
        .every((row) => row.time === undefined),
    );
    await native("terminal.clear");
    await fixture.command("output", "\r\nafter-local-clear\r\n");
    await expect
      .poll(async () => !!(await find("after-local-clear"))?.time)
      .toBe(true);
    const clearSequence = (await find("after-local-clear")).sequence;
    await fixture.command("output", "\x1bcafter-remote-reset\r\n");
    await expect
      .poll(async () => !!(await find("after-remote-reset"))?.time)
      .toBe(true);
    assert.ok(
      Number((await find("after-remote-reset")).sequence) >
        Number(clearSequence),
    );
    // Presentation replacement keeps per-tab choices; HEX suspends the columns explicitly.
    for (const view of ["hex", "text"]) {
      await page
        .getByRole("button", { name: "Terminal settings", exact: true })
        .click();
      const dialog = page.getByRole("dialog");
      await dialog.locator('[name="view"]').selectOption(view);
      await dialog.getByRole("button", { name: "Apply", exact: true }).click();
      await expect(dialog).toHaveCount(0);
      await expect(gutter()).toBeVisible();
      if (view === "hex") await expect(gutter()).toHaveClass(/unsupported/);
      else await expect(gutter()).not.toHaveClass(/unsupported/);
    }
    await fixture.command("output", "\r\nafter-local-clear\r\n");
    await expect
      .poll(async () => !!(await find("after-local-clear"))?.time)
      .toBe(true);
    await desktop.close();
    desktop = undefined;
    await launch();
    await expect(rows()).toContainText("after-local-clear");
    await expect(gutter()).toBeVisible();
    assert.ok(
      (await snapshot())
        .filter((row) => row.text?.trim())
        .every((row) => row.time === undefined),
    );
    assert.equal((await observer.list())[0].state, "open");
    assert.deepEqual(errors, []);
    console.log(
      "PASS: independent local line columns, off-state/background collection, unknown replay, Unicode/reflow/font/scroll alignment, bounds/drop/clear/restart, nine locales and body-only copy.",
    );
  } catch (error) {
    if (page) await page.screenshot({ path: "test-results/b11-failure.png" });
    throw error;
  } finally {
    if (desktop) await desktop.close();
    await observer.disconnect();
    await fixture.stop();
  }
}
