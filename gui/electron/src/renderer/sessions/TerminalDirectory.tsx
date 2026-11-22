import { useEffect, useRef, useState } from "react";
import {
  presetEndpoint,
  type ConnectionRecord,
} from "../../shared/connection-history";
import type {
  OpenTabRow,
  SessionSummary,
  SavedPolicyTarget,
} from "../../shared/api";
import { clientErrorMessageId } from "../../shared/host-recovery";
import { host, unwrap } from "../host";
import {
  DirectoryFilter,
  ConnectionLight,
  useDirectoryFilter,
} from "./DirectoryFilter";
import {
  orderedKeys,
  moveDirectoryRow,
  readDirectoryOrder,
} from "./directory-order";
import { Icon, TransportIcon } from "../components/Icon";

type Group = {
  transport: string;
  endpoint: string;
  label: string;
  profile?: ConnectionRecord;
  tabs: OpenTabRow[];
  live?: SessionSummary;
};
export function TerminalDirectory({
  t,
  visible,
  orderScope,
  rows,
  history,
  sessions,
  selected,
  onSelect,
  onAttach,
  policies,
  onSavedProtection,
  onSettings,
  onOpen,
  onProtect,
  onContext,
}: {
  t: (id: string) => string;
  visible: boolean;
  orderScope: string;
  rows: OpenTabRow[];
  history: ConnectionRecord[];
  sessions: SessionSummary[];
  selected?: string;
  onSelect: (id: string) => void;
  onAttach: (s: SessionSummary) => void;
  policies?: SavedPolicyTarget[];
  onSavedProtection: (p: ConnectionRecord, enabled: boolean) => void;
  onSettings: (s?: SessionSummary, p?: ConnectionRecord) => void;
  onOpen: (p: ConnectionRecord) => void;
  onProtect: (s: SessionSummary) => void;
  onContext: (event: React.MouseEvent<HTMLElement>, s: SessionSummary) => void;
}) {
  const [others, setOthers] = useState<OpenTabRow[]>([]);
  const [error, setError] = useState("");
  const filter = useDirectoryFilter();
  const storageKey = "channelterm.directory-order.v1:" + orderScope;
  const [order, setOrder] = useState<string[]>([]);
  const dragging = useRef("");
  const [dropMark, setDropMark] = useState("");
  useEffect(() => {
    function loadOrder() {
      try {
        setOrder(readDirectoryOrder(localStorage.getItem(storageKey)));
      } catch {
        setOrder([]);
        setError("workspace.storageError");
      }
    }
    function changed(e: StorageEvent) {
      if (e.key === storageKey || e.key === null) loadOrder();
    }
    loadOrder();
    window.addEventListener("storage", changed);
    return () => window.removeEventListener("storage", changed);
  }, [storageKey]);
  const [expanded, setExpanded] = useState<Set<string>>(new Set());
  const [menu, setMenu] = useState("");
  useEffect(() => {
    if (!visible) return;
    let active = true;
    const update = async () => {
      if (document.hidden) return;
      try {
        const all = await unwrap(host.openedTabs());
        if (active) setOthers(all);
      } catch (e) {
        if (active) setError(clientErrorMessageId(e));
      }
    };
    void update();
    const timer = setInterval(() => void update(), 1500);
    document.addEventListener("visibilitychange", update);
    return () => {
      active = false;
      clearInterval(timer);
      document.removeEventListener("visibilitychange", update);
    };
  }, [visible]);

  const groups = new Map<string, Group>();
  function group(transport: string, endpoint: string, label: string) {
    const key = JSON.stringify([transport, endpoint]);
    let value = groups.get(key);
    if (!value) {
      value = { transport, endpoint, label: label || endpoint, tabs: [] };
      groups.set(key, value);
    }
    return value;
  }
  // Favorites come first. Old records may share an endpoint but retain distinct form presets.
  for (const p of [...history].sort(
    (a, b) =>
      Number(b.favorite) - Number(a.favorite) || b.lastUsed - a.lastUsed,
  )) {
    const g = group(p.request.kind, presetEndpoint(p.request), p.request.label);
    g.profile ??= p;
  }
  for (const tab of [...rows, ...others]) {
    if (!tab.endpoint || !tab.transport) continue;
    const g = group(tab.transport, tab.endpoint, tab.connectionLabel);
    if (!g.tabs.some((r) => r.id === tab.id)) g.tabs.push(tab);
    if (tab.connectionLabel) g.label = tab.connectionLabel;
  }
  for (const s of sessions) {
    const g = groups.get(JSON.stringify([s.transport, s.endpoint]));
    if (g && s.state === "open") g.live ??= s;
  }
  const keys = orderedKeys([...groups.keys()], order);
  const shown = keys.filter((key) => {
    const g = groups.get(key)!;
    return (
      filter.matchesStatus(!!g.live) &&
      filter.matchesText(
        g.label,
        g.endpoint,
        g.live?.session_ref ?? "",
        ...g.tabs.flatMap((r) => [r.label, r.reference]),
      )
    );
  });
  function move(source: string, target: string) {
    const next = moveDirectoryRow(keys, shown, source, target);
    if (next === keys) return;
    setOrder(next);
    try {
      localStorage.setItem(storageKey, JSON.stringify(next));
    } catch {
      setError("workspace.storageError");
    }
  }
  function focus(tab: OpenTabRow) {
    if (rows.some((r) => r.id === tab.id)) onSelect(tab.id);
    else
      void unwrap(host.focusTab(tab.id)).catch((e) =>
        setError(clientErrorMessageId(e)),
      );
  }
  return (
    <nav className="terminal-directory" aria-label={t("workbench.sessions")}>
      <h2>{t("workbench.sessions")}</h2>
      <DirectoryFilter t={t} filter={filter} />
      {!shown.length && (
        <p className="empty-list">
          {t(filter.active ? "workbench.noMatch" : "directory.empty")}
        </p>
      )}
      {shown.map((key, index) => {
        const g = groups.get(key)!;
        const { live, profile, tabs } = g;
        const protectedEntry =
          live?.control?.manual_only ??
          policies?.some(
            (p) =>
              p.transport === profile?.request.kind &&
              p.endpoint === g.endpoint,
          );
        const open = expanded.has(key);
        function click() {
          if (tabs.length === 1) focus(tabs[0]!);
          else if (tabs.length > 1)
            setExpanded((old) => {
              const next = new Set(old);
              if (open) next.delete(key);
              else next.add(key);
              return next;
            });
        }
        function activate() {
          const tab = tabs.find((r) => r.id === selected) ?? tabs[0];
          if (tab) focus(tab);
          else if (live) onAttach(live);
          else if (profile) onOpen(profile);
        }
        return (
          <div
            className="connection-card"
            key={key}
            onContextMenu={(e) => {
              if (live) {
                onContext(e, live);
              } else {
                e.preventDefault();
                setMenu(key);
              }
            }}
          >
            <button
              type="button"
              className={
                "directory-row session" +
                (tabs.some((r) => r.id === selected) ? " selected" : "")
              }
              data-session-id={live?.session_id}
              aria-expanded={tabs.length > 1 ? open : undefined}
              aria-pressed={tabs.some((r) => r.id === selected)}
              onClick={(event) => {
                if (event.detail === 0) activate();
                else if (event.detail === 1) click();
              }}
              onDoubleClick={activate}
              draggable={shown.length > 1}
              aria-keyshortcuts="Alt+ArrowUp Alt+ArrowDown"
              title={
                g.endpoint +
                (shown.length > 1 ? "\n" + t("directory.reorder") : "")
              }
              data-drop={
                dropMark === key
                  ? shown.indexOf(dragging.current) < index
                    ? "after"
                    : "before"
                  : undefined
              }
              onDragStart={(e) => {
                dragging.current = key;
                e.dataTransfer.effectAllowed = "move";
                e.dataTransfer.setData(
                  "application/x-channelterm-directory-row",
                  "row",
                );
              }}
              onDragOver={(e) => {
                if (!dragging.current || dragging.current === key) return;
                e.preventDefault();
                e.dataTransfer.dropEffect = "move";
                setDropMark(key);
              }}
              onDragLeave={() => setDropMark("")}
              onDrop={(e) => {
                if (!dragging.current) return;
                e.preventDefault();
                e.stopPropagation();
                move(dragging.current, key);
                dragging.current = "";
                setDropMark("");
              }}
              onDragEnd={() => {
                dragging.current = "";
                setDropMark("");
              }}
              onKeyDown={(e) => {
                if (!e.altKey || (e.key !== "ArrowUp" && e.key !== "ArrowDown"))
                  return;
                e.preventDefault();
                const target = shown[index + (e.key === "ArrowUp" ? -1 : 1)];
                if (target) move(key, target);
              }}
            >
              <strong>
                <ConnectionLight online={!!live} t={t} />
                <TransportIcon transport={g.transport} t={t} />
                {profile?.favorite ? "★ " : ""}
                {g.label}
              </strong>
              <small>
                {[
                  g.endpoint,
                  tabs.length > 1
                    ? String(tabs.length) + (open ? " ▾" : " ▸")
                    : (tabs[0]?.reference ?? live?.session_ref),
                ]
                  .filter(Boolean)
                  .join(" · ")}
              </small>
            </button>
            {(live || profile) && (
              <>
                <button
                  className="quiet"
                  aria-label={t("connection.settings")}
                  onClick={() => setMenu(menu === key ? "" : key)}
                >
                  ⋯
                </button>
                {menu === key && (
                  <button
                    onClick={() => {
                      onSettings(live, profile);
                      setMenu("");
                    }}
                  >
                    {t("connection.settings")}
                  </button>
                )}
                <label
                  className="directory-protection"
                  title={t(
                    protectedEntry ? "control.manual" : "control.protect",
                  )}
                >
                  <input
                    type="checkbox"
                    aria-label={t("control.protect")}
                    checked={!!protectedEntry}
                    disabled={live ? !live.control : !policies}
                    onChange={() =>
                      live
                        ? onProtect(live)
                        : profile && onSavedProtection(profile, !protectedEntry)
                    }
                  />
                  <Icon name={protectedEntry ? "locked" : "unlocked"} />
                  {protectedEntry && <span>{t("control.manual")}</span>}
                </label>
              </>
            )}
            {tabs.length > 1 && open && (
              <div className="directory-tabs">
                {tabs.map((tab) => (
                  <button
                    className="directory-row"
                    key={tab.id}
                    onClick={() => focus(tab)}
                    aria-pressed={tab.id === selected}
                  >
                    <TransportIcon transport={tab.transport} t={t} />
                    {tab.label}
                    <small>{tab.reference}</small>
                  </button>
                ))}
              </div>
            )}
          </div>
        );
      })}
      {error && <p role="alert">{t(error)}</p>}
    </nav>
  );
}
