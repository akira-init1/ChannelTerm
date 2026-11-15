import { useEffect, useRef, useState } from "react";
import type { Attachment } from "../../shared/api";
import {
  isUBootRequest,
  type UBootRequest,
  type UBootJob,
} from "../../shared/uboot";
import { clientErrorMessageId } from "../../shared/host-recovery";
import { host, unwrap } from "../host";

/** Board settings remain a transient draft. Main confirms and the Host executes one attempt. */
export function UBootDialog({
  attachment,
  t,
  onClose,
}: {
  attachment: Attachment;
  t: (id: string) => string;
  onClose: () => void;
}) {
  const dialog = useRef<HTMLDialogElement>(null);
  const currentJob = useRef<UBootJob | undefined>(undefined);
  const [draft, setDraft] = useState<UBootRequest>({
    mode: "wait_reset",
    rebootCommand: "",
    verifiedIdleBash: false,
    countdown: "",
    prompt: "",
    bootStarted: [""],
    interruptHex: "",
    timeoutMS: 60000,
    capture: false,
  });
  const [bootText, setBootText] = useState("");
  const [job, setJob] = useState<UBootJob>();
  const [confirming, setConfirming] = useState(false);
  const [error, setError] = useState("");
  currentJob.current = job;
  const active = confirming || (!!job && job.state !== "finished");
  useEffect(() => {
    dialog.current?.showModal();
    return () => {
      const job = currentJob.current;
      if (job && job.state !== "finished")
        void host.cancelUBoot(job.id).catch(() => {});
    };
  }, []);
  useEffect(() => {
    if (!job || job.state === "finished") return;
    let stopped = false;
    let timer: ReturnType<typeof setTimeout>;
    const poll = async () => {
      try {
        const next = await unwrap(host.ubootStatus());
        if (stopped) return;
        if (
          !next ||
          next.id !== job.id ||
          next.attachmentId !== attachment.id
        ) {
          setError("uboot.unknown");
          return;
        }
        setJob(next);
        if (next.state === "finished") return;
      } catch {
        if (!stopped) {
          setError("uboot.unknown");
          return;
        }
      }
      if (!stopped) timer = setTimeout(() => void poll(), 500);
    };
    void poll();
    return () => {
      stopped = true;
      clearTimeout(timer);
    };
  }, [job?.id, job?.state === "finished", attachment.id]);

  const start = async () => {
    if (active || !isUBootRequest(draft)) return;
    setConfirming(true);
    setError("");
    setJob(undefined);
    try {
      setJob(await unwrap(host.startUBoot(attachment.id, draft)));
    } catch (error) {
      setError(clientErrorMessageId(error));
    } finally {
      setConfirming(false);
    }
  };
  const stop = async () => {
    if (!job) return;
    try {
      await unwrap(host.cancelUBoot(job.id));
    } catch (error) {
      setError(clientErrorMessageId(error));
    }
  };
  const outcome = job?.outcome;
  return (
    <dialog
      ref={dialog}
      className="connection-dialog recording-dialog uboot-dialog"
      aria-labelledby="uboot-title"
      onClose={onClose}
      onCancel={(event) => {
        if (active && !error) event.preventDefault();
      }}
    >
      <h2 id="uboot-title">{t("uboot.open")}</h2>
      <div className="recording-content">
        <p>
          <code>{attachment.sessionId}</code>
        </p>
        <p className="connection-hint">{t("uboot.help")}</p>
        <fieldset disabled={active}>
          <label>
            {t("uboot.mode")}
            <select
              name="uboot-mode"
              value={draft.mode}
              onChange={(event) =>
                setDraft({
                  ...draft,
                  mode:
                    event.target.value === "reboot" ? "reboot" : "wait_reset",
                  rebootCommand: "",
                  verifiedIdleBash: false,
                })
              }
            >
              <option value="wait_reset">{t("uboot.waitReset")}</option>
              <option value="reboot">{t("uboot.reboot")}</option>
            </select>
          </label>
          {draft.mode === "reboot" && (
            <>
              <label>
                {t("uboot.command")}
                <input
                  name="uboot-command"
                  value={draft.rebootCommand}
                  maxLength={65536}
                  autoComplete="off"
                  onChange={(event) =>
                    setDraft({
                      ...draft,
                      rebootCommand: event.target.value,
                      verifiedIdleBash: false,
                    })
                  }
                />
              </label>
              <label className="display-checkbox">
                <input
                  type="checkbox"
                  name="uboot-idle"
                  checked={draft.verifiedIdleBash}
                  onChange={(event) =>
                    setDraft({
                      ...draft,
                      verifiedIdleBash: event.target.checked,
                    })
                  }
                />
                {t("uboot.idle")}
              </label>
            </>
          )}
          <label>
            {t("uboot.countdown")}
            <input
              name="uboot-countdown"
              value={draft.countdown}
              maxLength={256}
              autoComplete="off"
              onChange={(event) =>
                setDraft({ ...draft, countdown: event.target.value })
              }
            />
          </label>
          <label>
            {t("uboot.prompt")}
            <input
              name="uboot-prompt"
              value={draft.prompt}
              maxLength={256}
              autoComplete="off"
              onChange={(event) =>
                setDraft({ ...draft, prompt: event.target.value })
              }
            />
          </label>
          <label>
            {t("uboot.bootStarted")}
            <textarea
              name="uboot-boot-started"
              rows={3}
              maxLength={2055}
              value={bootText}
              onChange={(event) => {
                setBootText(event.target.value);
                setDraft({
                  ...draft,
                  bootStarted: event.target.value.split("\n"),
                });
              }}
            />
          </label>
          <label>
            {t("uboot.interrupt")}
            <input
              name="uboot-interrupt"
              value={draft.interruptHex}
              maxLength={64}
              autoComplete="off"
              onChange={(event) =>
                setDraft({ ...draft, interruptHex: event.target.value })
              }
            />
          </label>
          <label>
            {t("uboot.timeout")}
            <input
              name="uboot-timeout"
              type="number"
              min={1}
              max={180000}
              step={1}
              value={Number.isNaN(draft.timeoutMS) ? "" : draft.timeoutMS}
              onChange={(event) =>
                setDraft({ ...draft, timeoutMS: event.target.valueAsNumber })
              }
            />
          </label>
          <label className="display-checkbox">
            <input
              name="uboot-capture"
              type="checkbox"
              checked={draft.capture}
              onChange={(event) =>
                setDraft({ ...draft, capture: event.target.checked })
              }
            />
            {t("uboot.capture")}
          </label>
          <p className="connection-hint">{t("uboot.validation")}</p>
        </fieldset>
        <p className="connection-hint">{t("uboot.noRetry")}</p>
        {job && (
          <p role="status">
            {t(
              job.state === "finished"
                ? outcome
                  ? `uboot.result.${outcome.status}`
                  : "uboot.unknown"
                : job.state === "ready" && draft.mode === "reboot"
                  ? "uboot.rebootReady"
                  : `uboot.${job.state}`,
            )}
          </p>
        )}
        {job?.operationId && (
          <p>
            <code>{job.operationId}</code>
          </p>
        )}
        {outcome && (
          <>
            <p>
              {t("uboot.evidence")}: {outcome.bytesWritten} B ·{" "}
              {outcome.elapsedMS} ms · {outcome.outputStart}–{outcome.outputEnd}
            </p>
            {outcome.diagnosticUnavailable && (
              <p role="alert">{t("uboot.archiveFailed")}</p>
            )}
            {outcome.diagnosticId && (
              <button
                type="button"
                onClick={() =>
                  void unwrap(
                    host.saveDiagnostics({
                      kind: "existing",
                      diagnosticId: outcome.diagnosticId!,
                    }),
                  ).catch((error) => setError(clientErrorMessageId(error)))
                }
              >
                {t("uboot.saveEvidence")}
              </button>
            )}
          </>
        )}
        {(error || job?.error) && (
          <p role="alert">{t(error || clientErrorMessageId(job!.error!))}</p>
        )}
      </div>
      <footer>
        <button
          type="button"
          disabled={active || !isUBootRequest(draft)}
          onClick={() => void start()}
        >
          {t("uboot.start")}
        </button>
        {job && job.state !== "finished" && (
          <button
            type="button"
            disabled={job.state === "stopping"}
            onClick={() => void stop()}
          >
            {t("uboot.stop")}
          </button>
        )}
        <button type="button" disabled={active && !error} onClick={onClose}>
          {t("diagnostics.close")}
        </button>
      </footer>
    </dialog>
  );
}
