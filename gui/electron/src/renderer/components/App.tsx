import { useCallback, useEffect, useRef, useState } from "react";
import type {
  Attachment,
  ClientError,
  HostInfo,
  SessionSummary,
} from "../../shared/api";
import { host, unwrap } from "../host";
import { SessionList } from "../sessions/SessionList";
import { TerminalView } from "../terminal/TerminalView";
import { Icon } from "./Icon";
import { WorkbenchMenu } from "./WorkbenchMenu";

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
  const [operation, setOperation] = useState<
    "connecting" | "disconnecting" | "attaching" | "detaching"
  >();
  const working = operation !== undefined;
  const [sessions, setSessions] = useState<SessionSummary[]>([]);
  const [attachment, setAttachment] = useState<Attachment>();
  const [error, setError] = useState<ClientError>();
  const [dropped, setDropped] = useState(false);
  // Errors pause local input until recovery; resuming never grants Host permission.
  const [inputPaused, setInputPaused] = useState(false);
  // Invalidate list snapshots and pending attachment results across Host changes.
  const generation = useRef(0);
  // Manual refresh and periodic polling share one outstanding list request.
  const refreshing = useRef(false);
  const current = sessions.find(
    (entry) => entry.session_id === attachment?.sessionId,
  );

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
    void unwrap(host.info()).then(setInfo).catch(report);
  }, [report]);
  useEffect(() => {
    if (!connected) return;
    const timer = setInterval(() => {
      void refresh();
    }, 1500);
    return () => clearInterval(timer);
  }, [connected, refresh]);

  async function connect() {
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

  async function select(session: SessionSummary) {
    setOperation("attaching");
    setAttachment(undefined);
    setError(undefined);
    setDropped(false);
    const version = generation.current;
    try {
      const next = await unwrap(host.attach(session.session_id));
      if (version !== generation.current) {
        // Release a late client handle without reviving the old Host's view.
        await host.detach(next.id);
        return;
      }
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
    connected && current?.state === "open" && !current.lease && !inputPaused;
  // Distinguish pending/failed operations from a successfully loaded empty list
  // instead of using the same empty-state heading for every unattached view.
  const emptyTitle = working
    ? {
        connecting: "Connecting to Host…",
        disconnecting: "Disconnecting Host…",
        attaching: "Attaching Session…",
        detaching: "Detaching Session…",
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
        ? "Open a Session with the CLI, then refresh the list."
        : "Choose an open Session from the sidebar to view output and send input.";

  return (
    <div className={`app ${sidebarVisible ? "" : "sidebar-hidden"}`}>
      <WorkbenchMenu
        sidebarVisible={sidebarVisible}
        onToggleSidebar={() => setSidebarVisible((visible) => !visible)}
      />
      <div className="workspace">
        <div className="activity-rail" aria-label="Workspace navigation">
          <button
            className="rail-button"
            aria-label="Toggle Session sidebar"
            title="Toggle Session sidebar"
            aria-expanded={sidebarVisible}
            aria-controls="session-sidebar"
            onClick={() => setSidebarVisible((visible) => !visible)}
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
              onClick={() => void refresh()}
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
            onSelect={(item) => void select(item)}
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
              onClick={() => void (connected ? disconnect() : connect())}
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
        </aside>
        <main aria-label="Terminal workspace">
          <div className="terminal-toolbar">
            {/* A tab-shaped attachment header does not imply multiple live tabs. */}
            <div className={`terminal-tab ${attachment ? "attached" : ""}`}>
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
                  aria-label="Detach"
                  title="Detach Session"
                  disabled={working}
                  onClick={() => void detach()}
                >
                  <Icon name="close" />
                </button>
              )}
            </div>
            <span className="terminal-target">
              {current?.session_ref || "No Session attached"}
            </span>
          </div>
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
                      onClick={() => {
                        setInputPaused(false);
                        setError(undefined);
                      }}
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
              <div role="status" className="notice">
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
              key={attachment.id}
              attachment={attachment}
              writable={writable}
              onError={report}
              onDropped={() => setDropped(true)}
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
          <div className="terminal-input-hint">
            <Icon name="terminal" />
            <span>
              {attachment
                ? writable
                  ? "Type directly in the terminal · Enter sends input"
                  : "Terminal input is paused · Output remains visible"
                : "Attach a Session to use terminal input"}
            </span>
            <span className="input-mode">
              {attachment && writable ? "Direct input" : "Read-only"}
            </span>
          </div>
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
                  : "Attached · Input enabled"
            : "No attachment"}
        </span>
        <span className="status-detail">
          {attachment
            ? "UTF-8 · xterm · Local resize only"
            : "Host-owned Sessions"}
        </span>
      </footer>
    </div>
  );
}
