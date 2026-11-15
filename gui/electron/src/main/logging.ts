import {
  mkdir,
  open,
  rename,
  rm,
  writeFile,
  type FileHandle,
} from "node:fs/promises";
import { join } from "node:path";
import { randomUUID } from "node:crypto";
import type { RecordingRequest, RecordingStatus } from "../shared/logging";
import type { OutputCapture, CapturedOutput } from "./host/capture";
import type { HostClient } from "./host/client";
import { HostError } from "./host/errors";

/** UTF-8 display export, not a terminal emulator: controls are escaped, LF/tab retained. */
export class RecordingText {
  private decoder = new TextDecoder("utf-8");
  decode(bytes: Uint8Array = new Uint8Array(), final = false): Buffer {
    const text = this.decoder.decode(bytes, { stream: !final });
    return Buffer.from(
      text.replace(
        /[\x00-\x08\x0b-\x1f\x7f-\x9f]/g,
        (value) => `\\x${value.charCodeAt(0).toString(16).padStart(2, "0")}`,
      ),
    );
  }
}

/** One bounded set of exclusively created data/index segments under a new selected directory. */
export class RecordingWriter {
  private data?: FileHandle;
  private index?: FileHandle;
  private names: string[] = [];
  private sequence = 0;
  private size = 0;
  private offset = 0;
  private cursor: number;
  private text = new RecordingText();
  private opened = false;
  private closed = false;

  constructor(
    readonly status: RecordingStatus,
    private readonly request: RecordingRequest,
    start: number,
  ) {
    this.cursor = start;
  }

  private async manifest(state: RecordingStatus["state"]) {
    const path = join(this.status.directory, "manifest.json");
    const temporary = `${path}.${randomUUID()}.tmp`;
    try {
      await writeFile(
        temporary,
        JSON.stringify({
          version: 1,
          ...this.status,
          directory: undefined,
          state,
          next: this.cursor,
          segments: this.names,
          segmentLimit: this.request.segmentMiB * 1048576,
          maxSegments: this.request.segments,
        }) + "\n",
        { mode: 0o600, flag: "wx" },
      );
      await rename(temporary, path);
    } finally {
      await rm(temporary, { force: true }).catch(() => {});
    }
  }

  async start(): Promise<void> {
    await mkdir(this.status.directory, { mode: 0o700 });
    this.opened = true;
    // A crash leaves recording rather than a false clean-stop marker.
    await this.manifest("recording");
    await this.rotate();
  }

  private async closeSegment() {
    const data = this.data,
      index = this.index;
    this.data = undefined;
    this.index = undefined;
    const results = await Promise.allSettled([data?.close(), index?.close()]);
    for (const result of results)
      if (result.status === "rejected") throw result.reason;
  }

  private async rotate() {
    await this.closeSegment();
    while (this.names.length >= this.request.segments) {
      const oldest = this.names[0]!;
      await rm(join(this.status.directory, oldest));
      await rm(join(this.status.directory, oldest + ".jsonl"));
      this.names.shift();
      this.status.rotations++;
    }
    const name = `${String(this.sequence++).padStart(8, "0")}.${this.request.format === "raw" ? "bin" : "txt"}`;
    this.data = await open(join(this.status.directory, name), "wx", 0o600);
    this.index = await open(
      join(this.status.directory, name + ".jsonl"),
      "wx",
      0o600,
    );
    this.names.push(name);
    const header = Buffer.from(
      JSON.stringify({
        type: "segment",
        version: 1,
        format: this.request.format,
        sessionId: this.status.sessionId,
        start: this.cursor,
      }) + "\n",
    );
    await this.index.writeFile(header);
    await this.index.sync();
    this.size = header.length;
    this.offset = 0;
    await this.manifest("recording");
  }

  private async commit(data: Uint8Array, entry: Record<string, unknown>) {
    let line = Buffer.from(
      JSON.stringify({ ...entry, offset: this.offset, bytes: data.length }) +
        "\n",
    );
    if (
      this.size + data.length + line.length >
      this.request.segmentMiB * 1048576
    ) {
      await this.rotate();
      line = Buffer.from(
        JSON.stringify({ ...entry, offset: 0, bytes: data.length }) + "\n",
      );
    }
    if (!this.data || !this.index)
      throw new Error("Recording segment is closed");
    // Index records acknowledge only data that has completed fsync. A crash may leave
    // an unindexed tail or a partial final JSON line; neither is claimed as complete.
    await this.data.writeFile(data);
    await this.data.sync();
    await this.index.writeFile(line);
    await this.index.sync();
    this.size += data.length + line.length;
    this.offset += data.length;
    this.status.bytes += data.length;
  }

  async append(output: CapturedOutput): Promise<void> {
    if (this.closed) throw new Error("Recording is closed");
    if (output.dropped) {
      const tail =
        this.request.format === "text"
          ? this.text.decode(undefined, true)
          : new Uint8Array();
      await this.commit(tail, {
        type: "gap",
        from: this.cursor,
        to: output.start,
        receivedAt: new Date().toISOString(),
      });
      this.text = new RecordingText();
      this.status.gaps++;
    }
    const data =
      this.request.format === "raw"
        ? output.data
        : this.text.decode(output.data);
    await this.commit(data, {
      type: "output",
      from: output.start,
      to: output.next,
      receivedAt: new Date().toISOString(),
    });
    this.cursor = output.next;
  }

