import assert from "node:assert/strict";
import { expect } from "@playwright/test";
import { setupI18n } from "@lingui/core";
import { menuCatalogs } from "../src/locales/catalogs.ts";
import { locales } from "../src/shared/menus.ts";

/** Native rendering and real IPC over the fixture Registry; no physical device access. */
export async function checkDevices(page, desktop, dialog, fixture) {
  const panel = dialog.locator(".device-discovery");
  const row = (endpoint) =>
    panel.locator("li").filter({
      has: page.locator("strong", { hasText: new RegExp(`^${endpoint}$`) }),
    });
  const connect = dialog.getByRole("button", { name: "Connect", exact: true });
  const initial = await page.evaluate(() => window.channelterm.devices());
  assert.equal(initial.ok, true);
  const device = initial.value.devices[0];
  const request = {
    kind: "serial",
    port: device.endpoint,
    discovery: { deviceId: device.id, endpoint: device.endpoint },
    baud: 115200,
    dataBits: 8,
    parity: "none",
    stopBits: "1",
    flowControl: "none",
    label: "Must not open",
  };
  await expect(row("COM-FIXTURE")).toContainText("USB serial identity");
  await row("COM-FIXTURE").getByRole("button").click();
  await expect(connect).toBeEnabled();
  await expect(panel.locator(".device-decision")).toContainText(
    "No Session on this Host",
  );
  assert.equal((await fixture.command("device-open-count")).data, "0");
  await fixture.command("devices", "failed");
  await expect(panel.getByRole("alert")).toContainText("Host scan failed", {
    timeout: 10000,
  });
  await expect(connect).toBeDisabled();
  await expect(row("COM-FIXTURE")).toContainText("Unknown");
  let rejected = await page.evaluate(
    (request) => window.channelterm.openConnection(request),
    request,
  );
  assert.equal(rejected.ok, false);
  assert.equal(rejected.error.reason, "discovery_stale");
  await fixture.command("devices", "moved");
  await expect(row("COM-MOVED")).toContainText(
    "Previous endpoint: COM-FIXTURE",
    { timeout: 10000 },
  );
  await expect(connect).toBeDisabled();
  rejected = await page.evaluate(
    (request) => window.channelterm.openConnection(request),
    request,
  );
  assert.equal(rejected.ok, false);
  assert.equal(rejected.error.reason, "device_changed");
  await row("COM-MOVED").getByRole("button").click();
  await expect(connect).toBeEnabled();
  await expect(dialog.locator('[name="port"]')).toHaveValue("COM-MOVED");
  await fixture.command("devices", "absent");
  await expect(row("COM-MOVED")).toContainText("Not present", {
    timeout: 10000,
  });
  await expect(connect).toBeDisabled();
  await fixture.command("devices", "ambiguous");
  await expect(row("COM-FIXTURE")).toContainText(
    "Insufficient or conflicting identity evidence",
    { timeout: 10000 },
  );
  await expect(row("COM-DUPLICATE")).toContainText("This Host process only");
  await fixture.command("devices", "");
  // A stable identity can reappear; select it explicitly rather than following an old endpoint.
  await expect(
    row("COM-FIXTURE").filter({ hasText: "Persistent ID" }),
  ).toHaveCount(1, { timeout: 10000 });
  await row("COM-FIXTURE")
    .filter({ hasText: "Persistent ID" })
    .getByRole("button")
    .click();
  await expect(connect).toBeEnabled();
  const size = await desktop.evaluate(({ BrowserWindow }) => {
    const window = BrowserWindow.getAllWindows()[0];
    const size = window.getSize();
    window.setSize(760, 500);
    return size;
  });
  for (const locale of locales) {
    const i18n = setupI18n({ locale, messages: menuCatalogs });
    await desktop.evaluate(
      ({ Menu }, locale) =>
        Menu.getApplicationMenu().getMenuItemById(`language.${locale}`).click(),
      locale,
    );
    await expect(panel).toHaveAttribute("aria-label", i18n._("devices.title"));
    await expect(panel).toContainText(i18n._("devices.boundary"));
    await expect(panel.locator(".device-decision")).toContainText(
      i18n._("devices.externalUnknown"),
    );
    assert.equal(
      await dialog.evaluate((el) => el.scrollWidth <= el.clientWidth + 1),
      true,
      `${locale} discovery overflows dialog`,
    );
  }
  await page.screenshot({ path: "test-results/device-discovery-compact.png" });
  await desktop.evaluate(({ Menu, BrowserWindow }, size) => {
    Menu.getApplicationMenu().getMenuItemById("language.en").click();
    BrowserWindow.getAllWindows()[0].setSize(...size);
  }, size);
  await expect(panel).toHaveAttribute("aria-label", "Device discovery");
  await page.screenshot({ path: "test-results/device-discovery.png" });
  assert.equal((await fixture.command("device-open-count")).data, "0");
  // Switching back to manually typed intent removes the transient identity fence.
  await dialog.locator('[name="port"]').fill("MANUAL-FIXTURE");
  await expect(panel.locator(".device-decision")).toHaveCount(0);
  await panel
    .getByRole("button", { name: "Hide discovery", exact: true })
    .click();
  await expect(panel).toHaveCount(0);
}
