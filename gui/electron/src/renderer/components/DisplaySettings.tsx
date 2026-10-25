import { useEffect, useRef, useState, type FormEvent } from "react";
import {
  defaultDisplaySettings,
  isDisplaySettings,
  type DisplaySettings,
} from "../../shared/display-settings";

/** A local display draft; applying it never replaces the attachment or xterm buffer. */
export function DisplaySettingsDialog({
  value,
  t,
  onApply,
  onClose,
}: {
  value: DisplaySettings;
  t: (id: string) => string;
  onApply: (value: DisplaySettings) => boolean;
  onClose: () => void;
}) {
  const dialog = useRef<HTMLDialogElement>(null);
  const [draft, setDraft] = useState(value);
  const [error, setError] = useState("");
  useEffect(() => {
    dialog.current?.showModal();
  }, []);
  function submit(event: FormEvent) {
    event.preventDefault();
    if (!isDisplaySettings(draft)) return;
    if (onApply(draft)) onClose();
    else setError("display.storageError");
  }
  return (
    <dialog
      ref={dialog}
      className="connection-dialog"
      aria-labelledby="display-title"
      onClose={onClose}
    >
      <form onSubmit={submit}>
        <h2 id="display-title">{t("settings.display")}</h2>
        <label>
          {t("display.fontSize")}
          <input
            type="number"
            min={8}
            max={40}
            step={1}
            value={Number.isNaN(draft.fontSize) ? "" : draft.fontSize}
            onChange={(event) =>
              setDraft({ ...draft, fontSize: event.target.valueAsNumber })
            }
          />
        </label>
        <label>
          {t("display.fontFamily")}
          <input
            maxLength={256}
            value={draft.fontFamily}
            onChange={(event) =>
              setDraft({ ...draft, fontFamily: event.target.value })
            }
          />
        </label>
        <p className="connection-hint">{t("display.hint")}</p>
        {error && <p role="alert">{t(error)}</p>}
        <footer>
          <button
            type="button"
            onClick={() => setDraft(defaultDisplaySettings)}
          >
            {t("shortcuts.reset")}
          </button>
          <button type="button" onClick={onClose}>
            {t("connection.cancel")}
          </button>
          <button className="primary" disabled={!isDisplaySettings(draft)}>
            {t("terminal.apply")}
          </button>
        </footer>
      </form>
    </dialog>
  );
}
