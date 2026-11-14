import assert from "node:assert/strict";
import { expect } from "@playwright/test";

/** Exercise production IPC and capture/confirm semantics against the real isolated Host. */
export async function checkControl(desktop, page, observer, sessionId) {
  const panel = page.getByRole("region", { name: "Session control" });
  await expect(panel).toBeVisible();
  await page.getByTestId("control-takeover").click();
  const confirm = page.getByTestId("control-confirm");
  await expect(confirm).toContainText(sessionId);
  // Completion after the confirmation opens must not silently target the next generation.
  const old = (await observer.list()).find(
    (s) => s.session_id === sessionId,
  ).control;
  await observer.control({
    action: "allow",
    session_id: old.session_id,
    version: old.version,
    operation_id: old.operation_id,
  });
  await confirm.getByRole("button", { name: "Confirm", exact: true }).click();
  await expect(page.getByTestId("control-result")).toContainText(
    "Operation changed or completed",
  );
  await page
    .getByTestId("control-result")
    .getByRole("button", { name: "Dismiss", exact: true })
    .click();
  const secondary = await observer.openConnection({
    kind: "serial",
    port: "COM-FIXTURE",
    baud: 115200,
    dataBits: 8,
    parity: "none",
    stopBits: "1",
    flowControl: "none",
    label: "Control secondary",
    wake: false,
  });
  await page.getByRole("button", { name: "Refresh", exact: true }).click();
  await page.getByTestId("control-takeover").click();
  await page
    .locator("button.session")
    .filter({ hasText: "Control secondary" })
    .click();
  await expect(confirm).toContainText(sessionId);
  await expect(confirm).not.toContainText(secondary.sessionId);
  await confirm.getByRole("button", { name: "Confirm", exact: true }).click();
  const result = page.getByTestId("control-result");
  await expect(result).toContainText(/Recovery confirmed|Stopping/);
  await result.getByRole("button", { name: "Query original result" }).click();
  await expect(result).toContainText("Recovery confirmed");
  assert.equal(
    (await observer.list()).find((s) => s.session_id === secondary.sessionId)
      .control.ai_paused,
    false,
  );
  await page.locator("button.session").filter({ hasText: /SER-1/ }).click();
  await expect(page.getByTestId("control-allow")).toBeEnabled();
  await expect
    .poll(
      async () =>
        (await observer.list()).find((s) => s.session_id === sessionId).control
          .writable,
    )
    .toBe(false);
  await desktop.evaluate(({ BrowserWindow }) =>
    BrowserWindow.getAllWindows()[0].setSize(760, 500),
  );
  await expect
    .poll(() =>
      page.evaluate(() => document.documentElement.scrollWidth <= innerWidth),
    )
    .toBe(true);
  await page.screenshot({ path: "test-results/control-takeover.png" });
  // The one original result stays available when the Host connection goes away.
  await page
    .getByRole("button", { name: "Disconnect Host", exact: true })
    .click();
  await expect(result).toContainText(sessionId);
  await expect(
    result.getByRole("button", { name: "Query original result" }),
  ).toBeDisabled();
  await page
    .getByRole("button", { name: "Connect to Host", exact: true })
    .click();
  await result.getByRole("button", { name: "Query original result" }).click();
  await expect(result).toContainText("Recovery confirmed");
  await page.locator("button.session").filter({ hasText: /SER-1/ }).click();
  await page.getByTestId("control-allow").click();
  await confirm.getByRole("button", { name: "Confirm", exact: true }).click();
  await expect(result).toContainText("AI pause removed");
  await result.getByRole("button", { name: "Dismiss", exact: true }).click();
  await expect
    .poll(
      async () =>
        (await observer.list()).find((s) => s.session_id === sessionId).control
          .writable,
    )
    .toBe(true);
  const secondaryAttachment = await observer.attach(secondary.sessionId);
  await observer.closeSession(secondaryAttachment.id);
  await observer.detach(secondaryAttachment.id);
  const malformed = await page.evaluate(() =>
    window.channelterm.control({
      action: "takeover",
      session_id: "x",
      version: "CV-abcdefghijklmnop",
      operation_id: "",
      secret: "not-allowed",
    }),
  );
  assert.equal(malformed.ok, false);
  await desktop.evaluate(({ BrowserWindow }) =>
    BrowserWindow.getAllWindows()[0].setSize(1180, 780),
  );
}
