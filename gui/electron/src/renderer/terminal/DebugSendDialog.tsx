import { useEffect, useRef, useState } from "react";
import type { Attachment } from "../../shared/api";
import type { CommandSnippet, DebugPayload } from "../../shared/debug-send";
import { clientErrorMessageId } from "../../shared/host-recovery";
import { host, unwrap } from "../host";

/** Drafts live only in this dialog. Loading or saving a snippet never sends it. */
export function DebugSendDialog({
  attachment,
  t,
  onClose,
}: {
  attachment: Attachment;
  t: (id: string) => string;
  onClose: () => void;
}) {
  const dialog = useRef<HTMLDialogElement>(null);
  const [payload, setPayload] = useState<DebugPayload>({
    mode: "text",
    data: "",
    ending: "none",
  });
  const [entries, setEntries] = useState<CommandSnippet[]>([]);
  const [name, setName] = useState("");
  const [consent, setConsent] = useState(false);
  const [busy, setBusy] = useState(false);
  const [status, setStatus] = useState("");
  const [error, setError] = useState("");
  useEffect(() => {
    dialog.current?.showModal();
    let active = true;
    void unwrap(host.snippets())
      .then((value) => {
        if (active) setEntries(value);
      })
      .catch((error) => {
        if (active) setError(clientErrorMessageId(error));
      });
    return () => {
      active = false;
    };
  }, []);
  const run = async (action: "send" | "save" | "remove") => {
    if (busy) return;
    setBusy(true);
    setStatus("");
    setError("");
    try {
      if (action === "send") {
        const result = await unwrap(host.debugSend(attachment.id, payload));
        setStatus(
          result.sent
            ? `${t("send.sent")} ${result.bytes} B`
            : t("send.cancelled"),
        );
      } else {
        const next = await unwrap(
          host.changeSnippet(
            action === "save"
              ? {
                  action: "save",
                  snippet: { ...payload, name: name.trim() },
                  confirmPlaintext: true,
                }
              : { action: "remove", name },
          ),
        );
        setEntries(next);
        setConsent(false);
        setStatus(t(action === "save" ? "send.saved" : "send.removed"));
      }
    } catch (error) {
      setError(clientErrorMessageId(error));
    } finally {
      setBusy(false);
    }
  };
  return (
    <dialog
      ref={dialog}
      className="connection-dialog recording-dialog debug-send-dialog"
      onClose={onClose}
      onCancel={(event) => {
        if (busy) event.preventDefault();
      }}
      aria-labelledby="debug-send-title"
    >
      <h2 id="debug-send-title">{t("send.open")}</h2>
      <div className="recording-content">
        <p>
          <code>{attachment.sessionId}</code>
        </p>
        <p className="connection-hint">{t("send.help")}</p>
        <fieldset disabled={busy}>
          <label>
            {t("send.mode")}
            <select
              name="debug-format"
              value={payload.mode}
              onChange={(event) =>
                setPayload({
                  ...payload,
                  mode: event.target.value as DebugPayload["mode"],
                })
              }
            >
              <option value="text">{t("send.text")}</option>
              <option value="hex">HEX</option>
            </select>
          </label>
          <label>
            {t("send.data")}
            <textarea
              name="debug-content"
              value={payload.data}
              maxLength={16384}
              rows={5}
              spellCheck={false}
              onChange={(event) =>
                setPayload({ ...payload, data: event.target.value })
              }
            />
          </label>
          <label>
            {t("send.ending")}
            <select
              name="debug-ending"
              value={payload.ending}
              onChange={(event) =>
                setPayload({
                  ...payload,
                  ending: event.target.value as DebugPayload["ending"],
                })
              }
            >
              <option value="none">{t("send.none")}</option>
              <option value="lf">LF (0A)</option>
              <option value="cr">CR (0D)</option>
              <option value="crlf">CRLF (0D 0A)</option>
            </select>
          </label>
          <button
            type="button"
            disabled={!payload.data}
            onClick={() => void run("send")}
          >
            {t("send.preview")}
          </button>
          <p className="connection-hint">{t("send.noRetry")}</p>
          <label>
            {t("send.snippets")}
            <select
              value=""
              onChange={(event) => {
                const entry = entries.find(
                  (row) => row.name === event.target.value,
                );
                if (entry) {
                  setPayload({
                    mode: entry.mode,
                    data: entry.data,
                    ending: entry.ending,
                  });
                  setName(entry.name);
                  setConsent(false);
                  setStatus(t("send.loaded"));
                }
              }}
            >
              <option value="">{t("send.choose")}</option>
              {entries.map((entry) => (
                <option key={entry.name} value={entry.name}>
                  {entry.name}
                </option>
              ))}
            </select>
          </label>
          <label>
            {t("send.name")}
            <input
              maxLength={64}
              value={name}
              onChange={(event) => {
                setName(event.target.value);
                setConsent(false);
              }}
            />
          </label>
          <label className="connection-check">
            <input
              type="checkbox"
              checked={consent}
              onChange={(event) => setConsent(event.target.checked)}
            />
            {t("send.plaintext")}
          </label>
          <div className="setup-actions">
            <button
              type="button"
              disabled={!consent || !name.trim() || !payload.data}
              onClick={() => void run("save")}
            >
              {t("send.save")}
            </button>
            <button
              type="button"
              disabled={!entries.some((row) => row.name === name)}
              onClick={() => void run("remove")}
            >
              {t("send.remove")}
            </button>
          </div>
        </fieldset>
        {status && <p role="status">{status}</p>}
        {error && <p role="alert">{t(error)}</p>}
      </div>
      <footer>
        <button type="button" disabled={busy} onClick={onClose}>
          {t("diagnostics.close")}
        </button>
      </footer>
    </dialog>
  );
}
