import {
  useEffect,
  useImperativeHandle,
  useRef,
  type Ref,
  type MouseEvent,
} from "react";
import { Terminal } from "@xterm/xterm";
import { FitAddon } from "@xterm/addon-fit";
import type { Attachment, ClientError } from "../../shared/api";
import { host, unwrap } from "../host";
import { TerminalPresentation } from "./presentation";
import type { TerminalSettings } from "../../shared/terminal-settings";

/** Local xterm controls only; neither method writes bytes or changes Host history. */
export interface TerminalControls {
  clear(): void;
  focus(): void;
}

/**
 * TerminalView owns xterm, local fitting, and bounded I/O for one client attachment.
 * Read one chunk after the previous render completes to avoid an output backlog.
 * Main owns attachment cleanup; disposing this view never closes a Host Session.
 */
export function TerminalView({
  attachment,
  settings,
  writable,
  onError,
  onDropped,
  ref,
  onContextMenu,
}: {
  attachment: Attachment;
  settings: TerminalSettings;
  writable: boolean;
  onError: (error: ClientError) => void;
  onDropped: () => void;
  ref: Ref<TerminalControls>;
  onContextMenu: (event: MouseEvent<HTMLElement>) => void;
}) {
  const container = useRef<HTMLDivElement>(null);
  const local = useRef<Terminal | undefined>(undefined);
  useImperativeHandle(
    ref,
    () => ({
      // xterm.clear preserves the current line/parser and sends no remote reset.
      clear: () => local.current?.clear(),
      focus: () => local.current?.focus(),
    }),
    [],
  );
  const inputEnabled = useRef(writable);
  const callbacks = useRef({ onError, onDropped });
  const previousReadLoop = useRef<Promise<void>>(Promise.resolve());
  // Use the latest guards/callbacks without recreating xterm on status updates.
  callbacks.current = { onError, onDropped };
  inputEnabled.current = writable && settings.view !== "hex";

  useEffect(() => {
    const presentation = new TerminalPresentation(settings);
    const terminal = new Terminal({
      cursorBlink: true,
      fontFamily: 'Consolas, "Liberation Mono", monospace',
      fontSize: 14,
      scrollback: 5000,
      // xterm does not inherit the shell palette. Keep its background aligned
      // with --editor-background in styles/app.css to avoid seams around cells.
      theme: {
        background: "#101318",
        foreground: "#d9e0ea",
        cursor: "#aeafad",
        cursorAccent: "#101318",
        selectionBackground: "#264f78",
        black: "#000000",
        red: "#cd3131",
        green: "#0dbc79",
        yellow: "#e5e510",
        blue: "#2472c8",
        magenta: "#bc3fbc",
        cyan: "#11a8cd",
        white: "#e5e5e5",
        brightBlack: "#666666",
        brightRed: "#f14c4c",
        brightGreen: "#23d18b",
        brightYellow: "#f5f543",
        brightBlue: "#3b8eea",
        brightMagenta: "#d670d6",
        brightCyan: "#29b8db",
        brightWhite: "#e5e5e5",
      },
      allowProposedApi: false,
    });
    const fit = new FitAddon();
    local.current = terminal;
    terminal.loadAddon(fit);
    terminal.open(container.current!);
    fit.fit();
    terminal.focus();
    let stopped = false;
    let pendingBytes = 0;
    // A failed write invalidates queued payloads; recovery must not replay them.
    let inputVersion = 0;
    let writes = Promise.resolve();
    let finishRender: (() => void) | undefined;
    const resize = new ResizeObserver(() => fit.fit());
    resize.observe(container.current!);
    const listener = terminal.onData((data) => {
      if (!inputEnabled.current || stopped) return;
      const bytes = new TextEncoder().encode(data).length;
      if (pendingBytes + bytes > 65536) {
        callbacks.current.onError({
          code: "invalid_input",
          message: "Input queue is full. Remaining input was discarded.",
        });
        return;
      }
      pendingBytes += bytes;
      const version = inputVersion;
      writes = writes
        .then(async () => {
          if (stopped || version !== inputVersion || !inputEnabled.current)
            return;
          await unwrap(host.write(attachment.id, data));
        })
        .catch((error: ClientError) => {
          inputVersion++;
          if (!stopped) callbacks.current.onError(error);
        })
        .finally(() => {
          pendingBytes -= bytes;
        });
    });
    const previous = previousReadLoop.current;
    previousReadLoop.current = (async () => {
      // Effect replay/refresh must finish the old bounded read before starting
      // another one for this attachment. Main cancels it on a real detach.
      await previous;
      try {
        while (!stopped) {
          const output = await unwrap(host.read(attachment.id));
          if (stopped) break;
          if (output.dropped) {
            // Do not join a partial escape/UTF-8 sequence across missing bytes.
            terminal.reset();
            presentation.reset();
            callbacks.current.onDropped();
          }
          if (output.data.length)
            await new Promise<void>((done) => {
              finishRender = done;
              terminal.write(
                presentation.output(new Uint8Array(output.data), output.text),
                done,
              );
            });
        }
      } catch (error) {
        if (!stopped) callbacks.current.onError(error as ClientError);
      }
    })();
    return () => {
      stopped = true;
      finishRender?.();
      resize.disconnect();
      listener.dispose();
      terminal.dispose();
      local.current = undefined;
      // Main owns detach/cancellation; React effect cleanup must not detach a
      // newly mounted view (including StrictMode effect replay).
    };
  }, [attachment.id]);

  return (
    <div
      ref={container}
      className="terminal-view"
      aria-label="Terminal output and input"
      onContextMenu={onContextMenu}
    />
  );
}
