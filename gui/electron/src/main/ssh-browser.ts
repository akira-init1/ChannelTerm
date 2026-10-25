import { spawn, type ChildProcessWithoutNullStreams } from "node:child_process";
import { isConnectionRequest } from "../shared/connections";
import type {
  SSHBrowseRequest,
  SSHBrowseResult,
  SSHFileEntry,
} from "../shared/ssh-browser";
import type { HostClient } from "./host/client";
import { HostError } from "./host/errors";
import {
  identifier,
  object,
  integer,
  text,
  incompatible,
} from "./host/validation";

/** Validate the finite browse intent before selecting a Session or starting a process. */
export function sshBrowseRequest(value: unknown): SSHBrowseRequest {
  const r = object(value);
  identifier(r.sessionId);
  if (
    Object.keys(r).some(
      (key) => !["sessionId", "operation", "path", "connection"].includes(key),
    ) ||
    (r.operation !== "ls" && r.operation !== "stat") ||
    typeof r.path !== "string" ||
    !r.path.trim() ||
    r.path.length > 4096 ||
    /[\x00-\x1f\x7f]/.test(r.path) ||
    !isConnectionRequest(r.connection) ||
    r.connection.kind !== "ssh"
  )
    throw new HostError("invalid_input", "Invalid SFTP browse request.");
  return {
    sessionId: r.sessionId,
    operation: r.operation,
    path: r.path,
    connection: r.connection,
  };
}

/** Accept only the finite read-only machine stream; no successful rows before final exit. */
export class SSHListing {
  private started = false;
  private completed = false;
  private entries: SSHFileEntry[] = [];
  private names = new Set<string>();
  private nameBytes = 0;
  constructor(private readonly operation: "ls" | "stat") {}
  accept(value: unknown): void {
    const r = object(value);
    if (this.completed) throw incompatible();
    if (!this.started) {
      if (r.type !== "started") throw incompatible();
      this.started = true;
      return;
    }
    if (r.type === "failed" || r.type === "cancelled") {
      throw new HostError(
        r.type === "cancelled" ? "cancelled" : "tool_error",
        r.error_code === "listing_limit"
          ? "SFTP listing exceeds supported limits. Choose a smaller directory or inspect one path."
          : "SFTP browse failed. Check local credentials, host trust and the remote path.",
      );
    }
    if (r.type === "completed") {
      if (integer(r.entries) !== this.entries.length) throw incompatible();
      this.completed = true;
      return;
    }
    if (r.type !== "entry" || this.entries.length >= 1024) throw incompatible();
    const name = text(r.name),
      mode = text(r.mode),
      modified = text(r.modified);
    this.nameBytes += Buffer.byteLength(name);
    if (
      !name ||
      this.names.has(name) ||
      Buffer.byteLength(name) > 4096 ||
      this.nameBytes > 1024 * 1024 ||
      /[\x00-\x1f\x7f]/.test(name) ||
      (this.operation === "ls" &&
        (/[\/\\]/.test(name) || name === "." || name === "..")) ||
      !["file", "directory", "symlink", "other"].includes(String(r.kind)) ||
      !/^[A-Za-z?-]{10,32}$/.test(mode) ||
      modified.length > 40 ||
      !Number.isFinite(Date.parse(modified))
    )
      throw incompatible();
    // The finite values above are narrowed explicitly before crossing IPC.
    const kind = r.kind;
    if (
      kind !== "file" &&
      kind !== "directory" &&
      kind !== "symlink" &&
      kind !== "other"
    )
      throw incompatible();
    this.entries.push({ name, kind, size: integer(r.size), mode, modified });
    this.names.add(name);
  }
  finish(): SSHFileEntry[] {
    if (
      !this.completed ||
      (this.operation === "stat" && this.entries.length !== 1)
    )
      throw incompatible();
    return this.entries.map((entry) => ({ ...entry }));
  }
}

