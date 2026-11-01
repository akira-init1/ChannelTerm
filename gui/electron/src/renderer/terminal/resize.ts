/** Coalesce geometry only: one request in flight and one latest pending size. */
export class ResizeQueue {
  private pending?: { columns: number; rows: number };
  private timer?: ReturnType<typeof setTimeout>;
  private running = false;
  private disposed = false;

  constructor(
    private readonly send: (size: {
      columns: number;
      rows: number;
    }) => Promise<void>,
    private readonly failed: (error: unknown) => void,
    private readonly delay = 80,
  ) {}

  get busy(): boolean {
    return this.running || this.pending !== undefined;
  }

  schedule(columns: number, rows: number): void {
    if (this.disposed) return;
    this.pending = { columns, rows };
    this.arm();
  }

  private arm(): void {
    if (this.disposed || !this.pending || this.running || this.timer) return;
    this.timer = setTimeout(() => {
      this.timer = undefined;
      void this.flush();
    }, this.delay);
  }

  private async flush(): Promise<void> {
    const size = this.pending;
    this.pending = undefined;
    if (!size || this.disposed) return;
    this.running = true;
    try {
      await this.send(size);
    } catch (error) {
      this.pending = undefined; // Never replay an uncertain submission.
      if (!this.disposed) this.failed(error);
    } finally {
      this.running = false;
      this.arm();
    }
  }

  dispose(): void {
    this.disposed = true;
    clearTimeout(this.timer);
    this.pending = undefined;
  }
}
