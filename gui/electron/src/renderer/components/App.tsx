import { ActivityPanel } from "../sessions/ActivityPanel";
import { FileDialog } from "../transfers/FileDialog";
import { FileCloseNotice } from "../transfers/FileCloseNotice";
import { SSHFileDialog } from "../transfers/SSHFileDialog";
import { ResumeDialog } from "../transfers/ResumeDialog";
import { TransferPanel } from "../transfers/TransferPanel";
import type {
  TransferRequest,
  TransferStatus,
  SSHTransferRequest,
} from "../../shared/transfers";
import {
  useCallback,
  useEffect,
  useMemo,
  useRef,
  useState,
  type MouseEvent,
  type CSSProperties,
} from "react";
import { setupI18n } from "@lingui/core";
import type {
  Attachment,
  ClientError,
  HostInfo,
  SessionSummary,
} from "../../shared/api";
import { host, unwrap } from "../host";
import { TerminalTabs } from "../sessions/TerminalTabs";
import { SessionList } from "../sessions/SessionList";
import { CloseSessionDialog } from "../sessions/CloseSessionDialog";
import { TerminalView, type TerminalControls } from "../terminal/TerminalView";
import { Icon } from "./Icon";
import { WorkbenchMenu, type MenuPopupRequest } from "./WorkbenchMenu";
import {
  actionEnabled,
  isAction,
  sessionMenu,
  terminalMenu,
  tabMenu,
  systemMenuLocale,
  type Action,
  type LanguagePreference,
  type MenuState,
} from "../../shared/menus";
import { menuCatalogs } from "../../locales/catalogs";
import { ConnectionDialog } from "./ConnectionDialog";
import { SidebarResizer } from "./SidebarResizer";
import type { ConnectionRequest } from "../../shared/connections";
import {
  defaultTerminalSettings,
  type TerminalSettings,
} from "../../shared/terminal-settings";
import { TerminalSettingsDialog } from "./TerminalSettings";
import { ShortcutSettings } from "./ShortcutSettings";
import {
  defaultShortcuts,
  isShortcutBindings,
  shortcutActions,
  restoreShortcuts,
  shortcutFromEvent,
  type ShortcutBindings,
} from "../../shared/shortcuts";
import { BottomPanel } from "./BottomPanel";

import {
  defaultDisplaySettings,
  displayStorageKey,
  isDisplaySettings,
  restoreDisplaySettings,
  zoomFontSize,
  type DisplaySettings,
} from "../../shared/display-settings";
import { DisplaySettingsDialog } from "./DisplaySettings";

interface TerminalTab {
  id: string;
  attachment?: Attachment;
  settings: TerminalSettings;
  error?: ClientError;
  dropped: boolean;
  inputPaused: boolean;
  moving?: boolean;
  restored?: boolean;
  moveId?: string;
}

const shortcutStorageKey = "channelterm.layout-shortcuts.v1";

/**
 * App composes Host snapshots and local attachment controls into the workbench.
 * Main owns the client connection; the Host owns Sessions and write authorization.
 * Terminal bytes stay in TerminalView rather than passing through React state.
 */
