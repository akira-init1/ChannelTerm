import { createHash, randomBytes } from "node:crypto";
import { execFileSync, spawn } from "node:child_process";
import {
  chmod,
  copyFile,
  cp,
  mkdir,
  readFile,
  writeFile,
} from "node:fs/promises";
import { dirname, join, resolve } from "node:path";
import { fileURLToPath } from "node:url";

const project = fileURLToPath(new URL("../", import.meta.url));
const repository = resolve(project, "../..");
const brand = join(repository, "assets/brand/generated");
const windowsIcon = join(brand, "channelterm.ico");
const platforms = { win32: "windows", linux: "linux", darwin: "darwin" };
const arches = { x64: "amd64", arm64: "arm64" };

/** One native OS per invocation; architecture is explicit in both runtime and Go. */
export function packageOptions(
  args,
  platform = process.platform,
  arch = process.arch,
) {
  let directory = false;
  let archive = false;
  for (const arg of args) {
    if (arg === "--dir") directory = true;
    else if (arg === "--all") archive = true;
    else if (arg.startsWith("--arch=")) arch = arg.slice(7);
    else throw new Error(`Unknown package option: ${arg}`);
  }
  if (directory && archive) throw new Error("Choose either --dir or --all.");
  if (!Object.hasOwn(platforms, platform) || !Object.hasOwn(arches, arch))
    throw new Error(
      "Packaging supports Windows, Linux and macOS on x64/arm64 only.",
    );
  return {
    platform,
    arch,
    directory,
    archive,
    goos: platforms[platform],
    goarch: arches[arch],
  };
}

/** Explicit allowlist: no source, node_modules, developer profiles or credentials. */
export function appMetadata(source) {
  return {
    name: source.name,
    version: source.version,
    description: source.description,
    license: source.license,
    main: "dist/main/index.cjs",
    desktopName: "channelterm-desktop.desktop",
    // Preserve the development application's user-data identity after packaging.
    productName: source.name,
  };
}

export function builderConfiguration(
  options,
  stage,
  metadata,
  electronVersion,
) {
  return {
    appId: "io.github.akira-init1.channelterm.desktop",
    productName: "ChannelTerm Desktop",
    executableName: "channelterm-desktop",
    electronVersion,
    directories: { app: join(stage, "app"), output: join(stage, "artifacts") },
    files: ["package.json", "dist/**/*", "!node_modules/**/*"],
    extraResources: [
      { from: join(stage, "resources"), to: ".", filter: ["**/*"] },
    ],
    asar: true,
    npmRebuild: false,
    nodeGypRebuild: false,
    publish: null,
    artifactName: `ChannelTerm-Desktop-${metadata.version}-${options.goos}-${options.goarch}.${"${ext}"}`,
    win: {
      target: options.archive ? ["nsis", "zip"] : ["nsis"],
      signExecutable: false,
      icon: windowsIcon,
      extraResources: [
        {
          from: join(brand, "channelterm-window.ico"),
          to: "channelterm-window.ico",
        },
      ],
    },
    nsis: {
      installerIcon: windowsIcon,
      uninstallerIcon: windowsIcon,
      oneClick: false,
      perMachine: false,
      allowElevation: false,
      allowToChangeInstallationDirectory: true,
      runAfterFinish: false,
      deleteAppDataOnUninstall: false,
      createDesktopShortcut: false,
      createStartMenuShortcut: true,
      packElevateHelper: false,
      include: join(project, "scripts/installer.nsh"),
    },
    linux: {
      target: options.archive ? ["tar.gz", "deb"] : ["deb"],
      icon: join(brand, "png"),
      extraResources: [
        {
          from: join(brand, "channelterm-window.png"),
          to: "channelterm-window.png",
        },
      ],
      category: "Development",
      maintainer: "ChannelTerm contributors",
    },
    // Archive and disk-image users copy the app to a stable directory before starting
    // a persistent Host. No self-extracting portable runtime with temporary helpers.
    mac: {
      icon: join(brand, "channelterm.icns"),
      target: options.archive ? ["zip", "dmg"] : ["dmg"],
      identity: null,
      notarize: false,
      category: "public.app-category.developer-tools",
    },
  };
}

export async function sha256(path) {
  return createHash("sha256")
    .update(await readFile(path))
    .digest("hex");
}

function run(command, args, env) {
  return new Promise((accept, reject) => {
    const child = spawn(command, args, {
      cwd: repository,
      env,
      stdio: "inherit",
      windowsHide: true,
    });
    child.once("error", reject);
    child.once("exit", (code) =>
      code === 0
        ? accept()
        : reject(new Error(`${command} exited with ${code}`)),
    );
  });
}

