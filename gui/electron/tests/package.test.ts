import assert from "node:assert/strict";
import { execFile } from "node:child_process";
import {
  copyFile,
  mkdir,
  mkdtemp,
  readFile,
  rm,
  stat,
  writeFile,
} from "node:fs/promises";
import { tmpdir } from "node:os";
import { basename, join } from "node:path";
import { test } from "node:test";
import { promisify } from "node:util";
// Packaging runs outside application processes and has no renderer capability.
const script = new URL("../scripts/package.mjs", import.meta.url).href;
const {
  appMetadata,
  builderConfiguration,
  createBuildDirectory,
  packageOptions,
} = await import(script);

test("the icon command imports, regenerates and checks assets in a clean checkout", async (t) => {
  const { iconPython } = await import(
    new URL("../scripts/generate-icons.mjs", import.meta.url).href
  );
  const exec = promisify(execFile);
  const root = await mkdtemp(join(tmpdir(), "channelterm-icons-"));
  t.after(() => rm(root, { recursive: true, force: true }));
  const tool = join(root, "scripts", "replace_icons.py");
  const brand = join(root, "assets", "brand");
  const master = join(brand, "channelterm-logo.svg");
  await mkdir(join(root, "scripts"));
  await mkdir(brand, { recursive: true });
  await copyFile(
    new URL("../../../scripts/replace_icons.py", import.meta.url),
    tool,
  );
  await copyFile(
    new URL("../../../assets/brand/channelterm-logo.svg", import.meta.url),
    master,
  );
  const run = (...args: string[]) =>
    exec(iconPython, [tool, ...args], { cwd: root });

  // On Windows, staged file replacement must preserve the build user's inherited read access.
  const shell = (command: string) =>
    exec("powershell.exe", ["-NoProfile", "-Command", command], { cwd: brand });
  let sid = "";
  if (process.platform === "win32") {
    sid = (
      await shell(
        "[Security.Principal.WindowsIdentity]::GetCurrent().User.Value",
      )
    ).stdout.trim();
    assert.match(sid, /^S-1-\d+(?:-\d+)+$/);
    await exec("icacls", [brand, "/grant", `*${sid}:(OI)(CI)(RX)`]);
  }
  await Promise.all([run(), run()]);
  await run("--check");
  if (sid) {
    const acl = await shell(
      "[IO.File]::GetAccessControl('generated/channelterm-window.png').GetAccessRules($false, $true, [Security.Principal.SecurityIdentifier]).IdentityReference.Value",
    );
    assert.ok(acl.stdout.split(/\r?\n/).includes(sid));
  }

  const candidate = join(root, "新 logo.svg");
  const original = await readFile(master);
  await writeFile(candidate, "<svg");
  await assert.rejects(run(candidate));
  assert.deepEqual(await readFile(master), original);
  await run("--check");

  const replacement =
    '<svg xmlns="http://www.w3.org/2000/svg" viewBox="0 0 100 100"><circle cx="50" cy="50" r="46" fill="red"/></svg>';
  await writeFile(candidate, replacement);
  await run(candidate);
  assert.equal(await readFile(master, "utf8"), replacement);
  await run("--check");
  const icon = join(brand, "generated", "channelterm-window.ico");
  const expected = await readFile(icon);
  await rm(icon);
  await assert.rejects(run("--check"), /missing or stale icon/);
  await assert.rejects(stat(icon), { code: "ENOENT" });
  await run();
  assert.deepEqual(await readFile(icon), expected);
});

