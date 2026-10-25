import { checkFiles } from "./file-checks.mjs";
import { checkTerminalControls } from "./terminal-checks.mjs";
import assert from "node:assert/strict";
import { _electron as electron, expect } from "@playwright/test";
import { startFixture } from "../tests/fixture.ts";
import { HostClient } from "../src/main/host/client.ts";
import { checkConnections } from "./connection-checks.mjs";
import { checkStartup } from "./startup-checks.mjs";
import { checkWindows } from "./window-checks.mjs";
import { checkCompactLayout, checkWorkbenchPanels } from "./layout-checks.mjs";
import { randomBytes } from "node:crypto";
import { mkdtemp, writeFile } from "node:fs/promises";
import { resolve } from "node:path";
import { createServer } from "vite";
import { buildProcesses } from "./build.mjs";
import {
  checkOfflineMenus,
  checkAttachedMenus,
  checkContextTargets,
  checkClosedMenus,
  checkMenuAvailability,
  exitFromMenu,
  exitFromNativeMenu,
} from "./menu-checks.mjs";

// Resize the native window rather than emulating a browser viewport. Check that
// chrome stays visible and xterm follows its container without body scrolling.
async function checkLayout(desktop, page, state) {
  for (const [width, height] of [
    [760, 500],
    [1180, 780],
    [1500, 900],
  ]) {
    await desktop.evaluate(
      ({ BrowserWindow }, size) => {
        BrowserWindow.getAllWindows()[0].setSize(...size);
      },
      [width, height],
    );
    await expect.poll(() => page.evaluate(() => window.outerWidth)).toBe(width);
    await expect
      .poll(() =>
        page.evaluate(() => {
          const rect = (selector) =>
            document.querySelector(selector).getBoundingClientRect();
          const top = rect(".topbar");
          const side = rect(".sidebar");
          const main = rect("main");
          const bottom = rect(".statusbar");
          const surface = rect(".terminal-view, .terminal-empty");
          const panel = rect(".bottom-panel");
          const screen = document
            .querySelector(".xterm-screen")
            ?.getBoundingClientRect();
          return {
            top: top.top === 0,
            sidebar: side.top === top.bottom && main.left === side.right,
            bottom: bottom.bottom <= innerHeight && bottom.top === main.bottom,
            right: main.right <= innerWidth,
            width: surface.width > innerWidth * 0.6,
            height:
              surface.height + panel.height > innerHeight * 0.5 &&
              surface.height > 80,
            scroll:
              document.documentElement.scrollWidth <= innerWidth &&
              document.documentElement.scrollHeight <= innerHeight,
            terminal:
              !screen ||
              (screen.right <= surface.right &&
                screen.bottom <= surface.bottom),
          };
        }),
      )
      .toEqual({
        top: true,
        sidebar: true,
        bottom: true,
        right: true,
        width: true,
        height: true,
        scroll: true,
        terminal: true,
      })
      .catch(async (error) => {
        console.error(
          state,
          width,
          height,
          await page.evaluate(() =>
            [".terminal-view", ".xterm", ".xterm-screen"].map((selector) => ({
              selector,
              rect: document
                .querySelector(selector)
                ?.getBoundingClientRect()
                .toJSON(),
            })),
          ),
        );
        await page.screenshot({
          path: "test-results/workbench-layout-failure.png",
        });
        throw error;
      });
    await expect(
      page.getByRole("button", {
        name: /Connect to Host|Disconnect Host|Retry connection/,
      }),
    ).toBeInViewport();
    await expect(
      page.getByRole("searchbox", { name: "Search Sessions" }),
    ).toBeInViewport();
    if (width === 760) {
      await page.screenshot({
        path: `test-results/workbench-${state}-compact.png`,
      });
    }
  }
  await desktop.evaluate(({ BrowserWindow }) =>
    BrowserWindow.getAllWindows()[0].setSize(1180, 780),
  );
}

