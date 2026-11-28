import { randomUUID } from "node:crypto";
import type {
  ReleasePreview,
  ReleaseOutcome,
  ReleaseTarget,
} from "../../shared/api";
import { HostError } from "./errors";
import { identifier, incompatible, object, text } from "./validation";

/** One confirmation snapshot per window connection. Renderer only sees a single-use ticket. */
export class HostReleases {
  available: boolean | undefined;
  private pending?: ReleasePreview & { host: string };
  private busy = false;
  private previewVersion = 0;
  constructor(
    private readonly endpoint: URL,
    private readonly token: string,
    private readonly signal: () => AbortSignal,
  ) {}

  reset() {
    this.previewVersion++;
    this.available = false;
    this.pending = undefined;
  }

  private request(
    url: URL,
    method: string,
    signal: AbortSignal,
    body?: string,
  ) {
    return fetch(url, {
      method,
      headers: {
        Authorization: `Bearer ${this.token}`,
        "Content-Type": "application/json",
      },
      redirect: "error",
      body,
      signal: AbortSignal.any([
        signal,
        AbortSignal.timeout(method === "HEAD" ? 2000 : 10000),
      ]),
    });
  }

  async probe() {
    const signal = this.signal();
    try {
      const res = await this.request(
        new URL("/gui/v1/release", this.endpoint),
        "HEAD",
        signal,
      );
      this.available =
        res.status === 404 || res.status === 405
          ? undefined
          : res.status === 204 && !signal.aborted;
    } catch {
      this.available = false;
    }
  }

  async preview(id: string | null): Promise<ReleasePreview> {
    if (!this.available || this.busy)
      throw new HostError("busy", "Release is unavailable.");
    this.pending = undefined;
    const version = ++this.previewVersion;
    const signal = this.signal();
    const url = new URL("/gui/v1/release", this.endpoint);
    if (id !== null) {
      identifier(id);
      url.searchParams.set("target", id);
    }
    const res = await this.request(url, "GET", signal);
    if (!res.ok) {
      await res.body?.cancel();
      throw new HostError(
        res.status === 403 ? "unauthorized" : "tool_error",
        "Could not preview release.",
      );
    }
    const reader = res.body?.getReader();
    if (!reader) throw incompatible();
    const chunks: Buffer[] = [];
    let size = 0;
    try {
      for (;;) {
        const chunk = await reader.read();
        if (chunk.done) break;
        size += chunk.value.byteLength;
        if (size > 16 * 1024 * 1024) {
          await reader.cancel();
          throw incompatible();
        }
        chunks.push(Buffer.from(chunk.value));
      }
    } finally {
      reader.releaseLock();
    }
    const data = object(JSON.parse(Buffer.concat(chunks).toString("utf8")));
    if (version !== this.previewVersion)
      throw new HostError("cancelled", "Release preview was superseded.");
    signal.throwIfAborted();
    identifier(data.host);
    if (!Array.isArray(data.targets) || data.targets.length > 10000)
      throw incompatible();
    const targets: ReleaseTarget[] = data.targets.map((value) => {
      const row = object(value);
      identifier(row.id);
      identifier(row.reference);
      identifier(row.endpoint);
      identifier(row.transport);
      return {
        id: row.id,
        reference: row.reference,
        endpoint: row.endpoint,
        transport: row.transport,
        reason: text(row.reason),
      };
    });
    if (id !== null && (targets.length !== 1 || targets[0]?.id !== id))
      throw new HostError("closed", "The original registration ended.");
    const ticket = randomUUID();
    this.pending = { ticket, host: data.host, targets };
    return { ticket, targets };
  }

  discard(ticket: string) {
    if (this.pending?.ticket === ticket) this.pending = undefined;
  }

  async confirm(ticket: string): Promise<ReleaseOutcome[]> {
    const plan = this.pending;
    if (!plan || plan.ticket !== ticket || this.busy)
      throw new HostError("cancelled", "Release confirmation expired.");
    this.pending = undefined;
    this.busy = true;
    const results: ReleaseOutcome[] = [];
    const signal = this.signal();
    try {
      for (const target of plan.targets) {
        if (signal.aborted) {
          results.push("rejected");
          continue;
        }
        try {
          const body = JSON.stringify({
            host: plan.host,
            id: target.id,
            reference: target.reference,
          });
          const res = await this.request(
            new URL("/gui/v1/release", this.endpoint),
            "POST",
            signal,
            body,
          );
          await res.body?.cancel();
          results.push(
            res.status === 204
              ? "released"
              : res.status === 410
                ? "ended"
                : [401, 403, 409, 429].includes(res.status)
                  ? "rejected"
                  : "unknown",
          );
        } catch {
          results.push("unknown");
        }
      }
      return results;
    } finally {
      this.busy = false;
    }
  }
}
