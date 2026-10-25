import { useEffect, useRef, useState } from "react";
import { SearchAddon } from "@xterm/addon-search";
import type { Terminal } from "@xterm/xterm";

const limit = 1000;
const decorations = {
  matchBackground: "#665300",
  matchBorder: "#b59b35",
  matchOverviewRuler: "#b59b35",
  activeMatchBackground: "#694b16",
  activeMatchBorder: "#ffe680",
  activeMatchColorOverviewRuler: "#ffe680",
};

/** Attachment-local literal search; only xterm's retained text is inspected. */
export function TerminalSearch({
  terminal,
  focusVersion,
  hex,
  t,
  onClose,
}: {
  terminal: Terminal;
  focusVersion: number;
  hex: boolean;
  t: (id: string) => string;
  onClose: () => void;
}) {
  const input = useRef<HTMLInputElement>(null);
  const addon = useRef<SearchAddon | undefined>(undefined);
  const [query, setQuery] = useState("");
  const [caseSensitive, setCaseSensitive] = useState(false);
  const [composing, setComposing] = useState(false);
  const [result, setResult] = useState({ resultIndex: -1, resultCount: 0 });
  useEffect(() => {
    const search = new SearchAddon({ highlightLimit: limit });
    terminal.loadAddon(search);
    addon.current = search;
    const listener = search.onDidChangeResults((next) =>
      setResult((old) =>
        old.resultIndex === next.resultIndex &&
        old.resultCount === next.resultCount
          ? old
          : next,
      ),
    );
    return () => {
      listener.dispose();
      search.clearDecorations();
      terminal.clearSelection();
      search.dispose();
      addon.current = undefined;
    };
  }, [terminal]);
  useEffect(() => {
    input.current?.focus();
    input.current?.select();
  }, [focusVersion]);
  useEffect(() => {
    if (composing) return;
    const timer = setTimeout(() => {
      // The pinned addon caches highlighting by term; invalidate it when options
      // change too so case sensitivity cannot retain a stale count or markers.
      addon.current?.clearDecorations();
      addon.current?.findNext(query, {
        caseSensitive,
        regex: false,
        incremental: true,
        decorations,
      });
    }, 100);
    return () => clearTimeout(timer);
  }, [query, caseSensitive, composing]);
  function navigate(previous: boolean) {
    if (composing || !query) return;
    const options = { caseSensitive, regex: false, decorations };
    if (previous) addon.current?.findPrevious(query, options);
    else addon.current?.findNext(query, options);
  }
  const capped = result.resultCount >= limit;
  return (
    <section
      className="terminal-search"
      role="search"
      aria-label={t("terminal.find")}
      onKeyDown={(event) => {
        if (
          event.nativeEvent.isComposing ||
          event.nativeEvent.keyCode === 229 ||
          composing
        )
          return;
        if (event.key === "Escape") {
          event.preventDefault();
          event.stopPropagation();
          onClose();
        } else if (event.key === "Enter" && event.target === input.current) {
          event.preventDefault();
          event.stopPropagation();
          navigate(event.shiftKey);
        }
      }}
    >
      <input
        ref={input}
        type="search"
        maxLength={256}
        value={query}
        aria-label={t("search.query")}
        onChange={(event) => setQuery(event.target.value)}
        onCompositionStart={() => setComposing(true)}
        onCompositionEnd={() => setComposing(false)}
      />
      <label>
        <input
          type="checkbox"
          checked={caseSensitive}
          onChange={(event) => setCaseSensitive(event.target.checked)}
        />
        {t("search.case")}
      </label>
      <button
        type="button"
        disabled={!query || composing}
        aria-label={t("search.previous")}
        title={t("search.previous")}
        onClick={() => navigate(true)}
      >
        ↑
      </button>
      <button
        type="button"
        disabled={!query || composing}
        aria-label={t("search.next")}
        title={t("search.next")}
        onClick={() => navigate(false)}
      >
        ↓
      </button>
      <button
        type="button"
        onClick={() => {
          addon.current?.clearActiveDecoration();
          terminal.selectAll();
          terminal.focus();
        }}
      >
        {t("search.selectAll")}
      </button>
      <button
        type="button"
        aria-label={t("search.close")}
        title={t("search.close")}
        onClick={onClose}
      >
        ×
      </button>
      <span role="status" className="search-result">
        {!query
          ? t(hex ? "search.hexScope" : "search.scope")
          : result.resultCount === 0
            ? t("search.none")
            : `${result.resultIndex < 0 ? "–" : result.resultIndex + 1}/${capped ? "≥" : ""}${result.resultCount}${capped ? ` · ${t("search.limit")}` : ""}`}
      </span>
    </section>
  );
}
