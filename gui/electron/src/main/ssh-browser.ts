import { sshManageRequest, SSHManagementReceipt } from "./ssh-management";
import { spawn, type ChildProcessWithoutNullStreams } from "node:child_process";
import { isConnectionRequest } from "../shared/connections";
import type {
  SSHBrowseRequest,
  SSHBrowseResult,
  SSHFileEntry,
  SSHManageRequest,
  SSHManageResult,
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

/** One bounded SFTP companion per window; mutations require explicit native confirmation. */
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
      const listing = new SSHListing(r.operation);
      const entries = await this.execute(r, active, {
        accept: (value) => listing.accept(value),
        finish: (code) => {
          if (code !== 0)
            throw new HostError(
              "tool_error",
              "SFTP browse failed. Check local credentials, host trust and the remote path.",
            );
          return listing.finish();
        },
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

  /** Native confirmation precedes a fresh target check and one dedicated operation. */
  async manage(
    host: Pick<HostClient, "validateFileSession"> &
      Partial<Pick<HostClient, "fileOperation">>,
    value: unknown,
    confirm: (
      intent: Pick<
        SSHManageRequest,
        "sessionId" | "operation" | "path" | "target"
      > & { endpoint: string },
    ) => Promise<boolean>,
  ): Promise<SSHManageResult> {
    const r = sshManageRequest(value);
    if (this.active)
      throw new HostError("busy", "An SFTP operation is already pending.");
    const active: NonNullable<DesktopSSHBrowser["active"]> = {
      cancelled: false,
    };
    this.active = active;
    let provenance:
      Awaited<ReturnType<HostClient["fileOperation"]>> | undefined;
    const fallback = (reason: SSHManageResult["reason"]): SSHManageResult => ({
      operation: r.operation,
      path: r.path,
      ...(r.target ? { target: r.target } : {}),
      state: reason === "cancelled" ? "cancelled" : "failed",
      outcome: active.child?.pid ? "unknown" : "not_started",
      reason,
    });
    try {
      const session = await host.validateFileSession(r.sessionId);
      const address = r.connection.host.includes(":")
        ? `[${r.connection.host}]`
        : r.connection.host;
      if (
        session.transport !== "ssh" ||
        session.endpoint !==
          `${r.connection.user}@${address}:${r.connection.port}`
      )
        return fallback("target_changed");
      if (active.cancelled) return fallback("cancelled");
      if (
        !(await confirm({
          sessionId: r.sessionId,
          endpoint: session.endpoint,
          operation: r.operation,
          path: r.path,
          ...(r.target ? { target: r.target } : {}),
        }))
      )
        return fallback("cancelled");
      const current = await host.validateFileSession(r.sessionId);
      if (active.cancelled) return fallback("cancelled");
      if (
        current.session_ref !== session.session_ref ||
        current.endpoint !== session.endpoint
      )
        return fallback("target_changed");
      provenance = await host.fileOperation?.(r.sessionId, false);
      if (active.cancelled) return fallback("cancelled");
      const launching = await host.validateFileSession(r.sessionId);
      if (active.cancelled) return fallback("cancelled");
      if (
        launching.session_ref !== session.session_ref ||
        launching.endpoint !== session.endpoint
      )
        return fallback("target_changed");
      const receipt = new SSHManagementReceipt(r);
      const result = await this.execute(r, active, receipt);
      const after = await host.validateFileSession(r.sessionId);
      if (active.cancelled) return fallback("cancelled");
      if (
        after.session_ref !== session.session_ref ||
        after.endpoint !== session.endpoint
      )
        return fallback("target_changed");
      return result;
    } catch (error) {
      return fallback(
        error instanceof HostError && error.code === "cancelled"
          ? "cancelled"
          : "operation_failed",
      );
    } finally {
      r.connection.password = undefined;
      r.connection.keyPassphrase = undefined;
      try {
        await provenance?.done();
      } finally {
        this.active = undefined;
      }
    }
  }

  private execute<T>(
    r: SSHBrowseRequest | SSHManageRequest,
    active: NonNullable<DesktopSSHBrowser["active"]>,
    parser: { accept(value: unknown): void; finish(code: number | null): T },
  ): Promise<T> {
    const c = r.connection;
    const address = c.host.includes(":") ? `[${c.host}]` : c.host;
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
    if ("target" in r && r.target) args.push(r.target);
    return new Promise<T>((resolve, reject) => {
      const child = this.launch(this.executable, args);
      active.child = child;
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
            parser.accept(JSON.parse(line));
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
        if (pending.trim()) {
          reject(
            new HostError(
              "tool_error",
              "SFTP browse failed. Check local credentials, host trust and the remote path.",
            ),
          );
          return;
        }
        try {
          resolve(parser.finish(code));
        } catch (error) {
          reject(error);
        }
      });
    });
  }
}
