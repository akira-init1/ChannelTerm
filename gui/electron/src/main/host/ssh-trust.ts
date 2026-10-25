import { randomUUID } from "node:crypto";
import { isSSHTrustPrompt, type SSHTrustPrompt } from "../../shared/ssh-trust";
import { HostError } from "./errors";
import { incompatible, object } from "./validation";

/** One window's transient trust interaction, bound to its still-running setup. */
export class SSHTrustExchange {
  private pending?: {
    prompt: SSHTrustPrompt;
    resolve: (accept: boolean) => void;
  };

  current(): SSHTrustPrompt | undefined {
    return this.pending && { ...this.pending.prompt };
  }

  decide(id: unknown, accept: unknown): void {
    if (typeof id !== "string" || typeof accept !== "boolean")
      throw new HostError("invalid_input", "Invalid trust decision.");
    if (!this.pending || this.pending.prompt.id !== id)
      throw new HostError("cancelled", "SSH confirmation has expired.");
    const pending = this.pending;
    this.pending = undefined;
    pending.resolve(accept);
  }

  private async ask(
    prompt: SSHTrustPrompt,
    signal: AbortSignal,
  ): Promise<boolean> {
    if (this.pending)
      throw new HostError("busy", "SSH confirmation is already active.");
    if (signal.aborted)
      throw new HostError("cancelled", "SSH confirmation cancelled.");
    return new Promise((resolve, reject) => {
      const finish = (accept?: boolean) => {
        clearTimeout(timer);
        signal.removeEventListener("abort", abort);
        this.pending = undefined;
        if (accept === undefined)
          reject(
            new HostError(
              "cancelled",
              "SSH confirmation expired or cancelled.",
            ),
          );
        else resolve(accept);
      };
      const abort = () => finish();
      const timer = setTimeout(abort, 120000);
      signal.addEventListener("abort", abort, { once: true });
      this.pending = { prompt, resolve: finish };
    });
  }

  /** Bounded NDJSON from the authenticated fixed route; no untrusted URLs or keys. */
  async read(
    response: Response,
    signal: AbortSignal,
    decide: (capability: string, accept: boolean) => Promise<void>,
  ): Promise<Record<string, unknown>> {
    if (
      response.headers.get("content-type")?.split(";")[0] !==
      "application/x-ndjson"
    )
      throw incompatible();
    const reader = response.body?.getReader();
    if (!reader) throw incompatible();
    const decoder = new TextDecoder("utf-8", { fatal: true });
    let buffer = "";
    let bytes = 0;
    let prompted = false;
    let result: Record<string, unknown> | undefined;
    try {
      for (;;) {
        const { value, done } = await reader.read();
        if (done) {
          buffer += decoder.decode();
          break;
        }
        bytes += value.length;
        if (bytes > 16384) throw incompatible();
        buffer += decoder.decode(value, { stream: true });
        if (buffer.length > 8192) throw incompatible();
        let newline: number;
        while ((newline = buffer.indexOf("\n")) >= 0) {
          const frame = object(JSON.parse(buffer.slice(0, newline)) as unknown);
          buffer = buffer.slice(newline + 1);
          if (result) throw incompatible();
          if (frame.event === "trust") {
            if (
              prompted ||
              typeof frame.challenge_id !== "string" ||
              !/^[a-f0-9]{64}$/.test(frame.challenge_id)
            )
              throw incompatible();
            prompted = true;
            const prompt = {
              id: randomUUID(),
              address: frame.address,
              algorithm: frame.algorithm,
              fingerprint: frame.fingerprint,
            };
            if (!isSSHTrustPrompt(prompt)) throw incompatible();
            const accepted = await this.ask(prompt, signal);
            if (signal.aborted)
              throw new HostError("cancelled", "SSH confirmation cancelled.");
            await decide(frame.challenge_id, accepted);
          } else if (frame.event === "result" || frame.event === "error")
            result = frame;
          else throw incompatible();
        }
      }
      if (buffer || !result || signal.aborted) throw incompatible();
      return result;
    } finally {
      await reader.cancel().catch(() => {});
      reader.releaseLock();
    }
  }
}
