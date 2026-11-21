import type { IBuffer, IBufferCell, IMarker, Terminal } from "@xterm/xterm";
import { defaultHighlights } from "../../shared/default-highlights";

const prompts = [
  defaultHighlights.prompts.promptPattern,
  defaultHighlights.prompts.minimalShellPromptPattern,
  defaultHighlights.prompts.bootloaderPromptPattern,
  "^(?:[$#%](?:\\s|$)|PS [^>]+>|[A-Za-z]:\\\\[^>]*>)",
].map((pattern) => new RegExp(pattern));

/** Match displayed prompt lines only. Wrapped continuation rows cannot start a command. */
function matchesPrompt(buffer: IBuffer, row: number) {
  const line = buffer.getLine(row);
  if (!line || line.isWrapped) return false;
  const text = line
    .translateToString(true)
    .replace(/^\[\d\d:\d\d:\d\d\]\s+/, "");
  return prompts.some((pattern) => pattern.test(text + " "));
}

/** Keep concealed cells concealed in the plain-text history and accessibility labels. */
function visibleText(buffer: IBuffer, row: number, cell: IBufferCell) {
  const line = buffer.getLine(row);
  if (!line) return "";
  let text = "";
  for (let column = 0; column < line.length; column++) {
    const value = line.getCell(column, cell);
    if (!value || value.getWidth() === 0) continue;
    text += value.isInvisible()
      ? " ".repeat(value.getWidth())
      : value.getChars() || " ";
  }
  return text.replace(/ +$/, "");
}

interface Block {
  header: number;
  start: number;
  end: number;
}

/** Project backwards from xterm's viewport end. A hidden command body costs one jump. */
export function foldedRows(
  bottom: number,
  count: number,
  blocks: readonly Block[],
) {
  const rows: number[] = [];
  let block = blocks.length - 1;
  for (let row = bottom; row >= 0 && rows.length < count; row--) {
    while (block >= 0 && row < blocks[block]!.start) block--;
    if (block >= 0 && row < blocks[block]!.end)
      row = blocks[block--]!.start - 1;
    rows.push(row);
  }
  return rows.reverse();
}

/** The prompt row and the next prompt anchor each fold across scrollback trim/reflow. */
export class OutputFold {
  private readonly closed = new Map<IMarker, IMarker>();
  private readonly prompts = new Set<IMarker>();
  private readonly promptRows = new Set<number>();
  private readonly promptHandler;
  private blocks: Block[] = [];
  private readonly screen: Element | null;
  private readonly bufferChange;
  private readonly selectionChange;
  private readonly buttons = new Map<number, HTMLButtonElement>();
  private readonly onWheel = (event: WheelEvent) => {
    if (
      !this.blocks.length ||
      !event.deltaY ||
      event.ctrlKey ||
      event.shiftKey ||
      event.altKey ||
      event.metaKey
    )
      return;
    const buffer = this.terminal.buffer.active;
    let last = buffer.viewportY + this.terminal.rows - 1;
    const direction = Math.sign(event.deltaY);
    const steps = Math.min(
      this.terminal.rows,
      Math.max(
        1,
        Math.ceil(Math.abs(event.deltaY) / (event.deltaMode === 0 ? 32 : 1)),
      ),
    );
    for (let n = 0; n < steps; n++) {
      last += direction;
      const hidden = this.blocks.find(
        (block) => last >= block.start && last < block.end,
      );
      if (hidden) last = direction > 0 ? hidden.end : hidden.start - 1;
    }
    event.preventDefault();
    event.stopPropagation();
    this.terminal.scrollToLine(
      Math.max(0, Math.min(buffer.baseY, last - this.terminal.rows + 1)),
    );
  };

