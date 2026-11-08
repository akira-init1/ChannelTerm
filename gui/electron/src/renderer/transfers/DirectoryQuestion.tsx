import { useEffect, useRef, useState } from "react";
import { host, unwrap } from "../host";
import type { ClientError } from "../../shared/api";

/** One decision applies to the captured batch; remembering only changes local preferences. */
export function DirectoryQuestion({
  t,
  resumable,
  onChoose,
  onCancel,
}: {
  t: (id: string) => string;
  resumable: boolean;
  onChoose: (mode: "tar" | "files") => void;
  onCancel: () => void;
}) {
  const dialog = useRef<HTMLDialogElement>(null);
  const submitting = useRef(false);
  const alive = useRef(true);
  const [mode, setMode] = useState<"tar" | "files">("tar");
  const [remember, setRemember] = useState(false);
  const [busy, setBusy] = useState(false);
  const [error, setError] = useState("");
  useEffect(() => {
    alive.current = true;
    dialog.current?.showModal();
    return () => {
      alive.current = false;
    };
  }, []);
  return (
    <dialog
      ref={dialog}
      className="connection-dialog directory-question"
      aria-label={t("file.directoryQuestion")}
      onCancel={(e) => {
        e.preventDefault();
        e.stopPropagation();
        if (!busy) onCancel();
      }}
    >
      <h2>{t("file.directoryQuestion")}</h2>
      <p>{t("file.directoryHint")}</p>
      <label>
        <input
          type="radio"
          name="directory-mode"
          checked={mode === "tar"}
          disabled={busy}
          onChange={() => setMode("tar")}
        />
        {t("file.mode.tar")}
      </label>
      <label>
        <input
          type="radio"
          name="directory-mode"
          checked={mode === "files"}
          disabled={busy}
          onChange={() => setMode("files")}
        />
        {t("file.mode.files")}
      </label>
      <label>
        <input
          type="checkbox"
          checked={remember}
          disabled={busy}
          onChange={(e) => setRemember(e.target.checked)}
        />
        {t("file.rememberMode")}
      </label>
      {resumable && mode === "files" && (
        <p role="alert">{t("file.modeResumeConflict")}</p>
      )}
      {error && <p role="alert">{error}</p>}
      <footer>
        <button disabled={busy} onClick={onCancel}>
          {t("connection.cancel")}
        </button>
        <button
          className="primary"
          disabled={busy || (resumable && mode === "files")}
          onClick={async () => {
            if (submitting.current) return;
            submitting.current = true;
            setBusy(true);
            setError("");
            try {
              if (remember) await unwrap(host.saveTransferPreference(mode));
              if (alive.current) onChoose(mode);
            } catch (e) {
              setError((e as ClientError).message);
            } finally {
              submitting.current = false;
              setBusy(false);
            }
          }}
        >
          {t("file.startBatch")}
        </button>
      </footer>
    </dialog>
  );
}
