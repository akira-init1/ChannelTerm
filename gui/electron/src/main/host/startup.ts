import { spawn } from "node:child_process";
import { connect } from "node:net";
import { setTimeout as delay } from "node:timers/promises";
import { join } from "node:path";
import { HostError } from "./errors";

export const defaultHostEndpoint = "http://127.0.0.1:37099/mcp";

/** Only the canonical local endpoint permits process creation; no PATH search. */
export function companionPath(
  root: string,
  platform = process.platform,
): string {
  return join(
    root,
    "host",
    platform === "win32" ? "channelterm.exe" : "channelterm",
  );
}

/** Refusal proves absence. Timeouts and other errors must never trigger a spawn. */
export function localListener(signal: AbortSignal): Promise<boolean> {
  return new Promise((resolve, reject) => {
    const socket = connect({ host: "127.0.0.1", port: 37099, signal });
    const finish = (error?: Error, listening = false) => {
      socket.destroy();
      if (error) reject(error);
      else resolve(listening);
    };
    socket.once("connect", () => finish(undefined, true));
    socket.once("error", (error: NodeJS.ErrnoException) => {
      if (error.code === "ECONNREFUSED") finish();
      else
        finish(
          new HostError(
            signal.aborted ? "cancelled" : "disconnected",
            "Cannot check the local Host listener. Retry the connection.",
          ),
        );
    });
    socket.setTimeout(1000, () =>
      finish(
        new HostError(
          "disconnected",
          "Local Host listener check timed out. Retry the connection.",
        ),
      ),
    );
  });
}

/** Start only the app-owned executable with fixed arguments and no shell/stdio. */
export function launchCompanion(
  executable: string,
  onExit: () => void,
): Promise<void> {
  return new Promise((resolve, reject) => {
    const env = { ...process.env };
    delete env.ELECTRON_RUN_AS_NODE;
    // A persistent standalone Host must not inherit the CLI's temporary-owner flag.
    delete env.CHANNELTERM_INTERNAL_TEMPORARY_HOST;
    const child = spawn(
      executable,
      [
        "mcp",
        "--transport",
        "http",
        "--listen",
        "127.0.0.1:37099",
        "--path",
        "/mcp",
      ],
      {
        shell: false,
        detached: true,
        windowsHide: true,
        stdio: "ignore",
        env,
      },
    );
    child.once("exit", onExit);
    child.once("error", () =>
      reject(
        new HostError(
          "configuration",
          "Could not start the bundled Host. Rebuild the companion with npm run host:build, then retry.",
        ),
      ),
    );
    child.once("spawn", () => {
      // Host owns shared Sessions and intentionally survives GUI exit.
      child.unref();
      resolve();
    });
  });
}

interface StartupDependencies {
  listening(signal: AbortSignal): Promise<boolean>;
  launch(onExit: () => void): Promise<void>;
  pause(signal: AbortSignal): Promise<void>;
}

/** Readiness only; the caller must still authenticate and verify Host identity. */
export class LocalHostStartup {
  private launched = false;
  constructor(private readonly dependencies: StartupDependencies) {}

  async ensure(endpoint: string, signal: AbortSignal): Promise<void> {
    if (endpoint !== defaultHostEndpoint) return;
    signal.throwIfAborted();
    if (await this.dependencies.listening(signal)) return;
    signal.throwIfAborted();
    if (!this.launched) {
      this.launched = true;
      try {
        await this.dependencies.launch(() => {
          this.launched = false;
        });
      } catch (error) {
        this.launched = false;
        throw error;
      }
    }
    // A timeout must not launch a second copy while the previous process is alive.
    for (let attempt = 0; attempt < 40; attempt++) {
      await this.dependencies.pause(signal);
      signal.throwIfAborted();
      if (await this.dependencies.listening(signal)) return;
      if (!this.launched)
        throw new HostError(
          "disconnected",
          "Local Host exited during startup. Check its configuration and retry.",
        );
    }
    throw new HostError(
      "disconnected",
      "Local Host did not become ready. Check the companion installation and port 37099, then retry. Restart the GUI if the Host exited during startup.",
    );
  }
}

export function localHostStartup(executable: string): LocalHostStartup {
  return new LocalHostStartup({
    listening: localListener,
    launch: (onExit) => launchCompanion(executable, onExit),
    pause: (signal) => delay(250, undefined, { signal }),
  });
}