  constructor(
    private readonly terminal: Terminal,
    private readonly margin: HTMLElement,
    private readonly history: HTMLElement,
    private readonly surface: HTMLElement,
    private readonly translate: (id: string) => string,
    private readonly schedule: () => void,
    private readonly onToggle: () => void,
  ) {
    this.screen = terminal.element?.querySelector(".xterm-screen") ?? null;
    this.promptHandler = terminal.parser.registerOscHandler(133, (data) => {
      if (
        (data === "A" || data.startsWith("A;")) &&
        terminal.buffer.active.type === "normal"
      ) {
        const buffer = terminal.buffer.active;
        const row = buffer.baseY + buffer.cursorY;
        // A remote stream may repeat markers without advancing. Bound both work and storage.
        if (![...this.prompts].some((marker) => marker.line === row)) {
          const marker = terminal.registerMarker(0);
          this.prompts.add(marker);
          marker.onDispose(() => this.prompts.delete(marker));
          if (this.prompts.size > 512)
            this.prompts.values().next().value?.dispose();
        }
      }
      return false;
    });
    this.bufferChange = terminal.buffer.onBufferChange(() => this.clear());
    this.selectionChange = terminal.onSelectionChange(() => {
      if (!this.closed.size) return;
      const selection = terminal.getSelectionPosition();
      if (
        selection &&
        this.blocks.some(
          (block) =>
            selection.start.y < block.end && selection.end.y >= block.start,
        )
      )
        this.clear();
    });
    surface.addEventListener("wheel", this.onWheel, {
      capture: true,
      passive: false,
    });
  }

  private bodyStart(buffer: IBuffer, header: number) {
    let first = header + 1;
    while (buffer.getLine(first)?.isWrapped) first++;
    return first;
  }

  private refreshPromptRows() {
    this.promptRows.clear();
    for (const marker of this.prompts) this.promptRows.add(marker.line);
  }

  private isHeader(buffer: IBuffer, row: number) {
    return this.promptRows.has(row) || matchesPrompt(buffer, row);
  }

  private toggle(row: number) {
    const buffer = this.terminal.buffer.active;
    this.refreshPromptRows();
    if (buffer.type !== "normal" || !this.isHeader(buffer, row)) return;
    const top =
      foldedRows(
        buffer.viewportY + this.terminal.rows - 1,
        this.terminal.rows,
        this.blocks,
      )[0] ?? 0;
    for (const [header, end] of this.closed) {
      if (header.line !== row) continue;
      this.closed.delete(header);
      header.dispose();
      end.dispose();
      this.showFrom(top);
      return;
    }
    const cursor = buffer.baseY + buffer.cursorY;
    const start = this.bodyStart(buffer, row);
    let end = start;
    while (end <= cursor && !this.isHeader(buffer, end)) end++;
    if (end <= start || end > cursor) return;
    this.onToggle();
    this.terminal.clearSelection();
    this.closed.set(
      this.terminal.registerMarker(row - cursor),
      this.terminal.registerMarker(end - cursor),
    );
    this.showFrom(top);
  }

  /** Fill newly exposed space with following commands, even when a block spans many screens. */
  private showFrom(top: number) {
    const buffer = this.terminal.buffer.active;
    this.blocks = this.ranges(buffer);
    let bottom = top + this.terminal.rows - 1;
    for (const block of this.blocks) {
      if (block.end <= top) continue;
      if (block.start > bottom) break;
      bottom += block.end - Math.max(top, block.start);
    }
    this.terminal.scrollToLine(
      Math.max(0, Math.min(buffer.baseY, bottom - this.terminal.rows + 1)),
    );
    this.schedule();
    this.terminal.focus();
  }

  private ranges(buffer: IBuffer) {
    const blocks: Block[] = [];
    for (const [header, next] of this.closed) {
      const start = this.bodyStart(buffer, header.line);
      if (
        header.isDisposed ||
        next.isDisposed ||
        start >= next.line ||
        next.line > buffer.baseY + buffer.cursorY ||
        !this.isHeader(buffer, header.line) ||
        !this.isHeader(buffer, next.line)
      ) {
        this.closed.delete(header);
        header.dispose();
        next.dispose();
      } else blocks.push({ header: header.line, start, end: next.line });
    }
    return blocks.sort((a, b) => a.header - b.header);
  }

