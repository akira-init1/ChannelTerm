import { useEffect, useRef, useState } from "react";
import type { TransferCheckpoint } from "../../shared/transfers";
import type { ClientError } from "../../shared/api";
import { host, unwrap } from "../host";

/** A saved job is intent, not a live Session. Main revalidates both at execution. */
export function ResumeDialog({
  sessionId,
  name,
  t,
  onResume,
  onClose,
}: {
  sessionId: string;
  name: string;
  t: (id: string) => string;
  onResume: (sessionId: string, id: string) => void;
  onClose: () => void;
}) {
  const dialog = useRef<HTMLDialogElement>(null);
  const [jobs, setJobs] = useState<TransferCheckpoint[]>([]);
  const [busy, setBusy] = useState(true);
  const [error, setError] = useState("");
  const submitted = useRef(false);
  useEffect(() => {
    dialog.current?.showModal();
    let stopped = false;
    void unwrap(host.transferCheckpoints(sessionId))
      .then((result) => {
        if (!stopped) setJobs(result);
      })
      .catch((failure: ClientError) => {
        if (!stopped) setError(failure.message);
      })
      .finally(() => {
        if (!stopped) setBusy(false);
      });
    return () => {
      stopped = true;
    };
  }, [sessionId]);
  return (
    <dialog
      ref={dialog}
      className="connection-dialog file-dialog"
      aria-label={t("file.resume")}
      onCancel={(event) => {
        event.preventDefault();
        onClose();
      }}
    >
      <h2>
        {t("file.resume")} · {name}
      </h2>
      <p className="file-hint">{t("file.resumeHint")}</p>
      {busy && <p>{t("file.loading")}</p>}
      {error && <p role="alert">{error}</p>}
      {!busy && !error && jobs.length === 0 && <p>{t("file.noCheckpoints")}</p>}
      <div className="resume-jobs">
        {jobs.map((job) => (
          <section key={job.id} className="resume-job">
            <strong>{job.name}</strong>
            <span>
              {t(`file.${job.direction}`)} · {t(`file.${job.kind}`)} ·{" "}
              {job.offset.toLocaleString()} / {job.total.toLocaleString()} B
            </span>
            {job.phase === "publishing" ? (
              <p role="status">{t("file.publicationUncertain")}</p>
            ) : (
              <button
                disabled={busy}
                onClick={() => {
                  if (submitted.current) return;
                  submitted.current = true;
                  onResume(sessionId, job.id);
                }}
              >
                {t("file.resume")}
              </button>
            )}
          </section>
        ))}
      </div>
      <footer>
        <button onClick={onClose}>{t("connection.cancel")}</button>
      </footer>
    </dialog>
  );
}
