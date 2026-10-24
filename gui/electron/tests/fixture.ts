import { spawn } from "node:child_process";
import { createInterface } from "node:readline";
import { resolve } from "node:path";
import { randomBytes } from "node:crypto";

/** Run the real Go protocol stack with a disposable token and in-memory terminal. */
export async function startFixture() {
  const token = randomBytes(32).toString("base64url");
  const process = spawn(
    resolve(
      "test-results",
      globalThis.process.platform === "win32" ? "host.exe" : "host",
    ),
    [],
    {
      stdio: ["pipe", "pipe", "pipe"],
      windowsHide: true,
      env: { ...globalThis.process.env, CHANNELTERM_HTTP_AUTH_TOKEN: token },
    },
  );
  let diagnostics = "";
  process.stderr.on("data", (data: Buffer) => {
    diagnostics += data.toString();
  });
  const lines = createInterface({ input: process.stdout });
  const queue: Record<string, string>[] = [];
  let notify: ((value: Record<string, string>) => void) | undefined;
  lines.on("line", (line) => {
    const value = JSON.parse(line) as Record<string, string>;
    if (notify) {
      const done = notify;
      notify = undefined;
      done(value);
    } else queue.push(value);
  });
  function next(): Promise<Record<string, string>> {
    const ready = queue.shift();
    if (ready) return Promise.resolve(ready);
    return new Promise((accept, reject) => {
      const timer = setTimeout(() => {
        notify = undefined;
        reject(new Error(`Fixture timeout: ${diagnostics}`));
      }, 15000);
      notify = (value) => {
        clearTimeout(timer);
        accept(value);
      };
    });
  }
  const ready = await next();
  if (!ready.endpoint) throw new Error("Fixture did not publish its endpoint");
  return {
    endpoint: ready.endpoint,
    token,
    sshPort: Number(ready.ssh_port),
    knownHosts: ready.known_hosts!,
    process,
    async command(action: string, data?: string) {
      process.stdin.write(JSON.stringify({ action, data }) + "\n");
      const reply = await next();
      if (reply.error) throw new Error(reply.error);
    },
    async stop() {
      if (process.exitCode !== null) return;
      const exited = new Promise<void>((done) =>
        process.once("exit", () => done()),
      );
      process.stdin.end();
      const timer = setTimeout(() => process.kill(), 3000);
      await exited;
      clearTimeout(timer);
      lines.close();
    },
  };
}
