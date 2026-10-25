import assert from "node:assert/strict";
import { _electron as electron, expect } from "@playwright/test";
import { startFixture } from "../tests/fixture.ts";
import { HostClient } from "../src/main/host/client.ts";
import { checkSSHFiles } from "./file-checks.mjs";

/** Real GUI -> trusted IPC -> authenticated Host -> Application connection setup. */
export async function checkConnections(environment, launchArgs) {
  const fixture = await startFixture();
  const observer = new HostClient(fixture.endpoint, fixture.token);
  let desktop;
  try {
    desktop = await electron.launch({
      args: launchArgs,
      env: {
        ...environment,
        CHANNELTERM_GUI_ENDPOINT: fixture.endpoint,
        CHANNELTERM_HTTP_AUTH_TOKEN: fixture.token,
      },
    });
    const page = await desktop.firstWindow();
    const errors = [];
    page.on("pageerror", (error) => errors.push(error.message));
    const popup = page.getByRole("menu", {
      name: "Workbench menu",
      exact: true,
    });
    async function open(kind) {
      await page.locator('[data-menu="session"]').click();
      await popup
        .getByRole("menuitem", { name: "New Connection...", exact: true })
        .click();
      const dialog = page.getByRole("dialog");
      await expect(dialog).toBeVisible();
      await dialog
        .getByRole("combobox", { name: "Protocol", exact: true })
        .selectOption(kind);
      return dialog;
    }
    await page.locator('[data-menu="session"]').click();
    await expect(
      popup.getByRole("menuitem", { name: "New Connection...", exact: true }),
    ).toBeDisabled();
    await page.keyboard.press("Escape");
    await page
      .getByRole("button", { name: "Connect to Host", exact: true })
      .click();
    await expect(
      page.getByText("Host connected", { exact: true }),
    ).toBeVisible();
    await observer.connect();
    let dialog = await open("serial");
    const protocol = dialog.getByRole("combobox", {
      name: "Protocol",
      exact: true,
    });
    await protocol.selectOption("ssh");
    await dialog
      .getByRole("combobox", { name: "Authentication", exact: true })
      .selectOption("password");
    await dialog
      .locator('[name="password"]')
      .fill("discard-on-protocol-switch");
    await protocol.selectOption("serial");
    await expect(dialog.locator('[name="password"]')).toHaveCount(0);
    await expect(dialog.locator('[name="port"]')).toHaveValue("");
    await protocol.selectOption("ssh");
    await expect(
      dialog.getByRole("combobox", { name: "Authentication", exact: true }),
    ).toHaveValue("key");
    await expect(dialog.locator('[name="port"]')).toHaveValue("22");
    await protocol.selectOption("serial");
    const baud = dialog.getByRole("combobox", {
      name: "Baud rate",
      exact: true,
    });
    await expect(baud).toHaveValue("115200");
    await baud.selectOption("9600");
    await expect(dialog.locator('[name="baud"]')).toHaveValue("9600");
    await baud.selectOption("custom");
    const customBaud = dialog.getByRole("spinbutton", {
      name: "Custom baud rate",
      exact: true,
    });
    await customBaud.fill("123456");
    await expect(dialog.locator('[name="baud"]')).toHaveCount(1);
    await expect(dialog.locator('[name="baud"]')).toHaveValue("123456");
    await baud.selectOption("115200");
    await expect(customBaud).toHaveCount(0);
    await dialog
      .getByRole("button", { name: "Scan ports", exact: true })
      .click();
    await expect(dialog.locator("datalist option")).toHaveAttribute(
      "value",
      "COM-FIXTURE",
    );
    await dialog.locator('[name="port"]').fill("MISSING");
    await dialog.getByRole("button", { name: "Connect", exact: true }).click();
    await expect(dialog.getByRole("alert")).toContainText(
      "Host could not open",
    );
    await dialog.locator('[name="port"]').fill("COM-FIXTURE");
    await dialog.locator('[name="label"]').fill("Created serial");
    await dialog.locator('[name="encoding"]').selectOption("gbk");
    await dialog.locator('[name="description"]').fill("Local board console");
    await page.screenshot({ path: "test-results/connection-serial.png" });
    await dialog.getByRole("button", { name: "Connect", exact: true }).click();
    await expect(dialog).not.toBeVisible();
    await expect(page.locator(".terminal-tab.active")).toContainText(
      "Created serial",
    );
    await expect(page.locator(".xterm-screen:visible")).toBeVisible();
    const serial = (await observer.list()).find(
      (s) => s.label === "Created serial",
    );
    assert.ok(serial);
    const serialObserver = await observer.attach(serial.session_id);
    await observer.read(serialObserver.id);
    await expect(page.locator(".terminal-settings-button")).toContainText(
      "GBK",
    );
    await expect(page.locator(".terminal-description")).toHaveText(
      "Local board console",
    );
    await page
      .locator(".terminal-pane:not([hidden]) .xterm-helper-textarea")
      .focus();
    await page.keyboard.insertText("中文");
    const received = [];
    await expect
      .poll(async () => {
        received.push(...(await observer.read(serialObserver.id)).data);
        return Buffer.from(received).includes(
          Buffer.from([0xd6, 0xd0, 0xce, 0xc4]),
        );
      })
      .toBe(true);
    await expect(
      page.locator(".terminal-pane:not([hidden]) .xterm-rows"),
    ).toContainText("中文");
    await page
      .getByRole("button", { name: "Terminal settings", exact: true })
      .click();
    dialog = page.getByRole("dialog");
    await expect(dialog.locator('[name="encoding"]')).toHaveValue("gbk");
    await dialog.locator('[name="view"]').selectOption("hex");
    await page.screenshot({ path: "test-results/terminal-settings.png" });
    await dialog.getByRole("button", { name: "Apply", exact: true }).click();
    await expect(dialog).not.toBeVisible();
    await expect(page.locator(".input-status")).toHaveText("Hex (read-only)");
    await expect(
      page.locator(".terminal-pane:not([hidden]) .xterm-rows"),
    ).toContainText("d6 d0 ce c4");
    await page
      .locator(".terminal-pane:not([hidden]) .xterm-helper-textarea")
      .focus();
    await page.keyboard.type("MUST-NOT-WRITE");
    assert.deepEqual((await observer.read(serialObserver.id)).data, []);
    await page.screenshot({ path: "test-results/terminal-hex.png" });
    // Attachment settings remain local; the same shared Session stays open.
    assert.equal(
      (await observer.list()).find((s) => s.session_id === serial.session_id)
        .state,
      "open",
    );
    await page
      .getByRole("button", { name: "Terminal settings", exact: true })
      .click();
    dialog = page.getByRole("dialog");
    await dialog.locator('[name="view"]').selectOption("text");
    await dialog.locator('[name="encoding"]').selectOption("utf-8");
    await dialog.getByRole("button", { name: "Apply", exact: true }).click();
    await expect(dialog).not.toBeVisible();
    await expect(page.locator(".input-status")).toContainText("Input enabled");
    dialog = await open("ssh");
    await dialog
      .getByRole("combobox", { name: "Authentication", exact: true })
      .selectOption("password");
    await dialog.locator('[name="host"]').fill("127.0.0.1");
    await dialog.locator('[name="port"]').fill(String(fixture.sshPort));
    await dialog.locator('[name="user"]').fill("fixture-user");
    await dialog.locator('[name="knownHostsFile"]').fill(fixture.knownHosts);
    await dialog.locator('[name="password"]').fill("wrong-fixture-password");
    await dialog.getByRole("button", { name: "Connect", exact: true }).click();
    await expect(dialog.getByRole("alert")).toContainText(
      "authentication failed",
    );
    await expect(dialog.locator('[name="password"]')).toHaveValue("");
    await expect(page.locator(".terminal-tab.active")).toContainText(
      "Created serial",
    );
    await dialog.locator('[name="password"]').fill("fixture-password");
    await dialog.locator('[name="label"]').fill("Created SSH");
    await desktop.evaluate(({ BrowserWindow }) =>
      BrowserWindow.getAllWindows()[0].setSize(760, 500),
    );
    await dialog
      .getByRole("button", { name: "Connect", exact: true })
      .scrollIntoViewIfNeeded();
    await expect(
      dialog.getByRole("button", { name: "Connect", exact: true }),
    ).toBeInViewport();
    await page.screenshot({ path: "test-results/connection-ssh.png" });
    await dialog.getByRole("button", { name: "Connect", exact: true }).click();
    await expect(dialog).not.toBeVisible();
    await expect(page.locator(".terminal-tab.active")).toContainText(
      "Created SSH",
    );
    const ssh = (await observer.list()).find((s) => s.label === "Created SSH");
    assert.ok(ssh);
    const attachment = await observer.attach(ssh.session_id);
    await checkSSHFiles(desktop, page, fixture);
    await page
      .locator(".terminal-pane:not([hidden]) .xterm-helper-textarea")
      .focus();
    await page.keyboard.type("gui-ssh-input");
    await page.keyboard.press("Enter");
    let output = "";
    await expect
      .poll(async () => {
        output += Buffer.from(
          (await observer.read(attachment.id)).data,
        ).toString();
        return output;
      })
      .toContain("gui-ssh-input");
    await page
      .locator(".terminal-tab.active")
      .getByRole("button", { name: "Close tab (detach)", exact: true })
      .click();
    await expect(page.locator(".terminal-tab.active")).toContainText(
      "Created serial",
    );
    await expect(page.locator(".xterm-screen:visible")).toHaveCount(1);
    assert.equal(
      (await observer.list()).find((s) => s.session_id === ssh.session_id)
        ?.state,
      "open",
    );
    assert.equal(
      (await observer.list()).find((s) => s.session_id === serial.session_id)
        ?.state,
      "open",
    );
    assert.deepEqual(errors, []);
  } finally {
    await desktop?.close();
    await observer.disconnect();
    await fixture.stop();
  }
}
