import { defaultHighlights } from "../../shared/default-highlights";
import { HighlightVT } from "./highlight-vt";

const encoder = new TextEncoder();
const decoder = new TextDecoder("utf-8", { fatal: true, ignoreBOM: true });
const rules = defaultHighlights.rules.map((rule) => ({
  ...rule,
  pattern: new RegExp(rule.pattern, `${rule.flags}gd`),
}));
const prompt = new RegExp(defaultHighlights.prompts.promptPattern, "d");
const minimal = new RegExp(defaultHighlights.prompts.minimalShellPromptPattern);
const bootloader = new RegExp(
  defaultHighlights.prompts.bootloaderPromptPattern,
);
const plainPrompt =
  /^(?:[$#>](?:[ \t]+|$)|\((?:gdb|initramfs)\)|(?:>>>|\.\.\.)(?:[ \t]+|$))/;
const maxPending = 8192;

function style(kind: string): string {
  if (!(kind in defaultHighlights.palette)) return "";
  const color =
    defaultHighlights.palette[kind as keyof typeof defaultHighlights.palette];
  if ("ansi" in color) return `\x1b[${color.ansi}m`;
  const rgb = Number.parseInt(color.hex.slice(1), 16);
  return `\x1b[38;2;${rgb >> 16};${(rgb >> 8) & 255};${rgb & 255}m`;
}

interface Span {
  start: number;
  end: number;
  kind: string;
  priority: number;
}

function renderSpans(text: string, spans: Span[]): string {
  spans.sort(
    (a, b) => b.priority - a.priority || a.start - b.start || b.end - a.end,
  );
  // At most 8 KiB of input and 2048 static-rule matches. A cell's first owner
  // wins, retaining lower-priority fragments without quadratic span subtraction.
  const owners: (string | undefined)[] = new Array(text.length);
  for (const span of spans)
    for (let index = span.start; index < span.end; index++)
      owners[index] ??= span.kind;
  let result = "";
  let count = 0;
  for (let start = 0; start < text.length;) {
    const kind = owners[start];
    let end = start + 1;
    while (end < text.length && owners[end] === kind) end++;
    if (++count > 512) return text;
    const value = text.slice(start, end);
    result += kind ? style(kind) + value + "\x1b[0m" : value;
    start = end;
  }
  return result;
}

function promptText(text: string): string | undefined {
  if (minimal.test(text) || bootloader.test(text) || plainPrompt.test(text))
    return text;
  const match = prompt.exec(text);
  if (!match?.indices) return;
  const kinds = ["promptUser", "promptHost", "promptPath", "promptUser"];
  return renderSpans(
    text,
    kinds.map((kind, index) => {
      const [start, end] = match.indices![index + 1]!;
      return { start, end, kind, priority: 1 };
    }),
  );
}

function renderLine(bytes: Uint8Array): Uint8Array {
  let text: string;
  try {
    text = decoder.decode(bytes);
  } catch {
    return bytes;
  }
  if (/[\x00-\x08\x0b\x0c\x0e-\x1f\x7f]/.test(text) || /\r(?!\r*\n)/.test(text))
    return bytes;
  const ending = /\r*\n$/.exec(text)?.[0] ?? "";
  const content = text.slice(0, text.length - ending.length);
  const renderedPrompt = promptText(content);
  if (renderedPrompt !== undefined)
    return encoder.encode(renderedPrompt + ending);
  const spans: Span[] = [];
  for (const rule of rules) {
    for (const match of text.matchAll(rule.pattern)) {
      const range = match.indices?.[rule.group];
      if (!range || range[0] === range[1]) continue;
      spans.push({
        start: range[0],
        end: range[1],
        kind: rule.kind,
        priority: rule.priority,
      });
      if (spans.length > 2048) return bytes;
    }
  }
  return encoder.encode(renderSpans(text, spans));
}

/** Bounded local byte styling. Remote controls, invalid UTF-8 and long lines remain raw. */
export class SemanticHighlighter {
  private pending: number[] = [];
  private rawLine = false;
  private passthrough = false;
  private vt = new HighlightVT();

  get hasPending() {
    return this.pending.length > 0;
  }

  reset() {
    this.pending = [];
    this.rawLine = false;
    this.passthrough = false;
    this.vt = new HighlightVT();
  }

  /** Release an unknown prompt after the view's bounded latency timer, without styling its echo. */
  release(): Uint8Array {
    const result = new Uint8Array(this.pending);
    if (result.length) this.rawLine = true;
    this.pending = [];
    return result;
  }

  write(bytes: Uint8Array, enabled = true): Uint8Array {
    const output: Uint8Array[] = [];
    let raw: number[] = [];
    const flushRaw = () => {
      if (raw.length) output.push(new Uint8Array(raw));
      raw = [];
    };
    if (!enabled) {
      output.push(this.release(), bytes);
      for (const byte of bytes) this.vt.feed(byte);
      this.passthrough = !this.vt.safe;
      this.rawLine = this.vt.lineHasText;
    } else
      for (const byte of bytes) {
        if ((byte === 27 || byte === 194) && !this.passthrough) {
          raw.push(...this.pending);
          this.pending = [];
          this.rawLine = false;
          this.passthrough = true;
        }
        const boundary = this.vt.feed(byte);
        if (this.passthrough) {
          raw.push(byte);
          if (boundary || (byte === 10 && this.vt.safe))
            this.passthrough = false;
          continue;
        }
        this.pending.push(byte);
        if (byte === 10) {
          flushRaw();
          const line = new Uint8Array(this.pending);
          output.push(this.rawLine ? line : renderLine(line));
          this.pending = [];
          this.rawLine = false;
        } else if (
          this.rawLine ||
          this.pending.length > maxPending ||
          (byte < 32 && byte !== 9 && byte !== 13) ||
          byte === 127
        ) {
          raw.push(...this.pending);
          this.pending = [];
          this.rawLine = true;
        }
      }
    flushRaw();
    // Recognize prompts once per received chunk, never repeatedly rescan a long
    // line per byte. Incomplete UTF-8 stays bounded until the next chunk/timer.
    if (this.pending.length && !this.rawLine && this.pending.at(-1) !== 13) {
      try {
        const text = decoder.decode(new Uint8Array(this.pending));
        const rendered = promptText(text);
        if (rendered !== undefined) {
          output.push(encoder.encode(rendered));
          this.pending = [];
          this.rawLine = true;
        }
      } catch {
        /* A split UTF-8 character is still pending. */
      }
    }
    const result = new Uint8Array(
      output.reduce((size, chunk) => size + chunk.length, 0),
    );
    let offset = 0;
    for (const chunk of output) {
      result.set(chunk, offset);
      offset += chunk.length;
    }
    return result;
  }
}
