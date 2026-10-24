import { spawn } from "node:child_process";
import { access, mkdir } from "node:fs/promises";
import { fileURLToPath } from "node:url";
import { resolve } from "node:path";

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
}

/** Missing developer tooling must not prevent reuse of an already-running Host. */
export async function prepareHost() {
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
  await buildHost({ onlyIfMissing: true }).catch(() => {
    console.error(
      "Companion Host could not be prepared. The GUI can still connect to an existing Host; use npm run host:build to repair the companion.",
    );
  });
}

if (
  process.argv[1] &&
  resolve(process.argv[1]) === fileURLToPath(import.meta.url)
)
  await buildHost({ fileClient: process.argv.includes("--files") });
