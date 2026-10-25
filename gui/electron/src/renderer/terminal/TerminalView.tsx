import {
  useEffect,
  useImperativeHandle,
  useRef,
  useState,
  type Ref,
  type MouseEvent,
} from "react";
import { Terminal } from "@xterm/xterm";
import { FitAddon } from "@xterm/addon-fit";
import type { Attachment, ClientError } from "../../shared/api";
import { host, unwrap } from "../host";
import { TerminalPresentation } from "./presentation";
import type { TerminalSettings } from "../../shared/terminal-settings";
import type { DisplaySettings } from "../../shared/display-settings";
import { PastePreview } from "./PastePreview";

/** Controls for the current view; paste uses the existing bounded, authorized input path. */
export interface TerminalControls {
  clear(): void;
  focus(): void;
  copy(): void;
  paste(): void;
}

/**
 * TerminalView owns xterm, local fitting, and bounded I/O for one client attachment.
 * Read one chunk after the previous render completes to avoid an output backlog.
 * Main owns attachment cleanup; disposing this view never closes a Host Session.
 */
export function TerminalView({
  attachment,
  targetName,
  t,
  settings,
  display,
  onZoom,
  writable,
  visible,
  onReady,
  onError,
  onDropped,
  ref,
  onContextMenu,
}: {
  attachment: Attachment;
  targetName: string;
  t: (id: string) => string;
  settings: TerminalSettings;
  display: DisplaySettings;
  onZoom: (delta: number) => void;
  writable: boolean;
  visible: boolean;
  onReady: () => void;
  onError: (error: ClientError) => void;
  onDropped: () => void;
  ref: Ref<TerminalControls>;
  onContextMenu: (event: MouseEvent<HTMLElement>) => void;
}) {
  const container = useRef<HTMLDivElement>(null);
  const local = useRef<Terminal | undefined>(undefined);
  const fitting = useRef<FitAddon | undefined>(undefined);
  const clipboardBusy = useRef(false);
  const inputEpoch = useRef(0);
  const pendingPaste = useRef(false);
  const [pasteDraft, setPasteDraft] = useState<{
    text: string;
    epoch: number;
    target: Terminal;
  }>();
  async function copy() {
    const text = local.current?.getSelection();
    if (!text || clipboardBusy.current) return;
    clipboardBusy.current = true;
    const target = local.current;
    try {
      await unwrap(host.writeClipboard(attachment.id, text));
    } catch (error) {
      if (local.current === target)
        callbacks.current.onError(error as ClientError);
    } finally {
      clipboardBusy.current = false;
    }
  }
  async function paste() {
    const target = local.current;
    if (
      !target ||
      !inputEnabled.current ||
      !visibility.current ||
      clipboardBusy.current ||
      document.querySelector("dialog[open]")
    )
      return;
    const epoch = inputEpoch.current;
    clipboardBusy.current = true;
    try {
      const text = await unwrap(host.readClipboard(attachment.id));
      // A clipboard reply can arrive after detach, lease acquisition or a view replacement.
      if (
        local.current === target &&
        inputEnabled.current &&
        visibility.current &&
        inputEpoch.current === epoch &&
        !document.querySelector("dialog[open]") &&
        text
      ) {
        if (/[\r\n]/.test(text)) {
          pendingPaste.current = true;
          setPasteDraft({ text, epoch, target });
        } else {
          target.paste(text);
          target.focus();
        }
      }
    } catch (error) {
      if (local.current === target)
        callbacks.current.onError(error as ClientError);
    } finally {
      clipboardBusy.current = false;
    }
  }
  useImperativeHandle(
    ref,
    () => ({
      // xterm.clear preserves the current line/parser and sends no remote reset.
      clear: () => local.current?.clear(),
      focus: () => local.current?.focus(),
      copy: () => void copy(),
      paste: () => void paste(),
    }),
    [attachment.id],
  );
  const inputEnabled = useRef(writable);
  const callbacks = useRef({ onError, onDropped, onZoom, onReady });
  const previousReadLoop = useRef<Promise<void>>(Promise.resolve());
  const visibility = useRef(visible);
  const resumeRead = useRef<(() => void) | undefined>(undefined);
  if (visibility.current && !visible) inputEpoch.current++;
  visibility.current = visible;
  // Use the latest guards/callbacks without recreating xterm on status updates.
  callbacks.current = { onError, onDropped, onZoom, onReady };
  const enabled = writable && settings.view !== "hex";
  if (inputEnabled.current && !enabled) inputEpoch.current++;
  inputEnabled.current = enabled;

  useEffect(() => {
    const presentation = new TerminalPresentation(settings);
    const terminal = new Terminal({
      cursorBlink: true,
      fontFamily: `${display.fontFamily}, monospace`,
      fontSize: display.fontSize,
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
    fitting.current = fit;
    terminal.loadAddon(fit);
    terminal.open(container.current!);
    if (visibility.current) {
      fit.fit();
      terminal.focus();
    }
    let stopped = false;
    // Historical device queries are not current requests to this attachment.
    // Intercept queries at the parser (not by filtering user keystrokes or raw output).
    // Live queries still reach xterm's standard handlers, including split sequences.
    let replay = true;
    const queryHandlers = [
      { final: "n" },
      { prefix: "?", final: "n" },
      { final: "c" },
      { prefix: ">", final: "c" },
    ].map((id) => terminal.parser.registerCsiHandler(id, () => replay));
    const element = container.current!;
    const nativePaste = (event: ClipboardEvent) => {
      // Capture before xterm's own paste listener so native paste shares Main validation and preview.
      event.preventDefault();
      event.stopImmediatePropagation();
      void paste();
    };
    element.addEventListener("paste", nativePaste, true);
    const wheel = (event: WheelEvent) => {
      if (!event.ctrlKey || event.altKey || event.metaKey || event.shiftKey)
        return;
      event.preventDefault();
      event.stopPropagation();
      if (event.deltaY) callbacks.current.onZoom(event.deltaY);
    };
    element.addEventListener("wheel", wheel, { passive: false, capture: true });
    let pendingBytes = 0;
    // A failed write invalidates queued payloads; recovery must not replay them.
    let inputVersion = 0;
    let writes = Promise.resolve();
    let finishRender: (() => void) | undefined;
    const resize = new ResizeObserver(() => {
      if (visibility.current) fit.fit();
    });
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
      let ready = false;
      try {
        while (!stopped) {
          // Hidden tabs retain their bounded xterm buffer and Host cursor without redrawing.
          // A read already in flight can finish; further reads wait until selection.
          if (!visibility.current)
            await new Promise<void>((resolve) => {
              resumeRead.current = resolve;
            });
          if (stopped) break;
          const output = await unwrap(host.read(attachment.id));
          if (stopped) break;
          replay = !!output.replay;
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
          if (!ready && !stopped) {
            ready = true;
            callbacks.current.onReady();
          }
        }
      } catch (error) {
        if (!stopped) callbacks.current.onError(error as ClientError);
      }
    })();
    return () => {
      stopped = true;
      resumeRead.current?.();
      resumeRead.current = undefined;
      finishRender?.();
      resize.disconnect();
      element.removeEventListener("wheel", wheel, true);
      element.removeEventListener("paste", nativePaste, true);
      queryHandlers.forEach((handler) => handler.dispose());
      listener.dispose();
      terminal.dispose();
      local.current = undefined;
      fitting.current = undefined;
      // Main owns detach/cancellation; React effect cleanup must not detach a
      // newly mounted view (including StrictMode effect replay).
    };
  }, [attachment.id]);

  useEffect(() => {
    if (!visible) return;
    fitting.current?.fit();
    resumeRead.current?.();
    resumeRead.current = undefined;
  }, [visible]);

  useEffect(() => {
    if (!local.current) return;
    local.current.options.fontSize = display.fontSize;
    local.current.options.fontFamily = `${display.fontFamily}, monospace`;
    if (visibility.current) fitting.current?.fit();
  }, [display]);

  return (
    <>
      <div
        ref={container}
        className="terminal-view"
        aria-label="Terminal output and input"
        onContextMenu={onContextMenu}
      />
      {pasteDraft && (
        <PastePreview
          text={pasteDraft.text}
          sessionId={attachment.sessionId}
          targetName={targetName}
          t={t}
          valid={
            inputEnabled.current &&
            inputEpoch.current === pasteDraft.epoch &&
            local.current === pasteDraft.target &&
            visible
          }
          onCancel={() => {
            pendingPaste.current = false;
            setPasteDraft(undefined);
            if (visibility.current) local.current?.focus();
          }}
          onConfirm={() => {
            // Read the live guard again: a lease, detach, tab change or replacement invalidates this draft permanently.
            if (
              !pendingPaste.current ||
              !inputEnabled.current ||
              inputEpoch.current !== pasteDraft.epoch ||
              local.current !== pasteDraft.target ||
              !visibility.current
            )
              return;
            pendingPaste.current = false;
            setPasteDraft(undefined);
            pasteDraft.target.paste(pasteDraft.text);
            pasteDraft.target.focus();
          }}
        />
      )}
    </>
  );
}
