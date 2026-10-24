import test from "node:test";
import assert from "node:assert/strict";
import { execFile } from "node:child_process";
import { mkdir, mkdtemp, writeFile, readFile, rm } from "node:fs/promises";
import { resolve, join } from "node:path";
import { pathToFileURL } from "node:url";
import { promisify } from "node:util";

test("license collection skips absent optional packages but rejects missing required packages or installed licenses", async () => {
  await mkdir("test-results", { recursive: true });
  const directory = await mkdtemp(resolve("test-results", "notices-"));
  const cwd = join(directory, "gui", "electron");
  const script = pathToFileURL(resolve("scripts/notices.mjs")).href;
  async function run() {
    return promisify(execFile)(
      process.execPath,
      [
        "--input-type=module",
        "--eval",
        `import { writeNotices } from ${JSON.stringify(script)}; await writeNotices();`,
      ],
      { cwd, windowsHide: true },
    );
  }
  try {
    await mkdir(join(cwd, "dist"), { recursive: true });
    await mkdir(join(cwd, "node_modules", "installed"), { recursive: true });
    await writeFile(join(directory, "LICENSE"), "Fixture project license");
    const license = join(cwd, "node_modules", "installed", "LICENSE");
    await writeFile(license, "Fixture dependency license");
    const packages = {
      "node_modules/installed": {
        version: "1",
        license: "MIT",
        optional: true,
      },
      "node_modules/other-platform": {
        version: "1",
        license: "MIT",
        optional: true,
      },
    };
    const lock = join(cwd, "package-lock.json");
    await writeFile(lock, JSON.stringify({ packages }));
    await run();
    assert.match(
      await readFile(join(cwd, "dist", "THIRD_PARTY_NOTICES.txt"), "utf8"),
      /Fixture dependency license/,
    );
    packages["node_modules/other-platform"].optional = false;
    await writeFile(lock, JSON.stringify({ packages }));
    await assert.rejects(run(), /ENOENT/);
    packages["node_modules/other-platform"].optional = true;
    await writeFile(lock, JSON.stringify({ packages }));
    await rm(license);
    await assert.rejects(run(), /Missing license text/);
  } finally {
    // Remove only the exact mkdtemp fixture directory owned by this test.
    await rm(directory, { recursive: true, force: true });
  }
});
