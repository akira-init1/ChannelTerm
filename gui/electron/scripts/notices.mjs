import { copyFile, readFile, readdir, stat, writeFile } from "node:fs/promises";
import { join } from "node:path";

/** Preserve installed runtime dependency licenses alongside the bundled JS. */
export async function writeNotices() {
  const lock = JSON.parse(await readFile("package-lock.json", "utf8"));
  const sections = [
    "ChannelTerm Electron: installed runtime dependency notices\n",
  ];
  for (const [directory, entry] of Object.entries(lock.packages)) {
    if (!directory || (entry.dev && directory !== "node_modules/electron"))
      continue;
    let files;
    try {
      files = await readdir(directory);
    } catch (error) {
      // npm locks optional packages for other OS/CPU targets without installing
      // them. They are not redistributed; required or installed licenses still fail closed.
      if (entry.optional && error.code === "ENOENT") continue;
      throw error;
    }
    const names = files.filter((name) =>
      /^(license|licence|copying|notice)(\.|$|-)/i.test(name),
    );
    const texts = [];
    for (const name of names) {
      const path = join(directory, name);
      if ((await stat(path)).isFile()) texts.push(await readFile(path, "utf8"));
    }
    if (!texts.length) throw new Error(`Missing license text for ${directory}`);
    sections.push(
      `${directory} @ ${entry.version} (${entry.license})\n${texts.join("\n")}`,
    );
  }
  await writeFile(
    "dist/THIRD_PARTY_NOTICES.txt",
    sections.join("\n\n--------------------\n\n"),
  );
  await copyFile("../../LICENSE", "dist/LICENSE");
}