  async finish(state: "stopped" | "failed"): Promise<void> {
    if (this.closed) return;
    this.closed = true;
    try {
      if (!this.opened) return;
      if (state === "stopped" && this.request.format === "text") {
        const tail = this.text.decode(undefined, true);
        if (tail.length)
          await this.commit(tail, {
            type: "decoder-end",
            from: this.cursor,
            to: this.cursor,
          });
      }
      await this.closeSegment();
      await this.manifest(state);
    } finally {
      await this.closeSegment();
    }
  }
}

type Recording = {
  status: RecordingStatus;
  controller: AbortController;
  starting?: Promise<void>;
  done?: Promise<void>;
};

/** Window-owned capture: one read and disk write at a time, at most four active/16 retained records. */
export class DesktopRecordings {
  private records = new Map<string, Recording>();
  private selecting = false;
  get choosing(): boolean {
    return this.selecting;
  }
  get active(): boolean {
    return [...this.records.values()].some(
      (record) => record.starting || record.done,
    );
  }

  constructor(
    private readonly createWriter: (
      status: RecordingStatus,
      request: RecordingRequest,
      start: number,
    ) => Pick<RecordingWriter, "start" | "append" | "finish"> = (
      status,
      request,
      start,
    ) => new RecordingWriter(status, request, start),
  ) {}

  list(): RecordingStatus[] {
    return [...this.records.values()].map((record) => ({ ...record.status }));
  }

  async start(
    host: Pick<HostClient, "validateAttachment" | "captureOutput">,
    request: RecordingRequest,
    choose: () => Promise<string | undefined>,
  ): Promise<void> {
    host.validateAttachment(request.attachmentId);
    if (
      this.selecting ||
      [...this.records.values()].filter((record) => record.done).length >= 4
    )
      throw new HostError("busy", "Stop a recording before starting another.");
    this.selecting = true;
    const controller = new AbortController();
    const id = randomUUID();
    const status: RecordingStatus = {
      id,
      sessionId: "",
      directory: "",
      format: request.format,
      state: "starting",
      bytes: 0,
      gaps: 0,
      rotations: 0,
    };
    const record: Recording = { status, controller };
    let started = () => {};
    record.starting = new Promise<void>((resolve) => {
      started = resolve;
    });
    this.records.set(id, record);
    let capture: OutputCapture | undefined;
    let writer:
      Pick<RecordingWriter, "start" | "append" | "finish"> | undefined;
    let phase: "source" | "disk" = "source";
    try {
      const parent = await choose();
      controller.signal.throwIfAborted();
      host.validateAttachment(request.attachmentId);
      if (!parent) {
        this.records.delete(id);
        return;
      }
      capture = await host.captureOutput(
        request.attachmentId,
        controller.signal,
      );
      controller.signal.throwIfAborted();
      status.sessionId = capture.sessionId;
      status.directory = join(parent, `channelterm-log-${id}`);
      phase = "disk";
      writer = this.createWriter(status, request, capture.start);
      await writer.start();
      phase = "source";
      controller.signal.throwIfAborted();
      status.state = "recording";
      record.done = this.run(record, capture, writer);
      capture = undefined;
      writer = undefined;
    } catch (error) {
      status.state =
        controller.signal.aborted && phase === "source" ? "stopped" : "failed";
      if (status.state === "failed") status.error = phase;
      await writer?.finish(status.state).catch(() => {
        status.state = "failed";
        status.error = "disk";
      });
      await capture?.close();
      if (controller.signal.aborted)
        throw new HostError("cancelled", "Recording start cancelled.");
      if (error instanceof HostError) throw error;
      throw new HostError(
        "tool_error",
        "Recording could not start. Check the selected destination and Host.",
      );
    } finally {
      started();
      record.starting = undefined;
      this.selecting = false;
      for (const [key, value] of this.records) {
        if (this.records.size <= 16) break;
        if (!value.done && value.status.state !== "starting")
          this.records.delete(key);
      }
    }
  }

  private async run(
    record: Recording,
    capture: OutputCapture,
    writer: Pick<RecordingWriter, "start" | "append" | "finish">,
  ): Promise<void> {
    let error: "source" | "disk" = "source";
    let outcome: "stopped" | "failed" = "stopped";
    try {
      while (!record.controller.signal.aborted) {
        error = "source";
        const output = await capture.read();
        if (record.controller.signal.aborted) break;
        if (!output || (!output.data.length && !output.dropped)) continue;
        error = "disk";
        await writer.append(output);
      }
    } catch {
      outcome =
        error === "source" && record.controller.signal.aborted
          ? "stopped"
          : "failed";
      if (outcome === "failed") record.status.error = error;
    } finally {
      record.status.state = "stopping";
      await writer.finish(outcome).catch(() => {
        outcome = "failed";
        record.status.error = "disk";
      });
      await capture.close().catch(() => {
        outcome = "failed";
        record.status.error ??= "source";
      });
      record.status.state = outcome;
      record.done = undefined;
    }
  }

  async stop(id: string): Promise<void> {
    const record = this.records.get(id);
    if (!record)
      throw new HostError(
        "invalid_input",
        "Recording not found in this window.",
      );
    if (record.done || record.status.state === "starting") {
      record.status.state = "stopping";
      record.controller.abort();
    }
    await record.starting;
    await record.done;
  }

  async stopAll(): Promise<void> {
    await Promise.all([...this.records.keys()].map((id) => this.stop(id)));
  }
}
