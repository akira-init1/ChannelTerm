import { useEffect, useRef, useState, type FormEvent } from "react";
import { isDiagnosticID, type DiagnosticSaved } from "../../shared/diagnostics";
import type { ClientError } from "../../shared/api";
import { host, unwrap } from "../host";

/** Snapshot defaults exclude raw bytes; an existing ID downloads its already-defined contents. */
export function DiagnosticsDialog({
  attachmentId,
  t,
  onClose,
}: {
  attachmentId?: string;
  t: (id: string) => string;
  onClose: () => void;
}) {
  const dialog = useRef<HTMLDialogElement>(null);
  const alive = useRef(true),
    submitting = useRef(false);
  const [kind, setKind] = useState(attachmentId ? "snapshot" : "existing");
  const [busy, setBusy] = useState(false),
    [error, setError] = useState("");
  const [saved, setSaved] = useState<DiagnosticSaved>();
  const [cancelled, setCancelled] = useState(false);
  useEffect(() => {
    alive.current = true;
    dialog.current?.showModal();
    return () => {
      alive.current = false;
      if (submitting.current) void host.cancelDiagnostics();
    };
  }, []);
  async function submit(event: FormEvent<HTMLFormElement>) {
    event.preventDefault();
    if (submitting.current) return;
    const form = new FormData(event.currentTarget),
      id = String(form.get("diagnosticId") ?? "").trim();
    if (kind === "existing" && !isDiagnosticID(id)) {
      setError(t("diagnostics.invalid"));
      return;
    }
    if (kind === "snapshot" && !attachmentId) return;
    submitting.current = true;
    setBusy(true);
    setError("");
    setSaved(undefined);
    setCancelled(false);
    try {
      const result = await unwrap(
        host.saveDiagnostics(
          kind === "snapshot"
            ? {
                kind: "snapshot",
                attachmentId: attachmentId!,
                includeRaw: form.get("raw") === "on",
              }
            : { kind: "existing", diagnosticId: id },
        ),
      );
      if (alive.current) {
        setSaved(result);
        setCancelled(!result);
      }
    } catch (error) {
      if (alive.current) {
        if ((error as ClientError).code === "cancelled") setCancelled(true);
        else setError((error as ClientError).message);
      }
    } finally {
      submitting.current = false;
      if (alive.current) setBusy(false);
    }
  }
  return (
    <dialog
      ref={dialog}
      className="connection-dialog"
      aria-labelledby="diagnostics-title"
      onClose={onClose}
      onCancel={(event) => {
        if (submitting.current) {
          event.preventDefault();
          void host.cancelDiagnostics();
        }
      }}
    >
      <form onSubmit={(event) => void submit(event)}>
        <h2 id="diagnostics-title">{t("diagnostics.title")}</h2>
        <p className="connection-hint">{t("diagnostics.hint")}</p>
        <fieldset disabled={busy}>
          <label>
            {t("diagnostics.source")}
            <select
              value={kind}
              onChange={(event) => setKind(event.target.value)}
            >
              <option value="snapshot" disabled={!attachmentId}>
                {t("diagnostics.snapshot")}
              </option>
              <option value="existing">{t("diagnostics.existing")}</option>
            </select>
          </label>
          {kind === "snapshot" ? (
            <label className="display-checkbox">
              <input type="checkbox" name="raw" />
              {t("diagnostics.raw")}
            </label>
          ) : (
            <label>
              {t("diagnostics.id")}
              <input
                name="diagnosticId"
                required
                maxLength={29}
                placeholder="DIAG-…"
              />
            </label>
          )}
          <p className="connection-hint">
            {t(
              kind === "snapshot"
                ? "diagnostics.rawHint"
                : "diagnostics.existingHint",
            )}
          </p>
        </fieldset>
        <p className="connection-hint">{t("diagnostics.destination")}</p>
        {error && <p role="alert">{error}</p>}
        {cancelled && <p role="status">{t("diagnostics.cancelled")}</p>}
        {saved && (
          <div role="status" className="diagnostic-result">
            <p>{t("diagnostics.saved")}</p>
            <p>{saved.name}</p>
            <code>{saved.id}</code>
            <p>{saved.size} B</p>
            <code>SHA-256: {saved.sha256}</code>
          </div>
        )}
        <footer>
          <button
            type="button"
            onClick={() => (busy ? void host.cancelDiagnostics() : onClose())}
          >
            {t(busy ? "diagnostics.cancel" : "diagnostics.close")}
          </button>
          <button className="primary" type="submit" disabled={busy}>
            {t(busy ? "diagnostics.saving" : "diagnostics.save")}
          </button>
        </footer>
      </form>
    </dialog>
  );
}
