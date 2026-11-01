import assert from "node:assert/strict";
import { cp, mkdir, mkdtemp, readFile, writeFile } from "node:fs/promises";
import { resolve } from "node:path";
import { randomBytes } from "node:crypto";
import { _electron as electron, expect } from "@playwright/test";
import {
  localListener,
  defaultHostEndpoint,
} from "../src/main/host/startup.ts";
import { HostClient } from "../src/main/host/client.ts";
import { startFixture } from "../tests/fixture.ts";

/** Exercise Main's real launch path in a disposable app with a no-hardware Host. */
export async function checkStartup(environment, preloadArgs) {
  // This regression uses the suite's isolated endpoint, even when the user's
  // default port is occupied. Never skip the legacy-configuration recovery path.
  await checkCredentialRecovery(environment, preloadArgs);
  if (await localListener(new AbortController().signal)) {
    console.log(
      "SKIP automatic Host desktop test: port 37099 is already occupied; existing listener left untouched.",
    );
    return;
  }
  const directory = await mkdtemp(resolve("test-results/startup-"));
  // The parent suite still owns a live window; a second Electron process must not share its profile.
  const profile = resolve(directory, "profile");
  await mkdir(profile);
  const profilePreload = resolve(directory, "profile.cjs");
  await writeFile(
    profilePreload,
    `const { app } = require("electron");\napp.setPath("userData", ${JSON.stringify(profile)});\napp.setPath("sessionData", ${JSON.stringify(profile)});\n`,
  );
  await cp("dist", resolve(directory, "dist"), {
    recursive: true,
    filter: (source) => !source.endsWith("host"),
  });
  await mkdir(resolve(directory, "dist/host"), { recursive: true });
  const executable =
    process.platform === "win32" ? "channelterm.exe" : "channelterm";
  await cp(
    resolve("test-results", process.platform === "win32" ? "host.exe" : "host"),
    resolve(directory, "dist/host", executable),
  );
  await writeFile(
    resolve(directory, "package.json"),
    JSON.stringify({
      name: "channelterm-startup-test",
      main: "dist/main/index.cjs",
    }),
  );
  const control = resolve(directory, "stop-host");
  const token = randomBytes(24).toString("hex");
  const env = {
    ...environment,
    CHANNELTERM_GUI_ENDPOINT: defaultHostEndpoint,
    CHANNELTERM_HTTP_AUTH_TOKEN: token,
    CHANNELTERM_FIXTURE_STARTUP_CONTROL: control,
  };
  const observer = new HostClient(defaultHostEndpoint, token);
  let desktop;
  let ownsFixture = false;
  const applications = [];
  async function open(environment) {
    desktop = await electron.launch({
      args: [...preloadArgs, "-r", profilePreload, "--lang=en-US", directory],
      env: environment,
    });
    applications.push(desktop);
    return desktop.firstWindow();
  }
  async function quit() {
    const pid = await desktop.evaluate(() => process.pid);
    await desktop.evaluate(({ app }) => {
      setImmediate(() => app.quit());
    });
    // On Windows Playwright launches through cmd.exe, whose close event may wait
    // for the detached Host. Verify the actual Electron process, not that wrapper.
    await expect
      .poll(
        () => {
          try {
            process.kill(pid, 0);
            return false;
          } catch {
            return true;
          }
        },
        { timeout: 10000 },
      )
      .toBe(true);
    desktop = undefined;
  }
  try {
    let page = await open(env);
    await expect(
      page.getByRole("button", { name: "Disconnect Host", exact: true }),
    ).toBeVisible({ timeout: 20000 });
    assert.equal(await readFile(control + ".ready", "utf8"), "ready");
    ownsFixture = true;
    await observer.connect();
    assert.equal((await observer.list()).length, 1);
    await page.locator('[data-menu="session"]').click();
    await page
      .getByRole("menuitem", { name: "New Connection...", exact: true })
      .click();
    await expect(page.getByRole("dialog")).toBeVisible();
    await expect(
      page
        .getByRole("combobox", { name: "Protocol", exact: true })
        .locator('option[value="ssh"]'),
    ).toBeEnabled();
    await page.keyboard.press("Escape");
    await page
      .getByRole("button", { name: "Disconnect Host", exact: true })
      .click();
    await expect(
      page.getByRole("button", { name: "Connect to Host", exact: true }),
    ).toBeVisible();
    await page.waitForTimeout(1700);
    await expect(
      page.getByRole("button", { name: "Connect to Host", exact: true }),
    ).toBeVisible();
    await quit();
    assert.equal(
      (await observer.list()).length,
      1,
      "shared Session survives GUI exit",
    );
    // With the companion removed, a second GUI must reuse the existing listener.
    const { rename } = await import("node:fs/promises");
    await rename(
      resolve(directory, "dist/host", executable),
      resolve(directory, "dist/host", executable + ".unused"),
    );
    page = await open(env);
    await expect(
      page.getByRole("button", { name: "Disconnect Host", exact: true }),
    ).toBeVisible();
    await quit();
    page = await open({
      ...env,
      CHANNELTERM_HTTP_AUTH_TOKEN: "wrong-disposable-token",
    });
    await expect(
      page.getByRole("button", { name: "Retry connection", exact: true }),
    ).toBeVisible();
    await expect(page.getByText(/Host rejected authentication/)).toBeVisible();
    assert.equal((await observer.list()).length, 1);
    console.log(
      "PASS automatic local Host startup, reuse, authentication failure, manual disconnect and shared lifetime",
    );
  } catch (error) {
    console.error("Startup scenario failed:", error);
    throw error;
  } finally {
    if (desktop) await quit().catch(() => {});
    await observer.disconnect().catch(() => {});
    // This private control stops only our disposable fixture, never a user Host.
    if (
      ownsFixture ||
      (await readFile(control + ".ready", "utf8").catch(() => "")) === "ready"
    ) {
      await writeFile(control, "stop");
      await expect
        .poll(() => localListener(new AbortController().signal))
        .toBe(false);
    }
    await Promise.all(applications.map((app) => app.close().catch(() => {})));
  }
}

