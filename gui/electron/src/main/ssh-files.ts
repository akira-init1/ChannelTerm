import {
  spawn,
  type ChildProcess,
  type ChildProcessWithoutNullStreams,
} from "node:child_process";
import { randomUUID } from "node:crypto";
import { basename, join } from "node:path";
import { lstat } from "node:fs/promises";
import { isConnectionRequest } from "../shared/connections";
import type { SSHTransferRequest, TransferStatus } from "../shared/transfers";
import type { HostClient } from "./host/client";
import { HostError } from "./host/errors";
import { identifier, object } from "./host/validation";

/** Strict intent validation runs before native selection consumption or process launch. */
export function sshTransferRequest(value: unknown): SSHTransferRequest {
  const r = object(value);
  identifier(r.sessionId);
  identifier(r.selectionId);
  if (
    Object.keys(r).some(
      (key) =>
        ![
          "sessionId",
          "selectionId",
          "direction",
          "remotePath",
          "protocol",
          "resume",
          "recursive",
          "connection",
        ].includes(key),
    ) ||
    !isConnectionRequest(r.connection) ||
    r.connection.kind !== "ssh" ||
    (r.direction !== "send" && r.direction !== "receive") ||
    (r.protocol !== "sftp" && r.protocol !== "scp") ||
    typeof r.remotePath !== "string" ||
    !r.remotePath.trim() ||
    r.remotePath.length > 4096 ||
    /[\x00-\x1f\x7f]/.test(r.remotePath) ||
    typeof r.resume !== "boolean" ||
    typeof r.recursive !== "boolean" ||
    (r.resume && (r.protocol !== "sftp" || r.recursive))
  )
    throw new HostError("invalid_input", "Invalid SSH file transfer settings.");
  return {
    sessionId: r.sessionId,
    selectionId: r.selectionId,
    direction: r.direction,
    remotePath: r.remotePath,
    protocol: r.protocol,
    resume: r.resume,
    recursive: r.recursive,
    connection: r.connection,
  };
}

/** A bounded machine record updates presentation only; exit success still gates completion. */
export function applySSHProgress(job: TransferStatus, value: unknown): boolean {
  const r = object(value);
  if (r.type === "started") return false;
  if (r.type === "progress") {
    if (
      typeof r.name !== "string" ||
      r.name.length > 4096 ||
      !safeCount(r.transferred) ||
      !safeCount(r.total) ||
      r.transferred > r.total
    )
      throw new HostError("incompatible", "Invalid SSH transfer progress.");
    job.source = r.name;
    job.transferred = r.transferred;
    job.total = r.total;
    job.state = "transferring";
    return false;
  }
  if (r.type === "verifying" || r.type === "verified") {
    job.state = "verifying";
    return false;
  }
  if (
    ["completed", "failed", "cancelled"].includes(String(r.type)) &&
    safeCount(r.files) &&
    safeCount(r.bytes) &&
    safeCount(r.verified_files)
  ) {
    if (r.type !== "completed")
      job.state = r.type === "cancelled" ? "cancelled" : "failed";
    return r.type === "completed";
  }
  throw new HostError("incompatible", "Invalid SSH file companion response.");
}
function safeCount(value: unknown): value is number {
  return typeof value === "number" && Number.isSafeInteger(value) && value >= 0;
}

/** Main owns only its dedicated child and bounded display history, never a Session lease.
 * Killing this child closes its dedicated SSH sockets. Partial files are retained;
 * SFTP resume revalidates the entire prefix after cancellation or process loss.
 */
