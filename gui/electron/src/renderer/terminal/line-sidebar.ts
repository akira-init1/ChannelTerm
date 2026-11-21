import type { Terminal } from "@xterm/xterm";
import type { LineDisplay } from "../../shared/display-settings";
import { LineMetadata } from "./line-metadata";

/** View-owned gutter; text never enters xterm, selection, search, or Host output. */
export class LineSidebar {
  readonly metadata: LineMetadata;
  private frame: number | undefined;
  private disposed = false;
  private readonly listeners;

  constructor(
    private readonly terminal: Terminal,
    private readonly element: HTMLElement,
    private readonly state: () => {
      display?: LineDisplay;
      visible: boolean;
      text: boolean;
      t: (id: string) => string;
    },
    private readonly drawFolds: () => number[] | undefined,
  ) {
    this.metadata = new LineMetadata(terminal);
    this.listeners = [
      terminal.onRender(() => this.schedule()),
      terminal.onScroll(() => this.schedule()),
      terminal.onResize(() => this.schedule()),
      terminal.buffer.onBufferChange(() => this.schedule()),
    ];
  }

  schedule(): void {
    if (this.disposed || this.frame !== undefined || !this.state().visible)
      return;
    this.frame = requestAnimationFrame(() => {
      this.frame = undefined;
      this.draw();
    });
  }

  private draw(): void {
    if (this.disposed) return;
    const { display, visible, text, t } = this.state();
    if (!visible) return;
    const shown = !!(display?.timestamps || display?.sequence);
    const supported = text && this.terminal.buffer.active.type === "normal";
    const projected = this.drawFolds();
    this.element.hidden = !shown;
    this.element.classList.toggle("unsupported", !supported);
    this.element.setAttribute("aria-label", t("terminal.auxiliary"));
    this.element.title = t(supported ? "line.hint" : "line.unavailable");
    if (!shown || !supported) {
      this.element.replaceChildren();
      if (shown) this.element.textContent = "—";
      return;
    }
    // Pinned xterm DOM renderer geometry, never its private JavaScript buffer/render services.
    // The native regression checks alignment after font changes, reflow and viewport scrolling.
    const screen = this.terminal.element?.querySelector(".xterm-screen");
    const height = screen?.getBoundingClientRect().height ?? 0;
    if (!height || !this.terminal.rows) return;
    this.element.style.fontSize = `${this.terminal.options.fontSize ?? 14}px`;
    this.element.style.lineHeight = `${height / this.terminal.rows}px`;
    const rows = projected
      ? this.metadata.projectedViewport(projected)
      : this.metadata.viewport(
          this.terminal.buffer.active.viewportY,
          this.terminal.rows,
        );
    // Grow only with displayed ordinals; long-lived views must not overlap the time column.
    const digits = Math.max(
      4,
      ...rows.map((row) => String(row.sequence ?? "").length),
    );
    this.element.style.setProperty("--line-sequence-width", `${digits + 1}ch`);
    const fragment = document.createDocumentFragment();
    for (const row of rows) {
      const entry = document.createElement("div");
      entry.className = "line-observation";
      entry.dataset.continuation = String(row.continuation);
      if (row.sequence !== undefined)
        entry.dataset.sequence = String(row.sequence);
      if (row.receivedAt !== undefined)
        entry.dataset.receivedAt = String(row.receivedAt);
      if (display?.timestamps) {
        const stamp = document.createElement("span");
        stamp.className = "line-time";
        const date =
          row.receivedAt === undefined ? undefined : new Date(row.receivedAt);
        stamp.textContent = !row.present
          ? ""
          : date
            ? `[${[date.getHours(), date.getMinutes(), date.getSeconds()]
                .map((value) => String(value).padStart(2, "0"))
                .join(":")}]`
            : "—";
        stamp.title = date
          ? `${t("line.received")} ${date.toISOString()}`
          : t("line.unknown");
        entry.append(stamp);
      }
      if (display?.sequence) {
        const ordinal = document.createElement("span");
        ordinal.className = "line-sequence";
        ordinal.textContent = row.present ? String(row.sequence ?? "—") : "";
        entry.append(ordinal);
      }
      fragment.append(entry);
    }
    this.element.replaceChildren(fragment);
  }

  clear(): void {
    this.metadata.clear();
    this.schedule();
  }

  dispose(): void {
    if (this.disposed) return;
    this.disposed = true;
    if (this.frame !== undefined) cancelAnimationFrame(this.frame);
    this.listeners.forEach((listener) => listener.dispose());
    this.metadata.dispose();
    this.element.replaceChildren();
  }
}
