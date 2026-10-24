import { FileDialog } from "../transfers/FileDialog";
import { TransferPanel } from "../transfers/TransferPanel";
import type { TransferRequest, TransferStatus } from "../../shared/transfers";
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
import { SessionList } from "../sessions/SessionList";
import { TerminalView, type TerminalControls } from "../terminal/TerminalView";
import { Icon } from "./Icon";
import { WorkbenchMenu, type MenuPopupRequest } from "./WorkbenchMenu";
import {
  actionEnabled,
  isAction,
  sessionMenu,
  terminalMenu,
  type Action,
  type MenuLocale,
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
  const [viewportWidth, setViewportWidth] = useState(window.innerWidth);
  const defaultSidebarWidth = viewportWidth <= 900 ? 210 : 260;
  const maximumSidebarWidth = Math.max(180, Math.min(480, viewportWidth - 400));
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
  const [attachment, setAttachment] = useState<Attachment>();
  const preferences = useRef(new Map<string, TerminalSettings>());
  const [terminalSettings, setTerminalSettings] = useState<TerminalSettings>(
    defaultTerminalSettings,
  );
  const [settingsTarget, setSettingsTarget] = useState<Attachment>();
  const [error, setError] = useState<ClientError>();
  const [dropped, setDropped] = useState(false);
  // Errors pause local input until recovery; resuming never grants Host permission.
  const [inputPaused, setInputPaused] = useState(false);
  const [locale, setLocale] = useState<MenuLocale>("en");
  const i18n = useMemo(
    () => setupI18n({ locale, messages: menuCatalogs }),
    [locale],
  );
  const t = (id: string) => i18n._(id);
  const [helpOpen, setHelpOpen] = useState(false);
  const [connectionKind, setConnectionKind] =
    useState<ConnectionRequest["kind"]>();
  const [menuContext, setMenuContext] = useState<MenuPopupRequest>();
  const terminal = useRef<TerminalControls>(null);
  const [fileDialog, setFileDialog] = useState<{
    sessionId: string;
    name: string;
    direction: "send" | "receive";
  }>();
  const [filePending, setFilePending] = useState(false);
  const [fileError, setFileError] = useState("");
  const [transferJobs, setTransferJobs] = useState<TransferStatus[]>([]);
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
      setTransferJobs((previous) =>
        previous.map((job) =>
          job.released ? job : { ...job, state: "unknown" },
        ),
      );
      return;
    }
    async function poll() {
      const jobs: TransferStatus[] = [];
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
            (!observedSessions.split(",").includes(job.sessionId) ||
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
    setFilePending(true);
    setFileError("");
    void unwrap(host.startTransfer(request))
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
      connected,
      sshAvailable: !!info?.sshAvailable,
      busy: working || !!fileDialog || filePending,
      sidebarVisible,
      attachment,
      currentOpen: current?.state === "open",
      currentSerial: current?.transport === "serial",
      inputPaused,
      leased: !!current?.lease,
    }),
    [
      locale,
      connected,
      info?.sshAvailable,
      working,
      fileDialog,
      filePending,
      current?.transport,
      sidebarVisible,
      attachment,
      current?.state,
      current?.lease,
      inputPaused,
    ],
  );
  const dispatchRef = useRef<(action: Action) => void>(() => {});
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

  const report = useCallback((failure: ClientError) => {
    if (failure.code === "cancelled") return;
    setError(failure);
    setInputPaused(true);
    if (
      ["disconnected", "unauthorized", "incompatible"].includes(failure.code)
    ) {
      generation.current++;
      setConnected(false);
      setAttachment(undefined);
      setSessions([]);
      void host.disconnect();
    }
  }, []);

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
    void unwrap(host.info())
      .then((next) => {
        if (!active) return;
        setInfo(next);
        // StrictMode replays effects; only one initial connection is permitted.
        // An explicit disconnect never schedules another automatic attempt.
        if (next.autoConnect && !initialConnection.current) {
          initialConnection.current = true;
          actionBusy.current = true;
          void connect().finally(() => {
            actionBusy.current = false;
          });
        }
      })
      .catch((error: ClientError) => {
        if (active) report(error);
      });
    return () => {
      active = false;
    };
  }, [report]);
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
    setAttachment(undefined);
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
  ) {
    setOperation("attaching");
    setError(undefined);
    const version = generation.current;
    try {
      const next = await unwrap(host.attach(session.session_id, settings));
      if (version !== generation.current) {
        // Release a late client handle without reviving the old Host's view.
        await host.detach(next.id);
        return;
      }
      setDropped(false);
      if (
        preferences.current.size >= 64 &&
        !preferences.current.has(session.session_id)
      )
        preferences.current.delete(preferences.current.keys().next().value!);
      preferences.current.set(session.session_id, settings);
      setTerminalSettings(settings);
      setInputPaused(false);
      setAttachment(next);
    } catch (error) {
      report(error as ClientError);
    } finally {
      setOperation(undefined);
    }
  }

  async function detach() {
    if (!attachment) return;
    const id = attachment.id;
    setAttachment(undefined);
    setOperation("detaching");
    setError(undefined);
    try {
      await unwrap(host.detach(id));
    } catch (error) {
      report(error as ClientError);
    } finally {
      setOperation(undefined);
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

  function enabled(action: Action): boolean {
    if (
      !actionEnabled(action, {
        ...menuState,
        busy: working || actionBusy.current || !!fileDialog || filePending,
      })
    )
      return false;
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
    switch (action.id) {
      case "file.send":
      case "file.receive": {
        const target = sessions.find(
          (entry) =>
            entry.session_id === action.sessionId &&
            entry.state === "open" &&
            entry.transport === "serial" &&
            !entry.lease,
        );
        if (target)
          setFileDialog({
            sessionId: target.session_id,
            name: target.label || target.session_ref,
            direction: action.id === "file.send" ? "send" : "receive",
          });
        return;
      }
      case "connection.serial":
        setConnectionKind("serial");
        return;
      case "connection.ssh":
        setConnectionKind("ssh");
        return;
      case "view.sidebar":
        setSidebarVisible((visible) => !visible);
        return;
      case "language.change":
        setLocale(action.locale);
        return;
      case "help.quick":
        setHelpOpen(true);
        return;
      case "terminal.clear":
        terminal.current?.clear();
        return;
      case "terminal.focus":
        terminal.current?.focus();
        return;
      case "terminal.resume":
        setInputPaused(false);
        setError(undefined);
        terminal.current?.focus();
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
          await detach();
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
        ? "Use Connection > New connection to open a Session."
        : "Choose an open Session from the sidebar to view output and send input.";

  return (
    <div className={`app ${sidebarVisible ? "" : "sidebar-hidden"}`}>
      {fileDialog && connected && (
        <FileDialog
          key={fileDialog.sessionId + fileDialog.direction}
          {...fileDialog}
          t={t}
          onStart={startTransfer}
          onClose={() => setFileDialog(undefined)}
        />
      )}
      {connectionKind && connected && (
        <ConnectionDialog
          key={connectionKind}
          kind={connectionKind}
          t={t}
          onOpen={openConnection}
          onClose={() => setConnectionKind(undefined)}
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
          { "--sidebar-width": `${visibleSidebarWidth}px` } as CSSProperties
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
                  menuState,
                  item.session_id,
                  item.state === "open" && !item.lease,
                  item.transport === "serial",
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
            {/* A tab-shaped attachment header does not imply multiple live tabs. */}
            <div
              className={`terminal-tab ${attachment ? "attached" : ""}`}
              onContextMenu={(event) => {
                if (attachment)
                  openContext(
                    event,
                    sessionMenu(
                      menuState,
                      attachment.sessionId,
                      current?.state === "open" && !current.lease,
                      current?.transport === "serial",
                    ),
                  );
              }}
            >
              <Icon
                name={
                  current?.transport === "serial"
                    ? "serial"
                    : current?.transport === "ssh"
                      ? "monitor"
                      : "terminal"
                }
              />
              <h2 title={current?.label || current?.endpoint}>
                {current?.label || current?.session_ref || "Terminal"}
              </h2>
              {attachment && (
                <button
                  className="quiet icon-button"
                  aria-label={t("tab.close")}
                  title={t("tab.close")}
                  disabled={working}
                  onClick={() => dispatch({ id: "tab.close", attachment })}
                >
                  <Icon name="close" />
                </button>
              )}
            </div>
            {attachment && current?.transport === "serial" && (
              <button
                className="terminal-settings-button quiet"
                aria-label={t("terminal.settings")}
                title={terminalSettings.description || t("terminal.settings")}
                disabled={
                  working ||
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
                  target?.state === "open"
                )
                  await select(target, settings);
              }}
            />
          )}
          <div className="notices" aria-label="Terminal notices">
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
          {attachment ? (
            <TerminalView
              ref={terminal}
              key={attachment.id}
              attachment={attachment}
              settings={terminalSettings}
              writable={
                writable && !fileDialog && !filePending && !settingsTarget
              }
              onError={report}
              onDropped={() => setDropped(true)}
              onContextMenu={(event) =>
                openContext(event, terminalMenu(menuState))
              }
            />
          ) : (
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
        </main>
      </div>
      <footer className="statusbar" aria-label="Workbench status">
        <span className="status-session" title={current?.endpoint}>
          <Icon name={current?.transport === "serial" ? "serial" : "monitor"} />
          {current?.session_ref || "No Session"}
        </span>
        <span>
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
