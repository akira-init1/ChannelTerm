import { spawn } from "node:child_process";
import { access, mkdir } from "node:fs/promises";
import { fileURLToPath } from "node:url";
import { resolve } from "node:path";
import {
  hostSourceFingerprint,
  recordHostBuild,
  prepareHostBuild,
} from "./host-freshness.mjs";

/** Developer preparation only: Electron never executes Go or searches PATH for a Host. */
export async function buildHost({
  onlyIfMissing = false,
  fileClient = false,
} = {}) {
  const root = fileURLToPath(new URL("../../../", import.meta.url));
  const directory = resolve(
    root,
    "gui/electron/dist",
    fileClient ? "files" : "host",
  );
  const executable = resolve(
    directory,
    process.platform === "win32" ? "channelterm.exe" : "channelterm",
  );
  // A persistent Host may be using this executable (especially on Windows).
  // Normal GUI launch must neither replace it nor require Go once it exists.
  if (
    onlyIfMissing &&
    (await access(executable).then(
      () => true,
      () => false,
    ))
  )
    return;
  await mkdir(directory, { recursive: true });
  const source = fileClient ? undefined : await hostSourceFingerprint(root);
  const env = { ...process.env, CGO_ENABLED: "0" };
  // Always build for the machine running Electron, independent of cross-build settings.
  delete env.GOOS;
  delete env.GOARCH;
  await new Promise((resolveBuild, reject) => {
    const child = spawn(
      "go",
      ["build", "-o", executable, "./cmd/channelterm"],
      { cwd: root, env, stdio: "inherit", windowsHide: true },
    );
    child.once("error", reject);
    child.once("exit", (code) =>
      code === 0
        ? resolveBuild()
        : reject(new Error("Companion Host build failed.")),
    );
  });
  if (source) await recordHostBuild(executable, source);
}

/** Missing developer tooling must not prevent reuse of an already-running Host. */
export async function prepareHost() {
  delete process.env.CHANNELTERM_INTERNAL_GUI_COMPANION_STATE;
  // A running Windows Host locks its executable. Keep the independently updatable
  // file client in a separate fixed location; both are built from the same CLI.
  await buildHost({ onlyIfMissing: true, fileClient: true }).catch(() => {
    console.error(
      "File companion unavailable. Run npm run files:build before using file operations.",
    );
  });
  if (
    process.env.CHANNELTERM_GUI_ENDPOINT &&
    process.env.CHANNELTERM_GUI_ENDPOINT !== "http://127.0.0.1:37099/mcp"
  )
    return;
  try {
    const root = fileURLToPath(new URL("../../../", import.meta.url));
    const executable = resolve(
      root,
      "gui/electron/dist/host",
      process.platform === "win32" ? "channelterm.exe" : "channelterm",
    );
    const state = await prepareHostBuild({
      root,
      executable,
      build: () => buildHost(),
    });
    if (state === "outdated") {
      process.env.CHANNELTERM_INTERNAL_GUI_COMPANION_STATE = "outdated";
      console.error(
        "Companion Host is outdated or unverified. An existing listener was preserved. Finish shared Session work and stop its Host, then run npm run host:build and restart the GUI.",
      );
    }
  } catch {
    process.env.CHANNELTERM_INTERNAL_GUI_COMPANION_STATE = "outdated";
    console.error(
      "Companion Host could not be verified or rebuilt. Existing Hosts remain available, but launching this companion is blocked. Run npm run host:build after stopping its Host, then restart the GUI.",
    );
  }
}

if (
  process.argv[1] &&
  resolve(process.argv[1]) === fileURLToPath(import.meta.url)
)
  await buildHost({ fileClient: process.argv.includes("--files") });
