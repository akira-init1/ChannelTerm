const sizeProbe = "\x1b7\x1b[r\x1b[999;999H\x1b[6n\x1b8";

/** Tracks only safe styling boundaries; xterm remains the terminal emulator. */
export class HighlightVT {
  private state = "ground";
  private parameters = "";
  private overflow = false;
  private utf8C2 = false;
  private alternate = false;
  private attributes = new Set<number>();
  private probe = 0;
  lineHasText = false;

  get safe() {
    return this.state === "ground" && !this.alternate && !this.attributes.size;
  }

  /** True only when a presentation-neutral sequence establishes a prompt boundary. */
  feed(byte: number): boolean {
    const c1 = this.utf8C2 && byte >= 128 && byte <= 159;
    this.utf8C2 = byte === 194;
    if (c1) {
      // xterm recognizes UTF-8 encoded C1 controls too. Preserve their bytes and
      // track their persistent state without mistaking them for printable text.
      if (byte === 155) {
        this.state = "csi";
        this.parameters = "";
        this.overflow = false;
      } else if (byte === 157) this.state = "osc";
      else if ([144, 152, 158, 159].includes(byte)) this.state = "string";
      else if (byte === 156) this.state = "ground";
      return false;
    }
    let probeComplete = false;
    if (this.probe && byte === sizeProbe.charCodeAt(this.probe)) {
      if (++this.probe === sizeProbe.length) {
        this.probe = 0;
        probeComplete = true;
      }
    } else {
      this.probe = byte === 27 && !this.lineHasText && this.safe ? 1 : 0;
    }
    if (byte === 24 || byte === 26) {
      this.state = "ground";
      this.parameters = "";
      return false;
    }
    switch (this.state) {
      case "ground":
        if (byte === 27) this.state = "escape";
        else if (byte === 10) this.lineHasText = false;
        else if (byte >= 32 && byte !== 127) this.lineHasText = true;
        break;
      case "escape":
        this.state = "ground";
        if (byte === 56) return probeComplete && !this.lineHasText && this.safe;
        if (byte === 91) {
          this.state = "csi";
          this.parameters = "";
          this.overflow = false;
        } else if (byte === 93) this.state = "osc";
        else if ([80, 88, 94, 95].includes(byte)) this.state = "string";
        else if (byte === 27) this.state = "escape";
        else if (byte === 99) {
          this.alternate = false;
          this.attributes.clear();
          this.lineHasText = false;
          return true;
        }
        break;
      case "csi":
        if (byte === 27) this.state = "escape";
        else if (byte >= 48 && byte <= 63) {
          if (this.parameters.length < 128)
            this.parameters += String.fromCharCode(byte);
          else this.overflow = true;
        } else if (byte >= 32 && byte <= 47) {
          // Intermediates do not alter the state subset used by CLI highlighting.
        } else {
          const boundary = byte >= 64 && byte <= 126 && this.finishCSI(byte);
          this.state = "ground";
          return !!boundary && !this.lineHasText && this.safe;
        }
        break;
      case "osc":
        if (byte === 7) {
          this.state = "ground";
          return !this.lineHasText && this.safe;
        }
        if (byte === 27) this.state = "osc-escape";
        break;
      case "osc-escape":
        if (byte === 92) {
          this.state = "ground";
          return !this.lineHasText && this.safe;
        }
        if (byte !== 27) this.state = "osc";
        break;
      case "string":
        if (byte === 27) this.state = "string-escape";
        break;
      case "string-escape":
        if (byte === 92) this.state = "ground";
        else if (byte !== 27) this.state = "string";
    }
    return false;
  }

  private finishCSI(final: number): boolean {
    const parameters = this.parameters;
    this.parameters = "";
    if (this.overflow) {
      this.attributes.add(-1);
      return false;
    }
    if (final === 109) {
      const parts = parameters.split(";");
      for (let index = 0; index < parts.length; index++) {
        const part = parts[index]!;
        const code = Number(part.split(":")[0]);
        this.setSGR(code);
        if (!part.includes(":") && [38, 48, 58].includes(code)) {
          if (parts[index + 1] === "5") index += 2;
          else if (parts[index + 1] === "2") index += 4;
        }
      }
      return true;
    }
    if ((final === 104 || final === 108) && parameters.startsWith("?")) {
      let boundary = true;
      for (const part of parameters.slice(1).split(";")) {
        const mode = Number(part);
        if ([47, 1047, 1049].includes(mode)) {
          this.alternate = final === 104;
          boundary &&= !this.alternate;
        } else if (mode !== 25 && mode !== 2004) boundary = false;
      }
      return boundary;
    }
    return false;
  }

  private setSGR(code: number) {
    if (code === 0) return this.attributes.clear();
    const category =
      code === 2
        ? 1
        : code === 21
          ? 4
          : code === 6
            ? 5
            : (code >= 30 && code <= 38) || (code >= 90 && code <= 97)
              ? 30
              : (code >= 40 && code <= 48) || (code >= 100 && code <= 107)
                ? 40
                : code === 52
                  ? 51
                  : code;
    const resets: Record<number, number> = {
      22: 1,
      23: 3,
      24: 4,
      25: 5,
      27: 7,
      28: 8,
      29: 9,
      39: 30,
      49: 40,
      54: 51,
      55: 53,
      59: 58,
    };
    const reset = resets[code];
    if (reset !== undefined) this.attributes.delete(reset);
    // Unknown SGR values share one bit of state; untrusted output cannot grow
    // this set by continually inventing new parameter numbers.
    else
      this.attributes.add(
        [1, 3, 4, 5, 7, 8, 9, 30, 40, 51, 53, 58].includes(category)
          ? category
          : -1,
      );
  }
}
