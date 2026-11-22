import { useCallback, useEffect, useRef, useState } from "react";
import type { PeerSessions } from "../../shared/api";
import { clientErrorMessageId } from "../../shared/host-recovery";
import { host, unwrap } from "../host";
import {
  DirectoryFilter,
  ConnectionLight,
  useDirectoryFilter,
} from "./DirectoryFilter";
import { Icon, TransportIcon } from "../components/Icon";

export function LanDirectory({
  visible,
  t,
}: {
  visible: boolean;
  t: (id: string) => string;
}) {
  const filter = useDirectoryFilter();
  const [peers, setPeers] = useState<PeerSessions[]>([]);
  const [error, setError] = useState("");
  const [loaded, setLoaded] = useState(false);
  const [peerSelection, setPeerSelection] = useState("");
  const attaching = useRef(new Set<string>());
  const peerRequest = useRef(0);
  const loading = useRef(false);
  const refresh = useCallback(async () => {
    if (loading.current || document.hidden) return;
    loading.current = true;
    const version = ++peerRequest.current;
    try {
      const result = await unwrap(host.peers());
      if (version === peerRequest.current) {
        setPeers(result);
        setLoaded(true);
        setError("");
      }
    } catch (e) {
      if (version === peerRequest.current) {
        setPeers([]);
        setLoaded(true);
        setError(clientErrorMessageId(e));
      }
    } finally {
      if (version === peerRequest.current) loading.current = false;
    }
  }, []);
  useEffect(() => {
    if (!visible) return;
    let timer: ReturnType<typeof setInterval> | undefined;
    function visibility() {
      clearInterval(timer);
      if (document.hidden) return;
      void refresh();
      timer = setInterval(() => void refresh(), 5000);
    }
    visibility();
    document.addEventListener("visibilitychange", visibility);
    return () => {
      clearInterval(timer);
      document.removeEventListener("visibilitychange", visibility);
      peerRequest.current++;
      loading.current = false;
    };
  }, [visible, refresh]);
  const shown = peers.flatMap((peer) => {
    const hostMatches = filter.matchesText(peer.label);
    const sessions = peer.sessions.filter(
      (s) =>
        filter.matchesStatus(s.state === "open") &&
        (hostMatches ||
          filter.matchesText(s.label, s.endpoint, s.session_ref, s.transport)),
    );
    return sessions.length || (hostMatches && filter.matchesStatus(!peer.error))
      ? [{ ...peer, sessions }]
      : [];
  });
  return (
    <nav className="terminal-directory" aria-label={t("directory.lan")}>
      <div className="section-heading">
        <h2>{t("directory.lan")}</h2>
        <button
          className="quiet icon-button"
          aria-label={t("directory.lan") + " · " + t("session.refresh")}
          onClick={() => void refresh()}
        >
          <Icon name="refresh" />
        </button>
      </div>
      <DirectoryFilter t={t} filter={filter} />
      {shown.map((peer) => (
        <details open key={peer.id}>
          <summary>
            <ConnectionLight online={!peer.error} t={t} />
            {peer.label}
          </summary>
          {peer.error && (
            <p role="status">{t(clientErrorMessageId({ code: peer.error }))}</p>
          )}
          {peer.sessions.map((s) => {
            const key = JSON.stringify([peer.id, s.session_id]);
            async function attach() {
              if (s.state !== "open" || attaching.current.has(key)) return;
              attaching.current.add(key);
              try {
                await unwrap(host.attachPeer(peer.id, s.session_id));
              } catch (e) {
                setError(clientErrorMessageId(e));
              } finally {
                attaching.current.delete(key);
              }
            }
            return (
              <button
                className="directory-row"
                key={s.session_id}
                disabled={s.state !== "open"}
                aria-pressed={peerSelection === key}
                onClick={(event) => {
                  setPeerSelection(key);
                  if (event.detail === 0) void attach();
                }}
                onDoubleClick={() => void attach()}
              >
                <strong>
                  <ConnectionLight online={s.state === "open"} t={t} />
                  <TransportIcon transport={s.transport} t={t} />
                  {s.label || s.session_ref}
                </strong>
                <small>
                  {s.session_ref} · {s.endpoint}
                </small>
              </button>
            );
          })}
        </details>
      ))}
      {loaded && !!peers.length && !shown.length && !error && (
        <p className="empty-list">{t("workbench.noMatch")}</p>
      )}
      {loaded && !peers.length && !error && (
        <p className="empty-list">{t("directory.noHosts")}</p>
      )}
      {error && <p role="alert">{t(error)}</p>}
    </nav>
  );
}
