import { useCallback, useEffect, useRef, useState } from "react";
import type { DebugOverview } from "../../shared/debug";
import { debugStale } from "../../shared/debug";
import type { SessionSummary } from "../../shared/api";
import { host, unwrap } from "../host";

/** Observe only while visible. Late results cannot revive a disconnected Host. */
export function useDebugOverview(connected: boolean, visible: boolean) {
  const [snapshot, setSnapshot] = useState<DebugOverview>();
  const [failed, setFailed] = useState(false);
  const [busy, setBusy] = useState(false);
  const pending = useRef(false),
    generation = useRef(0);
  const refresh = useCallback(async () => {
    if (!connected || pending.current) return;
    pending.current = true;
    setBusy(true);
    const current = generation.current;
    try {
      const result = await unwrap(host.debugOverview());
      if (current === generation.current) {
        setSnapshot(result);
        setFailed(false);
      }
    } catch {
      if (current === generation.current) setFailed(true);
    } finally {
      pending.current = false;
      setBusy(false);
    }
  }, [connected]);
  useEffect(() => {
    generation.current++;
    if (!connected) {
      setSnapshot(undefined);
      setFailed(false);
    }
    if (!connected || !visible) return;
    void refresh();
    const timer = setInterval(() => void refresh(), 2000);
    return () => {
      clearInterval(timer);
      generation.current++;
    };
  }, [connected, visible, refresh]);
  return { snapshot, failed, busy, refresh };
}

/** Read-only selection/filtering never creates attachments or changes a lease. */
export function DebugSidebar({
  snapshot,
  failed,
  busy,
  connected,
  sessions,
  t,
  refresh,
  locate,
}: {
  snapshot?: DebugOverview;
  failed: boolean;
  busy: boolean;
  connected: boolean;
  sessions: SessionSummary[];
  t: (id: string) => string;
  refresh: () => void;
  locate: (id: string) => void;
}) {
  const [query, setQuery] = useState("");
  const [selected, setSelected] = useState("");
  const stale = !snapshot || debugStale(snapshot, failed || !connected);
  const entries =
    snapshot?.operations.filter((r) =>
      [r.name, r.id, r.session_id, r.mode].some((s) =>
        s.toLowerCase().includes(query.toLowerCase()),
      ),
    ) ?? [];
  const groups = ["serial", "ssh", "unassociated"];
  function group(id: string) {
    return (
      sessions.find((s) => s.session_id === id)?.transport ?? "unassociated"
    );
  }
  return (
    <section className="debug-sidebar" aria-label={t("debug.title")}>
      <div className="section-heading">
        <h2>{t("debug.title")}</h2>
        <button
          disabled={!connected || busy}
          onClick={refresh}
          aria-label={t("debug.refresh")}
        >
          {t("debug.refresh")}
        </button>
      </div>
      <input
        type="search"
        value={query}
        onChange={(e) => setQuery(e.target.value)}
        aria-label={t("debug.filter")}
        placeholder={t("debug.filter")}
      />
      <p role="status">
        {!connected
          ? t("debug.offline")
          : failed
            ? t("debug.unavailable")
            : !snapshot
              ? t("debug.loading")
              : stale
                ? t("debug.stale")
                : t("debug.scope")}
      </p>
      {snapshot && (
        <time dateTime={snapshot.observed_at}>
          {t("debug.updated")}:{" "}
          {new Date(snapshot.observed_at).toLocaleString()}
        </time>
      )}
      {snapshot && !failed && snapshot.operations.length === 0 && (
        <p>{t("debug.empty")}</p>
      )}
      {groups.map((g) => {
        const items = entries.filter((r) => group(r.session_id) === g);
        if (!items.length) return null;
        return (
          <section key={g}>
            <h3>{t(`debug.${g}`)}</h3>
            {items.map((r) => {
              const expired = stale || r.stale;
              const session = sessions.find(
                (s) => s.session_id === r.session_id,
              );
              return (
                <article key={r.id} className="debug-entry">
                  <button
                    className="debug-select"
                    aria-expanded={selected === r.id}
                    onClick={() => setSelected(selected === r.id ? "" : r.id)}
                  >
                    <strong>{r.name}</strong>
                    <span>{t(`debug.${r.mode}`)}</span>
                  </button>
                  <p>
                    {t("debug.connection")}:{" "}
                    {t(`debug.${expired ? "unknown" : r.connection}`)} ·{" "}
                    {t("debug.targetState")}:{" "}
                    {t(`debug.${expired ? "unknown" : r.target_state}`)}
                  </p>
                  {expired && <p>{t("debug.stale")}</p>}
                  {selected === r.id && (
                    <dl>
                      <dt>{t("debug.operation")}</dt>
                      <dd>{r.id}</dd>
                      <dt>{t("debug.source")}</dt>
                      <dd>{t(`debug.source.${r.source}`)}</dd>
                      <dt>{t("debug.client")}</dt>
                      <dd>
                        {r.client.client_id || t("debug.unknown")}{" "}
                        {r.client.name}
                      </dd>
                      <dt>{t("debug.updated")}</dt>
                      <dd>{new Date(r.updated_at).toLocaleString()}</dd>
                      <dt>{t("debug.lastState")}</dt>
                      <dd>{t(`debug.${r.last_target_state}`)}</dd>
                      {r.mode === "ssh_tunnel" && (
                        <>
                          <dt>{t("debug.tunnel")}</dt>
                          <dd>
                            {t(`debug.${expired ? "unknown" : r.tunnel_state}`)}
                          </dd>
                        </>
                      )}
                      <dt>{t("debug.session")}</dt>
                      <dd>
                        {r.session_id || t("debug.unassociated")}
                        {r.session_id && !session && (
                          <p>{t("debug.sessionMissing")}</p>
                        )}
                      </dd>
                      {r.ports.map((p) => (
                        <div key={p.role}>
                          <dt>
                            {t(`debug.role.${p.role}`)} (
                            {t(`debug.perspective.${p.perspective}`)})
                          </dt>
                          <dd>
                            {p.state === "observed"
                              ? `${p.address} · ${t("debug.observed")}`
                              : t(`debug.${p.state}`)}
                            {p.configured && (
                              <p>
                                {t("debug.configured")}: {p.configured}
                              </p>
                            )}
                          </dd>
                        </div>
                      ))}
                    </dl>
                  )}
                  {session && (
                    <button onClick={() => locate(r.session_id)}>
                      {t("debug.locate")}
                    </button>
                  )}
                </article>
              );
            })}
          </section>
        );
      })}
    </section>
  );
}
