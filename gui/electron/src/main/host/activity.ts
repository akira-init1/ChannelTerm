import type { ActivityRow, ActivitySnapshot } from "../../shared/activity";
import { incompatible, integer, object, text } from "./validation";

const pageSize = 32;
const retainedRows = 200;
const maxActivityBytes = 16 * 1024 * 1024;

function timestamp(value: unknown): string {
  const stamp = text(value);
  if (
    stamp.length > 64 ||
    !/^\d{4}-\d\d-\d\dT\d\d:\d\d:\d\d(?:\.\d{1,9})?(?:Z|[+-]\d\d:\d\d)$/.test(
      stamp,
    ) ||
    !Number.isFinite(Date.parse(stamp))
  )
    throw incompatible();
  // Activity uses the Host's zone; structured events use UTC. Display both in UTC.
  return new Date(stamp).toISOString();
}
function label(value: unknown, maximum = 64): string {
  const result = text(value);
  if (result.length > maximum || /[\x00-\x1f\x7f]/.test(result))
    throw incompatible();
  return result;
}
function detail(value: unknown, maximum = 128): string {
  return typeof value === "string"
    ? value.slice(0, maximum).replace(/[\x00-\x1f\x7f]/g, " ")
    : "";
}

/** Cursors and bounded history belong to one attachment, independently of output/transfer reads. */
export class ActivityFeed {
  activityCursor?: number;
  eventCursor?: number;
  private rows: ActivityRow[] = [];
  private dropped = false;
  private truncated = false;
  constructor(private readonly sessionId: string) {}

  parameters(kind: "activity" | "events"): Record<string, unknown> {
    const cursor = kind === "activity" ? this.activityCursor : this.eventCursor;
    return {
      session_id: this.sessionId,
      max_events: pageSize,
      ...(cursor === undefined ? {} : { cursor, timeout_ms: 1 }),
    };
  }

  /** Validate both streams before advancing either cursor. A failed page cannot silently consume observations. */
  apply(
    activity: Record<string, unknown> | undefined,
    events: Record<string, unknown> | undefined,
  ): ActivitySnapshot {
    const added: ActivityRow[] = [];
    let dropped = false;
    let payloadBytes = 0;
    const parse = (
      response: Record<string, unknown> | undefined,
      kind: "activity" | "events",
      cursor: number | undefined,
    ) => {
      if (!response) return cursor;
      const next = integer(response.next);
      if (
        !Array.isArray(response.events) ||
        response.events.length > pageSize ||
        typeof response.dropped !== "boolean" ||
        next < response.events.length ||
        (cursor !== undefined &&
          (next - response.events.length < cursor ||
            (!response.dropped && next - cursor !== response.events.length)))
      )
        throw incompatible();
      dropped ||= response.dropped;
      for (let index = 0; index < response.events.length; index++) {
        const event = object(response.events[index]);
        const sequence = next - response.events.length + index;
        const row: ActivityRow = {
          id: `${kind}:${sequence}`,
          timestamp: timestamp(event.timestamp),
          actor: label(event.actor),
          type: "write",
          details: "",
          history: cursor === undefined,
          clientReport: false,
        };
        if (kind === "activity") {
          row.type = label(event.operation);
          const data = text(event.data);
          if (
            event.encoding !== "base64" ||
            data.length > 22369624 ||
            data.length % 4 !== 0 ||
            !/^[A-Za-z0-9+/]*={0,2}$/.test(data)
          )
            throw incompatible();
          row.bytes =
            (data.length / 4) * 3 -
            (data.endsWith("==") ? 2 : data.endsWith("=") ? 1 : 0);
          payloadBytes += row.bytes;
          if (payloadBytes > maxActivityBytes) throw incompatible();
        } else {
          if (
            text(event.session_id) !== this.sessionId ||
            integer(event.id) !== sequence
          )
            throw incompatible();
          row.type = label(event.type);
          const metadata =
            event.metadata === undefined ? {} : object(event.metadata);
          // Never forward whole metadata: it may gain capabilities or sensitive fields.
          row.details = [
            "type",
            "state",
            "operation_id",
            "transfer_id",
            "kind",
            "reason",
          ]
            .flatMap((key) => {
              const value = detail(metadata[key]);
              return value ? [`${key}: ${value}`] : [];
            })
            .join(" · ")
            .slice(0, 512);
          if (
            typeof metadata.exit_code === "number" &&
            Number.isSafeInteger(metadata.exit_code)
          )
            row.details += ` · exit_code: ${metadata.exit_code}`;
          if (row.type === "DEBUG_CLIENT_REPORT") {
            row.clientReport = true;
            row.details = detail(metadata.text, 512);
          }
          row.details = row.details.slice(0, 512);
        }
        added.push(row);
      }
      return next;
    };
    const activityCursor = parse(activity, "activity", this.activityCursor);
    const eventCursor = parse(events, "events", this.eventCursor);
    const combined = [...this.rows, ...added].sort(
      (left, right) =>
        Date.parse(left.timestamp) - Date.parse(right.timestamp) ||
        left.id.localeCompare(right.id, "en", { numeric: true }),
    );
    this.truncated ||= combined.length > retainedRows;
    this.dropped ||= dropped;
    this.rows = combined.slice(-retainedRows);
    this.activityCursor = activityCursor;
    this.eventCursor = eventCursor;
    return {
      rows: this.rows.map((row) => ({ ...row })),
      dropped: this.dropped,
      truncated: this.truncated,
    };
  }
}
