import { useEffect, useRef, useState } from "react";
import {
  presetEndpoint,
  connectionForSession,
  connectionHistoryKey,
  connectionHighlightsChanged,
  editConnection,
  isFolderName,
  readDirectoryFolders,
  maxConnectionHistoryLength,
  restoreConnections,
  type DirectoryEdit,
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
import type { PopupEntry } from "../components/WorkbenchMenu";
import {
  DirectoryEditDialog,
  directoryLabels,
  type DirectoryEditor,
} from "./DirectoryEditDialog";
import { ProtocolFilter } from "./ProtocolFilter";
import type { ConnectionRequest } from "../../shared/connections";

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
  folderScope,
  rows,
  localSessionIds,
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
  folderScope?: string;
  rows: OpenTabRow[];
  localSessionIds: ReadonlyMap<string, string>;
  history: ConnectionRecord[];
  sessions: SessionSummary[];
  selected?: string;
  onSelect: (id: string) => void;
  onAttach: (s: SessionSummary) => void;
  policies?: SavedPolicyTarget[];
  onSavedProtection: (p: ConnectionRecord, enabled: boolean) => void;
  onSettings: (
    s?: SessionSummary,
    p?: ConnectionRecord,
    rename?: boolean,
  ) => void;
  onOpen: (p: ConnectionRecord) => void;
  onProtect: (s: SessionSummary) => void;
  onContext: (
    event: React.MouseEvent<HTMLElement>,
    s: SessionSummary | undefined,
    entries: PopupEntry[],
  ) => void;
}) {
  const [others, setOthers] = useState<OpenTabRow[]>([]);
  const [error, setError] = useState("");
  const filter = useDirectoryFilter();
  const storageKey = "channelterm.directory-order.v1:" + orderScope;
  const folderKey =
    folderScope && "channelterm.directory-folders.v1:" + folderScope;
  const [createdFolders, setCreatedFolders] = useState<string[]>([]);
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
  useEffect(() => {
    function loadFolders() {
      try {
        setCreatedFolders(
          folderKey
            ? readDirectoryFolders(localStorage.getItem(folderKey))
            : [],
        );
      } catch {
        setCreatedFolders([]);
        setError("workspace.storageError");
      }
    }
    function changed(e: StorageEvent) {
      if (e.key === folderKey || e.key === null) loadFolders();
    }
    loadFolders();
    window.addEventListener("storage", changed);
    return () => window.removeEventListener("storage", changed);
  }, [folderKey]);
  const [expanded, setExpanded] = useState<Set<string>>(new Set());
  const [protocol, setProtocol] = useState<ConnectionRequest["kind"]>();
  const [editor, setEditor] = useState<DirectoryEditor>();
  async function createFolder(name: string) {
    if (!folderKey || !isFolderName(name) || folders.includes(name))
      return false;
    try {
      // Windows share local storage. Serialize the read and insert so simultaneous creates survive.
      return await navigator.locks.request(folderKey, () => {
        const names = readDirectoryFolders(localStorage.getItem(folderKey));
        if (names.includes(name) || names.length >= 256) return false;
        const next = [...names, name];
        localStorage.setItem(folderKey, JSON.stringify(next));
        setCreatedFolders(next);
        if (!filter.matchesText(name)) filter.setQuery("");
        filter.setStatus("all");
        setProtocol(undefined);
        return true;
      });
    } catch {
      setError("workspace.storageError");
      return false;
    }
  }
  function edit(
    profile: ConnectionRecord,
    action: DirectoryEdit,
    value?: string,
  ) {
    try {
      const entries = restoreConnections(
        localStorage.getItem(connectionHistoryKey),
      );
      const next = editConnection(
        entries,
        profile,
        action,
        value,
        action === "copy" ? crypto.randomUUID() : undefined,
      );
      if (!next) return false;
      const raw = JSON.stringify(next);
      if (raw.length > maxConnectionHistoryLength) return false;
      localStorage.setItem(connectionHistoryKey, raw);
      window.dispatchEvent(new Event(connectionHighlightsChanged));
      return true;
    } catch {
      setError("workspace.storageError");
      return false;
    }
  }
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
  const profilesByEndpoint = new Map<string, ConnectionRecord[]>();
  for (const profile of history) {
    const key = JSON.stringify([
      profile.request.kind,
      presetEndpoint(profile.request),
    ]);
    const records = profilesByEndpoint.get(key) ?? [];
    records.push(profile);
    profilesByEndpoint.set(key, records);
  }
  function profileKey(profile: ConnectionRecord, endpointKey: string) {
    return profilesByEndpoint.get(endpointKey)?.length === 1
      ? endpointKey
      : JSON.stringify([endpointKey, profile.id ?? profile.request]);
  }
  function matchingGroup(transport: string, endpoint: string, label: string) {
    const key = JSON.stringify([transport, endpoint]);
    const records = profilesByEndpoint.get(key);
    const match =
      records && connectionForSession(records, { transport, endpoint, label });
    if (match) return groups.get(profileKey(match, key));
    return groups.get(key);
  }
  function group(
    transport: string,
    endpoint: string,
    label: string,
    key = JSON.stringify([transport, endpoint]),
  ) {
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
    const endpoint = presetEndpoint(p.request);
    const key = JSON.stringify([p.request.kind, endpoint]);
    const g = group(
      p.request.kind,
      endpoint,
      p.request.label,
      profileKey(p, key),
    );
    g.profile = p;
  }
  for (const tab of [...rows, ...others]) {
    if (!tab.endpoint || !tab.transport) continue;
    const g =
      matchingGroup(tab.transport, tab.endpoint, tab.connectionLabel) ??
      group(tab.transport, tab.endpoint, tab.connectionLabel);
    if (!g.tabs.some((r) => r.id === tab.id)) g.tabs.push(tab);
    if (tab.connectionLabel) g.label = tab.connectionLabel;
  }
  for (const s of sessions) {
    if (s.state !== "open") continue;
    const belongsToSession = (tab: OpenTabRow) =>
      localSessionIds.get(tab.id) === s.session_id ||
      (!!s.session_ref && tab.reference === s.session_ref);
    const shared =
      [...groups.values()].find(
        (candidate) =>
          candidate.transport === s.transport &&
          candidate.endpoint === s.endpoint &&
          candidate.tabs.some(belongsToSession),
      ) ??
      matchingGroup(s.transport, s.endpoint, s.label) ??
      group(s.transport, s.endpoint, s.label);
    if (!shared.live) {
      shared.live = s;
      continue;
    }
    // Independent SSH shells may share both the endpoint and saved preset.
    const separate = group(
      s.transport,
      s.endpoint,
      s.label,
      JSON.stringify([s.transport, s.endpoint, s.session_id]),
    );
    separate.live = s;
    separate.tabs = shared.tabs.filter(belongsToSession);
    shared.tabs = shared.tabs.filter((tab) => !belongsToSession(tab));
  }
  const keys = orderedKeys([...groups.keys()], order);
  const protocols = (["serial", "ssh"] as const).filter((kind) =>
    [...groups.values()].some((g) => g.transport === kind),
  );
  const assignedFolders = new Set(
    history.flatMap((p) => (p.folder ? [p.folder] : [])),
  );
  const folders = [...new Set([...assignedFolders, ...createdFolders])].sort(
    (a, b) => a.localeCompare(b),
  );
  const shown = keys.filter((key) => {
    const g = groups.get(key)!;
    return (
      (!protocol || protocol === g.transport) &&
      filter.matchesStatus(!!g.live) &&
      filter.matchesText(
        g.label,
        g.endpoint,
        g.profile?.folder ?? "",
        g.live?.session_ref ?? "",
        ...g.tabs.flatMap((r) => [r.label, r.reference]),
      )
    );
  });
  function move(source: string, target: string, siblings: string[]) {
    const next = moveDirectoryRow(keys, siblings, source, target);
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
  function renderRow(key: string, siblings: string[]) {
    const index = siblings.indexOf(key);
    const g = groups.get(key)!;
    const { live, profile, tabs } = g;
    const protectedEntry =
      live?.control?.manual_only ??
      policies?.some(
        (p) =>
          p.transport === profile?.request.kind && p.endpoint === g.endpoint,
      );
    const open = expanded.has(key);
    function toggleProtection() {
      if (live) onProtect(live);
      else if (profile) onSavedProtection(profile, !protectedEntry);
    }
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
    function showMenu(event: React.MouseEvent<HTMLElement>) {
      const entries: PopupEntry[] = [];
      const add = (id: string, onChoose: () => void) =>
        entries.push({ id, label: id, onChoose });
      if (tabs.length || (!live && profile))
        add(tabs.length ? "directory.focus" : "connection.open", activate);
      if (profile || live) {
        add("directory.rename", () => onSettings(live, profile, true));
        add("connection.settings", () => onSettings(live, profile));
      }
      if (profile) {
        for (const action of ["copy", "folder"] as const)
          add(directoryLabels[action], () => setEditor({ action, profile }));
        add(
          profile.favorite ? "history.unfavorite" : "directory.favorite",
          () => {
            if (!edit(profile, "favorite")) setError("directory.editFailed");
          },
        );
      }
      if (profile || live)
        entries.push({
          id: "control.protect",
          label: "control.protect",
          checked: !!protectedEntry,
          enabled: live ? !!live.control : !!policies,
          onChoose: toggleProtection,
        });
      if (profile)
        add("history.remove", () => setEditor({ action: "forget", profile }));
      onContext(event, live, entries);
    }
    return (
      <div className="connection-card" key={key} onContextMenu={showMenu}>
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
          draggable={siblings.length > 1}
          aria-keyshortcuts="Alt+ArrowUp Alt+ArrowDown"
          title={
            g.endpoint +
            (siblings.length > 1 ? "\n" + t("directory.reorder") : "")
          }
          data-drop={
            dropMark === key
              ? siblings.indexOf(dragging.current) < index
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
            if (
              !siblings.includes(dragging.current) ||
              dragging.current === key
            )
              return;
            e.preventDefault();
            e.dataTransfer.dropEffect = "move";
            setDropMark(key);
          }}
          onDragLeave={() => setDropMark("")}
          onDrop={(e) => {
            if (!dragging.current) return;
            e.preventDefault();
            e.stopPropagation();
            move(dragging.current, key, siblings);
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
            const target = siblings[index + (e.key === "ArrowUp" ? -1 : 1)];
            if (target) move(key, target, siblings);
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
              aria-label={t("directory.actions")}
              aria-haspopup="menu"
              onClick={showMenu}
            >
              ⋯
            </button>
            <label
              className="directory-protection"
              title={t(protectedEntry ? "control.manual" : "control.protect")}
            >
              <input
                type="checkbox"
                aria-label={t("control.protect")}
                checked={!!protectedEntry}
                disabled={live ? !live.control : !policies}
                onChange={toggleProtection}
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
  }
  const unfiled = shown.filter((key) => !groups.get(key)?.profile?.folder);
  const showEmptyFolder = (folder: string) =>
    !assignedFolders.has(folder) &&
    !protocol &&
    filter.status === "all" &&
    filter.matchesText(folder);
  return (
    <nav
      className="terminal-directory sessions-directory"
      aria-label={t("workbench.sessions")}
      tabIndex={0}
      onContextMenu={(event) => {
        if (
          !(event.target instanceof Element) ||
          (event.target !== event.currentTarget &&
            !event.target.classList.contains("empty-list"))
        )
          return;
        onContext(event, undefined, [
          {
            id: "directory.newSession",
            label: "directory.newSession",
            action: { id: "connection.new" },
          },
          {
            id: "directory.newFolder",
            label: "directory.newFolder",
            enabled: !!folderKey,
            onChoose: () => setEditor({ action: "newFolder" }),
          },
        ]);
      }}
    >
      <h2>{t("workbench.sessions")}</h2>
      <DirectoryFilter t={t} filter={filter} />
      <ProtocolFilter
        t={t}
        history={history}
        protocols={protocols}
        selected={protocol}
        onSelect={setProtocol}
      />
      {!shown.length && !folders.some(showEmptyFolder) && (
        <p className="empty-list">
          {t(
            filter.active || protocol ? "workbench.noMatch" : "directory.empty",
          )}
        </p>
      )}
      {folders.map((folder) => {
        const members = shown.filter(
          (key) => groups.get(key)?.profile?.folder === folder,
        );
        return (
          (members.length > 0 || showEmptyFolder(folder)) && (
            <details open className="directory-folder" key={folder}>
              <summary>
                <Icon name="folder" /> {folder}{" "}
                <small>({members.length})</small>
              </summary>
              {members.map((key) => renderRow(key, members))}
            </details>
          )
        );
      })}
      {unfiled.map((key) => renderRow(key, unfiled))}
      {editor && (
        <DirectoryEditDialog
          {...editor}
          t={t}
          folders={folders}
          onClose={() => setEditor(undefined)}
          onSave={(value) =>
            editor.action === "newFolder"
              ? createFolder(value)
              : edit(editor.profile, editor.action, value)
          }
        />
      )}
      {error && <p role="alert">{t(error)}</p>}
    </nav>
  );
}