export function App() {
  const [info, setInfo] = useState<HostInfo>();
  const [connected, setConnected] = useState(false);
  // Collapsing navigation changes available space, not the active attachment.
  const [sidebarVisible, setSidebarVisible] = useState(true);
  const [sidebarWidth, setSidebarWidth] = useState<number>();
  const [auxiliaryVisible, setAuxiliaryVisible] = useState(false);
  const [auxiliaryWidth, setAuxiliaryWidth] = useState(260);
  const [panelVisible, setPanelVisible] = useState(false);
  const [panelTab, setPanelTab] = useState<"transfers" | "status" | "activity">(
    "transfers",
  );
  const [shortcutsOpen, setShortcutsOpen] = useState(false);
  const mac = /Mac/i.test(navigator.platform);
  const [shortcuts, setShortcuts] = useState<ShortcutBindings>(() => {
    try {
      const stored: unknown = JSON.parse(
        localStorage.getItem(shortcutStorageKey) ?? "null",
      );
      return restoreShortcuts(stored, mac);
    } catch {
      /* Missing/unavailable storage uses platform defaults. */
    }
    return defaultShortcuts(mac);
  });
  const [displayOpen, setDisplayOpen] = useState(false);
  const [display, setDisplay] = useState<DisplaySettings>(() => {
    try {
      const stored: unknown = JSON.parse(
        localStorage.getItem(displayStorageKey) ?? "null",
      );
      return restoreDisplaySettings(stored);
    } catch {
      /* Unavailable storage uses display defaults. */
    }
    return defaultDisplaySettings;
  });
  const [displayError, setDisplayError] = useState(false);
  function saveDisplay(value: DisplaySettings): boolean {
    if (!isDisplaySettings(value)) return false;
    try {
      localStorage.setItem(displayStorageKey, JSON.stringify(value));
    } catch {
      return false;
    }
    setDisplay(value);
    setDisplayError(false);
    return true;
  }
  function zoomDisplay(delta: number) {
    const next = {
      ...display,
      fontSize: zoomFontSize(display.fontSize, delta),
    };
    if (next.fontSize === display.fontSize) return;
    if (!saveDisplay(next)) {
      setDisplay(next);
      setDisplayError(true);
    }
  }
  const [viewportWidth, setViewportWidth] = useState(window.innerWidth);
  const defaultSidebarWidth = viewportWidth <= 900 ? 210 : 260;
  const maximumSidebarWidth = Math.max(
    180,
    Math.min(
      480,
      sidebarVisible && auxiliaryVisible
        ? Math.floor(
            (viewportWidth - (viewportWidth <= 900 ? 40 : 48) - 320) / 2,
          )
        : viewportWidth - 400,
    ),
  );
  const visibleAuxiliaryWidth = Math.min(auxiliaryWidth, maximumSidebarWidth);
  const visibleSidebarWidth = Math.min(
    sidebarWidth ?? defaultSidebarWidth,
    maximumSidebarWidth,
  );
  useEffect(() => {
    const resize = () => setViewportWidth(window.innerWidth);
    window.addEventListener("resize", resize);
    return () => window.removeEventListener("resize", resize);
  }, []);
  const [operation, setOperation] = useState<
    "connecting" | "disconnecting" | "attaching" | "detaching" | "opening"
  >();
  const working = operation !== undefined;
  const [sessions, setSessions] = useState<SessionSummary[]>([]);
  const [tabs, setTabState] = useState<TerminalTab[]>([]);
  const tabsRef = useRef(tabs);
  // Update the ref synchronously so async completions cannot target a replaced tab.
  function setTabs(
    update: TerminalTab[] | ((tabs: TerminalTab[]) => TerminalTab[]),
  ) {
    const next =
      typeof update === "function" ? update(tabsRef.current) : update;
    tabsRef.current = next;
    setTabState(next);
  }
  const [selectedTabId, setSelectedTabId] = useState<string>();
  const selectedTabRef = useRef(selectedTabId);
  selectedTabRef.current = selectedTabId;
  const activeTab = tabs.find((tab) => tab.id === selectedTabId);
  const attachment = activeTab?.attachment;
  const terminalSettings = activeTab?.settings ?? defaultTerminalSettings;
  const dropped = activeTab?.dropped ?? false;
  const inputPaused = activeTab?.inputPaused ?? false;
  const preferences = useRef(new Map<string, TerminalSettings>());
  const [settingsTarget, setSettingsTarget] = useState<Attachment>();
  const [closeTarget, setCloseTarget] = useState<{
    attachment: Attachment;
    session: SessionSummary;
  }>();
  useEffect(() => {
    if (!connected) setCloseTarget(undefined);
  }, [connected]);
  const [connectionError, setError] = useState<ClientError>();
  const error = activeTab?.error ?? connectionError;
  const [windowCount, setWindowCount] = useState(1);
  const copyingTarget = useRef(false);
  const [copyPending, setCopyPending] = useState(false);
  const [copyResult, setCopyResult] = useState<{
    id: string;
    locale: string;
    message: string;
  }>();
  useEffect(() => {
    if (!copyResult) return;
    const timer = window.setTimeout(() => setCopyResult(undefined), 5000);
    return () => window.clearTimeout(timer);
  }, [copyResult]);
  useEffect(() => host.onWindowCount(setWindowCount), []);
  function updateTab(id: string | undefined, patch: Partial<TerminalTab>) {
    setTabs((tabs) =>
      tabs.map((tab) => (tab.id === id ? { ...tab, ...patch } : tab)),
    );
  }
  function setInputPaused(inputPaused: boolean) {
    updateTab(selectedTabRef.current, { inputPaused });
  }
  function removeTab(id: string) {
    const index = tabsRef.current.findIndex((tab) => tab.id === id);
    const next = tabsRef.current.filter((tab) => tab.id !== id);
    setTabs(next);
    if (selectedTabRef.current === id)
      setSelectedTabId(next[Math.min(index, next.length - 1)]?.id);
  }
  function newTab() {
    if (tabsRef.current.length >= 32) return;
    const tab: TerminalTab = {
      id: crypto.randomUUID(),
      settings: defaultTerminalSettings,
      dropped: false,
      inputPaused: false,
    };
    setTabs((tabs) => [...tabs, tab]);
    setSelectedTabId(tab.id);
    setSidebarVisible(true);
    setChoosingSession(true);
  }
  const [languagePreference, setLanguagePreference] =
    useState<LanguagePreference>("system");
  const [systemLocale, setSystemLocale] = useState(() =>
    systemMenuLocale(navigator.languages),
  );
  useEffect(() => {
    const changed = () =>
      setSystemLocale(systemMenuLocale(navigator.languages));
    window.addEventListener("languagechange", changed);
    return () => window.removeEventListener("languagechange", changed);
  }, []);
  const locale =
    languagePreference === "system" ? systemLocale : languagePreference;
  const i18n = useMemo(
    () => setupI18n({ locale, messages: menuCatalogs }),
    [locale],
  );
  const t = (id: string) => i18n._(id);
  const [helpOpen, setHelpOpen] = useState(false);
  const [fileCloseBlocked, setFileCloseBlocked] = useState(false);
  useEffect(
    () =>
      host.onFileCloseBlocked(() => {
        setPanelVisible(true);
        setPanelTab("transfers");
        setFileCloseBlocked(true);
      }),
    [],
  );
  const [connectionKind, setConnectionKind] =
    useState<ConnectionRequest["kind"]>();
  const [menuContext, setMenuContext] = useState<MenuPopupRequest>();
  const terminals = useRef(new Map<string, TerminalControls>());
  const acceptingMoves = useRef(new Set<string>());
  const sessionSidebar = useRef<HTMLElement>(null);
  const [choosingSession, setChoosingSession] = useState(false);
  useEffect(() => {
    if (!choosingSession) return;
    sessionSidebar.current
      ?.querySelector<HTMLInputElement>('input[type="search"]')
      ?.focus();
    setChoosingSession(false);
  }, [choosingSession]);
  const [fileDialog, setFileDialog] = useState<{
    sessionId: string;
    name: string;
    direction: "send" | "receive";
  }>();
  const [filePending, setFilePending] = useState(false);
  const [sshFileDialog, setSSHFileDialog] = useState<{
    sessionId: string;
    name: string;
    endpoint: string;
    direction: "send" | "receive";
  }>();
  const [resumeTarget, setResumeTarget] = useState<{
    sessionId: string;
    name: string;
  }>();
  const [fileError, setFileError] = useState("");
  const [transferJobs, setTransferJobs] = useState<TransferStatus[]>([]);
  const latestTransfer = useRef("");
  useEffect(() => {
    const latest = transferJobs.at(-1);
    const key = latest ? `${latest.sessionId}:${latest.id}` : "";
    if (key && key !== latestTransfer.current) {
      setPanelVisible(true);
      setPanelTab("transfers");
    }
    latestTransfer.current = key;
  }, [transferJobs]);
  const observedSessions = sessions
    .filter((entry) => entry.state === "open" && entry.transport === "serial")
    .slice(0, 16)
    .map((entry) => entry.session_id)
    .join(",");
  useEffect(() => {
    let stopped = false;
    let timer: ReturnType<typeof setTimeout>;
    if (!connected) {
      preferences.current.clear();
      setSettingsTarget(undefined);
      setFileDialog(undefined);
      setSSHFileDialog(undefined);
      setResumeTarget(undefined);
      setTransferJobs((previous) =>
        previous.map((job) =>
          job.released ? job : { ...job, state: "unknown" },
        ),
      );
      return;
    }
    async function poll() {
      const jobs: TransferStatus[] = [];
      let sshUnavailable = false;
      try {
        jobs.push(...(await unwrap(host.sshTransfers())));
      } catch {
        sshUnavailable = true;
      }
      if (stopped) return;
      const unavailable = new Set<string>();
      for (const id of observedSessions.split(",").filter(Boolean)) {
        try {
          jobs.push(...(await unwrap(host.transfers(id))));
        } catch {
          // Do not leave a stale progress snapshot looking authoritative.
          unavailable.add(id);
        }
        if (stopped) return;
      }
      setTransferJobs((previous) => {
        const retained = new Map(
          previous.map((job) => [job.sessionId + ":" + job.id, job]),
        );
        for (const job of jobs) retained.set(job.sessionId + ":" + job.id, job);
        return [...retained.values()]
          .slice(-64)
          .map((job) =>
            !job.released &&
            (job.protocol
              ? sshUnavailable
              : !observedSessions.split(",").includes(job.sessionId) ||
                unavailable.has(job.sessionId))
              ? { ...job, state: "unknown" }
              : job,
          );
      });
      timer = setTimeout(() => void poll(), 750);
    }
    void poll();
    return () => {
      stopped = true;
      clearTimeout(timer);
    };
  }, [connected, observedSessions]);
  function startTransfer(request: TransferRequest) {
    setPanelVisible(true);
    setPanelTab("transfers");
    setFilePending(true);
    setFileError("");
    void unwrap(host.startTransfer(request))
      .catch((error: ClientError) => setFileError(error.message))
      .finally(() => setFilePending(false));
  }
  function startSSHTransfer(request: SSHTransferRequest) {
    setPanelVisible(true);
    setPanelTab("transfers");
    setFilePending(true);
    setFileError("");
    void unwrap(host.startSSHTransfer(request))
      .catch((error: ClientError) => setFileError(error.message))
      .finally(() => setFilePending(false));
  }
  function resumeTransfer(sessionId: string, id: string) {
    setPanelVisible(true);
    setPanelTab("transfers");
    setResumeTarget(undefined);
    setFilePending(true);
    setFileError("");
    void unwrap(host.resumeTransfer(sessionId, id))
      .catch((error: ClientError) => setFileError(error.message))
      .finally(() => setFilePending(false));
  }
  // Synchronous exclusion covers two menu/toolbar clicks before React rerenders.
  const actionBusy = useRef(false);
  // Invalidate list snapshots and pending attachment results across Host changes.
  const generation = useRef(0);
  // Manual refresh and periodic polling share one outstanding list request.
  const refreshing = useRef(false);
  const initialConnection = useRef(false);
  const current = sessions.find(
    (entry) => entry.session_id === attachment?.sessionId,
  );
  const menuState = useMemo<MenuState>(
    () => ({
      locale,
      languagePreference,
      connected,
      sshAvailable: !!info?.sshAvailable,
      busy:
        working ||
        !!closeTarget ||
        !!activeTab?.moving ||
        !!fileDialog ||
        !!sshFileDialog ||
        filePending,
      sidebarVisible,
      auxiliaryVisible,
      panelVisible,
      shortcuts,
      attachment,
      tabId: selectedTabId,
      tabCount: tabs.length,
      windowCount,
      currentOpen: current?.state === "open",
      currentSerial: current?.transport === "serial",
      currentSSH: current?.transport === "ssh",
      inputPaused,
      hexView: terminalSettings.view === "hex",
      leased: !!current?.lease,
    }),
    [
      locale,
      languagePreference,
      connected,
      info?.sshAvailable,
      working,
      closeTarget,
      fileDialog,
      sshFileDialog,
      filePending,
      current?.transport,
      sidebarVisible,
      auxiliaryVisible,
      panelVisible,
      shortcuts,
      attachment,
      activeTab?.moving,
      selectedTabId,
      tabs.length,
      windowCount,
      current?.state,
      current?.lease,
      inputPaused,
      terminalSettings.view,
    ],
  );
  const dispatchRef = useRef<(action: Action) => void>(() => {});
  useEffect(() => {
    // Capture assigned chords before xterm; only an explicit paste sends input.
    // Dialogs, menus, ordinary editors and IME retain their own keyboard behavior.
    const keydown = (event: KeyboardEvent) => {
      const target = event.target;
      if (
        !(target instanceof Element) ||
        document.querySelector("dialog[open]")
      )
        return;
      const binding = shortcutFromEvent(event);
      const id = shortcutActions.find(
        (action) => binding && shortcuts[action] === binding,
      );
      const inFind =
        id === "terminal.find" && target.closest(".terminal-search");
      if (
        target.closest('[role="menu"], .app-menu') ||
        (target.closest('input, textarea, select, [contenteditable="true"]') &&
          !target.closest(".xterm") &&
          !inFind)
      )
        return;
      if (
        !id ||
        (id.startsWith("terminal.") && !target.closest(".xterm") && !inFind)
      )
        return;
      event.preventDefault();
      event.stopPropagation();
      if (!event.repeat) {
        if (
          id === "terminal.copy" ||
          id === "terminal.paste" ||
          id === "terminal.find"
        ) {
          if (attachment)
            dispatchRef.current({ id, attachment: { ...attachment } });
        } else dispatchRef.current({ id });
      }
    };
    window.addEventListener("keydown", keydown, true);
    return () => window.removeEventListener("keydown", keydown, true);
  }, [shortcuts, attachment]);
  useEffect(
    () => host.onMenuAction((action) => dispatchRef.current(action)),
    [],
  );
  useEffect(() => {
    // Only bounded display hints cross this channel; native menus cannot authorize I/O.
    void unwrap(host.syncMenu(menuState)).catch((error: ClientError) =>
      setError(error),
    );
  }, [menuState]);

  const report = useCallback(
    (failure: ClientError, tabId = selectedTabRef.current) => {
      if (failure.code === "cancelled") return;
      if (
        ["disconnected", "unauthorized", "incompatible"].includes(failure.code)
      ) {
        generation.current++;
        setError(failure);
        setConnected(false);
        setTabs([]);
        setSelectedTabId(undefined);
        setSessions([]);
        void host.disconnect();
      } else if (tabId && tabsRef.current.some((tab) => tab.id === tabId)) {
        updateTab(tabId, { error: failure, inputPaused: true });
      } else setError(failure);
    },
    [],
  );

  const refresh = useCallback(async () => {
    if (refreshing.current) return;
    refreshing.current = true;
    const version = generation.current;
    try {
      const result = await unwrap(host.list());
      if (version === generation.current) setSessions(result);
    } catch (error) {
      if (version === generation.current) report(error as ClientError);
    } finally {
      refreshing.current = false;
    }
  }, [report]);

  useEffect(() => {
    let active = true;
    void Promise.all([unwrap(host.info()), unwrap(host.windowInfo())])
      .then(async ([next, windowState]) => {
        if (!active || initialConnection.current) return;
        initialConnection.current = true;
        setInfo(next);
        setWindowCount(windowState.count);
        if (windowState.languagePreference)
          setLanguagePreference(windowState.languagePreference);
        if (windowState.connected) {
          setConnected(true);
          if (windowState.incoming) {
            const incoming = windowState.incoming;
            const id = crypto.randomUUID();
            setTabs([
              {
                id,
                attachment: incoming.attachment,
                settings: incoming.settings,
                inputPaused: incoming.inputPaused,
                dropped: false,
                restored: true,
                moveId: incoming.moveId,
              },
            ]);
            setSelectedTabId(id);
          }
          await refresh();
        } else if (next.autoConnect || windowState.autoConnect) {
          actionBusy.current = true;
          try {
            await connect();
          } finally {
            actionBusy.current = false;
          }
        }
      })
      .catch((error: ClientError) => {
        if (active) report(error);
      });
    return () => {
      active = false;
    };
  }, [report, refresh]);
  useEffect(() => {
    if (!connected) return;
    const timer = setInterval(() => {
      void refresh();
    }, 1500);
    return () => clearInterval(timer);
  }, [connected, refresh]);

  async function connect() {
    initialConnection.current = true;
    setOperation("connecting");
    setError(undefined);
    generation.current++;
    try {
      setInfo(await unwrap(host.connect()));
      setConnected(true);
      setInputPaused(false);
      await refresh();
    } catch (error) {
      report(error as ClientError);
    } finally {
      setOperation(undefined);
    }
  }

  async function disconnect() {
    preferences.current.clear();
    setSettingsTarget(undefined);
    setConnectionKind(undefined);
    setOperation("disconnecting");
    generation.current++;
    setTabs([]);
    setSelectedTabId(undefined);
    setConnected(false);
    setSessions([]);
    setError(undefined);
    try {
      await unwrap(host.disconnect());
    } catch (error) {
      report(error as ClientError);
    } finally {
      setOperation(undefined);
    }
  }

  async function select(
    session: SessionSummary,
    settings = preferences.current.get(session.session_id) ??
      defaultTerminalSettings,
    replaceTabId?: string,
  ) {
    const selected = tabsRef.current.find(
      (tab) => tab.id === selectedTabRef.current,
    );
    if (!replaceTabId && selected?.attachment) {
      const existing = tabsRef.current.find(
        (tab) => tab.attachment?.sessionId === session.session_id,
      );
      if (existing) {
        setSelectedTabId(existing.id);
        return;
      }
    }
    const target = replaceTabId
      ? tabsRef.current.find((tab) => tab.id === replaceTabId)
      : selected && !selected.attachment
        ? selected
        : undefined;
    if (replaceTabId && !target) return;
    if (!target && tabsRef.current.length >= 32) return;
    const tabId = target?.id ?? crypto.randomUUID();
    setOperation("attaching");
    setError(undefined);
    const version = generation.current;
    try {
      const next = await unwrap(host.attach(session.session_id, settings));
      const liveTarget = tabsRef.current.find((tab) => tab.id === target?.id);
      if (
        version !== generation.current ||
        (target &&
          (!liveTarget || liveTarget.attachment?.id !== target.attachment?.id))
      ) {
        await host.detach(next.id);
        return;
      }
      if (target?.attachment) await unwrap(host.detach(target.attachment.id));
      if (version !== generation.current) {
        await host.detach(next.id);
        return;
      }
      if (
        preferences.current.size >= 64 &&
        !preferences.current.has(session.session_id)
      )
        preferences.current.delete(preferences.current.keys().next().value!);
      preferences.current.set(session.session_id, settings);
      const tab: TerminalTab = {
        id: tabId,
        attachment: next,
        settings,
        inputPaused: false,
        dropped: false,
      };
      setTabs((tabs) =>
        target
          ? tabs.map((item) => (item.id === tabId ? tab : item))
          : [...tabs, tab],
      );
      setSelectedTabId(tabId);
    } catch (error) {
      report(error as ClientError, target?.id);
    } finally {
      setOperation(undefined);
    }
  }

  async function detach(tab: TerminalTab) {
    if (tab.moving) return;
    setOperation("detaching");
    setError(undefined);
    try {
      if (tab.attachment) await unwrap(host.detach(tab.attachment.id));
      removeTab(tab.id);
    } catch (error) {
      report(error as ClientError, tab.id);
    } finally {
      setOperation(undefined);
    }
  }

  async function move(tab: TerminalTab) {
    if (!tab.attachment || tab.moving) return;
    updateTab(tab.id, { moving: true });
    try {
      await unwrap(host.moveTab(tab.attachment.id, tab.inputPaused));
      removeTab(tab.id);
    } catch (error) {
      updateTab(tab.id, {
        error: { ...(error as ClientError), message: t("tab.moveFailed") },
      });
    } finally {
      updateTab(tab.id, { moving: false });
    }
  }

  async function acceptMove(tab: TerminalTab) {
    if (
      !tab.moveId ||
      !tab.attachment ||
      acceptingMoves.current.has(tab.moveId)
    )
      return;
    acceptingMoves.current.add(tab.moveId);
    try {
      await unwrap(host.acceptMovedTab(tab.moveId, tab.attachment.id));
      updateTab(tab.id, { moveId: undefined });
    } catch (error) {
      report(error as ClientError, tab.id);
    }
  }

  async function openConnection(
    request: ConnectionRequest,
    settings?: TerminalSettings,
  ) {
    if (!connected || working || actionBusy.current)
      throw {
        code: "busy",
        message: "Connect to an idle Host first.",
      } satisfies ClientError;
    actionBusy.current = true;
    setOperation("opening");
    const version = generation.current;
    try {
      const opened = await unwrap(host.openConnection(request));
      const entries = await unwrap(host.list());
      if (version !== generation.current)
        throw {
          code: "cancelled",
          message:
            "Host changed. Refresh Sessions to check the connection result.",
        } satisfies ClientError;
      setSessions(entries);
      const target = entries.find(
        (entry) => entry.session_id === opened.sessionId,
      );
      if (!target || target.state !== "open")
        throw {
          code: "closed",
          message:
            "The created Session is unavailable. Refresh Sessions before trying again.",
        } satisfies ClientError;
      await select(target, settings);
    } finally {
      actionBusy.current = false;
      setOperation(undefined);
    }
  }

  function tabForAction(action: Action): TerminalTab | undefined {
    if ("attachment" in action)
      return tabsRef.current.find(
        (tab) =>
          tab.attachment?.id === action.attachment.id &&
          tab.attachment.sessionId === action.attachment.sessionId,
      );
    if ("tabId" in action)
      return tabsRef.current.find((tab) => tab.id === action.tabId);
    return undefined;
  }

  function stateForTab(tab?: TerminalTab): MenuState {
    const session = sessions.find(
      (session) => session.session_id === tab?.attachment?.sessionId,
    );
    return {
      ...menuState,
      attachment: tab?.attachment,
      tabId: tab?.id,
      busy:
        working ||
        !!closeTarget ||
        actionBusy.current ||
        !!fileDialog ||
        !!sshFileDialog ||
        filePending ||
        !!tab?.moving,
      currentOpen: session?.state === "open",
      currentSerial: session?.transport === "serial",
      currentSSH: session?.transport === "ssh",
      leased: !!session?.lease,
      inputPaused: !!tab?.inputPaused,
      hexView: tab?.settings.view === "hex",
    };
  }

  function enabled(action: Action): boolean {
    const targeted = "attachment" in action || "tabId" in action;
    const tab = targeted ? tabForAction(action) : activeTab;
    if ((targeted && !tab) || !actionEnabled(action, stateForTab(tab)))
      return false;
    if (
      action.id === "file.send" ||
      action.id === "file.receive" ||
      action.id === "file.resume"
    )
      return sessions.some(
        (session) =>
          session.session_id === action.sessionId &&
          session.state === "open" &&
          (session.transport === "ssh"
            ? action.id !== "file.resume"
            : session.transport === "serial" && !session.lease),
      );
    return (
      action.id !== "session.attach" ||
      (attachment?.sessionId !== action.sessionId &&
        sessions.some(
          (session) =>
            session.session_id === action.sessionId && session.state === "open",
        ))
    );
  }

  async function execute(action: Action) {
    if (!isAction(action) || !enabled(action)) return;
    const targetTab = tabForAction(action);
    const terminal = targetTab
      ? terminals.current.get(targetTab.id)
      : undefined;
    switch (action.id) {
      case "session.copyAI": {
        if (copyingTarget.current) return;
        copyingTarget.current = true;
        setCopyPending(true);
        setCopyResult(undefined);
        try {
          await unwrap(host.copyAITarget(action.attachment.id, locale));
          setCopyResult({
            id: action.attachment.id,
            locale,
            message: "target.copied",
          });
        } catch {
          setCopyResult({
            id: action.attachment.id,
            locale,
            message: "target.failed",
          });
        } finally {
          copyingTarget.current = false;
          setCopyPending(false);
        }
        return;
      }
      case "session.close": {
        const session = sessions.find(
          (entry) => entry.session_id === action.attachment.sessionId,
        );
        if (session)
          setCloseTarget({
            attachment: { ...action.attachment },
            session: { ...session },
          });
        return;
      }
      case "window.new":
        await unwrap(host.newWindow()).catch(report);
        return;
      case "window.close":
        await unwrap(host.closeWindow()).catch(report);
        return;
      case "window.next":
      case "window.previous":
        await unwrap(
          host.focusWindow(action.id === "window.next" ? "next" : "previous"),
        ).catch(report);
        return;
      case "tab.new":
        newTab();
        return;
      case "tab.select":
        setSelectedTabId(action.tabId);
        return;
      case "tab.next":
      case "tab.previous": {
        const index = tabsRef.current.findIndex(
          (tab) => tab.id === selectedTabRef.current,
        );
        const next =
          (index +
            (action.id === "tab.next" ? 1 : -1) +
            tabsRef.current.length) %
          tabsRef.current.length;
        setSelectedTabId(tabsRef.current[next]?.id);
        return;
      }
      case "tab.move":
        if (targetTab) await move(targetTab);
        return;
      case "file.send":
      case "file.resume":
      case "file.receive": {
        const target = sessions.find(
          (entry) =>
            entry.session_id === action.sessionId &&
            entry.state === "open" &&
            (entry.transport === "ssh" ||
              (entry.transport === "serial" && !entry.lease)),
        );
        if (
          target &&
          action.id === "file.resume" &&
          target.transport === "serial"
        ) {
          setResumeTarget({
            sessionId: target.session_id,
            name: target.label || target.session_ref,
          });
          return;
        }
        if (target?.transport === "ssh" && action.id !== "file.resume") {
          setSSHFileDialog({
            sessionId: target.session_id,
            name: target.label || target.session_ref,
            endpoint: target.endpoint,
            direction: action.id === "file.send" ? "send" : "receive",
          });
          return;
        }
        if (target?.transport === "serial")
          setFileDialog({
            sessionId: target.session_id,
            name: target.label || target.session_ref,
            direction: action.id === "file.send" ? "send" : "receive",
          });
        return;
      }
      case "connection.new":
      case "connection.serial":
        setConnectionKind("serial");
        return;
      case "connection.ssh":
        setConnectionKind("ssh");
        return;
      case "session.choose":
        setSidebarVisible(true);
        // The existing Session picker owns attachment; reveal it without choosing a target.
        setChoosingSession(true);
        return;
      case "session.info":
        if (targetTab) setSelectedTabId(targetTab.id);
        setAuxiliaryVisible(true);
        return;
      case "view.sidebar":
        setSidebarVisible((visible) => !visible);
        return;
      case "view.auxiliary":
        setAuxiliaryVisible((visible) => !visible);
        return;
      case "view.panel":
        setPanelVisible((visible) => !visible);
        return;
      case "view.transfers":
      case "view.status":
      case "view.activity":
        setPanelVisible(true);
        setPanelTab(
          action.id === "view.transfers"
            ? "transfers"
            : action.id === "view.activity"
              ? "activity"
              : "status",
        );
        return;
      case "settings.display":
        setDisplayOpen(true);
        return;
      case "settings.shortcuts":
        setShortcutsOpen(true);
        return;
      case "language.change":
        setLanguagePreference(action.locale);
        return;
      case "help.quick":
        setHelpOpen(true);
        return;
      case "terminal.copy":
        terminal?.copy();
        return;
      case "terminal.find":
        terminal?.find();
        return;
      case "terminal.paste":
        terminal?.paste();
        return;
      case "terminal.clear":
        terminal?.clear();
        return;
      case "terminal.focus":
        terminal?.focus();
        return;
      case "terminal.resume":
        if (targetTab)
          updateTab(targetTab.id, { inputPaused: false, error: undefined });
        setError(undefined);
        terminal?.focus();
        return;
      case "app.quit":
        await unwrap(host.quit()).catch(report);
        return;
    }
    actionBusy.current = true;
    try {
      switch (action.id) {
        case "host.connect":
          await connect();
          break;
        case "host.disconnect":
          await disconnect();
          break;
        case "session.refresh":
          await refresh();
          break;
        case "session.attach": {
          const target = sessions.find(
            (entry) => entry.session_id === action.sessionId,
          );
          if (target?.state === "open") await select(target);
          break;
        }
        case "tab.close":
        case "tab.closeEmpty":
          if (targetTab) await detach(targetTab);
          break;
      }
    } finally {
      actionBusy.current = false;
    }
  }
  const dispatch = (action: Action) => {
    void execute(action);
  };
  dispatchRef.current = dispatch;

  function openContext(
    event: MouseEvent<HTMLElement>,
    entries: MenuPopupRequest["entries"],
  ) {
    event.preventDefault();
    const opener = event.currentTarget;
    const rect = opener.getBoundingClientRect();
    setMenuContext({
      entries,
      opener,
      x: event.clientX || rect.left,
      y: event.clientY || rect.bottom,
    });
  }

  const hostStatus =
    operation === "connecting"
      ? "Connecting to Host…"
      : operation === "disconnecting"
        ? "Disconnecting Host…"
        : connected
          ? "Host connected"
          : error
            ? "Host connection failed"
            : "Host disconnected";
  // Cached state is only an input guard; the Host still checks every write.
  const writable =
    connected &&
    !working &&
    current?.state === "open" &&
    !current.lease &&
    !inputPaused;
  // Distinguish pending/failed operations from a successfully loaded empty list
  // instead of using the same empty-state heading for every unattached view.
  const emptyTitle = working
    ? {
        connecting: "Connecting to Host…",
        disconnecting: "Disconnecting Host…",
        attaching: "Attaching Session…",
        detaching: "Detaching Session…",
        opening: "Opening connection…",
      }[operation]
    : !connected
      ? error
        ? "Host unavailable"
        : "No Host connected"
      : error
        ? "Session unavailable"
        : sessions.length === 0
          ? "No Sessions available"
          : "Select a Session";
  const emptyHint = working
    ? "Please wait."
    : !connected
      ? error
        ? "Check that your configured Host is running, then retry the connection."
        : "Connect to your running Host using the Host panel."
      : sessions.length === 0
        ? t("session.emptyHint")
        : "Choose an open Session from the sidebar to view output and send input.";

  return (
    <div className={`app ${sidebarVisible ? "" : "sidebar-hidden"}`}>
      {displayOpen && (
        <DisplaySettingsDialog
          value={display}
          t={t}
          onApply={saveDisplay}
          onClose={() => setDisplayOpen(false)}
        />
      )}
      {shortcutsOpen && (
        <ShortcutSettings
          value={shortcuts}
          mac={mac}
          t={t}
          onClose={() => setShortcutsOpen(false)}
          onApply={(bindings) => {
            if (!isShortcutBindings(bindings)) return false;
            try {
              localStorage.setItem(
                shortcutStorageKey,
                JSON.stringify(bindings),
              );
            } catch {
              return false;
            }
            setShortcuts(bindings);
            return true;
          }}
        />
      )}
      {fileCloseBlocked && (
        <FileCloseNotice t={t} onClose={() => setFileCloseBlocked(false)} />
      )}
      {resumeTarget && connected && (
        <ResumeDialog
          {...resumeTarget}
          t={t}
          onResume={resumeTransfer}
          onClose={() => setResumeTarget(undefined)}
        />
      )}
      {fileDialog && connected && (
        <FileDialog
          key={fileDialog.sessionId + fileDialog.direction}
          {...fileDialog}
          t={t}
          onStart={startTransfer}
          onClose={() => setFileDialog(undefined)}
        />
      )}
      {sshFileDialog && connected && (
        <SSHFileDialog
          {...sshFileDialog}
          t={t}
          onStart={startSSHTransfer}
          onClose={() => setSSHFileDialog(undefined)}
        />
      )}
      {connectionKind && connected && (
        <ConnectionDialog
          key={connectionKind}
          kind={connectionKind}
          sshAvailable={!!info?.sshAvailable}
          sshAdvanced={!!info?.sshAdvanced}
          t={t}
          onOpen={openConnection}
          onClose={() => setConnectionKind(undefined)}
        />
      )}
      {closeTarget && connected && (
        <CloseSessionDialog
          target={closeTarget.session}
          t={t}
          onClose={() => setCloseTarget(undefined)}
          onConfirm={async () => {
            const captured = closeTarget.attachment;
            const targetTab = tabsRef.current.find(
              (tab) =>
                tab.attachment?.id === captured.id &&
                tab.attachment.sessionId === captured.sessionId,
            );
            if (!targetTab || targetTab.moving || actionBusy.current)
              throw { code: "cancelled" };
            actionBusy.current = true;
            updateTab(targetTab.id, { inputPaused: true });
            try {
              await unwrap(host.closeSession(captured.id));
              await refresh();
            } finally {
              actionBusy.current = false;
            }
          }}
        />
      )}
      <WorkbenchMenu
        state={menuState}
        t={t}
        execute={dispatch}
        enabled={enabled}
        context={menuContext}
        onContextClose={() => setMenuContext(undefined)}
        helpOpen={helpOpen}
        onHelpClose={() => setHelpOpen(false)}
      />
      <div
        className="workspace"
        style={
          {
            "--sidebar-width": `${sidebarVisible ? visibleSidebarWidth : 0}px`,
            "--auxiliary-width": `${auxiliaryVisible ? visibleAuxiliaryWidth : 0}px`,
          } as CSSProperties
        }
      >
        <div className="activity-rail" aria-label="Workspace navigation">
          <button
            className="rail-button"
            aria-label="Toggle Session sidebar"
            title="Toggle Session sidebar"
            aria-expanded={sidebarVisible}
            aria-controls="session-sidebar"
            onClick={() => dispatch({ id: "view.sidebar" })}
          >
            <Icon name="terminal" />
          </button>
          <span
            className={`rail-host ${connected ? "online" : ""}`}
            title={hostStatus}
          >
            <span className="dot" />
          </span>
        </div>
        <aside
          ref={sessionSidebar}
          id="session-sidebar"
          className="sidebar"
          aria-label="Session sidebar"
          hidden={!sidebarVisible}
        >
          <div className="section-heading">
            <h2>
              Sessions <span className="session-count">{sessions.length}</span>
            </h2>
            <button
              className="quiet icon-button"
              aria-label="Refresh"
              title="Refresh Sessions"
              disabled={!connected || working}
              onClick={() => dispatch({ id: "session.refresh" })}
            >
              <Icon name="refresh" />
            </button>
          </div>
          <SessionList
            sessions={sessions}
            selected={attachment?.sessionId}
            disabled={!connected || working}
            emptyMessage={
              connected
                ? "No Sessions found."
                : "Connect a Host to list Sessions."
            }
            onSelect={(item) =>
              dispatch({ id: "session.attach", sessionId: item.session_id })
            }
            onContextMenu={(event, item) =>
              openContext(
                event,
                sessionMenu(
                  stateForTab(
                    tabs.find(
                      (tab) => tab.attachment?.sessionId === item.session_id,
                    ),
                  ),
                  item.session_id,
                  item.state === "open" &&
                    (item.transport === "ssh" || !item.lease),
                  item.transport === "serial",
                  item.transport === "ssh",
                ),
              )
            }
          />
          <section className="host-panel" aria-label="Host connection">
            <h2>Host connection</h2>
            <div className="host-connection">
              <div
                role="status"
                className={`connection ${connected ? "online" : error ? "failed" : ""}`}
              >
                <span className="dot" aria-hidden="true" />
                {hostStatus}
              </div>
              <code className="host-endpoint" title={info?.endpoint}>
                {info?.endpoint ?? "Loading configuration…"}
              </code>
            </div>
            <button
              className={connected ? "" : "primary"}
              disabled={working}
              onClick={() =>
                dispatch({ id: connected ? "host.disconnect" : "host.connect" })
              }
            >
              <Icon name="plug" />
              {operation === "connecting"
                ? "Connecting…"
                : operation === "disconnecting"
                  ? "Disconnecting…"
                  : connected
                    ? "Disconnect Host"
                    : error
                      ? "Retry connection"
                      : "Connect to Host"}
            </button>
            <p className="host-hint">Detaching leaves Sessions running.</p>
          </section>
          {sidebarVisible && (
            <SidebarResizer
              width={visibleSidebarWidth}
              maximum={maximumSidebarWidth}
              defaultWidth={defaultSidebarWidth}
              label={t("view.resizeSidebar")}
              hint={t("view.resizeSidebarHelp")}
              onResize={setSidebarWidth}
            />
          )}
        </aside>
        <main aria-label="Terminal workspace">
          <div className="terminal-toolbar">
            <TerminalTabs
              tabs={tabs.map((tab) => {
                const session = sessions.find(
                  (session) => session.session_id === tab.attachment?.sessionId,
                );
                return {
                  id: tab.id,
                  label:
                    session?.label || session?.session_ref || t("tab.empty"),
                  transport: session?.transport,
                  moving: tab.moving,
                };
              })}
              selected={selectedTabId}
              disabled={working}
              t={t}
              onNew={() => dispatch({ id: "tab.new" })}
              onSelect={(tabId) => dispatch({ id: "tab.select", tabId })}
              onClose={(tabId) => {
                const tab = tabs.find((tab) => tab.id === tabId);
                if (tab)
                  dispatch(
                    tab.attachment
                      ? { id: "tab.close", attachment: tab.attachment }
                      : { id: "tab.closeEmpty", tabId },
                  );
              }}
              onContextMenu={(event, tabId) =>
                openContext(
                  event,
                  tabMenu(stateForTab(tabs.find((tab) => tab.id === tabId))),
                )
              }
            />
            {attachment && current?.transport === "serial" && (
              <button
                className="terminal-settings-button quiet"
                aria-label={t("terminal.settings")}
                title={terminalSettings.description || t("terminal.settings")}
                disabled={
                  working ||
                  !!activeTab?.moving ||
                  !!fileDialog ||
                  filePending ||
                  current.state !== "open"
                }
                onClick={() => setSettingsTarget(attachment)}
              >
                {terminalSettings.encoding.toUpperCase()} ·{" "}
                {t(
                  terminalSettings.view === "hex"
                    ? "terminal.hex"
                    : "terminal.text",
                )}
                {terminalSettings.description && (
                  <span className="terminal-description">
                    {terminalSettings.description}
                  </span>
                )}
              </button>
            )}
          </div>
          {settingsTarget && (
            <TerminalSettingsDialog
              t={t}
              value={terminalSettings}
              onClose={() => setSettingsTarget(undefined)}
              onApply={async (settings) => {
                const target = sessions.find(
                  (entry) => entry.session_id === settingsTarget.sessionId,
                );
                if (
                  attachment?.id === settingsTarget.id &&
                  connected &&
                  !working &&
                  !activeTab?.moving &&
                  target?.state === "open"
                )
                  await select(target, settings, activeTab?.id);
              }}
            />
          )}
          <div className="notices" aria-label="Terminal notices">
            {activeTab?.restored && (
              <div role="status" className="notice">
                {t("tab.restoreHint")}
              </div>
            )}
            {activeTab?.moving && (
              <div role="status" className="notice">
                {t("tab.moving")}
              </div>
            )}
            {displayError && (
              <div role="alert" className="notice">
                {t("display.storageError")}
              </div>
            )}
            {shortcuts["terminal.find"] === "" &&
              Object.values(shortcuts).includes("Ctrl+Shift+F") && (
                <div role="status" className="notice">
                  {t("search.conflict")}
                  <button onClick={() => setShortcutsOpen(true)}>
                    {t("settings.shortcuts")}
                  </button>
                </div>
              )}
            {info?.lifetime === "attachment" && connected && (
              <div className="notice">
                This Host belongs to another attachment. It may stop when its
                owner exits.
              </div>
            )}
            {error && (
              <div role="alert" className="notice error">
                {error.message}
                {inputPaused &&
                  connected &&
                  attachment &&
                  current?.state === "open" &&
                  !current.lease && (
                    <button
                      onClick={() =>
                        dispatch({ id: "terminal.resume", attachment })
                      }
                    >
                      Resume input
                    </button>
                  )}
              </div>
            )}
            {attachment && dropped && (
              <div role="status" className="notice">
                Output overwritten in the Host buffer. The terminal was reset;
                older bytes are unavailable.
              </div>
            )}
            {current?.lease && (
              <div role="status" className="notice notice-lease">
                Lease busy · {current.lease.type}. Reading continues; input is
                disabled.
              </div>
            )}
            {attachment && current?.state !== "open" && (
              <div className="notice">
                Session closed or unavailable. Detach and select an open
                Session.
              </div>
            )}
          </div>
          {tabs.map(
            (tab) =>
              tab.attachment && (
                <div
                  key={tab.attachment.id}
                  className="terminal-pane"
                  hidden={tab.id !== selectedTabId}
                >
                  <TerminalView
                    ref={(controls) => {
                      if (controls) terminals.current.set(tab.id, controls);
                      else terminals.current.delete(tab.id);
                    }}
                    attachment={tab.attachment}
                    targetName={
                      sessions.find(
                        (entry) =>
                          entry.session_id === tab.attachment?.sessionId,
                      )?.label || tab.attachment.sessionId
                    }
                    t={t}
                    settings={tab.settings}
                    display={display}
                    onZoom={zoomDisplay}
                    visible={tab.id === selectedTabId}
                    writable={
                      tab.id === selectedTabId &&
                      writable &&
                      !tab.moving &&
                      !tab.moveId &&
                      !fileDialog &&
                      !sshFileDialog &&
                      !filePending &&
                      !settingsTarget
                    }
                    onReady={() => void acceptMove(tab)}
                    onError={(failure) => report(failure, tab.id)}
                    onDropped={() => updateTab(tab.id, { dropped: true })}
                    onContextMenu={(event) =>
                      openContext(event, terminalMenu(stateForTab(tab)))
                    }
                  />
                </div>
              ),
          )}
          {!attachment && (
            <div className="terminal-empty" aria-busy={working}>
              <div className="empty-message">
                <span className="terminal-glyph" aria-hidden="true">
                  ›_
                </span>
                <div>
                  <h3>{emptyTitle}</h3>
                  <p>{emptyHint}</p>
                </div>
              </div>
            </div>
          )}
          <BottomPanel
            visible={panelVisible}
            tab={panelTab}
            onTab={setPanelTab}
            onClose={() => dispatch({ id: "view.panel" })}
            t={t}
            activity={
              <ActivityPanel
                key={attachment?.id ?? "empty"}
                attachmentId={connected ? attachment?.id : undefined}
                t={t}
              />
            }
            status={
              <div className="panel-status">
                <p>{hostStatus}</p>
                <p>
                  {current
                    ? `${current.label || current.session_ref} · ${current.state}`
                    : emptyTitle}
                </p>
                {error && <p role="alert">{error.message}</p>}
                {current?.lease && (
                  <p>
                    {t("details.lease")}: {current.lease.type}
                  </p>
                )}
              </div>
            }
          >
            <TransferPanel
              jobs={transferJobs}
              connected={connected}
              sessionNames={Object.fromEntries(
                sessions.map((session) => [
                  session.session_id,
                  session.label || session.session_ref,
                ]),
              )}
              pending={filePending}
              error={fileError}
              t={t}
            />
          </BottomPanel>
        </main>
        <aside
          id="auxiliary-sidebar"
          className="auxiliary-sidebar"
          hidden={!auxiliaryVisible}
          aria-label={t("view.auxiliary")}
        >
          <div className="section-heading">
            <h2>{t("details.title")}</h2>
            <button
              className="quiet icon-button"
              aria-label={t("panel.close")}
              onClick={() => dispatch({ id: "view.auxiliary" })}
            >
              <Icon name="close" />
            </button>
          </div>
          <div className="session-details">
            {current ? (
              <dl>
                <dt>{t("details.session")}</dt>
                <dd>{current.label || current.session_ref}</dd>
                <dt>{t("details.endpoint")}</dt>
                <dd>{current.endpoint}</dd>
                <dt>{t("details.transport")}</dt>
                <dd>{current.transport}</dd>
                <dt>{t("details.state")}</dt>
                <dd>{current.state}</dd>
                <dt>{t("details.id")}</dt>
                <dd>{current.session_id}</dd>
                {current.lease && (
                  <>
                    <dt>{t("details.lease")}</dt>
                    <dd>{current.lease.type}</dd>
                  </>
                )}
              </dl>
            ) : (
              <p>{t("details.empty")}</p>
            )}
          </div>
          {auxiliaryVisible && (
            <SidebarResizer
              width={visibleAuxiliaryWidth}
              maximum={maximumSidebarWidth}
              defaultWidth={260}
              edge="left"
              controls="auxiliary-sidebar"
              label={t("view.resizeAuxiliary")}
              hint={t("view.resizeSidebarHelp")}
              onResize={setAuxiliaryWidth}
            />
          )}
        </aside>
      </div>
      <footer className="statusbar" aria-label="Workbench status">
        <span
          className="status-session"
          title={
            current
              ? `${current.label || current.endpoint} · ${current.session_ref} · ${current.session_id}`
              : undefined
          }
        >
          <Icon name={current?.transport === "serial" ? "serial" : "monitor"} />
          {current ? (
            <>
              <span className="target-label">
                {current.label || current.endpoint}
              </span>
              <span className="target-reference">{current.session_ref}</span>
              <code className="target-id" title={current.session_id}>
                SID{" "}
                {current.session_id.length > 14
                  ? `${current.session_id.slice(0, 6)}…${current.session_id.slice(-6)}`
                  : current.session_id}
              </code>
            </>
          ) : (
            t("target.empty")
          )}
        </span>
        <button
          className="target-copy"
          title={t("target.hint")}
          disabled={
            !attachment ||
            !enabled({ id: "session.copyAI", attachment }) ||
            copyPending
          }
          onClick={() =>
            attachment &&
            dispatch({ id: "session.copyAI", attachment: { ...attachment } })
          }
        >
          {t("target.copy")}
        </button>
        <span className="target-feedback" role="status">
          {copyResult?.id === attachment?.id && copyResult?.locale === locale
            ? t(copyResult.message)
            : ""}
        </span>
        <span className="session-total">
          {connected
            ? `${sessions.length} Session${sessions.length === 1 ? "" : "s"}`
            : "Offline"}
        </span>
        <span className="input-status" role="status">
          {attachment
            ? current?.state !== "open"
              ? "Session unavailable · Read-only"
              : current.lease
                ? "Read-only · Lease active"
                : inputPaused
                  ? "Input paused"
                  : terminalSettings.view === "hex"
                    ? t("terminal.hex")
                    : "Attached · Input enabled"
            : "No attachment"}
        </span>
        <span className="status-detail">
          {attachment
            ? `${terminalSettings.encoding.toUpperCase()} · ${terminalSettings.view === "hex" ? "HEX" : "xterm"} · Local resize only`
            : "Host-owned Sessions"}
        </span>
      </footer>
    </div>
  );
}