  /** Share a single animation frame with the metadata gutter. Only materialize visible rows. */
  draw(): number[] | undefined {
    const terminal = this.terminal;
    const buffer = terminal.buffer.active;
    const height =
      (this.screen?.getBoundingClientRect().height ?? 0) / terminal.rows;
    if (!height || buffer.type !== "normal") return;
    this.refreshPromptRows();
    this.blocks = this.ranges(buffer);
    const rows = foldedRows(
      buffer.viewportY + terminal.rows - 1,
      terminal.rows,
      this.blocks,
    );
    this.drawMargin(buffer, rows, height);
    if (!this.blocks.length) {
      this.resetProjection();
      return;
    }

    // Preserve xterm's actual cursor, IME, styling and pointer coordinates for the
    // contiguous live tail. Only the historical prefix above the last gap is projected.
    let tail = rows.length;
    while (
      tail > 0 &&
      rows[tail - 1]! >= buffer.viewportY &&
      (tail === rows.length || rows[tail - 1]! + 1 === rows[tail])
    )
      tail--;
    const source =
      (rows[tail] ?? buffer.viewportY + terminal.rows) - buffer.viewportY;
    const last = rows.at(-1)! - buffer.viewportY + 1;
    this.surface.style.setProperty(
      "--fold-offset",
      `${(tail - source) * height}px`,
    );
    this.surface.style.setProperty("--fold-top", `${source * height}px`);
    this.surface.style.setProperty(
      "--fold-bottom",
      `${Math.max(0, terminal.rows - last) * height}px`,
    );
    this.surface.dataset.folded = "";
    this.history.style.font = `${terminal.options.fontSize}px/${height}px ${terminal.options.fontFamily}`;
    this.history.hidden = tail === 0;
    const cell = buffer.getNullCell();
    const text = rows
      .slice(0, tail)
      .map((row) => visibleText(buffer, row, cell))
      .join("\n");
    if (text !== this.history.textContent) this.history.textContent = text;
    this.history.style.height = `${tail * height}px`;
    return rows;
  }

  private drawMargin(buffer: IBuffer, rows: number[], height: number) {
    this.margin.style.setProperty("--fold-row-height", `${height}px`);
    const cell = buffer.getNullCell();
    const cursor = buffer.baseY + buffer.cursorY;
    const wanted = new Set<number>();
    // Visible headers are ordered. Only the last one may need a lookahead off screen.
    const headers = rows.filter(
      (row) => row <= cursor && this.isHeader(buffer, row),
    );
    for (let i = 0; i < headers.length; i++) {
      const header = headers[i]!;
      const start = this.bodyStart(buffer, header);
      let end = headers[i + 1];
      if (end === undefined) {
        end = start;
        while (end <= cursor && !this.isHeader(buffer, end)) end++;
      }
      if (end <= start || end > cursor) continue;
      wanted.add(header);
      let button = this.buttons.get(header);
      if (!button) {
        button = document.createElement("button");
        button.type = "button";
        button.onclick = () => this.toggle(header);
        this.margin.append(button);
        this.buttons.set(header, button);
      }
      const collapsed = this.blocks.some((block) => block.header === header);
      button.textContent = collapsed ? "+" : "−";
      button.setAttribute("aria-expanded", String(!collapsed));
      button.title = this.translate(
        collapsed ? "terminal.expandOutput" : "terminal.collapseOutput",
      );
      button.setAttribute(
        "aria-label",
        button.title + " · " + visibleText(buffer, header, cell),
      );
      button.style.top = `${rows.indexOf(header) * height}px`;
      // The elbow ends at the center of the last output row. The margin clips
      // a guide whose command continues below the viewport.
      button.style.setProperty(
        "--guide-height",
        `${(end - header - 1) * height}px`,
      );
    }
    for (const [row, button] of this.buttons)
      if (!wanted.has(row)) {
        button.remove();
        this.buttons.delete(row);
      }
  }

  selection() {
    const selection = window.getSelection();
    return selection &&
      this.history.contains(selection.anchorNode) &&
      this.history.contains(selection.focusNode)
      ? selection.toString()
      : "";
  }

  private resetProjection() {
    if (!this.surface.hasAttribute("data-folded")) return;
    this.surface.removeAttribute("data-folded");
    this.history.hidden = true;
    this.history.textContent = "";
  }

  clear() {
    for (const [header, end] of this.closed) {
      header.dispose();
      end.dispose();
    }
    this.closed.clear();
    this.blocks = [];
    this.resetProjection();
    this.margin.replaceChildren();
    this.buttons.clear();
    this.schedule();
  }

  reset() {
    for (const marker of this.prompts) marker.dispose();
    this.promptRows.clear();
    this.clear();
  }

  dispose() {
    this.reset();
    this.promptHandler.dispose();
    this.bufferChange.dispose();
    this.selectionChange.dispose();
    this.surface.removeEventListener("wheel", this.onWheel, true);
  }
}
