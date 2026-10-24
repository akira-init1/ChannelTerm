import { randomUUID } from "node:crypto";
import { spawn } from "node:child_process";
import { basename, join } from "node:path";
import { lstat } from "node:fs/promises";
import type { LocalSelection, RemoteDirectory } from "../shared/transfers";
import type { HostClient } from "./host/client";
import { loadToken } from "./host/config";
import { HostError } from "./host/errors";
import { identifier, object, text, incompatible } from "./host/validation";

/** One Main-owned confirmation, bound to the original Host request and Session. */
export class TransferConfirmation {
  private pending?: {
    id: string;
    host: Pick<HostClient, "beginTransferCancel" | "resolveTransferCancel">;
    sessionId: string;
    requestId?: string;
  };

  async begin(
    host: Pick<HostClient, "beginTransferCancel" | "resolveTransferCancel">,
    sessionId: string,
    transferId: string,
  ): Promise<string> {
    identifier(sessionId);
    identifier(transferId);
    if (this.pending)
      throw new HostError("busy", "A transfer confirmation is already open.");
    const pending: NonNullable<TransferConfirmation["pending"]> = {
      id: randomUUID(),
      host,
      sessionId,
    };
    this.pending = pending;
    try {
      pending.requestId = await host.beginTransferCancel(sessionId, transferId);
      if (this.pending !== pending) {
        await host.resolveTransferCancel(sessionId, pending.requestId, false);
        throw new HostError("cancelled", "Confirmation was dismissed.");
      }
      return pending.id;
    } catch (error) {
      if (this.pending === pending) this.pending = undefined;
      throw error;
    }
  }

  async resolve(id: unknown, confirm: unknown): Promise<void> {
    identifier(id);
    if (typeof confirm !== "boolean")
      throw new HostError("invalid_input", "Invalid confirmation answer.");
    const pending = this.pending;
    if (!pending || pending.id !== id || !pending.requestId)
      throw new HostError("closed", "Confirmation is no longer active.");
    // Consume before awaiting so duplicate clicks cannot send a second answer.
    this.pending = undefined;
    await pending.host.resolveTransferCancel(
      pending.sessionId,
      pending.requestId,
      confirm,
    );
  }

  dismiss(): void {
    const pending = this.pending;
    this.pending = undefined;
    if (pending?.requestId) {
      // Reload/close is a negative answer; Host expiry handles an unreachable owner.
      void pending.host
        .resolveTransferCancel(pending.sessionId, pending.requestId, false)
        .catch(() => {});
    }
  }
}

export function remotePath(value: unknown): string {
  if (
    typeof value !== "string" ||
    value.length === 0 ||
    Buffer.byteLength(value) > 4096 ||
    /[\x00-\x1f\x7f]/.test(value)
  )
    throw new HostError("invalid_input", "Invalid remote path.");
  return value.startsWith("-") ? "./" + value : value;
}