async function checkCredentialRecovery(environment, preloadArgs) {
  const directory = await mkdtemp(resolve("test-results/credential-recovery-"));
  const config = resolve(directory, "config");
  await mkdir(config);
  const profilePreload = resolve(directory, "profile.cjs");
  await writeFile(
    profilePreload,
    `const { app } = require("electron");\napp.setPath("userData", ${JSON.stringify(directory)});\napp.setPath("sessionData", ${JSON.stringify(directory)});\n`,
  );
  const legacyPath = resolve(config, "http-auth-token");
  await writeFile(legacyPath, "retired-test-token\n");
  const fixture = await startFixture();
  const env = {
    ...environment,
    CHANNELTERM_HOST_CONFIG_DIR: config,
    CHANNELTERM_GUI_ENDPOINT: fixture.endpoint,
  };
  delete env.CHANNELTERM_HTTP_AUTH_TOKEN;
  const observer = new HostClient(fixture.endpoint, fixture.token);
  let desktop;
  try {
    await observer.connect();
    const sessions = await observer.list();
    desktop = await electron.launch({
      args: [...preloadArgs, "-r", profilePreload, "--lang=en-US", "."],
      env,
    });
    const page = await desktop.firstWindow();
    await page
      .getByRole("button", { name: "Connect to Host", exact: true })
      .click();
    await expect(page.getByRole("alert")).toContainText(
      "Old Host configuration detected",
    );
    await page
      .getByRole("button", { name: "Retry connection", exact: true })
      .click();
    await expect(page.getByRole("alert")).toContainText(
      "Retrying alone cannot upgrade",
    );
    await desktop.evaluate(({ Menu }) =>
      Menu.getApplicationMenu().getMenuItemById("language.zh-CN").click(),
    );
    await expect(page.getByRole("alert")).toContainText(
      "仅点击重试无法升级 Host",
    );
    await page.screenshot({
      path: "test-results/host-credential-recovery.png",
    });
    assert.equal(await readFile(legacyPath, "utf8"), "retired-test-token\n");
    assert.deepEqual(await observer.list(), sessions);
    // Simulate completion of Host initialization; credentials stay private and
    // the next explicit retry must reread files without restarting Electron.
    const hostID = "H-0123456789abcdef0123456789abcdef";
    await writeFile(
      resolve(config, "http-clients-" + hostID + ".json.local-token"),
      fixture.token,
    );
    await writeFile(
      resolve(config, "http-host.json"),
      JSON.stringify({ version: 1, host_id: hostID }),
    );
    await page
      .getByRole("button", { name: "Retry connection", exact: true })
      .click();
    await expect(
      page.getByRole("button", { name: "Disconnect Host", exact: true }),
    ).toBeVisible();
    assert.deepEqual(await observer.list(), sessions);
    console.log(
      "PASS legacy-only credentials on an occupied isolated Host: localized diagnostics, safe retry, preserved Sessions and recovery after initialization.",
    );
  } finally {
    await desktop?.close().catch(() => {});
    await observer.disconnect();
    await fixture.stop();
  }
}
