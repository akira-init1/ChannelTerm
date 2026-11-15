import { useEffect, useRef, useState } from "react";
import type { Attachment } from "../../shared/api";
import type { RecordingStatus } from "../../shared/logging";
import { clientErrorMessageId } from "../../shared/host-recovery";
import { host, unwrap } from "../host";

/** Low-frequency status only; capture bytes never enter React or Renderer. */
export function useRecordings() {
  const [records, setRecords] = useState<RecordingStatus[]>([]);
  useEffect(() => {
    let active = true,
      pending = false;
    const update = async () => {
      if (pending) return;
      pending = true;
      try {
        const next = await unwrap(host.recordings());
        if (active) setRecords(next);
      } catch {
        /* The window may be disposing; Main owns capture cleanup. */
      } finally {
        pending = false;
      }
    };
    void update();
    const timer = setInterval(() => void update(), 1000);
    return () => {
      active = false;
      clearInterval(timer);
    };
  }, []);
  return records;
}

/** Starts only explicit forward recording; stopping never closes the shared Session. */
export function RecordingDialog({
  attachment,
  records,
  t,
  onClose,
}: {
  attachment?: Attachment;
  records: RecordingStatus[];
  t: (id: string) => string;
  onClose: () => void;
}) {
  const dialog = useRef<HTMLDialogElement>(null);
  const [format, setFormat] = useState<"raw" | "text">("raw");
  const [segmentMiB, setSegmentMiB] = useState(8);
  const [segments, setSegments] = useState(4);
  const [busy, setBusy] = useState(false);
  const [error, setError] = useState("");
  useEffect(() => {
    dialog.current?.showModal();
  }, []);
  const run = async (work: () => Promise<unknown>) => {
    if (busy) return;
    setBusy(true);
    setError("");
    try {
      await work();
    } catch (error) {
      setError(clientErrorMessageId(error));
    } finally {
      setBusy(false);
    }
  };
  return (
    <dialog
      className="connection-dialog recording-dialog"
      ref={dialog}
      onClose={onClose}
      onCancel={(event) => {
        if (busy) event.preventDefault();
      }}
      aria-labelledby="recording-title"
    >
      <h2 id="recording-title">{t("logs.open")}</h2>
      <div className="recording-content">
        <p className="connection-hint">{t("logs.warning")}</p>
        <p className="connection-hint">{t("logs.lifecycle")}</p>
        <form
          onSubmit={(event) => {
            event.preventDefault();
            if (attachment)
              void run(() =>
                unwrap(
                  host.startRecording({
                    attachmentId: attachment.id,
                    format,
                    segmentMiB,
                    segments,
                  }),
                ),
              );
          }}
        >
          <fieldset disabled={busy || !attachment}>
            <p>
              <code>{attachment?.sessionId ?? t("target.empty")}</code>
            </p>
            <label>
              {t("logs.format")}
              <select
                value={format}
                onChange={(event) =>
                  setFormat(event.target.value === "text" ? "text" : "raw")
                }
              >
                <option value="raw">{t("logs.raw")}</option>
                <option value="text">{t("logs.text")}</option>
              </select>
            </label>
            <div className="connection-grid">
              <label>
                {t("logs.segmentSize")}
                <select
                  value={segmentMiB}
                  onChange={(event) =>
                    setSegmentMiB(Number(event.target.value))
                  }
                >
                  {[1, 4, 8, 16, 64].map((size) => (
                    <option key={size} value={size}>
                      {size} MiB
                    </option>
                  ))}
                </select>
              </label>
              <label>
                {t("logs.segments")}
                <input
                  type="number"
                  min={1}
                  max={8}
                  step={1}
                  value={segments}
                  onChange={(event) => setSegments(event.target.valueAsNumber)}
                  required
                />
              </label>
            </div>
            <button type="submit">{t("logs.start")}</button>
          </fieldset>
        </form>
        <p className="connection-hint">{t("logs.recovery")}</p>
        <ul className="recording-list">
          {records.map((record) => (
            <li key={record.id}>
              <div>
                <strong>{t(`logs.${record.state}`)}</strong> ·{" "}
                <code>{record.sessionId}</code>
              </div>
              <div className="recording-path">{record.directory}</div>
              <div>
                {record.bytes} B · {t("logs.gaps")}: {record.gaps} ·{" "}
                {t("logs.rotations")}: {record.rotations}
              </div>
              {record.error && (
                <p role="alert">{t(`logs.error.${record.error}`)}</p>
              )}
              {["starting", "recording", "stopping"].includes(record.state) && (
                <button
                  type="button"
                  disabled={busy || record.state === "stopping"}
                  onClick={() =>
                    void run(() => unwrap(host.stopRecording(record.id)))
                  }
                >
                  {t("logs.stop")}
                </button>
              )}
            </li>
          ))}
        </ul>
        {error && <p role="alert">{t(error)}</p>}
      </div>
      <footer>
        <button
          type="button"
          disabled={busy}
          onClick={() => void run(() => unwrap(host.deleteRecordingFile()))}
        >
          {t("logs.delete")}
        </button>
        <button type="button" disabled={busy} onClick={onClose}>
          {t("diagnostics.close")}
        </button>
      </footer>
    </dialog>
  );
}
