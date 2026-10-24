import { execFile } from "node:child_process";
import { promisify } from "node:util";
import { fileURLToPath } from "node:url";

/** Compile PO catalogs before either bundler resolves its generated imports. */
export async function compileCatalogs(strict) {
  // Resolve the pinned official CLI rather than using a global executable or shell.
  const cli = fileURLToPath(
    new URL("lingui.js", import.meta.resolve("@lingui/cli")),
  );
  try {
    const { stdout, stderr } = await promisify(execFile)(
      process.execPath,
      [cli, "compile", "--workers", "1", ...(strict ? ["--strict"] : [])],
      { cwd: fileURLToPath(new URL("..", import.meta.url)), windowsHide: true },
    );
    process.stdout.write(stdout);
    process.stderr.write(stderr);
  } catch (error) {
    if (error.stdout) process.stdout.write(error.stdout);
    if (error.stderr) process.stderr.write(error.stderr);
    throw error;
  }
}
