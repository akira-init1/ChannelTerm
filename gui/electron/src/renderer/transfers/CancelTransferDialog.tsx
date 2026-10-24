import { useEffect, useId, useRef } from "react";

/** Presentation only: Main retains the Host confirmation and validates the answer. */
export function CancelTransferDialog({
  name,
  t,
  onAnswer,
}: {
  name: string;
  t: (id: string) => string;
  onAnswer: (confirm: boolean) => void;
}) {
  const dialog = useRef<HTMLDialogElement>(null);
  const continueButton = useRef<HTMLButtonElement>(null);
  const titleId = useId();
  useEffect(() => {
    dialog.current?.showModal();
    continueButton.current?.focus();
    return () => dialog.current?.close();
  }, []);
  return (
    <dialog
      ref={dialog}
      className="connection-dialog transfer-confirmation"
      aria-labelledby={titleId}
      onCancel={(event) => {
        event.preventDefault();
        onAnswer(false);
      }}
      onClick={(event) => {
        if (event.target === event.currentTarget) onAnswer(false);
      }}
    >
      <h2 id={titleId}>{t("file.cancelQuestion")}</h2>
      <p className="transfer-confirmation-target">{name}</p>
      <footer>
        <button ref={continueButton} onClick={() => onAnswer(false)}>
          {t("file.continue")}
        </button>
        <button className="primary" onClick={() => onAnswer(true)}>
          {t("file.cancel")}
        </button>
      </footer>
    </dialog>
  );
}
