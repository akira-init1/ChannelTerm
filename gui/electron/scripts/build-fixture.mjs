import { mkdirSync } from "node:fs";
import { resolve } from "node:path";
import { execFileSync } from "node:child_process";

mkdirSync("test-results", { recursive: true });
execFileSync(
  "go",
  [
    "build",
    "-o",
    resolve("test-results", process.platform === "win32" ? "host.exe" : "host"),
    "./gui/electron/tests/host",
  ],
  {
    cwd: "../..",
    stdio: "inherit",
    env: { ...process.env, CGO_ENABLED: "0" },
  },
);
