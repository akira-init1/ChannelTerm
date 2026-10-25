import { useEffect, useRef } from "react";

/** A local, bounded paste draft; cancel never submits bytes to the terminal. */
export function PastePreview({
  text,
  sessionId,
  targetName,
  valid,
  t,
  onConfirm,
  onCancel,
}: {
  text: string;
  sessionId: string;
  targetName: string;
  valid: boolean;
  t: (id: string) => string;
  onConfirm: () => void;
  onCancel: () => void;
}) {
  const dialog = useRef<HTMLDialogElement>(null);
  useEffect(() => {
    dialog.current?.showModal();
  }, []);
  return (
    <dialog
      ref={dialog}
      className="connection-dialog paste-dialog"
      aria-labelledby="paste-title"
      onClose={onCancel}
    >
      <h2 id="paste-title">{t("paste.title")}</h2>
      <p>{targetName}</p>
      <p>
        session_id: <code>{sessionId}</code>
      </p>
      <p>{t("paste.warning")}</p>
      <pre className="paste-preview">{text.replace(/\x1b/g, "␛")}</pre>
      {!valid && <p role="alert">{t("paste.stale")}</p>}
      <footer>
        <button type="button" autoFocus onClick={onCancel}>
          {t("connection.cancel")}
        </button>
        <button type="button" disabled={!valid} onClick={onConfirm}>
          {t("paste.confirm")}
        </button>
      </footer>
    </dialog>
  );
}
