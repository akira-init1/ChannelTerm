import type { TransferStatus } from "../../shared/transfers";
import { integer, object, text, incompatible } from "./validation";

/** Bounded presentation history. Only Host cursor metadata suppresses terminal bytes. */
export class TransferEvents {
  cursor = 0;
  activeTransferId?: string;
  private start?: number;
  private ranges: { start: number; end: number }[] = [];
  private unknown = false;
  private historyCutoff = 0;
  private jobs = new Map<string, TransferStatus>();
  constructor(private sessionId: string) {}

  apply(response: Record<string, unknown>): boolean {
    const next = integer(response.next);
    if (
      next < this.cursor ||
      !Array.isArray(response.events) ||
      response.events.length > 1024 ||
      typeof response.dropped !== "boolean"
    )
      throw incompatible();
    if (response.dropped) {
      this.unknown = true;
      this.start = undefined;
      this.ranges = [];
      for (const job of this.jobs.values())
        if (!job.released) job.state = "unknown";
    }
    for (const raw of response.events) {
      const event = object(raw);
      integer(event.id);
      if (text(event.session_id) !== this.sessionId) throw incompatible();
      const type = text(event.type);
      const m = event.metadata === undefined ? {} : object(event.metadata);
      if (
        (type === "LEASE_ACQUIRED" || type === "LEASE_RELEASED") &&
        (m.type === "file-transfer" || m.type === "debug")
      ) {
        const cursor = integer(m.output_cursor);
        if (type === "LEASE_ACQUIRED") {
          this.start = cursor;
          this.activeTransferId =
            typeof m.transfer_id === "string" ? m.transfer_id : undefined;
        } else {
          if (this.start !== undefined && cursor < this.start)
            throw incompatible();
          this.ranges.push({
            start: this.unknown ? 0 : (this.start ?? 0),
            end: cursor,
          });
          if (this.unknown) this.historyCutoff = cursor;
          const overflow = this.ranges.length > 1024;
          this.ranges = this.ranges.slice(-1024);
          // Retained terminal history can outlive event history. Never expose an
          // evicted transfer's protocol bytes when an attachment replays it.
          if (overflow) {
            this.historyCutoff = this.ranges[0]!.end;
            this.ranges[0]!.start = 0;
          }
          this.start = undefined;
          this.activeTransferId = undefined;
          this.unknown = false;
          const job = this.jobs.get(String(m.transfer_id));
          if (job) {
            job.released = true;
            if (job.state === "cleaning") job.state = "completed";
          }
        }
      }
      if (!type.startsWith("FILE_TRANSFER_")) continue;
      const id = text(m.transfer_id);
      if (!id || id.length > 512) throw incompatible();
      let job = this.jobs.get(id);
      if (!job) {
        job = {
          id,
          sessionId: this.sessionId,
          direction: "",
          source: "",
          destination: "",
          state: "transferring",
          transferred: 0,
          total: 0,
          speed: 0,
          released: false,
        };
        this.jobs.set(id, job);
        if (this.jobs.size > 32)
          this.jobs.delete(this.jobs.keys().next().value!);
      }
      for (const [field, key] of [
        ["direction", "direction"],
        ["source", "source_path"],
        ["destination", "requested_path"],
      ] as const) {
        if (typeof m[key] === "string" && m[key].length <= 4096)
          job[field] = m[key];
      }
      if (m.total !== undefined) job.total = integer(m.total);
      const count = m.transferred ?? m.sent ?? m.received;
      if (count !== undefined) job.transferred = integer(count);
      if (
        typeof m.speed === "number" &&
        Number.isFinite(m.speed) &&
        m.speed >= 0
      )
        job.speed = m.speed;
      if (type === "FILE_TRANSFER_PROGRESS")
        job.state =
          job.total > 0 && job.transferred >= job.total
            ? "verifying"
            : "transferring";
      if (type === "FILE_TRANSFER_COMPLETED") {
        job.state = job.released ? "completed" : "cleaning";
        job.sha256 =
          typeof m.sha256 === "string" && /^[a-f0-9]{64}$/.test(m.sha256)
            ? m.sha256
            : undefined;
        if (
          typeof m.resolved_path === "string" &&
          m.resolved_path.length <= 4096
        )
          job.destination = m.resolved_path;
      }
      if (type === "FILE_TRANSFER_FAILED") job.state = "failed";
      if (type === "FILE_TRANSFER_CANCELLED") {
        job.state = "cancelled";
        job.released = m.lease_released === true;
      }
    }
    this.cursor = next;
    return response.dropped;
  }

  filter(bytes: Uint8Array, end: number): { data: number[]; dropped: boolean } {
    if (this.unknown) return { data: [], dropped: true };
    const start = end - bytes.length;
    const data: number[] = [];
    const limit = Math.min(end, this.start ?? end);
    let cursor = start;
    // Walk intervals once instead of searching all intervals for every byte.
    for (const range of this.ranges) {
      if (range.end <= cursor) continue;
      if (range.start >= limit) break;
      for (; cursor < Math.min(range.start, limit); cursor++)
        data.push(bytes[cursor - start]!);
      cursor = Math.max(cursor, range.end);
    }
    for (; cursor < limit; cursor++) data.push(bytes[cursor - start]!);
    return { data, dropped: start < this.historyCutoff };
  }
  snapshots(): TransferStatus[] {
    return [...this.jobs.values()].map((job) => ({ ...job }));
  }
}
