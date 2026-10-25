import { defaultHighlights } from "../../shared/default-highlights";
import { HighlightVT } from "./highlight-vt";

const encoder = new TextEncoder();
const decoder = new TextDecoder("utf-8", { fatal: true });
const prompt = new RegExp(defaultHighlights.prompts.timestampPromptPattern);
const excluded = new RegExp(defaultHighlights.prompts.timestampExcludedPattern);

/** Bounded local receipt-time decoration; replay, controls and interactive screens stay raw. */
export class PromptTimestamp {
  private pending: number[] = [];
  private rawLine = false;
  private enabled = false;
  private vt = new HighlightVT();

  constructor(private readonly now: () => Date = () => new Date()) {}

  get hasPending() {
    return this.pending.length > 0;
  }

  reset() {
    this.pending = [];
    this.rawLine = false;
    this.enabled = false;
    this.vt = new HighlightVT();
  }

  /** Abandon the candidate before a preference/replay transition or latency deadline. */
  release(): Uint8Array {
    const result = new Uint8Array(this.pending);
    if (result.length) this.rawLine = true;
    this.pending = [];
    return result;
  }

  write(bytes: Uint8Array, enabled: boolean): Uint8Array {
    const output: number[] = [];
    if (enabled !== this.enabled) {
      output.push(...this.release());
      this.enabled = enabled;
    }
    for (const byte of bytes) {
      this.vt.feed(byte);
      if (!enabled || this.rawLine || this.vt.alternateScreen) {
        output.push(...this.release(), byte);
        this.rawLine = byte !== 10 && byte !== 13;
      } else {
        this.pending.push(byte);
        if (byte === 10 || this.pending.length > 8192) {
          output.push(...this.release());
          this.rawLine = byte !== 10;
        }
      }
    }
    if (!enabled) this.rawLine = this.vt.lineHasText;
    if (!this.pending.length) return new Uint8Array(output);
    let text: string;
    try {
      text = decoder.decode(new Uint8Array(this.pending));
    } catch {
      return new Uint8Array(output);
    }
    // Strip only complete SGR for recognition; preserve every original byte.
    const visible = text.replace(/\x1b\[[0-?]*m/g, "");
    if (/\x1b(?:\[[0-?]*)?$/.test(visible)) return new Uint8Array(output);
    if (/[\x00-\x1f\x7f-\x9f]/.test(visible) || excluded.test(visible)) {
      output.push(...this.release());
    } else if (prompt.test(visible)) {
      const date = this.now();
      const time = [date.getHours(), date.getMinutes(), date.getSeconds()]
        .map((value) => String(value).padStart(2, "0"))
        .join(":");
      output.push(...encoder.encode(`[${time}] `), ...this.release());
    }
    return new Uint8Array(output);
  }
}