const fixture = await startFixture();
// Shortcut settings persist: keep every test window off the user's GUI profile.
const profile = await mkdtemp(resolve("test-results/desktop-profile-"));
const profilePreload = resolve(profile, "profile.cjs");
await writeFile(
  profilePreload,
  `const { app } = require("electron");\napp.setPath("userData", ${JSON.stringify(profile)});\napp.setPath("sessionData", ${JSON.stringify(profile)});\n`,
);
const preloadArgs = ["-r", profilePreload];
const launchArgs = [...preloadArgs, "--lang=en-US", "."];
const observer = new HostClient(fixture.endpoint, fixture.token);
const desktopEnvironment = {
  ...process.env,
  CHANNELTERM_GUI_ENDPOINT: fixture.endpoint,
  CHANNELTERM_HTTP_AUTH_TOKEN: fixture.token,
  CHANNELTERM_GUI_DEV_URL: "",
};
delete desktopEnvironment.ELECTRON_RUN_AS_NODE;
let desktop;
let devServer;
try {
  if (process.argv.includes("--dev")) {
    await buildProcesses({ strictCatalogs: false });
    const nonce = randomBytes(16).toString("hex");
    devServer = await createServer({ html: { cspNonce: nonce } });
    await devServer.listen();
    desktopEnvironment.CHANNELTERM_GUI_DEV_URL = "http://127.0.0.1:5173";
    desktopEnvironment.CHANNELTERM_GUI_DEV_NONCE = nonce;
  }
  await observer.connect();
  const sessionId = (await observer.list())[0].session_id;
  const attachment = await observer.attach(sessionId);
  desktop = await electron.launch({
    args: launchArgs,
    env: desktopEnvironment,
  });
  const page = await desktop.firstWindow();
  assert.equal(
    await desktop.evaluate(({ app }) => app.getPath("sessionData")),
    profile,
  );
  const errors = [];
  page.on("pageerror", (error) => errors.push(error.message));
  await expect(
    page.getByRole("navigation", { name: "Application menu", exact: true }),
  ).toBeVisible();
  await expect(
    page.getByRole("heading", { name: "ChannelTerm", exact: true }),
  ).toHaveCount(0);
  await expect(page.locator(".topbar")).toHaveCSS("height", "32px");
  await expect(page.locator(".brand-mark")).toHaveCount(0);
  await expect(page.getByText("Desktop preview", { exact: true })).toHaveCount(
    0,
  );
  await expect(
    page.getByText("Terminal workspace", { exact: true }),
  ).toHaveCount(0);
  await checkOfflineMenus(desktop, page);
  await expect
    .poll(() =>
      desktop.evaluate(({ BrowserWindow }) =>
        BrowserWindow.getAllWindows()[0].isVisible(),
      ),
    )
    .toBe(true);
  const preferences = await desktop.evaluate(({ BrowserWindow }) =>
    BrowserWindow.getAllWindows()[0].webContents.getLastWebPreferences(),
  );
  assert.equal(preferences.sandbox, true);
  assert.equal(preferences.contextIsolation, true);
  assert.equal(preferences.nodeIntegration, false);
  assert.deepEqual(
    await page.evaluate(() => [typeof window.require, typeof window.process]),
    ["undefined", "undefined"],
  );
  assert.equal(
    await page.evaluate(
      async () => (await window.channelterm.attach({})).error.code,
    ),
    "invalid_input",
  );
  await expect(
    page.getByRole("heading", { name: "No Host connected" }),
  ).toBeVisible();
  await expect(page.getByRole("button", { name: "Refresh" })).toBeDisabled();
  await expect(
    page.getByText("Connect a Host to list Sessions."),
  ).toBeVisible();
  await expect(
    page.getByRole("button", {
      name: /MCP|transfer|Agent|Start Host|New Session/i,
    }),
  ).toHaveCount(0);
  await checkLayout(desktop, page, "disconnected");
  await page.screenshot({ path: "test-results/workbench-disconnected.png" });
  await page.getByRole("button", { name: "Connect to Host" }).click();
  await expect(
    page.locator(".host-panel").getByText("Host connected", { exact: true }),
  ).toBeVisible();
  await expect(
    page.getByRole("heading", { name: "Select a Session" }),
  ).toBeVisible();
  await page.getByRole("button", { name: /SER-1/ }).click();
  await expect(page.locator(".xterm-screen")).toBeVisible();
  // FitAddon reads the container's size. Wrapper spacing must neither inflate
  // that size nor leak onto xterm's own .terminal class and clip fitted cells.
  await expect(page.locator(".terminal-view")).toHaveCSS("padding", "0px");
  await expect(page.locator(".xterm")).toHaveCSS("padding", "0px");
  await expect(page.getByRole("button", { name: /SER-1/ })).toHaveAttribute(
    "aria-pressed",
    "true",
  );
  await expect(
    page.getByText("Attached · Input enabled", { exact: true }),
  ).toBeVisible();
  await checkLayout(desktop, page, "attached");
  await checkCompactLayout(desktop, page);
  await checkWorkbenchPanels(desktop, page, observer, attachment);
  await checkFiles(desktop, page, fixture);
  // Searching/collapsing navigation only changes presentation, never attachment.
  await page
    .getByRole("searchbox", { name: "Search Sessions" })
    .fill("no-such-session");
  await expect(page.getByText("No matching Sessions.")).toBeVisible();
  await expect(page.locator(".xterm-screen")).toBeVisible();
  await page
    .getByRole("searchbox", { name: "Search Sessions" })
    .fill("ISOLATED");
  await expect(page.locator(".session")).toHaveCount(1);
  await page.getByRole("searchbox", { name: "Search Sessions" }).fill("");
  await page.locator(".session-group summary").click();
  await expect(page.locator(".session")).not.toBeVisible();
  await page.getByRole("button", { name: "Refresh", exact: true }).click();
  await expect(page.locator(".session")).not.toBeVisible();
  await page.locator(".session-group summary").click();
  const beforeCollapse = await page.locator(".terminal-view").boundingBox();
  await page.getByRole("button", { name: "Toggle Session sidebar" }).click();
  await expect(
    page.getByRole("complementary", { name: "Session sidebar" }),
  ).not.toBeVisible();
  await expect
    .poll(
      async () => (await page.locator(".terminal-view").boundingBox()).width,
    )
    .toBeGreaterThan(beforeCollapse.width);
  await expect(page.locator(".xterm-screen")).toBeVisible();
  await page.getByRole("button", { name: "Toggle Session sidebar" }).click();
  await expect(page.getByRole("button", { name: /SER-1/ })).toHaveAttribute(
    "aria-pressed",
    "true",
  );
  await page.screenshot({ path: "test-results/workbench-reference.png" });
  await page.locator(".xterm-helper-textarea").focus();
  await page.keyboard.type("desktop-roundtrip");
  let received = "";
  await expect
    .poll(async () => {
      received += Buffer.from(
        (await observer.read(attachment.id)).data,
      ).toString();
      return received;
    })
    .toContain("desktop-roundtrip");
  await checkTerminalControls(desktop, page, fixture, observer, attachment);
  await checkAttachedMenus(desktop, page, fixture, observer, attachment);
  await fixture.command("lease");
  await expect(page.getByText(/Lease busy/)).toBeVisible();
  await expect(
    page.getByText("Read-only · Lease active", { exact: true }),
  ).toBeVisible();
  await fixture.command("output", "\r\nread-while-leased\r\n");
  await expect(page.locator(".xterm-rows")).toContainText("read-while-leased");
  await page.locator(".xterm-helper-textarea").focus();
  await page.keyboard.type("blocked-by-busy");
  await page.screenshot({ path: "test-results/desktop.png" });
  await fixture.command("release");
  await expect(page.getByText(/Lease busy/)).toHaveCount(0);
  assert.ok(
    !Buffer.from((await observer.read(attachment.id)).data)
      .toString()
      .includes("blocked-by-busy"),
  );
  await fixture.command("flood");
  await expect(
    page.getByText(/Output overwritten in the Host buffer/),
  ).toBeVisible();
  await expect(page.locator(".xterm-rows")).toContainText(
    "retained-after-overwrite",
  );
  await desktop.evaluate(({ Menu }) => {
    globalThis.staleClose =
      Menu.getApplicationMenu().getMenuItemById("tab.close");
  });
  await desktop.evaluate(({ Menu }) =>
    Menu.getApplicationMenu().getMenuItemById("window.tab.close").click(),
  );
  await expect(
    page.getByRole("heading", { name: "Select a Session" }),
  ).toBeVisible();
  await expect(
    page.getByText(/Output overwritten in the Host buffer/),
  ).toHaveCount(0);
  await page.getByRole("button", { name: /SER-1/ }).click();
  await expect(page.locator(".xterm-screen")).toBeVisible();
  await desktop.evaluate(() => globalThis.staleClose.click());
  // A second FIFO native action is a barrier before checking the stale callback.
  await desktop.evaluate(({ Menu }) =>
    Menu.getApplicationMenu().getMenuItemById("view.sidebar").click(),
  );
  await expect(page.locator(".sidebar")).not.toBeVisible();
  await expect(page.locator(".xterm-screen")).toBeVisible();
  await page.getByRole("button", { name: "Toggle Session sidebar" }).click();
  await checkContextTargets(desktop, page, observer);
  await page.getByRole("button", { name: "Disconnect Host" }).click();
  await expect(
    page.getByText("Host disconnected", { exact: true }),
  ).toBeVisible();
  await page.getByRole("button", { name: "Connect to Host" }).click();
  await page.getByRole("button", { name: /SER-1/ }).click();
  await expect(page.locator(".xterm-screen")).toBeVisible();
  // Exit GUI shares native close cleanup and leaves the Host/observer alive.
  const savedPanelBinding =
    process.platform === "darwin" ? "Meta+Shift+J" : "Ctrl+Shift+J";
  await page.evaluate((binding) => {
    const key = "channelterm.layout-shortcuts.v1";
    const bindings = JSON.parse(localStorage.getItem(key));
    bindings["view.panel"] = binding;
    localStorage.setItem(key, JSON.stringify(bindings));
  }, savedPanelBinding);
  await exitFromMenu(desktop, page);
  desktop = undefined;
  assert.equal((await observer.list())[0].state, "open");
  await observer.write(attachment.id, "after-window-close");
  assert.match(
    Buffer.from((await observer.read(attachment.id)).data).toString(),
    /after-window-close/,
  );
  assert.deepEqual(errors, []);
  // Test-only IPC snapshots exercise empty/large lists without adding Host
  // controls or opening hardware. A fresh process below restores real IPC.
  desktop = await electron.launch({
    args: launchArgs,
    env: desktopEnvironment,
  });
  const listPage = await desktop.firstWindow();
  await expect(listPage.locator('[data-layout="view.panel"]')).toHaveAttribute(
    "title",
    process.platform === "darwin"
      ? "Bottom panel (⌘⇧J)"
      : "Bottom panel (Ctrl+Shift+J)",
  );
  const listErrors = [];
  listPage.on("pageerror", (error) => listErrors.push(error.message));
  await desktop.evaluate(({ ipcMain }) => {
    ipcMain.removeHandler("host:list");
    ipcMain.handle("host:list", () => ({ ok: true, value: [] }));
  });
  await listPage.getByRole("button", { name: "Connect to Host" }).click();
  await expect(
    listPage.getByRole("heading", { name: "No Sessions available" }),
  ).toBeVisible();
  await expect(
    listPage.getByText("Host connected", { exact: true }),
  ).toBeVisible();
  await expect(listPage.getByRole("button", { name: "Refresh" })).toBeEnabled();
  await expect(listPage.getByText("0 Sessions", { exact: true })).toBeVisible();
  await checkMenuAvailability(desktop, true);
  await checkLayout(desktop, listPage, "empty");
  await desktop.evaluate(({ ipcMain }) => {
    ipcMain.removeHandler("host:list");
    ipcMain.handle("host:list", () => ({
      ok: true,
      value: Array.from({ length: 40 }, (_, index) => ({
        session_id: `display-fixture-${index}`,
        session_ref: `SER-${index + 1}`,
        transport: index % 2 === 0 ? "serial" : "ssh",
        label: "Long display label ".repeat(20),
        endpoint: "in-memory",
        state: index === 0 ? "closed" : "open",
      })),
    }));
  });
  await listPage.getByRole("button", { name: "Refresh" }).click();
  await expect(listPage.locator(".session")).toHaveCount(40);
  await expect(listPage.locator(".session-group summary")).toHaveText([
    "Serial (20)",
    "SSH (20)",
  ]);
  await listPage
    .getByRole("searchbox", { name: "Search Sessions" })
    .fill("ssh");
  await expect(listPage.locator(".session")).toHaveCount(20);
  await expect(listPage.locator(".session-group summary")).toHaveText([
    "SSH (20)",
  ]);
  await listPage.getByRole("searchbox", { name: "Search Sessions" }).fill("");
  await expect(listPage.locator(".session").first()).toBeDisabled();
  await checkLayout(desktop, listPage, "long-list");
  await listPage.locator(".session").last().scrollIntoViewIfNeeded();
  await expect(listPage.locator(".session").last()).toBeInViewport();
  await expect(listPage.locator(".statusbar")).toBeInViewport();
  assert.deepEqual(listErrors, []);
  // Corrupt preferences recover to platform defaults on the next app launch.
  await listPage.evaluate(() =>
    localStorage.setItem("channelterm.layout-shortcuts.v1", "{invalid"),
  );
  await exitFromNativeMenu(desktop);
  desktop = undefined;
  // A new window observes Session loss and then Host loss without taking ownership.
  desktop = await electron.launch({
    args: launchArgs,
    env: desktopEnvironment,
  });
  const recoveryPage = await desktop.firstWindow();
  await expect(
    recoveryPage.locator('[data-layout="view.panel"]'),
  ).toHaveAttribute(
    "title",
    process.platform === "darwin"
      ? "Bottom panel (⌘J)"
      : "Bottom panel (Ctrl+J)",
  );
  await recoveryPage.getByRole("button", { name: "Connect to Host" }).click();
  await recoveryPage.getByRole("button", { name: /SER-1/ }).click();
  await expect(recoveryPage.locator(".xterm-screen")).toBeVisible();
  await fixture.command("close");
  await expect(recoveryPage.getByText(/Session closed/).first()).toBeVisible();
  await checkClosedMenus(desktop, recoveryPage);
  await fixture.stop();
  await expect(
    recoveryPage.getByText("Host connection failed", { exact: true }),
  ).toBeVisible();
  await expect(recoveryPage.getByRole("alert")).toBeVisible();
  await expect(
    recoveryPage.getByRole("heading", { name: "Host unavailable" }),
  ).toBeVisible();
  await expect(recoveryPage.locator(".session")).toHaveCount(0);
  await expect(recoveryPage.locator(".xterm-screen")).toHaveCount(0);
  await checkMenuAvailability(desktop, false);
  await recoveryPage.getByRole("button", { name: "Retry connection" }).click();
  await expect(
    recoveryPage.getByRole("button", { name: "Retry connection" }),
  ).toBeEnabled();
  await expect(
    recoveryPage.getByRole("heading", { name: "Host unavailable" }),
  ).toBeVisible();
  await checkLayout(desktop, recoveryPage, "failed");
  await checkConnections(desktopEnvironment, launchArgs);
  await checkWindows(desktopEnvironment);
  await checkStartup(desktopEnvironment, preloadArgs);
  console.log(
    "PASS: native workbench at 760/1180/1500px, hover/keyboard flyouts, serial and SSH creation, authentication failure, shared I/O and lifetime, disconnected/empty/selection/attached/failure states, long lists, sandbox, lease blocking, overwrite, reconnect, detach/reattach, native close, surviving Host/observer, Session close and Host loss/retry. Empty/large lists use test-only IPC snapshots; connections use the real isolated Host with fake serial I/O and loopback SSH.",
  );
} finally {
  await desktop?.close();
  await observer.disconnect();
  await fixture.stop();
  await devServer?.close();
}
