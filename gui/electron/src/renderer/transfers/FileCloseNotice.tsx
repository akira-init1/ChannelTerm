import { useEffect, useId, useRef } from "react";

/** Dismissing this notice never cancels a transfer or retries a blocked close. */
export function FileCloseNotice({
  t,
  onClose,
}: {
  t: (id: string) => string;
  onClose: () => void;
}) {
  const dialog = useRef<HTMLDialogElement>(null);
  const button = useRef<HTMLButtonElement>(null);
  const titleId = useId();
  const descriptionId = useId();
  useEffect(() => {
    const element = dialog.current;
    element?.showModal();
    button.current?.focus();
    return () => element?.close();
  }, []);
  return (
    <dialog
      ref={dialog}
      className="connection-dialog transfer-confirmation"
      aria-labelledby={titleId}
      aria-describedby={descriptionId}
      onCancel={(event) => {
        event.preventDefault();
        onClose();
      }}
    >
      <h2 id={titleId}>{t("file.transfers")}</h2>
      <p id={descriptionId}>{t("file.closeHint")}</p>
      <footer>
        <button ref={button} className="primary" onClick={onClose}>
          {t("file.continue")}
        </button>
      </footer>
    </dialog>
  );
}
