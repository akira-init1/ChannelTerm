import { createHash } from "node:crypto";
import { createReadStream } from "node:fs";
import { readFile, stat } from "node:fs/promises";
import { join } from "node:path";

/** Bounded display metadata only, never a capability or a source authenticity claim. */
export function displayVersion(value: unknown): string | undefined {
  return typeof value === "string" &&
    /^[a-zA-Z0-9][a-zA-Z0-9.+_-]{0,127}$/.test(value)
    ? value
    : undefined;
}

/** Match packaged metadata to the actual fixed-path CLI. Source builds may be unknown. */
export async function companionVersion(
  root: string,
): Promise<string | undefined> {
  try {
    const path = join(root, "package-manifest.json");
    if ((await stat(path)).size > 8192) return;
    const manifest: unknown = JSON.parse(await readFile(path, "utf8"));
    if (
      !manifest ||
      typeof manifest !== "object" ||
      !("version" in manifest) ||
      !("companions" in manifest)
    )
      return;
    const version = displayVersion(manifest.version);
    const entries = manifest.companions;
    const name = `files/channelterm${process.platform === "win32" ? ".exe" : ""}`;
    if (
      !version ||
      !entries ||
      typeof entries !== "object" ||
      !(name in entries)
    )
      return;
    const expected: unknown = Reflect.get(entries, name);
    if (typeof expected !== "string" || !/^[a-f0-9]{64}$/.test(expected))
      return;
    const hash = createHash("sha256");
    let size = 0;
    for await (const chunk of createReadStream(join(root, name))) {
      size += chunk.length;
      if (size > 128 * 1024 * 1024) return;
      hash.update(chunk);
    }
    return hash.digest("hex") === expected ? version : undefined;
  } catch {
    return undefined;
  }
}
