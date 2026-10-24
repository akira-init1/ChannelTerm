import { useEffect, useRef, useState } from "react";
import { CancelTransferDialog } from "./CancelTransferDialog";
import type { TransferStatus } from "../../shared/transfers";
import { host, unwrap } from "../host";
import type { ClientError } from "../../shared/api";

/** Structured progress from all observed clients, never CLI output rendered into xterm. */
export function TransferPanel({
  jobs,
  sessionNames,
  connected,
  pending,
  error,
  t,
}: {
  jobs: TransferStatus[];
  sessionNames: Record<string, string>;
  connected: boolean;
  pending: boolean;
  error: string;
  t: (id: string) => string;
}) {
  const [expanded, setExpanded] = useState(true);
  const [cancelling, setCancelling] = useState("");
  const [failure, setFailure] = useState("");
  const [confirmation, setConfirmation] = useState<{
    id: string;
    name: string;
  }>();
  const confirmationId = useRef<string | undefined>(undefined);
  const isConnected = useRef(connected);
  isConnected.current = connected;
  const mounted = useRef(false);
  useEffect(() => {
    mounted.current = true;
    return () => {
      mounted.current = false;
      const id = confirmationId.current;
      confirmationId.current = undefined;
      if (id)
        void unwrap(host.resolveTransferCancel(id, false)).catch(() => {});
    };
  }, []);
  async function answer(confirm: boolean) {
    const id = confirmationId.current;
    if (!id) return;
    confirmationId.current = undefined;
    setConfirmation(undefined);
    try {
      await unwrap(host.resolveTransferCancel(id, confirm));
    } catch (error) {
      if (mounted.current) setFailure((error as ClientError).message);
    } finally {
      if (mounted.current) setCancelling("");
    }
  }
  useEffect(() => {
    setExpanded(connected);
    if (!connected) {
      void answer(false);
      setConfirmation(undefined);
    }
  }, [connected]);
  if (!jobs.length && !pending && !error) return null;
  return (
    <section className="transfer-panel" aria-label={t("file.transfers")}>
      <button
        className="transfer-heading"
        onClick={() => setExpanded((value) => !value)}
        aria-expanded={expanded}
      >
        {expanded ? "▾" : "▸"} {t("file.transfers")} · {jobs.length}
        {pending ? ` · ${t("file.running")}` : ""}
      </button>
      {expanded && (
        <div className="transfer-jobs">
          {jobs
            .slice(-8)
            .reverse()
            .map((job) => (
              <div className="transfer-job" key={`${job.sessionId}:${job.id}`}>
                <div title={`${job.source} → ${job.destination}`}>
                  <small title={job.sessionId}>
                    {sessionNames[job.sessionId] || job.sessionId.slice(0, 8)}{" "}
                    &middot;{" "}
                    {job.direction === "receive"
                      ? t("file.receive")
                      : t("file.send")}
                  </small>
                  <strong>{job.source.split(/[\\/]/).pop() || job.id}</strong>
                  <span>{t(`file.${job.state}`)}</span>
                </div>
                <progress
                  aria-label={job.source || job.id}
                  max={Math.max(1, job.total)}
                  value={job.transferred}
                />
                <div>
                  <small>
                    {job.transferred.toLocaleString()} /{" "}
                    {job.total.toLocaleString()} B ·{" "}
                    {Math.round(job.speed).toLocaleString()} B/s
                  </small>
                  {!job.released && (
                    <button
                      disabled={!!cancelling}
                      onClick={async () => {
                        setCancelling(job.id);
                        setFailure("");
                        try {
                          const id = await unwrap(
                            host.cancelTransfer(job.sessionId, job.id),
                          );
                          if (!mounted.current || !isConnected.current) {
                            await unwrap(host.resolveTransferCancel(id, false));
                            if (mounted.current) setCancelling("");
                            return;
                          }
                          confirmationId.current = id;
                          setConfirmation({
                            id,
                            name: `${sessionNames[job.sessionId] || job.sessionId} · ${job.source.split(/[\\/]/).pop() || job.id}`,
                          });
                        } catch (e) {
                          if (mounted.current) {
                            setFailure((e as ClientError).message);
                            setCancelling("");
                          }
                        }
                      }}
                    >
                      {t("connection.cancel")}
                    </button>
                  )}
                </div>
                {job.destination && (
                  <small
                    className="transfer-destination"
                    title={job.destination}
                  >
                    {job.destination}
                  </small>
                )}
                {job.sha256 && <small title={job.sha256}>SHA-256 ✓</small>}
              </div>
            ))}
          {(error || failure) && <p role="alert">{error || failure}</p>}
        </div>
      )}
      {confirmation && (
        <CancelTransferDialog
          name={confirmation.name}
          t={t}
          onAnswer={(confirm) => void answer(confirm)}
        />
      )}
    </section>
  );
}
