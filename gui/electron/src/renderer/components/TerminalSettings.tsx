import { useEffect, useRef, useState, type FormEvent } from "react";
import {
  defaultTerminalSettings,
  isTerminalSettings,
  type TerminalSettings,
} from "../../shared/terminal-settings";

type Translate = (id: string) => string;

/** Shared form fields for serial creation and local attachment presentation. */
export function TerminalSettingsFields({
  t,
  value = defaultTerminalSettings,
}: {
  t: Translate;
  value?: TerminalSettings;
}) {
  return (
    <>
      <div className="connection-grid">
        <label>
          {t("terminal.encoding")}
          <select name="encoding" defaultValue={value.encoding}>
            <option value="utf-8">UTF-8</option>
            <option value="gbk">GBK</option>
            <option value="gb18030">GB18030</option>
          </select>
        </label>
        <label>
          {t("terminal.view")}
          <select name="view" defaultValue={value.view}>
            <option value="text">{t("terminal.text")}</option>
            <option value="hex">{t("terminal.hex")}</option>
          </select>
        </label>
      </div>
      <label>
        {t("terminal.description")}
        <input
          name="description"
          maxLength={512}
          defaultValue={value.description}
        />
      </label>
      <p className="connection-hint">{t("terminal.localHint")}</p>
    </>
  );
}

export function readTerminalSettings(
  form: FormData,
): TerminalSettings | undefined {
  const value = {
    encoding: form.get("encoding"),
    view: form.get("view"),
    description: form.get("description"),
  };
  return isTerminalSettings(value) ? value : undefined;
}

/** Applying settings replaces only this client's attachment; no remote reset or close. */
export function TerminalSettingsDialog({
  t,
  value,
  onApply,
  onClose,
}: {
  t: Translate;
  value: TerminalSettings;
  onApply: (value: TerminalSettings) => Promise<void>;
  onClose: () => void;
}) {
  const dialog = useRef<HTMLDialogElement>(null);
  const pending = useRef(false);
  const [busy, setBusy] = useState(false);
  const [error, setError] = useState("");
  useEffect(() => {
    dialog.current?.showModal();
  }, []);
  async function submit(event: FormEvent<HTMLFormElement>) {
    event.preventDefault();
    if (pending.current) return;
    const settings = readTerminalSettings(new FormData(event.currentTarget));
    if (!settings) {
      setError(t("connection.invalid"));
      return;
    }
    pending.current = true;
    setBusy(true);
    try {
      await onApply(settings);
      onClose();
    } finally {
      pending.current = false;
      setBusy(false);
    }
  }
  return (
    <dialog
      ref={dialog}
      className="connection-dialog"
      aria-labelledby="terminal-settings-title"
      onCancel={(event) => {
        if (pending.current) event.preventDefault();
      }}
      onClose={onClose}
    >
      <form onSubmit={(event) => void submit(event)}>
        <h2 id="terminal-settings-title">{t("terminal.settings")}</h2>
        <fieldset disabled={busy}>
          <TerminalSettingsFields t={t} value={value} />
        </fieldset>
        {error && <p role="alert">{error}</p>}
        <footer>
          <button type="button" disabled={busy} onClick={onClose}>
            {t("connection.cancel")}
          </button>
          <button className="primary" disabled={busy} type="submit">
            {t("terminal.apply")}
          </button>
        </footer>
      </form>
    </dialog>
  );
}
