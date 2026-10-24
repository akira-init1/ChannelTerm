import iconv from "iconv-lite";
import type { TerminalEncoding } from "../../shared/terminal-encodings";
import { HostError } from "./errors";

/** One attachment's decoder; callers reset it after missing output.
 * Conversion stays in Main because browser TextDecoder aliases some ISO encodings
 * and lacks DOS codepages. No Node codec or Buffer capability reaches Renderer.
 */
export class TerminalCodec {
  private decoder: ReturnType<typeof iconv.getDecoder>;
  private unicodeDecoder?: TextDecoder;
  private big5Tail?: number;
  constructor(private readonly encoding: TerminalEncoding) {
    this.decoder = iconv.getDecoder(encoding);
    this.reset();
  }
  reset() {
    this.big5Tail = undefined;
    this.decoder = iconv.getDecoder(this.encoding);
    // iconv-lite 0.7.3 can truncate a supplementary character when its final
    // multibyte suffix arrives alone. Native CJK streaming decoders preserve it.
    this.unicodeDecoder = [
      "gbk",
      "gb18030",
      "shift_jis",
      "euc-jp",
      "euc-kr",
    ].includes(this.encoding)
      ? new TextDecoder(this.encoding)
      : undefined;
  }
  decode(data: number[]): string {
    if (this.encoding === "big5") {
      // Retain at most one lead byte and decode complete pairs together. This
      // preserves HKSCS supplementary mappings that native ICU maps to PUA.
      const bytes = Buffer.from(
        this.big5Tail === undefined ? data : [this.big5Tail, ...data],
      );
      this.big5Tail = undefined;
      let end = 0;
      while (end < bytes.length) {
        const lead = bytes[end]!;
        if (lead >= 0x81 && lead <= 0xfe) {
          if (end + 1 === bytes.length) {
            this.big5Tail = lead;
            break;
          }
          const trail = bytes[end + 1]!;
          if (
            (trail >= 0x40 && trail <= 0x7e) ||
            (trail >= 0xa1 && trail <= 0xfe)
          ) {
            end += 2;
            continue;
          }
        }
        end++;
      }
      return iconv.decode(bytes.subarray(0, end), "big5");
    }
    if (this.unicodeDecoder)
      return this.unicodeDecoder.decode(Uint8Array.from(data), {
        stream: true,
      });
    if (this.encoding === "ascii")
      return data
        .map((b) => (b < 128 ? String.fromCharCode(b) : "\ufffd"))
        .join("");
    return this.decoder.write(Buffer.from(data));
  }
  encode(data: string): Buffer {
    const bytes = iconv.encode(data, this.encoding);
    if (
      (this.encoding === "ascii" && /[^\x00-\x7f]/.test(data)) ||
      (this.encoding !== "utf-8" && iconv.decode(bytes, this.encoding) !== data)
    )
      throw new HostError(
        "invalid_input",
        "Input cannot be represented in the selected encoding.",
      );
    return bytes;
  }
}
