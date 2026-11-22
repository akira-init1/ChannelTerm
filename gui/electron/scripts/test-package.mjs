import { chooseSession } from "./session-picker.mjs";
import assert from "node:assert/strict";
import { execFileSync } from "node:child_process";
import { cp, mkdtemp, readFile } from "node:fs/promises";
import { tmpdir } from "node:os";
import { dirname, join, resolve } from "node:path";
import { _electron as electron, expect } from "@playwright/test";
import { startFixture } from "../tests/fixture.ts";
import { HostClient } from "../src/main/host/client.ts";
import { sha256 } from "./package.mjs";
import { checkConnections } from "./connection-checks.mjs";

const input = process.argv[2];
if (!input)
  throw new Error("Usage: npm run test:package -- <unpacked app directory>");
// Copy outside the repository into a path with spaces/non-ASCII characters. The
// actual package and its native helpers run without a developer PATH or profile.
const root = await mkdtemp(join(tmpdir(), "ChannelTerm 包 验收-"));
const application = join(root, "Application");
await cp(resolve(input), application, { recursive: true });
const executablePath =
  process.platform === "darwin"
    ? join(application, "Contents/MacOS/channelterm-desktop")
    : join(
        application,
        process.platform === "win32"
          ? "channelterm-desktop.exe"
          : "channelterm-desktop",
      );
const resources =
  process.platform === "darwin"
    ? join(application, "Contents/Resources")
    : join(application, "resources");
const manifest = JSON.parse(
  await readFile(join(resources, "package-manifest.json"), "utf8"),
);
const env = { ...process.env };
for (const key of Object.keys(env)) {
  if (
    key.toLowerCase() === "path" ||
    key.startsWith("CHANNELTERM_") ||
    key === "ELECTRON_RUN_AS_NODE"
  )
    delete env[key];
}
env[process.platform === "win32" ? "Path" : "PATH"] = root;
for (const [relative, hash] of Object.entries(manifest.companions)) {
  assert.match(relative, /^(host|files)\/channelterm(?:\.exe)?$/);
  const executable = join(resources, relative);
  assert.equal(await sha256(executable), hash);
  const version = execFileSync(executable, ["version"], {
    env,
    encoding: "utf8",
    windowsHide: true,
  });
  assert.ok(version.includes(manifest.version));
  assert.ok(version.includes(manifest.commit));
  assert.ok(version.includes(`${manifest.platform}/${manifest.arch}`));
}
await readFile(join(resources, "HOST_THIRD_PARTY_NOTICES"));
await readFile(join(resources, "LICENSE.channelterm"));
await readFile(join(resources, "DESKTOP_THIRD_PARTY_NOTICES"));
const fixture = await startFixture();
const observer = new HostClient(fixture.endpoint, fixture.token);
const profile = join(root, "profile");
let desktop;
try {
  await observer.connect();
  const before = await observer.list();
  desktop = await electron.launch({
    executablePath,
    args: ["--lang=en-US", `--user-data-dir=${profile}`],
    env: {
      ...env,
      CHANNELTERM_GUI_ENDPOINT: fixture.endpoint,
      CHANNELTERM_HTTP_AUTH_TOKEN: fixture.token,
    },
  });
  const page = await desktop.firstWindow();
  const runtime = await desktop.evaluate(({ app }) => ({
    packaged: app.isPackaged,
    name: app.getName(),
    version: app.getVersion(),
    userData: app.getPath("userData"),
    appPath: app.getAppPath(),
  }));
  assert.equal(resolve(runtime.userData), resolve(profile));
  assert.equal(runtime.packaged, true);
  assert.equal(runtime.name, "@channelterm/electron");
  assert.equal(runtime.version, manifest.version);
  assert.equal(dirname(runtime.appPath), resources);
  const entries = await desktop.evaluate(({ app }) =>
    process.getBuiltinModule("fs").readdirSync(app.getAppPath()),
  );
  assert.deepEqual(entries.sort(), ["dist", "package.json"]);
  await page
    .getByRole("button", { name: "Connect to Host...", exact: true })
    .click();
  await expect(page.locator(".host-panel .connection")).toHaveText(
    "Host connected",
  );
  await chooseSession(page, "Isolated echo");
  await page
    .locator(".terminal-pane:not([hidden]) .xterm-helper-textarea")
    .focus();
  await page.keyboard.type("packaged-echo");
  await expect(page.locator(".xterm-rows")).toContainText("packaged-echo");
  await desktop.close();
  desktop = undefined;
  assert.deepEqual(
    (await observer.list()).map((session) => [
      session.session_id,
      session.state,
    ]),
    before.map((session) => [session.session_id, session.state]),
  );
} finally {
  await desktop?.close();
  await observer.disconnect();
  await fixture.stop();
}
// Reuse the production-IPC connection, host-key and real loopback SFTP transfer /
// cancellation scenarios against this package, with a second disposable profile.
await checkConnections(
  env,
  ["--lang=en-US", `--user-data-dir=${join(root, "connections")}`],
  executablePath,
);
console.log(
  `PASS packaged runtime, native helper versions/hashes, isolated empty PATH, shared Session survival and connection/SFTP checks: ${root}`,
);
