import assert from "node:assert/strict";
import { mkdtemp, mkdir, writeFile, readFile, cp } from "node:fs/promises";
import { resolve, join } from "node:path";
import { spawn, execFile } from "node:child_process";
import { promisify } from "node:util";
import { createServer, connect } from "node:net";
import { once } from "node:events";
import { _electron as electron, expect } from "@playwright/test";
import { setupI18n } from "@lingui/core";
import { menuCatalogs } from "../src/locales/catalogs.ts";
import { locales } from "../src/shared/menus.ts";
import { buildHost } from "./build-host.mjs";
import { localClientTokenPath } from "../src/main/host/config.ts";
import { HostClient } from "../src/main/host/client.ts";

const execute = promisify(execFile);

/** Real local Host authority and disposable client homes; never modifies user registrations. */
export async function checkMCPSetup(environment) {
  await buildHost({ fileClient: true });
  const profile = await mkdtemp(resolve("test-results/mcp-setup-"));
  const home = join(profile, "home");
  const hostDirectory = join(profile, "host");
  await mkdir(home);
  const config = join(home, ".codex", "config.toml");
  await mkdir(join(home, ".codex"));
  await writeFile(config, 'unrelated = "keep-me"\n');
  const preload = join(profile, "profile.cjs");
  await writeFile(
    preload,
    `const {app}=require("electron");app.setPath("userData",${JSON.stringify(profile)});app.setPath("sessionData",${JSON.stringify(profile)});`,
  );
  const listener = createServer();
  listener.listen(0, "127.0.0.1");
  await once(listener, "listening");
  const port = listener.address().port;
  await new Promise((done) => listener.close(done));
  const endpoint = `http://127.0.0.1:${port}/mcp`;
  const env = {
    ...environment,
    USERPROFILE: home,
    HOME: home,
    APPDATA: join(home, "appdata"),
    XDG_CONFIG_HOME: join(home, ".config"),
    CODEX_HOME: join(home, ".codex"),
    CHANNELTERM_HOST_CONFIG_DIR: hostDirectory,
    CHANNELTERM_GUI_ENDPOINT: endpoint,
  };
  for (const key of [
    "ELECTRON_RUN_AS_NODE",
    "CHANNELTERM_HTTP_AUTH_TOKEN",
    "OPENCODE_CONFIG",
    "OPENCODE_CONFIG_CONTENT",
    "OPENCODE_CONFIG_DIR",
    "CLAUDE_CONFIG_DIR",
  ])
    delete env[key];
  const executable = resolve(
    "dist/files",
    process.platform === "win32" ? "channelterm.exe" : "channelterm",
  );
  const ownedHost = join(
    profile,
    process.platform === "win32" ? "test-host.exe" : "test-host",
  );
  await cp(executable, ownedHost);
  const hostProcess = spawn(
    ownedHost,
    [
      "mcp",
      "--transport",
      "http",
      "--listen",
      `127.0.0.1:${port}`,
      "--path",
      "/mcp",
    ],
    { env, windowsHide: true, stdio: "ignore" },
  );
  let desktop, observer;
  const cli = async (args) =>
    JSON.parse(
      (
        await execute(executable, args, {
          env,
          windowsHide: true,
          timeout: 15000,
          maxBuffer: 65536,
        })
      ).stdout,
    );
  try {
    let token;
    await expect
      .poll(
        async () => {
          try {
            const manifest = join(hostDirectory, "http-host.json");
            const identity = JSON.parse(await readFile(manifest, "utf8"));
            token = (
              await readFile(
                localClientTokenPath(manifest, identity.host_id),
                "utf8",
              )
            ).trim();
            return true;
          } catch {
            return false;
          }
        },
        { timeout: 15000 },
      )
      .toBe(true);
    await expect
      .poll(
        () =>
          new Promise((done) => {
            const socket = connect(port, "127.0.0.1");
            socket.setTimeout(500);
            socket.once("connect", () => {
              socket.destroy();
              done(true);
            });
            socket.once("error", () => {
              socket.destroy();
              done(false);
            });
            socket.once("timeout", () => {
              socket.destroy();
              done(false);
            });
          }),
        { timeout: 15000 },
      )
      .toBe(true);
    observer = new HostClient(endpoint, token);
    await observer.connect();
    desktop = await electron.launch({
      args: ["-r", preload, "--lang=en-US", "."],
      env,
    });
    const page = await desktop.firstWindow();
    await expect(page.locator('[data-menu="host"]')).toBeVisible();
    const invalid = await page.evaluate(() =>
      window.channelterm.mcpSetup({
        client: "codex",
        scope: "user",
        action: "configure",
        replace: false,
        path: "arbitrary",
      }),
    );
    assert.equal(invalid.error.code, "invalid_input");
    await desktop.evaluate(({ dialog }) => {
      globalThis.setupChoice = 0;
      globalThis.setupPreview = "";
      dialog.showMessageBox = async (_window, options) => {
        globalThis.setupPreview = options.detail;
        return { response: globalThis.setupChoice };
      };
    });
    const open = async () => {
      await page.locator('[data-menu="host"]').click();
      await page.locator('[data-action="setup.open"]').click();
      await expect(
        page.getByRole("button", {
          name: "Preview configuration or repair",
          exact: true,
        }),
      ).toBeEnabled({ timeout: 30000 });
    };
    await open();
    await page
      .getByRole("button", {
        name: "Preview configuration or repair",
        exact: true,
      })
      .click();
    await expect(
      page.getByText("Cancelled. No configuration was applied.", {
        exact: true,
      }),
    ).toBeVisible();
    assert.equal(await readFile(config, "utf8"), 'unrelated = "keep-me"\n');
    assert.equal((await cli(["clients", "list"])).length, 1);
    await desktop.evaluate(() => {
      globalThis.setupChoice = 1;
    });
    await page
      .getByRole("button", {
        name: "Preview configuration or repair",
        exact: true,
      })
      .click();
    await expect(
      page.getByText(
        "Configuration saved and independent read-only access verified. Client reload is still required.",
        { exact: true },
      ),
    ).toBeVisible({ timeout: 30000 });
    const clients = await cli(["clients", "list"]);
    assert.equal(clients.length, 2);
    const first = clients.find((client) => client.kind === "agent");
    assert.equal(first.write_allowed, false);
    const stored = await readFile(config, "utf8");
    assert.ok(stored.includes("keep-me"));
    assert.ok(stored.includes("Authorization"));
    const preview = await desktop.evaluate(() => globalThis.setupPreview);
    assert.ok(preview.includes(config));
    assert.ok(preview.includes("[redacted]"));
    const secret = stored.match(/Bearer ([a-f0-9]{64})/)[1];
    assert.equal(preview.includes(secret), false);
    assert.equal(
      (await page.locator("body").innerText()).includes(secret),
      false,
    );
    assert.equal(
      (await page.locator("body").innerText()).includes(config),
      false,
    );
    await page
      .getByRole("button", {
        name: "Preview configuration or repair",
        exact: true,
      })
      .click();
    await expect(
      page.getByText(
        "Existing entry preserved. Verify it, or explicitly select replacement to repair it.",
        { exact: true },
      ),
    ).toBeVisible();
    assert.equal(await readFile(config, "utf8"), stored);
    await page
      .getByRole("button", { name: "Verify existing access", exact: true })
      .click();
    await expect(
      page.getByText(
        "Existing credential passed a read-only probe. Client reload and effective permissions are not inferred.",
        { exact: true },
      ),
    ).toBeVisible({ timeout: 15000 });
    // Revoke only this test's own agent, then repair without touching the local admin.
    await execute(executable, ["clients", "remove", first.client_id], {
      env,
      windowsHide: true,
      timeout: 15000,
    });
    await page
      .getByRole("button", { name: "Verify existing access", exact: true })
      .click();
    await expect(
      page.getByText(
        "Credential missing or authentication rejected. Check the Host domain, then explicitly repair this client.",
        { exact: true },
      ),
    ).toBeVisible({ timeout: 15000 });
    await page.getByRole("checkbox").check();
    await page
      .getByRole("button", {
        name: "Preview configuration or repair",
        exact: true,
      })
      .click();
    await expect(
      page.getByText(
        "Configuration saved and independent read-only access verified. Client reload is still required.",
        { exact: true },
      ),
    ).toBeVisible({ timeout: 30000 });
    assert.notEqual(await readFile(config, "utf8"), stored);
    await page.getByRole("button", { name: "Close", exact: true }).click();
    let previousLocale = "en";
    for (const locale of locales) {
      // Client discovery can change OS focus. Choose the language in the tested window.
      const previous = setupI18n({
        locale: previousLocale,
        messages: menuCatalogs,
      });
      await page.locator('[data-menu="settings"]').click();
      await page
        .getByRole("menuitem", {
          name: previous._("menu.language"),
          exact: true,
        })
        .hover();
      await page
        .getByRole("menuitemradio", {
          name: previous._("language." + locale),
          exact: true,
        })
        .click();
      const t = setupI18n({ locale, messages: menuCatalogs });
      await expect(
        page.getByRole("searchbox", {
          name: t._("workbench.search"),
          exact: true,
        }),
      ).toBeVisible();
      previousLocale = locale;
      await page.locator('[data-menu="host"]').click();
      await page.locator('[data-action="setup.open"]').click();
      const modal = page.getByRole("dialog", { name: t._("setup.open") });
      await expect(
        modal.getByRole("button", {
          name: t._("setup.configure"),
          exact: true,
        }),
      ).toBeEnabled({ timeout: 30000 });
      if (locale === "zh-CN") {
        await desktop.evaluate(({ BrowserWindow }) =>
          BrowserWindow.getAllWindows()[0].setSize(760, 500),
        );
        await expect(modal.locator("footer button").last()).toBeInViewport({
          ratio: 1,
        });
        await expect
          .poll(() =>
            modal
              .locator(".recording-content")
              .evaluate(
                (element) => element.scrollWidth <= element.clientWidth,
              ),
          )
          .toBe(true);
        await page.screenshot({ path: "test-results/mcp-setup-compact.png" });
      }
      await modal
        .getByRole("button", { name: t._("diagnostics.close"), exact: true })
        .click();
    }
    const available = (
      await page.evaluate(() => window.channelterm.mcpSetupClients())
    ).value;
    const official = async (name, args) =>
      execute(
        process.platform === "win32" ? "cmd.exe" : name,
        process.platform === "win32"
          ? ["/d", "/c", [name, ...args].join(" ")]
          : args,
        { env, cwd: home, windowsHide: true, timeout: 25000, maxBuffer: 65536 },
      );
    if (available.find((client) => client.id === "codex")?.version) {
      const parsed = await official("codex", ["mcp", "get", "channelterm"]);
      assert.ok(
        parsed.stdout.includes(endpoint),
        "installed Codex must read the saved configuration in a new process",
      );
      console.log(
        "PASS installed Codex reads isolated saved configuration in a new process (configuration parsing only; not a connection claim)",
      );
    }
    if (available.find((client) => client.id === "opencode")?.version) {
      const configured = await page.evaluate(() =>
        window.channelterm.mcpSetup({
          client: "opencode",
          scope: "user",
          action: "configure",
          replace: false,
        }),
      );
      assert.equal(configured.ok, true);
      assert.equal(configured.value.state, "applied");
      const listed = await official("opencode", ["--pure", "mcp", "list"]);
      assert.ok(
        /channelterm/.test(listed.stdout) && /connected/i.test(listed.stdout),
        "installed OpenCode must authenticate and report connected after reading saved configuration",
      );
      console.log(
        "PASS installed OpenCode reloads isolated saved configuration and reports an authenticated MCP connection; no agent prompt sent",
      );
    } else
      console.log(
        "SKIP installed OpenCode reload: executable/version unavailable; adapter and native workflow checks still run",
      );
    await desktop.close();
    desktop = undefined;
    assert.deepEqual(await observer.list(), []);
    console.log(
      "PASS native MCP setup: explicit fallback preview/cancel/apply, preserved config, read-only registration, revoked authentication/repair, secret and path isolation, nine locales and compact layout",
    );
  } finally {
    await desktop?.close();
    await observer?.disconnect();
    hostProcess.kill();
    await once(hostProcess, "exit").catch(() => {});
  }
}
