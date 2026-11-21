/** Public xterm marker subset; no private buffer or renderer state is used. */
export interface LineMarker {
  readonly id: number;
  readonly line: number;
  readonly isDisposed: boolean;
  dispose(): void;
}

interface MetadataBuffer {
  readonly type: "normal" | "alternate";
  readonly baseY: number;
  readonly cursorY: number;
  readonly cursorX: number;
  readonly length: number;
  getLine(index: number):
    | {
        readonly isWrapped: boolean;
        translateToString(trimRight?: boolean): string;
      }
    | undefined;
}

/** Minimal consumer-owned view of the public xterm API, also usable by deterministic tests. */
export interface MetadataTerminal {
  readonly buffer: {
    readonly active: MetadataBuffer;
    readonly normal: MetadataBuffer;
  };
  registerMarker(offset?: number): LineMarker | undefined;
}

/** Local first-observation metadata, never device event time or a claim of complete history. */
export interface LineObservation {
  present: boolean;
  sequence?: number;
  receivedAt?: number;
  continuation: boolean;
}

interface RetainedLine {
  marker: LineMarker;
  sequence: number;
  receivedAt?: number;
}

export interface MetadataBatch {
  readonly generation: number;
  readonly normal: boolean;
  readonly marker?: LineMarker;
  readonly receivedAt?: number;
}

/** Bounded metadata anchors follow logical lines through public marker reflow and trimming. */
export class LineMetadata {
  private readonly lines = new Map<number, RetainedLine>();
  private readonly batches = new Set<LineMarker>();
  private generation = 0;
  private sequence = 0;
  private disposed = false;

  constructor(
    private readonly terminal: MetadataTerminal,
    private readonly maximum = 6000,
  ) {
    if (!Number.isInteger(maximum) || maximum < 1 || maximum > 6000)
      throw new RangeError("Invalid line metadata limit.");
  }

  get size(): number {
    this.prune();
    return this.lines.size;
  }

  /** Capture an anchor before parsing a batch. Undefined time identifies historical/unknown output. */
  begin(receivedAt?: number, fromStart = false): MetadataBatch {
    const normal =
      !this.disposed && this.terminal.buffer.active.type === "normal";
    const buffer = this.terminal.buffer.normal;
    const marker = normal
      ? this.terminal.registerMarker(
          fromStart ? -(buffer.baseY + buffer.cursorY) : 0,
        )
      : undefined;
    if (marker) {
      this.batches.add(marker);
      // The view serializes rendered reads; keep even exceptional timer/pref transitions bounded.
      while (this.batches.size > 4) {
        const oldest = this.batches.values().next().value!;
        oldest.dispose();
        this.batches.delete(oldest);
      }
    }
    return {
      generation: this.generation,
      normal,
      marker,
      receivedAt:
        receivedAt !== undefined &&
        Number.isFinite(receivedAt) &&
        receivedAt >= 0
          ? receivedAt
          : undefined,
    };
  }

