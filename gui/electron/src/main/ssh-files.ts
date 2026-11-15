import {
  spawn,
  type ChildProcess,
  type ChildProcessWithoutNullStreams,
} from "node:child_process";
import { randomUUID } from "node:crypto";
import { basename, join } from "node:path";
import { lstat } from "node:fs/promises";
import { isConnectionRequest } from "../shared/connections";
import type {
  SSHTransferRequest,
  SSHTransferResult,
  TransferStatus,
} from "../shared/transfers";
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
          "overwrite",
          "safeReplace",
          "operationTimeoutSeconds",
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
    (r.overwrite !== undefined && typeof r.overwrite !== "boolean") ||
    (r.safeReplace !== undefined && typeof r.safeReplace !== "boolean") ||
    (r.operationTimeoutSeconds !== undefined &&
      (!safeCount(r.operationTimeoutSeconds) ||
        r.operationTimeoutSeconds > 86400)) ||
    (r.safeReplace === true &&
      (r.protocol !== "sftp" || r.overwrite !== true)) ||
    (r.resume &&
      (r.protocol !== "sftp" || r.recursive || r.overwrite || r.safeReplace))
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
    ...(r.overwrite !== undefined ? { overwrite: r.overwrite } : {}),
    ...(r.safeReplace !== undefined ? { safeReplace: r.safeReplace } : {}),
    ...(r.operationTimeoutSeconds !== undefined
      ? { operationTimeoutSeconds: r.operationTimeoutSeconds }
      : {}),
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
    safeCount(r.verified_files) &&
    r.verified_files <= r.files
  ) {
    const reason =
      r.error_code ??
      (r.type === "completed"
        ? ""
        : r.type === "cancelled"
          ? "cancelled"
          : "operation_failed");
    if (
      reason !== "" &&
      reason !== "not_found" &&
      reason !== "already_exists" &&
      reason !== "permission_denied" &&
      reason !== "timeout" &&
      reason !== "cancelled" &&
      reason !== "verification_failed" &&
      reason !== "operation_failed"
    )
      throw new HostError("incompatible", "Invalid SSH transfer result.");
    if (
      (r.type === "completed"
        ? reason !== "" || r.verified_files !== r.files
        : reason === "") ||
      (r.outcome !== undefined &&
        r.outcome !== (r.type === "completed" ? "confirmed" : "unknown"))
    )
      throw new HostError("incompatible", "Invalid SSH transfer outcome.");
    job.sshResult = {
      reported: true,
      files: r.files,
      bytes: r.bytes,
      verifiedFiles: r.verified_files,
      outcome: "unknown",
      reason,
    };
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
 * Killing this child closes its dedicated SSH sockets. Partial or staging files may
 * remain; Core owns safe-replacement cleanup and SFTP resume prefix verification.
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
    return this.jobs.map((job) => ({
      ...job,
      sshResult: job.sshResult ? { ...job.sshResult } : undefined,
      initiator: job.initiator ? { ...job.initiator } : undefined,
    }));
  }
  cancel(id: unknown): void {
    identifier(id);
    if (!this.active || this.active.job.id !== id)
      throw new HostError("closed", "SSH transfer is no longer active.");
    this.active.cancelled = true;
    this.active.child?.kill();
  }
  async transfer(
    host: Pick<HostClient, "validateFileSession"> &
      Partial<Pick<HostClient, "fileOperation">>,
    value: unknown,
    select: (r: SSHTransferRequest) => string,
    confirmOverwrite?: (intent: {
      sessionId: string;
      endpoint: string;
      source: string;
      destination: string;
      safeReplace: boolean;
      recursive: boolean;
    }) => Promise<boolean>,
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
      scope: "local",
      resumed: r.resume,
    };
    const active: NonNullable<DesktopSSHFiles["active"]> = {
      job,
      cancelled: false,
    };
    this.active = active;
    let provenance:
      Awaited<ReturnType<HostClient["fileOperation"]>> | undefined;
    try {
      const entry = await host.validateFileSession(r.sessionId);
      provenance = await host.fileOperation?.(r.sessionId, false);
      job.initiator = provenance?.source;
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
      job.source = r.direction === "send" ? basename(local) : r.remotePath;
      job.destination = r.direction === "send" ? r.remotePath : basename(local);
      this.jobs = [...this.jobs.slice(-31), job];
      if (r.overwrite) {
        if (
          !confirmOverwrite ||
          !(await confirmOverwrite({
            sessionId: r.sessionId,
            endpoint: entry.endpoint,
            source: paths[0]!,
            destination: paths[1]!,
            safeReplace: r.safeReplace === true,
            recursive: r.recursive,
          }))
        )
          throw new HostError("cancelled", "Overwrite was not confirmed.");
      }
      // Confirmation or local filesystem access can outlive a closed/replaced registration.
      const fresh = await host.validateFileSession(r.sessionId);
      if (
        fresh.transport !== entry.transport ||
        fresh.endpoint !== entry.endpoint ||
        fresh.session_id !== entry.session_id
      )
        throw new HostError("closed", "SSH transfer target changed.");
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
      if (r.overwrite) args.push("--overwrite");
      if (r.safeReplace) args.push("--safe-replace");
      if (r.operationTimeoutSeconds)
        args.push("--operation-timeout", `${r.operationTimeoutSeconds}s`);
      if (r.resume) args.push("--resume");
      if (r.recursive) args.push("--recursive");
      args.push("--", ...paths);
      if (active.cancelled)
        throw new HostError("cancelled", "SSH transfer cancelled.");
      await new Promise<void>((resolve, reject) => {
        const child = this.launch(this.executable, args);
        active.child = child;
        let pending = "",
          completed = false,
          started = false,
          terminal = false,
          timedOut = false,
          invalid = false;
        // The CLI owns the deadline. Only its own child is stopped if cleanup stalls.
        const timer = r.operationTimeoutSeconds
          ? setTimeout(
              () => {
                timedOut = true;
                child.kill();
              },
              (r.operationTimeoutSeconds + 5) * 1000,
            )
          : undefined;
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
              if (terminal || line.length > 32768)
                throw new Error("invalid record order/size");
              const record: unknown = JSON.parse(line);
              const kind = object(record).type;
              if (!started ? kind !== "started" : kind === "started")
                throw new Error("invalid record order");
              started = true;
              completed = applySSHProgress(job, record);
              terminal =
                kind === "completed" ||
                kind === "failed" ||
                kind === "cancelled";
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
        child.once("error", () => {
          clearTimeout(timer);
          reject(
            new HostError(
              "configuration",
              "SSH file companion could not start. Rebuild the file client.",
            ),
          );
        });
        child.once("close", (code) => {
          clearTimeout(timer);
          const summary: SSHTransferResult = job.sshResult ?? {
            reported: false,
            files: 0,
            bytes: 0,
            verifiedFiles: 0,
            outcome: child.pid ? "unknown" : "not_started",
            reason: "operation_failed",
          };
          job.sshResult = summary;
          if (
            active.cancelled ||
            (terminal &&
              !invalid &&
              !pending.trim() &&
              code !== null &&
              code !== 0 &&
              summary.reason === "cancelled")
          ) {
            job.state = "cancelled";
            summary.outcome = child.pid ? "unknown" : "not_started";
            summary.reason = "cancelled";
            resolve();
          } else if (
            code === 0 &&
            completed &&
            !timedOut &&
            !invalid &&
            !pending.trim()
          ) {
            job.state = "completed";
            summary.outcome = "confirmed";
            resolve();
          } else {
            job.state = "failed";
            summary.outcome = child.pid ? "unknown" : "not_started";
            if (timedOut) summary.reason = "timeout";
            else if (invalid || !terminal || summary.reason === "")
              summary.reason = "operation_failed";
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
      job.state =
        active.cancelled ||
        (error instanceof HostError && error.code === "cancelled")
          ? "cancelled"
          : "failed";
      job.sshResult ??= {
        reported: false,
        files: 0,
        bytes: 0,
        verifiedFiles: 0,
        outcome: active.child?.pid ? "unknown" : "not_started",
        reason: job.state === "cancelled" ? "cancelled" : "operation_failed",
      };
      throw error;
    } finally {
      try {
        await provenance?.done();
      } finally {
        job.released = true;
        job.speed = 0;
        if (this.active === active) this.active = undefined;
      }
    }
  }
}
