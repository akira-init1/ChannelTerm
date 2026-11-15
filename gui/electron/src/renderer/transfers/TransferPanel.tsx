import { clientErrorMessageId } from "../../shared/host-recovery";
import { useEffect, useRef, useState } from "react";
import { CancelTransferDialog } from "./CancelTransferDialog";
import type { TransferStatus } from "../../shared/transfers";
import { host, unwrap } from "../host";
import { OperationSource } from "../components/OperationSource";

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
  const [interruptTarget, setInterruptTarget] = useState<string>();
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
      if (mounted.current) setFailure(clientErrorMessageId(error));
    } finally {
      if (mounted.current) setCancelling("");
    }
  }
  useEffect(() => {
    if (!connected) {
      void answer(false);
      setConfirmation(undefined);
    }
  }, [connected]);
  return (
    <section className="transfer-panel" aria-label={t("file.transfers")}>
      {pending && (
        <p className="transfer-summary" role="status">
          {t("file.running")}
        </p>
      )}
      {!jobs.length && !pending && !error && (
        <p className="panel-empty">{t("file.empty")}</p>
      )}
      <div className="transfer-jobs">
        {jobs
          .slice(-8)
          .reverse()
          .map((job) => (
            <div className="transfer-job" key={`${job.sessionId}:${job.id}`}>
              <div title={`${job.source} → ${job.destination}`}>
                <small title={job.sessionId}>
                  {sessionNames[job.sessionId] ||
                    job.sessionId.slice(0, 8) ||
                    t("source.host")}{" "}
                  &middot; {job.protocol && `${job.protocol.toUpperCase()} · `}
                  {job.direction === "receive"
                    ? t("file.receive")
                    : t("file.send")}
                </small>
                <strong>{job.source.split(/[\\/]/).pop() || job.id}</strong>
                <span>{t(`file.${job.state}`)}</span>
              </div>
              <OperationSource source={job.initiator} t={t} />
              {job.scope === "local" && <small>{t("source.local")}</small>}
              {job.scope === "host" && <small>{t("source.host")}</small>}
              <details className="operation-source">
                <summary>{t("source.correlation")}</summary>
                <div>session_id: {job.sessionId || "—"}</div>
                <div>transfer_id: {job.id}</div>
                {job.managedId && (
                  <div>managed_transfer_id: {job.managedId}</div>
                )}
                {job.checkpointId && (
                  <div>checkpoint_id: {job.checkpointId}</div>
                )}
              </details>
              {(job.resumed || job.checkpointId) && (
                <div>
                  {job.resumed && <small>{t("source.resumed")}</small>}
                  <small>{t("source.original")}</small>
                  <OperationSource source={job.originalSource} t={t} />
                </div>
              )}
              <progress
                aria-label={job.source || job.id}
                max={Math.max(1, job.total)}
                value={
                  job.protocol
                    ? job.state === "completed"
                      ? Math.max(1, job.total)
                      : Math.min(job.transferred, job.total * 0.999)
                    : job.transferred
                }
              />
              <div>
                <small>
                  {job.transferred.toLocaleString()} /{" "}
                  {job.total.toLocaleString()} B ·{" "}
                  {Math.round(job.speed).toLocaleString()} B/s
                </small>
                {!job.released && (job.scope !== "host" || !!job.protocol) && (
                  <button
                    disabled={
                      !!cancelling || (job.scope === "host" && !connected)
                    }
                    onClick={async () => {
                      if (job.scope === "host" && job.managedId) {
                        setInterruptTarget(job.managedId);
                        return;
                      }
                      setCancelling(job.id);
                      setFailure("");
                      try {
                        if (job.protocol) {
                          await unwrap(host.cancelSSHTransfer(job.id));
                          if (mounted.current) setCancelling("");
                          return;
                        }
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
                          setFailure(clientErrorMessageId(e));
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
                <small className="transfer-destination" title={job.destination}>
                  {job.destination}
                </small>
              )}
              {job.sha256 && <small title={job.sha256}>SHA-256 ✓</small>}
            </div>
          ))}
        {(error || failure) && <p role="alert">{t(error || failure)}</p>}
      </div>
      {interruptTarget && (
        <div role="alert">
          <p>{t("control.sshWarning")}</p>
          <code>{interruptTarget}</code>
          <button
            disabled={!!cancelling || !connected}
            onClick={async () => {
              const id = interruptTarget;
              setInterruptTarget(undefined);
              setCancelling(id);
              setFailure("");
              try {
                await unwrap(host.interruptSSHTransfer(id));
              } catch {
                if (mounted.current) setFailure("control.unknown");
              } finally {
                if (mounted.current) setCancelling("");
              }
            }}
          >
            {t("control.confirm")}
          </button>
          <button onClick={() => setInterruptTarget(undefined)}>
            {t("control.dismiss")}
          </button>
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
