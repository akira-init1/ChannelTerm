import { createHash } from "node:crypto";
import { readFile, readdir, writeFile } from "node:fs/promises";
import { join } from "node:path";
import { connect } from "node:net";

// Developer provenance, not authentication. Include embedded resources and local
// module replacements; timestamps and Git HEAD alone miss dirty source changes.
export async function hostSourceFingerprint(root) {
  const hash = createHash("sha256");
  hash.update(`${process.platform}/${process.arch}/CGO=0\0`);
  async function visit(relative) {
    const entries = await readdir(join(root, relative), {
      withFileTypes: true,
    });
    for (const entry of entries.sort((a, b) => a.name.localeCompare(b.name))) {
      const name = `${relative}/${entry.name}`;
      if (entry.isDirectory()) await visit(name);
      else if (entry.isFile()) {
        hash.update(name + "\0");
        hash.update(
          createHash("sha256")
            .update(await readFile(join(root, name)))
            .digest(),
        );
      } else throw new Error("Unsupported link in companion build inputs");
    }
  }
  for (const name of ["go.mod", "go.sum"]) {
    hash.update(name + "\0");
    hash.update(await readFile(join(root, name)));
  }
  for (const name of ["cmd", "internal", "third_party"]) await visit(name);
  return hash.digest("hex");
}

async function binaryFingerprint(executable) {
  return createHash("sha256")
    .update(await readFile(executable))
    .digest("hex");
}

export async function recordHostBuild(executable, source) {
  await writeFile(
    executable + ".build.json",
    JSON.stringify({
      source,
      binary: await binaryFingerprint(executable),
    }) + "\n",
  );
}

export async function hostBuildCurrent(executable, source) {
  try {
    const recorded = JSON.parse(
      await readFile(executable + ".build.json", "utf8"),
    );
    return (
      recorded.source === source &&
      recorded.binary === (await binaryFingerprint(executable))
    );
  } catch {
    return false;
  }
}

// Only a refusal proves that a rebuild will not replace a known listening Host.
// Windows executable locking remains a second guard against startup races.
export function hostListening() {
  return new Promise((resolve, reject) => {
    const socket = connect({ host: "127.0.0.1", port: 37099 });
    socket.once("connect", () => {
      socket.destroy();
      resolve(true);
    });
    socket.once("error", (error) => {
      socket.destroy();
      if (error.code === "ECONNREFUSED") resolve(false);
      else
        reject(
          new Error("Cannot check the existing Host; companion left unchanged"),
        );
    });
    socket.setTimeout(1000, () => {
      socket.destroy();
      reject(new Error("Host probe timed out; companion left unchanged"));
    });
  });
}

export async function prepareHostBuild({
  root,
  executable,
  build,
  listening = hostListening,
}) {
  const source = await hostSourceFingerprint(root);
  if (await hostBuildCurrent(executable, source)) return "current";
  if (await listening()) return "outdated";
  await build();
  // The build owns recording its inputs; a failed build must never certify old bytes.
  if (
    (await hostSourceFingerprint(root)) !== source ||
    !(await hostBuildCurrent(executable, source))
  )
    throw new Error("Companion build inputs changed; retry preparation");
  return "rebuilt";
}
