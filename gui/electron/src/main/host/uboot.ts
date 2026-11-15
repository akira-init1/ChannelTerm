import {
  isUBootRequest,
  ubootStatuses,
  type UBootOutcome,
  type UBootRequest,
} from "../../shared/uboot";
import { HostError } from "./errors";
import { incompatible, integer, object, text } from "./validation";

/** One Main-owned long request. The SDK's cancellation notification cannot cancel a stateless HTTP request by itself. */
export interface UBootHTTPCall {
  useId: string;
  signal: AbortSignal;
  timeoutMS: number;
}

/** Bind HTTP cancellation/deadline only to the locally registered U-Boot operation. */
export function ubootHTTPCall(
  body: unknown,
  pending: UBootHTTPCall | undefined,
): UBootHTTPCall | undefined {
  if (!pending || typeof body !== "string" || body.length > 512 * 1024)
    return undefined;
  try {
    const request = object(JSON.parse(body));
    const params = object(request.params);
    if (
      request.method !== "tools/call" ||
      params.name !== "terminal_enter_uboot"
    )
      return undefined;
    const instance = object(object(params._meta)["io.channelterm/instance"]);
    return instance.use_id === pending.useId ? pending : undefined;
  } catch {
    return undefined;
  }
}

/** Translate only the existing finite tool arguments after independent Main validation. */
export function ubootArguments(
  sessionId: string,
  request: UBootRequest,
): Record<string, unknown> {
  if (!isUBootRequest(request))
    throw new HostError(
      "invalid_input",
      "Provide verified board settings for one U-Boot attempt.",
    );
  return {
    session_id: sessionId,
    mode: request.mode,
    ...(request.mode === "reboot"
      ? { reboot_command: request.rebootCommand }
      : {}),
    countdown_pattern: request.countdown,
    prompt_pattern: request.prompt,
    boot_started_patterns: [...request.bootStarted],
    interrupt_data_hex: request.interruptHex,
    timeout_ms: request.timeoutMS,
    diagnostics: request.capture ? "capture" : "metadata",
  };
}

function reference(value: unknown, prefix: string): string {
  const id = text(value);
  if (!new RegExp(`^${prefix}-[A-Za-z0-9]{16,64}$`).test(id))
    throw incompatible();
  return id;
}

/** Partial failed results retain evidence; successful writes alone cannot mean entry. */
export function ubootOutcome(
  value: unknown,
  sessionId: string,
  failed: boolean,
): UBootOutcome {
  const row = object(value);
  const status = ubootStatuses.find((status) => status === row.status);
  if (
    !status ||
    text(row.session_id) !== sessionId ||
    typeof row.countdown_matched !== "boolean" ||
    typeof row.prompt_matched !== "boolean" ||
    typeof row.dropped !== "boolean" ||
    (status === "entered") === failed
  )
    throw incompatible();
  const result: UBootOutcome = {
    operationId: reference(row.operation_id, "BOOT"),
    sessionId,
    status,
    outputStart: integer(row.output_start),
    outputEnd: integer(row.output_end),
    countdownCursor: integer(row.countdown_cursor),
    promptCursor: integer(row.prompt_cursor),
    countdownMatched: row.countdown_matched,
    promptMatched: row.prompt_matched,
    bytesWritten: integer(row.bytes_written),
    elapsedMS: integer(row.elapsed_ms),
    dropped: row.dropped,
    diagnosticUnavailable: row.diagnostic_error !== undefined,
    ...(row.diagnostic_id === undefined
      ? {}
      : { diagnosticId: reference(row.diagnostic_id, "DIAG") }),
  };
  if (
    result.outputEnd < result.outputStart ||
    (result.countdownMatched &&
      (result.countdownCursor < result.outputStart ||
        result.countdownCursor > result.outputEnd)) ||
    (result.promptMatched &&
      (result.promptCursor < result.outputStart ||
        result.promptCursor > result.outputEnd)) ||
    (status === "entered" &&
      (!result.countdownMatched || !result.promptMatched || result.dropped)) ||
    (row.diagnostic_error !== undefined &&
      typeof row.diagnostic_error !== "string")
  )
    throw incompatible();
  return result;
}

/** Independent bounded event cursor. Only this authenticated operation may authorize a ready display. */
export class UBootEventFeed {
  private cursor: number;
  private operationId?: string;
  private finished = false;
  constructor(
    private readonly sessionId: string,
    private readonly instanceId: string,
    private readonly useId: string,
    cursor: number,
  ) {
    this.cursor = integer(cursor);
    reference(instanceId, "CI");
    reference(useId, "CU");
  }

  parameters(): Record<string, unknown> {
    return {
      session_id: this.sessionId,
      cursor: this.cursor,
      max_events: 32,
      timeout_ms: 500,
    };
  }

  /** Validate the complete page before advancing; lost event evidence must cancel readiness. */
  apply(value: unknown): {
    ready: boolean;
    operationId?: string;
    finished: boolean;
  } {
    const page = object(value),
      next = integer(page.next);
    if (
      !Array.isArray(page.events) ||
      page.events.length > 32 ||
      typeof page.dropped !== "boolean" ||
      next < this.cursor ||
      next - page.events.length < this.cursor ||
      (!page.dropped && next - this.cursor !== page.events.length)
    )
      throw incompatible();
    if (page.dropped)
      throw new HostError(
        "tool_error",
        "U-Boot event evidence was lost; do not reset the device.",
      );
    let operationId = this.operationId,
      finished = this.finished;
    for (let index = 0; index < page.events.length; index++) {
      const event = object(page.events[index]);
      if (
        integer(event.id) !== this.cursor + index ||
        text(event.session_id) !== this.sessionId
      )
        throw incompatible();
      if (event.type !== "OPERATION_STATE") continue;
      const client = event.client === undefined ? {} : object(event.client);
      if (
        client.instance_id !== this.instanceId ||
        client.operation_id !== this.useId
      )
        continue;
      const metadata = object(event.metadata);
      if (metadata.operation !== "uboot") continue;
      const id = reference(metadata.operation_id, "BOOT");
      if (operationId && id !== operationId) throw incompatible();
      if (metadata.status === "ready") {
        if (operationId || finished) throw incompatible();
        integer(metadata.output_cursor);
        operationId = id;
      } else {
        if (!ubootStatuses.some((status) => status === metadata.status))
          throw incompatible();
        // Preflight failures can finish before readiness. They never authorize a reset.
        operationId = id;
        finished = true;
      }
    }
    this.cursor = next;
    this.operationId = operationId;
    this.finished = finished;
    return { ready: !!operationId && !finished, operationId, finished };
  }
}
