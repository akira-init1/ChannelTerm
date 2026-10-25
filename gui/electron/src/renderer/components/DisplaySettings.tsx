import { useEffect, useRef, useState, type FormEvent } from "react";
import {
  defaultDisplaySettings,
  isDisplaySettings,
  type DisplaySettings,
} from "../../shared/display-settings";
import { defaultHighlights } from "../../shared/default-highlights";

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
        <label>
          {t("display.terminalStyle")}
          <select
            aria-label={t("display.terminalStyle")}
            value={draft.terminalStyle}
            onChange={(event) => {
              const terminalStyle = event.target.value;
              if (
                terminalStyle === "semantic" ||
                terminalStyle === "monochrome"
              )
                setDraft({ ...draft, terminalStyle });
            }}
          >
            <option value="semantic">{t("display.semantic")}</option>
            <option value="monochrome">{t("display.monochrome")}</option>
          </select>
        </label>
        <pre
          className={`style-preview ${draft.terminalStyle}`}
          aria-label={t("display.preview")}
        >
          <span style={{ color: defaultHighlights.palette.error.hex }}>
            ERROR
          </span>{" "}
          <span style={{ color: defaultHighlights.palette.success.hex }}>
            ready
          </span>{" "}
          <span style={{ color: defaultHighlights.palette.number.hex }}>
            0x1000 42
          </span>
          {"\n"}
          <span style={{ color: defaultHighlights.palette.interface.hex }}>
            uart
          </span>{" "}
          <span style={{ color: defaultHighlights.palette.timestamp.hex }}>
            12:34:56
          </span>
        </pre>
        <p className="connection-hint">{t("display.styleHint")}</p>
        <label className="display-checkbox">
          <input
            type="checkbox"
            checked={draft.promptTimestamps}
            onChange={(event) =>
              setDraft({ ...draft, promptTimestamps: event.target.checked })
            }
          />
          {t("display.promptTimestamps")}
        </label>
        <p className="connection-hint">{t("display.promptTimestampHint")}</p>
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