/** The remote basename must stay a single local child, including Windows device aliases. */
export function downloadName(
  path: string,
  platform = process.platform,
): string {
  const name = path.split("/").filter(Boolean).pop() || "download";
  if (
    name === "." ||
    name === ".." ||
    /[\\\x00-\x1f]/.test(name) ||
    (platform === "win32" &&
      (/[<>:"|?*]/.test(name) ||
        /[. ]$/.test(name) ||
        /^(con|prn|aux|nul|com[1-9¹²³]|lpt[1-9¹²³])(?:\.|$)/i.test(name)))
  )
    throw new HostError(
      "invalid_input",
      "Remote name cannot be saved safely on this platform.",
    );
  return name;
}

export function directoryResult(value: unknown): RemoteDirectory {
  const result = object(value);
  const path = remotePath(result.path);
  if (
    !path.startsWith("/") ||
    !Array.isArray(result.entries) ||
    result.entries.length > 256 ||
    typeof result.truncated !== "boolean"
  )
    throw incompatible();
  const names = new Set<string>();
  const entries = result.entries.map(
    (raw): RemoteDirectory["entries"][number] => {
      const item = object(raw);
      const name = text(item.name);
      if (
        !name ||
        name.length > 4096 ||
        /[\/\x00-\x1f\x7f]/.test(name) ||
        name === "." ||
        name === ".." ||
        names.has(name)
      )
        throw incompatible();
      names.add(name);
      if (
        item.kind !== "file" &&
        item.kind !== "directory" &&
        item.kind !== "other"
      )
        throw incompatible();
      return { name, kind: item.kind };
    },
  );
  return { path, entries, truncated: result.truncated };
}

/** Main-owned paths and fixed companion invocation; no shell or Renderer-supplied command. */
export class DesktopFiles {
  private selections = new Map<
    string,
    { path: string; sessionId: string; direction: string; expires: number }
  >();
  private active?: {
    transferId?: string;
  };
  constructor(private executable: string) {}
  get busy(): boolean {
    return !!this.active;
  }

  /** A native selection authorizes only the current Host connection. */
  clearSelections(): void {
    this.selections.clear();
  }

  remember(
    path: string,
    sessionId: string,
    direction: "send" | "receive",
  ): LocalSelection {
    if (this.selections.size >= 16)
      this.selections.delete(this.selections.keys().next().value!);
    const id = randomUUID();
    this.selections.set(id, {
      path,
      sessionId,
      direction,
      expires: Date.now() + 10 * 60_000,
    });
    return { id, name: basename(path) };
  }

  async browse(
    host: HostClient,
    sessionId: string,
    path: unknown,
  ): Promise<RemoteDirectory> {
    identifier(sessionId);
    const wanted = remotePath(path);
    const result = await this.run(
      host,
      sessionId,
      ["file", "browse", wanted],
      true,
    );
    return directoryResult(result);
  }

  async transfer(host: HostClient, value: unknown): Promise<void> {
    const request = object(value);
    if (
      Object.keys(request).some(
        (key) =>
          ![
            "sessionId",
            "direction",
            "selectionId",
            "remotePath",
            "wire",
          ].includes(key),
      )
    )
      throw new HostError("invalid_input", "Invalid transfer request.");
    identifier(request.sessionId);
    identifier(request.selectionId);
    const wire = request.wire ?? "base64";
    if (wire !== "base64" && wire !== "raw")
      throw new HostError("invalid_input", "Invalid transfer wire encoding.");
    if (request.direction !== "send" && request.direction !== "receive")
      throw new HostError("invalid_input", "Invalid transfer direction.");
    const selected = this.selections.get(request.selectionId);
    if (
      !selected ||
      selected.expires < Date.now() ||
      selected.sessionId !== request.sessionId ||
      selected.direction !== request.direction
    )
      throw new HostError("invalid_input", "Select the local path again.");
    const path = remotePath(request.remotePath);
    if (request.direction === "send") {
      const info = await lstat(selected.path);
      if (!info.isFile() && !info.isDirectory())
        throw new HostError(
          "invalid_input",
          "Choose a regular file or directory.",
        );
    }
    this.selections.delete(request.selectionId);
    await this.run(
      host,
      request.sessionId,
      request.direction === "send"
        ? ["file", "send", selected.path, path, "--wire", wire]
        : [
            "file",
            "receive",
            path,
            join(selected.path, downloadName(path)),
            "--wire",
            wire,
          ],
      false,
    );
  }

  private async run(
    host: HostClient,
    sessionId: string,
    args: string[],
    browse: boolean,
  ): Promise<unknown> {
    if (this.active)
      throw new HostError(
        "busy",
        "A desktop file operation is already running.",
      );
    // Reserve before validation/credential awaits, so two clicks cannot spawn twice.
    const job: NonNullable<DesktopFiles["active"]> = {};
    this.active = job;
    const operation = (async () => {
      await host.validateSerial(sessionId);
      const token = await loadToken();
      return new Promise<unknown>((resolve, reject) => {
        const child = spawn(
          this.executable,
          [
            ...args,
            "--session",
            sessionId,
            "--endpoint",
            host.info().endpoint,
            "--json",
          ],
          {
            shell: false,
            windowsHide: true,
            stdio: ["ignore", "pipe", "pipe"],
            env: { ...process.env, CHANNELTERM_HTTP_AUTH_TOKEN: token },
          },
        );
        let pending = "";
        let total = 0;
        let result: unknown;
        let invalid = false;
        // Never forward stderr, which can include credentials, local paths or terminal bytes.
        child.stderr.resume();
        child.stdout.setEncoding("utf8");
        child.stdout.on("data", (chunk: string) => {
          total += Buffer.byteLength(chunk);
          if (total > 1024 * 1024 || invalid) {
            invalid = true;
            return;
          }
          pending += chunk;
          let end: number;
          while ((end = pending.indexOf("\n")) >= 0) {
            const line = pending.slice(0, end);
            pending = pending.slice(end + 1);
            try {
              const record = object(JSON.parse(line));
              if (browse) {
                if (result) throw incompatible();
                result = record;
              } else if (record.type === "started") {
                if (job.transferId) throw incompatible();
                identifier(record.transfer_id);
                job.transferId = record.transfer_id;
              } else if (record.type === "verified") {
                if (
                  !job.transferId ||
                  result ||
                  typeof record.sha256 !== "string" ||
                  !/^[a-f0-9]{64}$/.test(record.sha256) ||
                  typeof record.resolved_path !== "string" ||
                  !record.resolved_path ||
                  record.resolved_path.length > 4096
                )
                  throw incompatible();
                result = record;
              } else invalid = true;
            } catch {
              invalid = true;
            }
          }
        });
        child.once("error", () =>
          reject(
            new HostError(
              "configuration",
              "File companion could not start. Rebuild the bundled CLI.",
            ),
          ),
        );
        child.once("close", (code) => {
          if (code === 0 && result && !invalid && !pending.trim())
            resolve(result);
          else
            reject(
              new HostError(
                "tool_error",
                "File operation did not complete. Check the idle shell, path, tools and Session state; no automatic retry was made.",
              ),
            );
        });
      });
    })();
    try {
      return await operation;
    } finally {
      if (this.active === job) this.active = undefined;
    }
  }
}
