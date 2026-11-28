import { spawnSync } from "node:child_process";
import { existsSync } from "node:fs";
import { join, resolve } from "node:path";
import { fileURLToPath } from "node:url";

const root = fileURLToPath(new URL("../../../", import.meta.url));
const windows = process.platform === "win32";
const localPython = join(
  root,
  "bin/icon-venv",
  windows ? "Scripts/python.exe" : "bin/python",
);
export const iconPython = existsSync(localPython)
  ? localPython
  : windows
    ? "python"
    : "python3";
if (
  process.argv[1] &&
  resolve(process.argv[1]) === fileURLToPath(import.meta.url)
) {
  const result = spawnSync(
    iconPython,
    [join(root, "scripts/replace_icons.py"), ...process.argv.slice(2)],
    {
      stdio: "inherit",
    },
  );
  if (result.error) console.error(result.error.message);
  if (result.status !== 0) {
    console.error(
      "See Desktop brand icons in docs/development/electron-gui.md for Python setup.",
    );
    process.exitCode = result.status ?? 1;
  }
}
