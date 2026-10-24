import type { TerminalSettings } from "../../shared/terminal-settings";

/** One decoder per attachment. Transfer suppression happens in Main before these bytes arrive. */
export class TerminalPresentation {
  private decoder: TextDecoder;
  private offset = 0;
  constructor(private readonly settings: TerminalSettings) {
    this.decoder = new TextDecoder(settings.encoding);
  }
  reset() {
    this.decoder = new TextDecoder(this.settings.encoding);
    this.offset = 0;
  }
  output(data: Uint8Array): string | Uint8Array {
    if (this.settings.view === "text")
      return this.settings.encoding === "utf-8"
        ? data
        : this.decoder.decode(data, { stream: true });
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