test("concurrent package directories carry the same UTC timestamp and distinct IDs", async (t) => {
  const root = await mkdtemp(join(tmpdir(), "channelterm-build-"));
  t.after(() => rm(root, { recursive: true, force: true }));
  const built = "2026-11-28T14:18:07Z";
  const paths = await Promise.all([
    createBuildDirectory(root, built),
    createBuildDirectory(root, built),
  ]);
  assert.notEqual(paths[0], paths[1]);
  for (const path of paths) {
    assert.match(basename(path), /^build-[0-9a-f]{6}-20261128T141807Z$/);
    assert.ok((await stat(path)).isDirectory());
  }
  await assert.rejects(createBuildDirectory(join(root, "missing"), built), {
    code: "ENOENT",
  });
});

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
        { platform, arch, goos, goarch, directory: true, archive: false },
      );
    }
  }
  for (const args of [["--publish=always"], ["--arch=ia32"], ["--output=../"]])
    assert.throws(() => packageOptions(args, "win32", "x64"));
  assert.throws(() => packageOptions([], "android", "arm64"));
});

test("packages include the separate distribution archive only with --all", () => {
  for (const [platform, key, installer, archive] of [
    ["win32", "win", "nsis", "zip"],
    ["linux", "linux", "deb", "tar.gz"],
    ["darwin", "mac", "dmg", "zip"],
  ] as const) {
    for (const args of [[], ["--all", "--arch=arm64"]]) {
      const options = packageOptions(args, platform, "x64");
      const config = builderConfiguration(
        options,
        "stage",
        { version: "1" },
        "45.0.0",
      );
      assert.equal(options.directory, false);
      assert.deepEqual(
        [...config[key].target].sort(),
        args.includes("--all") ? [installer, archive].sort() : [installer],
      );
    }
  }
  for (const args of [
    ["--dir", "--all"],
    ["--all", "--dir"],
  ])
    assert.throws(() => packageOptions(args), /either --dir or --all/);
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

test("desktop packages use the generated multi-resolution brand icons", async () => {
  const config = builderConfiguration(
    packageOptions([], "win32", "x64"),
    "stage",
    { version: "0.1.0" },
    "45.0.0",
  );
  const sizes = [16, 24, 32, 48, 64, 128, 256, 512, 1024];
  for (const size of sizes) {
    const png = await readFile(join(config.linux.icon, `${size}x${size}.png`));
    assert.equal(png.toString("ascii", 12, 16), "IHDR");
    assert.equal(png.readUInt32BE(16), size);
    assert.equal(png.readUInt32BE(20), size);
    assert.equal(png[25], 6); // RGBA
  }
  assert.equal(config.win.icon, config.nsis.installerIcon);
  assert.equal(config.win.icon, config.nsis.uninstallerIcon);
  for (const [resource, filename, original] of [
    [config.win.extraResources[0], "channelterm-window.ico", config.win.icon],
    [
      config.linux.extraResources[0],
      "channelterm-window.png",
      join(config.linux.icon, "512x512.png"),
    ],
  ]) {
    assert.equal(resource.to, filename);
    assert.deepEqual(await readFile(resource.from), await readFile(original));
  }
  const ico = await readFile(config.win.icon);
  assert.equal(ico.readUInt16LE(2), 1);
  const frames = Array.from({ length: ico.readUInt16LE(4) }, (_, index) => {
    const entry = 6 + index * 16;
    assert.equal(ico[entry], ico[entry + 1]);
    return ico[entry] || 256;
  });
  assert.deepEqual(
    frames,
    sizes.filter((size) => size <= 256),
  );

  const icns = await readFile(config.mac.icon);
  assert.equal(icns.toString("ascii", 0, 4), "icns");
  assert.equal(icns.readUInt32BE(4), icns.length);
  const retinaSizes = new Map<string, number>();
  for (let offset = 8; offset < icns.length;) {
    const type = icns.toString("ascii", offset, offset + 4);
    const length = icns.readUInt32BE(offset + 4);
    assert.ok(length >= 8 && offset + length <= icns.length);
    if (type !== "TOC ") {
      assert.equal(icns.toString("ascii", offset + 20, offset + 24), "IHDR");
      retinaSizes.set(type, icns.readUInt32BE(offset + 24));
    }
    offset += length;
  }
  assert.equal(retinaSizes.get("ic10"), 1024);
  assert.equal(retinaSizes.get("ic11"), 32);
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
