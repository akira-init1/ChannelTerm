import { randomUUID } from "node:crypto";
import type { UBootJob, UBootOutcome } from "../shared/uboot";
import { HostError, publicError } from "./host/errors";

/** One bounded window-owned request/result; the Host retains operation and lease authority. */
export class DesktopUBoot {
  private job?: UBootJob;
  private controller?: AbortController;

  get busy(): boolean {
    return this.job !== undefined && this.job.state !== "finished";
  }

  snapshot(): UBootJob | undefined {
    return this.job
      ? {
          ...this.job,
          ...(this.job.outcome ? { outcome: { ...this.job.outcome } } : {}),
          ...(this.job.error ? { error: { ...this.job.error } } : {}),
        }
      : undefined;
  }

  start(
    attachmentId: string,
    prepared: {
      sessionId: string;
      run(
        signal: AbortSignal,
        ready: (id: string | undefined) => void,
      ): Promise<UBootOutcome>;
    },
  ): UBootJob {
    if (this.busy)
      throw new HostError("busy", "A U-Boot attempt is still pending.");
    const job: UBootJob = {
      id: randomUUID(),
      attachmentId,
      sessionId: prepared.sessionId,
      state: "waiting",
    };
    const controller = new AbortController();
    this.job = job;
    this.controller = controller;
    void prepared
      .run(controller.signal, (id) => {
        if (
          this.job !== job ||
          controller.signal.aborted ||
          job.state === "finished"
        )
          return;
        job.operationId = id;
        job.state = id ? "ready" : "waiting";
      })
      .then(
        (outcome) => {
          job.outcome = outcome;
        },
        (error) => {
          job.error = publicError(error);
        },
      )
      .finally(() => {
        job.state = "finished";
        if (this.job === job) this.controller = undefined;
      });
    return this.snapshot()!;
  }

  cancel(id: unknown): void {
    if (typeof id !== "string" || !this.job || id !== this.job.id)
      throw new HostError(
        "cancelled",
        "The U-Boot attempt is no longer current.",
      );
    this.stop();
  }

  /** Window disconnect/crash/exit cancels only its own request, with no recovery keys. */
  stop(): void {
    if (!this.busy || !this.job) return;
    this.job.state = "stopping";
    this.job.operationId = undefined;
    this.controller?.abort();
  }
}