/** Reserve a fresh directory even when multiple builds start in the same second. */
export async function createBuildDirectory(output, built) {
  const timestamp = built.replace(/[-:]/g, "");
  for (;;) {
    const id = randomBytes(3).toString("hex");
    const stage = join(output, `build-${id}-${timestamp}`);
    try {
      await mkdir(stage, { mode: 0o700 });
      return stage;
    } catch (error) {
      if (error.code !== "EEXIST") throw error;
    }
  }
}

/** Fresh directories avoid replacing a persistent developer or packaged Host. */
export async function packageDesktop(options) {
  const source = JSON.parse(
    await readFile(join(project, "package.json"), "utf8"),
  );
  const metadata = appMetadata(source);
  let commit = execFileSync("git", ["rev-parse", "--short=12", "HEAD"], {
    cwd: repository,
    encoding: "utf8",
    windowsHide: true,
  }).trim();
  if (
    execFileSync("git", ["status", "--porcelain"], {
      cwd: repository,
      encoding: "utf8",
      windowsHide: true,
    }).trim()
  )
    commit += "-dirty";
  const built = new Date().toISOString().replace(/\.\d+Z$/, "Z");
  const output = join(project, "release", `${options.goos}-${options.goarch}`);
  await mkdir(output, { recursive: true });
  const stage = await createBuildDirectory(output, built);
  const app = join(stage, "app");
  await mkdir(join(app, "dist"), { recursive: true });
  for (const asset of [
    "main",
    "preload",
    "renderer",
    "LICENSE",
    "THIRD_PARTY_NOTICES.txt",
  ])
    await cp(join(project, "dist", asset), join(app, "dist", asset), {
      recursive: true,
    });
  await writeFile(
    join(app, "package.json"),
    JSON.stringify(metadata, null, 2) + "\n",
  );
  const resources = join(stage, "resources");
  const executable =
    options.platform === "win32" ? "channelterm.exe" : "channelterm";
  const host = join(resources, "host", executable);
  const files = join(resources, "files", executable);
  await mkdir(dirname(host), { recursive: true });
  await mkdir(dirname(files), { recursive: true });
  const symbol = "github.com/akira-init1/ChannelTerm/internal/cli/command";
  const flags = `-s -w -X ${symbol}.version=${metadata.version} -X ${symbol}.buildCommit=${commit} -X ${symbol}.buildTime=${built}`;
  await run(
    "go",
    ["build", "-trimpath", "-ldflags", flags, "-o", host, "./cmd/channelterm"],
    {
      ...process.env,
      CGO_ENABLED: "0",
      GOOS: options.goos,
      GOARCH: options.goarch,
    },
  );
  await copyFile(host, files);
  await chmod(host, 0o755);
  await chmod(files, 0o755);
  await copyFile(
    join(repository, "THIRD_PARTY_NOTICES"),
    join(resources, "HOST_THIRD_PARTY_NOTICES"),
  );
  await copyFile(
    join(repository, "LICENSE"),
    join(resources, "LICENSE.channelterm"),
  );
  await copyFile(
    join(project, "THIRD_PARTY_NOTICES"),
    join(resources, "DESKTOP_THIRD_PARTY_NOTICES"),
  );
  const manifest = {
    version: metadata.version,
    commit,
    built,
    platform: options.goos,
    arch: options.goarch,
    electron: source.devDependencies.electron,
    companions: {
      [`host/${executable}`]: await sha256(host),
      [`files/${executable}`]: await sha256(files),
    },
  };
  await writeFile(
    join(resources, "package-manifest.json"),
    JSON.stringify(manifest, null, 2) + "\n",
  );
  const config = builderConfiguration(
    options,
    stage,
    metadata,
    source.devDependencies.electron,
  );
  // This command only creates local unsigned artifacts, never publishes or auto-signs.
  process.env.CSC_IDENTITY_AUTO_DISCOVERY = "false";
  const { build, Platform, Arch } = await import("electron-builder");
  const target =
    options.platform === "win32"
      ? Platform.WINDOWS
      : options.platform === "darwin"
        ? Platform.MAC
        : Platform.LINUX;
  const artifacts = await build({
    projectDir: project,
    config,
    publish: "never",
    targets: target.createTarget(
      options.directory ? ["dir"] : undefined,
      Arch[options.arch],
    ),
  });
  const sums = [];
  for (const artifact of artifacts)
    sums.push(
      `${await sha256(artifact)}  ${artifact.slice(dirname(artifact).length + 1)}`,
    );
  await writeFile(
    join(stage, "artifacts", "SHA256SUMS"),
    sums.join("\n") + "\n",
  );
  console.log(`Local unsigned package: ${join(stage, "artifacts")}`);
  return stage;
}

if (
  process.argv[1] &&
  resolve(process.argv[1]) === fileURLToPath(import.meta.url)
)
  await packageDesktop(packageOptions(process.argv.slice(2)));
