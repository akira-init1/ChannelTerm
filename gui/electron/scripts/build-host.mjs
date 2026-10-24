import { spawn } from "node:child_process";
import { access, mkdir } from "node:fs/promises";
import { fileURLToPath } from "node:url";
import { resolve } from "node:path";

/** Developer preparation only: Electron never executes Go or searches PATH for a Host. */
export async function buildHost({ onlyIfMissing = false } = {}) {
  const root = fileURLToPath(new URL("../../../", import.meta.url));
  const directory = resolve(root, "gui/electron/dist/host");
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
  await buildHost();