export class DesktopSSHFiles {
  private jobs: TransferStatus[] = [];
  private active?: {
    job: TransferStatus;
    child?: ChildProcess;
    cancelled: boolean;
  };
  constructor(
    private executable: string,
    private launch: (
      executable: string,
      args: string[],
    ) => ChildProcessWithoutNullStreams = (executable, args) =>
      spawn(executable, args, {
        shell: false,
        windowsHide: true,
        stdio: ["pipe", "pipe", "pipe"],
      }),
  ) {}
  get busy(): boolean {
    return !!this.active;
  }
  /** Reload/crash cleanup stops our child, without touching a shared Session. */
  disconnect(): void {
    if (this.active) this.cancel(this.active.job.id);
    this.jobs = [];
  }
  snapshots(): TransferStatus[] {
    return this.jobs.map((job) => ({ ...job }));
  }
  cancel(id: unknown): void {
    identifier(id);
    if (!this.active || this.active.job.id !== id)
      throw new HostError("closed", "SSH transfer is no longer active.");
    this.active.cancelled = true;
    this.active.child?.kill();
  }
  async transfer(
    host: Pick<HostClient, "validateFileSession">,
    value: unknown,
    select: (r: SSHTransferRequest) => string,
  ): Promise<void> {
    const r = sshTransferRequest(value);
    if (this.active)
      throw new HostError("busy", "An SSH file transfer is already running.");
    const job: TransferStatus = {
      id: randomUUID(),
      sessionId: r.sessionId,
      protocol: r.protocol,
      direction: r.direction,
      source: "",
      destination: "",
      state: "transferring",
      transferred: 0,
      total: 0,
      speed: 0,
      released: false,
    };
    const active: NonNullable<DesktopSSHFiles["active"]> = {
      job,
      cancelled: false,
    };
    this.active = active;
    try {
      const entry = await host.validateFileSession(r.sessionId);
      const c = r.connection;
      const address = c.host.includes(":") ? `[${c.host}]` : c.host;
      if (
        entry.transport !== "ssh" ||
        entry.endpoint !== `${c.user}@${address}:${c.port}`
      )
        throw new HostError(
          "invalid_input",
          "SSH file target differs from the selected Session.",
        );
      const selected = select(r);
      const info = await lstat(selected);
      if (
        info.isSymbolicLink() ||
        (r.direction === "receive"
          ? !info.isDirectory()
          : !info.isFile() && !info.isDirectory()) ||
        (r.direction === "send" &&
          info.isDirectory() &&
          (!r.recursive || r.resume))
      )
        throw new HostError(
          "invalid_input",
          "Choose a compatible regular file or directory.",
        );
      const remoteName = r.remotePath.replace(/\/$/, "").split("/").pop() ?? "";
      if (
        !remoteName ||
        /[\\:\x00-\x1f]/.test(remoteName) ||
        remoteName === "." ||
        remoteName === ".."
      )
        throw new HostError(
          "invalid_input",
          "Remote path must name a file or directory.",
        );
      const local =
        r.direction === "send" ? selected : join(selected, remoteName);
      const paths =
        r.direction === "send" ? [local, r.remotePath] : [r.remotePath, local];
      const args = [
        r.protocol,
        `${c.user}@${address}`,
        r.direction === "send" ? "put" : "get",
        "--port",
        String(c.port),
        "--json",
        "--credentials-stdin",
        "--verify=sha256",
        "--keepalive-interval",
        "10s",
        "--keepalive-count",
        "3",
      ];
      for (const path of c.identityFiles) args.push("--identity", path);
      if (c.knownHostsFile) args.push("--known-hosts", c.knownHostsFile);
      if (r.resume) args.push("--resume");
      if (r.recursive) args.push("--recursive");
      args.push("--", ...paths);
      if (active.cancelled)
        throw new HostError("cancelled", "SSH transfer cancelled.");
      job.source = r.direction === "send" ? basename(local) : r.remotePath;
      job.destination = r.direction === "send" ? r.remotePath : basename(local);
      this.jobs = [...this.jobs.slice(-31), job];
      await new Promise<void>((resolve, reject) => {
        const child = this.launch(this.executable, args);
        active.child = child;
        let pending = "",
          completed = false,
          invalid = false;
        let file = "",
          initial = 0,
          began = Date.now();
        child.stderr.resume(); // Never forward credential/path-bearing diagnostics.
        child.stdin.on("error", () => {}); // Early exit is reported by close/error.
        child.stdin.end(
          JSON.stringify({
            password: c.password,
            key_passphrase: c.keyPassphrase,
          }),
        );
        child.stdout.setEncoding("utf8");
        child.stdout.on("data", (chunk: string) => {
          if (invalid) return;
          pending += chunk;
          // Bound incomplete records, not total lifetime output of long transfers.
          if (pending.length > 65536) {
            invalid = true;
            child.kill();
            return;
          }
          let end: number;
          while ((end = pending.indexOf("\n")) >= 0) {
            const line = pending.slice(0, end);
            pending = pending.slice(end + 1);
            try {
              if (completed || line.length > 32768)
                throw new Error("invalid record order/size");
              const record: unknown = JSON.parse(line);
              completed = applySSHProgress(job, record);
              if (object(record).type === "progress") {
                if (job.source !== file) {
                  file = job.source;
                  initial = job.transferred;
                  began = Date.now();
                }
                job.speed =
                  Math.max(0, job.transferred - initial) /
                  Math.max(0.001, (Date.now() - began) / 1000);
              }
            } catch {
              invalid = true;
              child.kill();
              return;
            }
          }
        });
        child.once("error", () =>
          reject(
            new HostError(
              "configuration",
              "SSH file companion could not start. Rebuild the file client.",
            ),
          ),
        );
        child.once("close", (code) => {
          if (active.cancelled) {
            job.state = "cancelled";
            resolve();
          } else if (code === 0 && completed && !invalid && !pending.trim()) {
            job.state = "completed";
            resolve();
          } else {
            job.state = "failed";
            reject(
              new HostError(
                "tool_error",
                "SSH transfer failed. Check credentials, host trust and paths. For SFTP resume, the existing file must match the source prefix. Partial files may remain.",
              ),
            );
          }
        });
      });
    } catch (error) {
      job.state = active.cancelled ? "cancelled" : "failed";
      throw error;
    } finally {
      job.released = true;
      job.speed = 0;
      if (this.active === active) this.active = undefined;
    }
  }
}
