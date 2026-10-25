import { open, unlink } from "node:fs/promises";
import { basename } from "node:path";
import type { DiagnosticRequest, DiagnosticSaved } from "../shared/diagnostics";
import type { HostClient } from "./host/client";
import { HostError } from "./host/errors";

/** One window-owned save, selected by native dialog; no renderer paths or overwrites. */
export class DesktopDiagnostics {
  private pending?: AbortController;

  cancel(): void {
    this.pending?.abort();
  }

  async save(
    host: Pick<HostClient, "downloadDiagnostic" | "validateAttachment">,
    request: DiagnosticRequest,
    choose: () => Promise<string | undefined>,
  ): Promise<DiagnosticSaved | undefined> {
    if (this.pending)
      throw new HostError("busy", "A diagnostic save is already pending.");
    if (request.kind === "snapshot")
      host.validateAttachment(request.attachmentId);
    const controller = new AbortController();
    this.pending = controller;
    const signal = AbortSignal.any([
      controller.signal,
      AbortSignal.timeout(60000),
    ]);
    let path: string | undefined,
      file: Awaited<ReturnType<typeof open>> | undefined,
      complete = false;
    try {
      path = await choose();
      signal.throwIfAborted();
      if (!path) return;
      if (request.kind === "snapshot")
        host.validateAttachment(request.attachmentId);
      // Exclusive creation rejects existing files and symlinks without replacing user data.
      file = await open(path, "wx", 0o600);
      const output = file;
      const meta = await host.downloadDiagnostic(
        request,
        signal,
        async (bytes) => {
          signal.throwIfAborted();
          try {
            await output.writeFile(bytes);
          } catch {
            throw new HostError(
              "invalid_input",
              "Could not write diagnostic ZIP. Check available space and destination permissions.",
            );
          }
        },
      );
      signal.throwIfAborted();
      await file.sync();
      await file.close();
      file = undefined;
      complete = true;
      return { ...meta, name: basename(path) };
    } catch (error) {
      if (signal.aborted)
        throw new HostError(
          "cancelled",
          "Diagnostic save cancelled or timed out.",
        );
      if (error instanceof HostError) throw error;
      throw new HostError(
        "invalid_input",
        "Could not save diagnostic ZIP. Choose a new file in a writable directory.",
      );
    } finally {
      try {
        if (file) {
          await file.close();
          if (path && !complete) await unlink(path);
        }
      } catch {
        throw new HostError(
          "tool_error",
          "The incomplete diagnostic file could not be removed. Check the selected destination.",
        );
      } finally {
        this.pending = undefined;
      }
    }
  }
}
