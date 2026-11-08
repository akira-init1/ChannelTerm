import { useEffect, useRef, useState } from "react";
import type { DirectoryPreference } from "../../shared/transfers";
import type { ClientError } from "../../shared/api";
import { host, unwrap } from "../host";

/** Settings are loaded anew so CLI changes are visible without restarting the GUI. */
export function TransferSettings({
  t,
  onClose,
}: {
  t: (id: string) => string;
  onClose: () => void;
}) {
  const dialog = useRef<HTMLDialogElement>(null);
  const saving = useRef(false);
  const [mode, setMode] = useState<DirectoryPreference>();
  const [busy, setBusy] = useState(false);
  const [error, setError] = useState("");
  useEffect(() => {
    dialog.current?.showModal();
    let live = true;
    void unwrap(host.transferPreference())
      .then((value) => {
        if (live) setMode(value);
      })
      .catch((e: ClientError) => {
        if (live) setError(e.message);
      });
    return () => {
      live = false;
    };
  }, []);
  return (
    <dialog
      ref={dialog}
      className="connection-dialog"
      aria-label={t("settings.transfers")}
      onCancel={(e) => {
        e.preventDefault();
        if (!busy) onClose();
      }}
    >
      <h2>{t("settings.transfers")}</h2>
      <p>{t("file.preferenceHint")}</p>
      <label>
        {t("file.directoryQuestion")}
        <select
          disabled={!mode || busy}
          value={mode ?? "ask"}
          onChange={(e) => {
            const value = e.target.value;
            if (value === "ask" || value === "tar" || value === "files")
              setMode(value);
          }}
        >
          {(["ask", "tar", "files"] as const).map((value) => (
            <option key={value} value={value}>
              {t(`file.mode.${value}`)}
            </option>
          ))}
        </select>
      </label>
      <p>{t("file.directoryHint")}</p>
      {error && <p role="alert">{error}</p>}
      <footer>
        <button disabled={busy} onClick={onClose}>
          {t("connection.cancel")}
        </button>
        <button
          disabled={!mode || busy}
          onClick={async () => {
            if (!mode || saving.current) return;
            saving.current = true;
            setBusy(true);
            setError("");
            try {
              await unwrap(host.saveTransferPreference(mode));
              onClose();
            } catch (e) {
              setError((e as ClientError).message);
            } finally {
              saving.current = false;
              setBusy(false);
            }
          }}
        >
          {t("file.savePreference")}
        </button>
      </footer>
    </dialog>
  );
}
