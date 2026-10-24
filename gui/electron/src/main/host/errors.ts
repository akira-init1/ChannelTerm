import type { ClientError, ErrorCode } from "../../shared/api";

/** Only locally authored diagnostics cross IPC; protocol bodies may contain secrets. */
export class HostError extends Error {
  constructor(
    public readonly code: ErrorCode,
    message: string,
  ) {
    super(message);
  }
}

export function publicError(error: unknown): ClientError {
  return error instanceof HostError
    ? { code: error.code, message: error.message }
    : {
        code: "disconnected",
        message:
          "Host request failed. Check the Host and reconnect. Input was not retried; delivery may be uncertain.",
      };
}
