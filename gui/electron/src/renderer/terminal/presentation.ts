import type { TerminalSettings } from "../../shared/terminal-settings";

/** Local hex formatting only. Main supplies decoded legacy text after transfer suppression. */
export class TerminalPresentation {
  private offset = 0;
  constructor(private readonly settings: TerminalSettings) {}
  reset() {
    this.offset = 0;
  }
  output(data: Uint8Array, text?: string): string | Uint8Array {
    if (this.settings.view === "text") return text ?? data;
    let result = "";
    for (let start = 0; start < data.length; start += 16) {
      const row = data.subarray(start, start + 16);
      const hex = Array.from(row, (b) => b.toString(16).padStart(2, "0"))
        .join(" ")
        .padEnd(47, " ");
      const ascii = Array.from(row, (b) =>
        b >= 32 && b < 127 ? String.fromCharCode(b) : ".",
      ).join("");
      result += `${this.offset.toString(16).padStart(8, "0")}  ${hex}  |${ascii}|\r\n`;
      this.offset += row.length;
    }
    return result;
  }
}
