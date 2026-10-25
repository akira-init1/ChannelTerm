import { useEffect, useRef, useState, type FormEvent } from "react";
import type { ClientError, SessionSummary } from "../../shared/api";

/** Explicit shared closure confirmation, bound to the captured target rather than selection. */
export function CloseSessionDialog({
  target,
  t,
  onConfirm,
  onClose,
}: {
  target: SessionSummary;
  t: (id: string) => string;
  onConfirm: () => Promise<void>;
  onClose: () => void;
}) {
  const dialog = useRef<HTMLDialogElement>(null);
  const pending = useRef(false);
  const [busy, setBusy] = useState(false);
  const [error, setError] = useState<string>();
  useEffect(() => {
    dialog.current?.showModal();
  }, []);
  async function submit(event: FormEvent) {
    event.preventDefault();
    if (pending.current) return;
    pending.current = true;
    setBusy(true);
    setError(undefined);
    try {
      await onConfirm();
      onClose();
    } catch (error) {
      const code = (error as ClientError).code;
      setError(
        t(
          code === "busy"
            ? "session.closeBusy"
            : code === "closed" || code === "cancelled"
              ? "session.closeStale"
              : "session.closeFailed",
        ),
      );
    } finally {
      pending.current = false;
      setBusy(false);
    }
  }
  return (
    <dialog
      ref={dialog}
      className="connection-dialog"
      aria-labelledby="close-session-title"
      onCancel={(event) => {
        if (pending.current) event.preventDefault();
      }}
      onClose={onClose}
    >
      <form onSubmit={(event) => void submit(event)}>
        <h2 id="close-session-title">{t("session.close")}</h2>
        <p>
          {target.label || target.session_ref} · {target.endpoint}
        </p>
        <p>
          <code>{target.session_id}</code>
        </p>
        <p>{t("session.closeWarning")}</p>
        {error && <p role="alert">{error}</p>}
        <footer>
          <button type="button" autoFocus disabled={busy} onClick={onClose}>
            {t("connection.cancel")}
          </button>
          <button type="submit" disabled={busy}>
            {t("session.closeConfirm")}
          </button>
        </footer>
      </form>
    </dialog>
  );
}
