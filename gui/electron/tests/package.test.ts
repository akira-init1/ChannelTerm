import assert from "node:assert/strict";
import { readFile } from "node:fs/promises";
import { test } from "node:test";
// Packaging runs outside application processes and has no renderer capability.
const script = new URL("../scripts/package.mjs", import.meta.url).href;
const { appMetadata, builderConfiguration, packageOptions } = await import(
  script
);

test("packaging maps all six runtime/helper pairs and rejects unsupported options", () => {
  for (const [platform, goos] of [
    ["win32", "windows"],
    ["linux", "linux"],
    ["darwin", "darwin"],
  ]) {
    for (const [arch, goarch] of [
      ["x64", "amd64"],
      ["arm64", "arm64"],
    ]) {
      assert.deepEqual(
        packageOptions([`--arch=${arch}`, "--dir"], platform, "x64"),
        { platform, arch, goos, goarch, directory: true },
      );
    }
  }
  for (const args of [["--publish=always"], ["--arch=ia32"], ["--output=../"]])
    assert.throws(() => packageOptions(args, "win32", "x64"));
  assert.throws(() => packageOptions([], "android", "arm64"));
});

test("packaging allowlist excludes development settings and preserves profile identity", () => {
  const source = {
    name: "@channelterm/electron",
    version: "0.1.0",
    description: "Desktop",
    license: "Apache-2.0",
    dependencies: { secret: "value" },
    build: { publish: "always" },
    config: { token: "fake-private" },
  };
  const metadata = appMetadata(source);
  assert.equal(metadata.productName, source.name);
  assert.equal(metadata.main, "dist/main/index.cjs");
  assert.equal(JSON.stringify(metadata).includes("fake-private"), false);
  assert.equal("dependencies" in metadata, false);
  const config = builderConfiguration(
    packageOptions([], "win32", "x64"),
    "stage",
    metadata,
    "45.0.0",
  );
  assert.equal(config.publish, null);
  assert.equal(config.nsis.deleteAppDataOnUninstall, false);
  assert.equal(config.nsis.runAfterFinish, false);
  assert.equal(config.nsis.allowElevation, false);
  assert.equal(config.executableName, "channelterm-desktop");
  assert.deepEqual(config.files, [
    "package.json",
    "dist/**/*",
    "!node_modules/**/*",
  ]);
});

test("installer running-process guard replaces process termination and fails closed", async () => {
  const text = await readFile(
    new URL("../scripts/installer.nsh", import.meta.url),
    "utf8",
  );
  assert.match(text, /!macro customCheckAppRunning/);
  assert.match(text, /FindProcess.*channelterm\.exe/);
  assert.match(text, /FindProcess.*channelterm-desktop\.exe/);
  assert.doesNotMatch(
    text,
    /(?:KillProcess|CloseProcess|taskkill|Stop-Process)/,
  );
  assert.match(text, /\$R0 != 603/);
  assert.match(text, /SetErrorLevel 2/);
});