/** One bounded read-only companion process per window; credentials are piped, never persisted. */
export class DesktopSSHBrowser {
  private active?: {
    cancelled: boolean;
    child?: ChildProcessWithoutNullStreams;
  };
  constructor(
    private readonly executable: string,
    private readonly launch = (executable: string, args: string[]) =>
      spawn(executable, args, {
        shell: false,
        windowsHide: true,
        stdio: ["pipe", "pipe", "pipe"],
      }),
  ) {}
  get busy(): boolean {
    return !!this.active;
  }
  cancel(): void {
    if (this.active) {
      this.active.cancelled = true;
      this.active.child?.kill();
    }
  }
  async browse(
    host: Pick<HostClient, "validateFileSession">,
    value: unknown,
  ): Promise<SSHBrowseResult> {
    const r = sshBrowseRequest(value);
    if (this.active)
      throw new HostError("busy", "An SFTP browse request is already pending.");
    const active: NonNullable<DesktopSSHBrowser["active"]> = {
      cancelled: false,
    };
    this.active = active;
    const c = r.connection;
    try {
      const session = await host.validateFileSession(r.sessionId);
      const address = c.host.includes(":") ? `[${c.host}]` : c.host;
      if (
        session.transport !== "ssh" ||
        session.endpoint !== `${c.user}@${address}:${c.port}`
      )
        throw new HostError(
          "invalid_input",
          "SFTP target differs from the selected Session.",
        );
      if (active.cancelled)
        throw new HostError("cancelled", "SFTP browse cancelled.");
      const args = [
        "sftp",
        `${c.user}@${address}`,
        r.operation,
        "--port",
        String(c.port),
        "--json",
        "--credentials-stdin",
        "--operation-timeout",
        "25s",
      ];
      for (const path of c.identityFiles) args.push("--identity", path);
      if (c.knownHostsFile) args.push("--known-hosts", c.knownHostsFile);
      args.push("--", r.path);
      const entries = await new Promise<SSHFileEntry[]>((resolve, reject) => {
        const child = this.launch(this.executable, args);
        active.child = child;
        const listing = new SSHListing(r.operation);
        let pending = "",
          received = 0,
          failure: unknown;
        const timer = setTimeout(() => {
          active.cancelled = true;
          child.kill();
        }, 30000);
        child.stderr.resume();
        child.stdout.setEncoding("utf8");
        child.stdin.on("error", () => {});
        child.stdin.end(
          JSON.stringify({
            password: c.password,
            key_passphrase: c.keyPassphrase,
          }),
        );
        c.password = undefined;
        c.keyPassphrase = undefined;
        child.stdout.on("data", (data: string) => {
          if (failure) return;
          try {
            received += Buffer.byteLength(data);
            if (received > 8 * 1024 * 1024) throw incompatible();
            pending += data;
            let newline: number;
            while ((newline = pending.indexOf("\n")) >= 0) {
              const line = pending.slice(0, newline);
              if (Buffer.byteLength(line) > 32768) throw incompatible();
              listing.accept(JSON.parse(line));
              pending = pending.slice(newline + 1);
            }
            if (Buffer.byteLength(pending) > 32768) throw incompatible();
          } catch (error) {
            failure = error;
            child.kill();
          }
        });
        child.once("error", () => {
          clearTimeout(timer);
          reject(
            new HostError(
              "configuration",
              "SFTP companion could not start. Rebuild the file client.",
            ),
          );
        });
        child.once("close", (code) => {
          clearTimeout(timer);
          if (active.cancelled) {
            reject(
              new HostError("cancelled", "SFTP browse cancelled or timed out."),
            );
            return;
          }
          if (failure) {
            reject(failure instanceof HostError ? failure : incompatible());
            return;
          }
          if (code !== 0 || pending.trim()) {
            reject(
              new HostError(
                "tool_error",
                "SFTP browse failed. Check local credentials, host trust and the remote path.",
              ),
            );
            return;
          }
          try {
            resolve(listing.finish());
          } catch (error) {
            reject(error);
          }
        });
      });
      const current = await host.validateFileSession(r.sessionId);
      if (
        active.cancelled ||
        current.session_ref !== session.session_ref ||
        current.endpoint !== session.endpoint
      )
        throw new HostError("cancelled", "SFTP target changed while browsing.");
      return { path: r.path, entries };
    } finally {
      c.password = undefined;
      c.keyPassphrase = undefined;
      this.active = undefined;
    }
  }
}
