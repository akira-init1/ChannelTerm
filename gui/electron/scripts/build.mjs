import { build } from "esbuild";
import { build as buildRenderer } from "vite";
import { writeNotices } from "./notices.mjs";
import { compileCatalogs } from "./i18n.mjs";

export async function buildProcesses({ strictCatalogs = true } = {}) {
  // Vite does not transform Main. Compile shared resources before esbuild and
  // Vite independently bundle their own copies of the generated catalogs.
  await compileCatalogs(strictCatalogs);
  await build({
    entryPoints: ["src/main/index.ts"],
    outfile: "dist/main/index.cjs",
    bundle: true,
    platform: "node",
    format: "cjs",
    external: ["electron"],
  });
  // A sandboxed preload cannot require arbitrary Node modules or ESM files.
  await build({
    entryPoints: ["src/preload/index.ts"],
    outfile: "dist/preload/index.cjs",
    bundle: true,
    platform: "node",
    format: "cjs",
    external: ["electron"],
  });
}

if (process.argv[1]?.endsWith("build.mjs")) {
  await buildProcesses();
  await buildRenderer();
  await writeNotices();
}
