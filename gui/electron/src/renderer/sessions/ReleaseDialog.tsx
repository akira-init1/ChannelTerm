import { useEffect, useRef, useState } from "react";
import type {
  ReleasePreview,
  ReleaseOutcome,
  ClientError,
} from "../../shared/api";
import { clientErrorMessageId } from "../../shared/host-recovery";
import { host, unwrap } from "../host";

/** Render idle policy in the window's language and preserve other Host activity descriptions. */
export function retentionLabel(reason: string, t: (id: string) => string) {
  return reason === "keep" || reason === "idle"
    ? t("release." + reason)
    : reason;
}

/** Confirm the Host's captured registrations and show one outcome for each. */
export function ReleaseDialog({
  id,
  t,
  onClose,
  onRefresh,
}: {
  id: string | null;
  t: (id: string) => string;
  onClose: () => void;
  onRefresh: () => Promise<void>;
}) {
  const dialog = useRef<HTMLDialogElement>(null);
  const inflight = useRef(false);
  const [plan, setPlan] = useState<ReleasePreview>();
  const [results, setResults] = useState<ReleaseOutcome[]>();
  const [error, setError] = useState<string>();
  const [busy, setBusy] = useState(false);
  useEffect(() => {
    dialog.current?.showModal();
    let ticket: string | undefined;
    let alive = true;
    void unwrap(host.previewRelease(id))
      .then((value) => {
        ticket = value.ticket;
        if (alive) setPlan(value);
        else void host.discardRelease(ticket);
      })
      .catch((e: ClientError) => {
        if (alive) setError(clientErrorMessageId(e));
      });
    return () => {
      alive = false;
      if (ticket) void host.discardRelease(ticket);
    };
  }, [id]);
  async function confirm() {
    if (!plan || inflight.current || results || error) return;
    inflight.current = true;
    setBusy(true);
    try {
      setResults(await unwrap(host.confirmRelease(plan.ticket)));
      await onRefresh();
    } catch (e) {
      setError(clientErrorMessageId(e as ClientError));
    } finally {
      inflight.current = false;
      setBusy(false);
    }
  }
  return (
    <dialog
      ref={dialog}
      className="connection-dialog release-dialog"
      aria-labelledby="release-title"
      onClose={onClose}
      onCancel={(event) => {
        if (inflight.current) event.preventDefault();
      }}
    >
      <h2 id="release-title">
        {t(id === null ? "host.releaseAll" : "session.release")}
        {plan && ` (${plan.targets.length})`}
      </h2>
      {plan && plan.targets.length > 0 && !results && (
        <p>{t("session.closeWarning")}</p>
      )}
      {plan?.targets.length === 0 && <p>{t("release.empty")}</p>}
      {plan && (
        <ul>
          {plan.targets.map((row, index) => (
            <li key={row.reference}>
              <strong>{row.endpoint}</strong> · {row.reference} ·{" "}
              {row.transport}
              <br />
              <code>{row.id}</code> ·{" "}
              {results
                ? t("release." + results[index])
                : retentionLabel(row.reason, t) || t("workbench.unknown")}
            </li>
          ))}
        </ul>
      )}
      {busy && <p role="status">{t("release.pending")}</p>}
      {error && <p role="alert">{t(error)}</p>}
      <footer>
        <button autoFocus disabled={busy} onClick={onClose}>
          {t(
            results || error || plan?.targets.length === 0
              ? "diagnostics.close"
              : "connection.cancel",
          )}
        </button>
        {!results && !error && !!plan?.targets.length && (
          <button disabled={busy} onClick={() => void confirm()}>
            {t("session.closeConfirm")}
          </button>
        )}
      </footer>
    </dialog>
  );
}
