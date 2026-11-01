import test from "node:test";
import { execFile } from "node:child_process";
import { promisify } from "node:util";
import { resolve } from "node:path";
import { pathToFileURL } from "node:url";

test("developer startup verifies build inputs, preserves active Hosts and rebuilds stale idle companions", async () => {
  const script = pathToFileURL(resolve("scripts/host-freshness.mjs")).href;
  // Run the actual developer module without importing untyped script code into TS.
  await promisify(execFile)(
    process.execPath,
    [
      "--input-type=module",
      "--eval",
      `
    import assert from "node:assert/strict";
    import { mkdtemp, mkdir, writeFile, readFile, rm, utimes } from "node:fs/promises";
    import { tmpdir } from "node:os";
    import { join } from "node:path";
    import { hostSourceFingerprint, recordHostBuild, hostBuildCurrent, prepareHostBuild } from ${JSON.stringify(script)};
    const root = await mkdtemp(join(tmpdir(), "cterm-build-"));
    try {
      for (const dir of ["cmd", "internal", "third_party"]) await mkdir(join(root, dir));
      await writeFile(join(root, "go.mod"), "module fixture");
      await writeFile(join(root, "go.sum"), "");
      const input = join(root, "internal", "embed.json");
      await writeFile(input, "old");
      const executable = join(root, "host");
      let builds = 0, probes = 0, active = false;
      const options = {
        root, executable,
        listening: async () => { probes++; return active; },
        build: async () => {
          builds++;
          const source = await hostSourceFingerprint(root);
          await writeFile(executable, "binary-" + builds);
          await recordHostBuild(executable, source);
        },
      };
      // An old binary without provenance must not be silently reused or replaced while busy.
      await writeFile(executable, "legacy");
      active = true;
      assert.equal(await prepareHostBuild(options), "outdated");
      assert.equal(await readFile(executable, "utf8"), "legacy");
      assert.equal(builds, 0);
      active = false;
      assert.equal(await prepareHostBuild(options), "rebuilt");
      assert.equal(builds, 1);
      const previousProbes = probes;
      assert.equal(await prepareHostBuild(options), "current");
      assert.equal(probes, previousProbes); // No Go invocation or listener probe for a verified build.
      await writeFile(input, "new");
      await utimes(input, 0, 0); // Timestamp rollback must not hide source changes.
      active = true;
      assert.equal(await prepareHostBuild(options), "outdated");
      active = false;
      await assert.rejects(prepareHostBuild({ ...options, build: async () => { throw Error("no compiler"); } }), /no compiler/);
      assert.equal(await hostBuildCurrent(executable, await hostSourceFingerprint(root)), false);
      await assert.rejects(prepareHostBuild({ ...options, listening: async () => { throw Error("probe timeout"); } }), /probe timeout/);
      assert.equal(builds, 1);
      assert.equal(await prepareHostBuild(options), "rebuilt");
      await writeFile(executable, "replaced binary");
      assert.equal(await hostBuildCurrent(executable, await hostSourceFingerprint(root)), false);
      assert.equal(await prepareHostBuild(options), "rebuilt");
      await writeFile(join(root, "third_party", "replacement.go"), "changed module");
      assert.equal(await hostBuildCurrent(executable, await hostSourceFingerprint(root)), false);
      await assert.rejects(prepareHostBuild({ ...options, build: async () => {
        await options.build();
        await writeFile(input, "changed during build");
      } }), /inputs changed/);
    } finally { await rm(root, { recursive: true, force: true }); }
  `,
    ],
    { windowsHide: true },
  );
});
