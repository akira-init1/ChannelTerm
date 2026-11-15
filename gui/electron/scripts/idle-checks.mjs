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
    await expect(page.locator(".host-idle-policy")).toContainText(
      "released after 1 s;",
    );
    await page
      .locator("button.session")
      .filter({ hasText: "Isolated echo" })
      .click();
    await expect(page.locator(".xterm-screen")).toBeVisible();
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
    await observer.detach(other.id);
    await fixture.command("sweep-idle");
    await delay(1100);
    await fixture.command("sweep-idle");
    assert.deepEqual(
      await observer.list(),
      [],
      "Host retained an unused Session",
    );
    assert.equal(
      observer.info().idlePolicy.mode,
      "release",
      "MCP remains reachable after release",
    );
    console.log(
      "PASS idle policy: another client's use survives GUI exit; final detach releases Session after grace and leaves MCP running",
    );
  } finally {
    await desktop?.close();
    await observer.disconnect();
    await fixture.stop();
  }
}