  /** Commit only normal-buffer lines after xterm parsed the write; alternate screens own no metadata. */
  finish(batch: MetadataBatch): void {
    try {
      if (
        this.disposed ||
        batch.generation !== this.generation ||
        this.terminal.buffer.active.type !== "normal" ||
        (batch.marker && !this.batches.has(batch.marker))
      )
        return;
      this.prune();
      const buffer = this.terminal.buffer.normal;
      const cursor = buffer.baseY + buffer.cursorY;
      const current = buffer.getLine(cursor);
      const end =
        current?.translateToString(true) || buffer.cursorX > 0
          ? cursor
          : cursor - 1;
      const start =
        batch.marker && !batch.marker.isDisposed
          ? Math.max(0, batch.marker.line)
          : 0;
      const time = batch.normal ? batch.receivedAt : undefined;
      const first = Math.max(start, end - this.maximum + 1);
      // A small write usually touches only a few rows. Avoid copying the entire
      // retained history into temporary arrays/sets for every parsed batch.
      const occupied = new Set<number>();
      this.lines.forEach((entry) => {
        const line = entry.marker.line;
        if (line >= first && line <= end) occupied.add(line);
      });
      // A single large write can trim more than a buffer. Only retained rows can acquire anchors.
      for (let index = first; index <= end && index < buffer.length; index++) {
        const line = buffer.getLine(index);
        if (
          !line ||
          line.isWrapped ||
          occupied.has(index) ||
          this.sequence >= Number.MAX_SAFE_INTEGER
        )
          continue;
        const marker = this.terminal.registerMarker(index - cursor);
        if (!marker || marker.isDisposed || marker.line !== index) {
          marker?.dispose();
          continue;
        }
        this.lines.set(marker.id, {
          marker,
          sequence: ++this.sequence,
          receivedAt: time,
        });
        while (this.lines.size > this.maximum) {
          const first = this.lines.values().next().value!;
          first.marker.dispose();
          this.lines.delete(first.marker.id);
        }
      }
    } finally {
      batch.marker?.dispose();
      if (batch.marker) this.batches.delete(batch.marker);
    }
  }

  /** Preserve source observations when collapsed command bodies leave gaps between rows. */
  projectedViewport(rows: readonly number[]): LineObservation[] {
    const result: LineObservation[] = [];
    for (let start = 0; start < rows.length;) {
      let end = start + 1;
      while (end < rows.length && rows[end] === rows[end - 1]! + 1) end++;
      result.push(...this.viewport(rows[start]!, end - start));
      start = end;
    }
    return result;
  }

  /** Snapshot only the requested viewport; wrapped continuations share the original observation. */
  viewport(start: number, count: number): LineObservation[] {
    if (this.disposed || this.terminal.buffer.active.type !== "normal")
      return [];
    this.prune();
    const buffer = this.terminal.buffer.normal;
    const result: LineObservation[] = [];
    const first = Math.max(0, Math.floor(start));
    const length = Math.max(0, Math.min(1024, Math.floor(count)));
    let logical = first;
    while (logical > 0 && buffer.getLine(logical)?.isWrapped) logical--;
    // Include the logical anchor preceding a wrapped first row, but no unrelated
    // history. Public markers still supply current positions after reflow/trim.
    const end = Math.min(first + length, buffer.length);
    const byLine = new Map<number, RetainedLine>();
    this.lines.forEach((entry) => {
      const line = entry.marker.line;
      if (line >= logical && line < end) byLine.set(line, entry);
    });
    for (
      let index = first;
      index < first + length && index < buffer.length;
      index++
    ) {
      const line = buffer.getLine(index);
      if (!line) break;
      if (!line.isWrapped) logical = index;
      const entry = byLine.get(logical);
      result.push({
        present:
          index < buffer.baseY + buffer.cursorY ||
          !!line.translateToString(true) ||
          (index === buffer.baseY + buffer.cursorY && buffer.cursorX > 0),
        sequence: entry?.sequence,
        receivedAt: entry?.receivedAt,
        continuation: line.isWrapped,
      });
    }
    return result;
  }

  /** xterm reset can leave old public markers alive. Explicitly clear them before reset/clear/loss. */
  clear(): void {
    this.generation++;
    for (const entry of this.lines.values()) entry.marker.dispose();
    for (const marker of this.batches) marker.dispose();
    this.lines.clear();
    this.batches.clear();
  }

  dispose(): void {
    if (this.disposed) return;
    this.disposed = true;
    this.clear();
  }

  private prune(): void {
    // Map entries iteration materializes a pair for every retained line. This
    // hot path also runs for hidden tabs; forEach avoids that allocation churn.
    this.lines.forEach((entry, id) => {
      if (entry.marker.isDisposed || entry.marker.line < 0)
        this.lines.delete(id);
    });
  }
}
