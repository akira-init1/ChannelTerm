import { chooseSession } from "./session-picker.mjs";
import assert from "node:assert/strict";
import { mkdtemp, writeFile } from "node:fs/promises";
import { resolve } from "node:path";
import { setTimeout as delay } from "node:timers/promises";
import { _electron as electron, expect } from "@playwright/test";
import { startFixture } from "../tests/fixture.ts";
import { HostClient } from "../src/main/host/client.ts";

/** Private fixture clock trigger; all usage/closure decisions are real Application code. */
export async function checkIdle(environment, preloadArgs) {
  const fixture = await startFixture({ idle: true });
  const observer = new HostClient(fixture.endpoint, fixture.token);
  const profile = await mkdtemp(resolve("test-results/idle-profile-"));
  const profilePreload = resolve(profile, "profile.cjs");
  await writeFile(
    profilePreload,
    `const { app } = require("electron"); app.setPath("userData", ${JSON.stringify(profile)}); app.setPath("sessionData", ${JSON.stringify(profile)});`,
  );
  let desktop;
  try {
    await observer.connect();
    const id = (await observer.list())[0].session_id;
    const other = await observer.attach(id);
    desktop = await electron.launch({
      args: [...preloadArgs, "-r", profilePreload, "--lang=en-US", "."],
      env: {
        ...environment,
        CHANNELTERM_GUI_ENDPOINT: fixture.endpoint,
        CHANNELTERM_HTTP_AUTH_TOKEN: fixture.token,
      },
    });
    const page = await desktop.firstWindow();
    await page
      .getByRole("button", { name: "Connect to Host...", exact: true })
      .click();
    await expect(page.locator(".host-panel")).toContainText("Host connected");
    await chooseSession(page, "Isolated echo");
    await expect(page.locator(".xterm-screen")).toBeVisible();
    await expect(page.locator(".host-idle-policy")).toHaveCount(0);
    await expect(page.getByTestId("control-takeover")).toBeEnabled();
    await expect(page.getByTestId("control-allow")).toBeVisible();
    const single = await observer.openConnection({
      kind: "serial",
      port: "COM8",
      label: "Last tab",
      baud: 115200,
      dataBits: 8,
      parity: "none",
      stopBits: "1",
      flowControl: "none",
    });
    await chooseSession(page, "Last tab", single.sessionId);
    const singleTab = page
      .locator(".terminal-tab.active")
      .filter({ hasText: "COM8" });
    await singleTab.locator(".icon-button").click();
    await expect(singleTab).toHaveCount(0);
    // With no scheduled sweep, only normal departure (and draining reads) can release COM8.
    await expect
      .poll(async () =>
        (await observer.list()).some(
          (row) => row.session_id === single.sessionId,
        ),
      )
      .toBe(false);
    await desktop.close();
    desktop = undefined;
    await fixture.command("sweep-idle");
    await delay(1100);
    await fixture.command("sweep-idle");
    assert.equal(
      (await observer.list())[0].session_id,
      id,
      "other client still owns an observation use",
    );
    // The fixture has no automatic sweeper. Final departure must close the stream by itself.
    await observer.detach(other.id);
    assert.deepEqual(
      await observer.list(),
      [],
      "Host retained an unused Session",
    );
    assert.equal(observer.info().idlePolicy.mode, "exit");
    await observer.disconnect();
    assert.equal((await fixture.command("sweep-idle")).data, "false");
    await delay(3100);
    assert.equal((await fixture.command("sweep-idle")).data, "true");
    console.log(
      "PASS idle policy: another client's use survives GUI exit; final detach releases the Session and final disconnect permits Host exit",
    );
  } finally {
    await desktop?.close();
    await observer.disconnect();
    await fixture.stop();
  }
}
