import { useEffect, useRef, useState, type FormEvent } from "react";
import type {
  LocalSelection,
  SSHTransferRequest,
} from "../../shared/transfers";
import type { ClientError } from "../../shared/api";
import { host, unwrap } from "../host";

/** Dedicated file credentials are entered afresh and cleared before dispatch. */
export function SSHFileDialog({
  sessionId,
  name,
  endpoint,
  direction,
  t,
  onStart,
  onClose,
}: {
  sessionId: string;
  name: string;
  endpoint: string;
  direction: "send" | "receive";
  t: (id: string) => string;
  onStart: (r: SSHTransferRequest) => void;
  onClose: () => void;
}) {
  const dialog = useRef<HTMLDialogElement>(null);
  const choosing = useRef(false);
  const [local, setLocal] = useState<LocalSelection>();
  const [busy, setBusy] = useState(false);
  const [error, setError] = useState("");
  const [protocol, setProtocol] = useState<"sftp" | "scp">("sftp");
  const [recursive, setRecursive] = useState(false);
  const [resume, setResume] = useState(false);
  useEffect(() => {
    dialog.current?.showModal();
  }, []);
  async function choose(kind: "file" | "directory") {
    if (choosing.current) return;
    choosing.current = true;
    setBusy(true);
    setError("");
    try {
      const selection = await unwrap(
        host.chooseTransferPath(sessionId, direction, kind),
      );
      if (selection) {
        setLocal(selection);
        if (direction === "send") {
          setRecursive(kind === "directory");
          if (kind === "directory") setResume(false);
        }
      }
    } catch (e) {
      setError((e as ClientError).message);
    } finally {
      choosing.current = false;
      setBusy(false);
    }
  }
  function submit(event: FormEvent<HTMLFormElement>) {
    event.preventDefault();
    if (!local || busy) return;
    const form = event.currentTarget;
    const data = new FormData(form);
    const value = (name: string) => String(data.get(name) ?? "");
    const at = endpoint.lastIndexOf("@");
    const colon = endpoint.lastIndexOf(":");
    if (at < 1 || colon < at) {
      setError(t("connection.invalid"));
      return;
    }
    const request: SSHTransferRequest = {
      sessionId,
      direction,
      selectionId: local.id,
      protocol,
      resume,
      recursive,
      remotePath: value("remotePath"),
      connection: {
        kind: "ssh",
        host: endpoint.slice(at + 1, colon).replace(/^\[|\]$/g, ""),
        user: endpoint.slice(0, at),
        port: Number(endpoint.slice(colon + 1)),
        label: "",
        identityFiles: value("identityFiles")
          .split(/\r?\n/)
          .map((s) => s.trim())
          .filter(Boolean),
        knownHostsFile: value("knownHostsFile").trim(),
        ...(value("password") ? { password: value("password") } : {}),
        ...(value("keyPassphrase")
          ? { keyPassphrase: value("keyPassphrase") }
          : {}),
      },
    };
    form
      .querySelectorAll<HTMLInputElement>('input[type="password"]')
      .forEach((input) => {
        input.value = "";
      });
    onStart(request);
    onClose();
  }
  return (
    <dialog
      ref={dialog}
      className="connection-dialog file-dialog ssh-file-dialog"
      aria-label={t(`file.${direction}`)}
      onCancel={(e) => {
        e.preventDefault();
        if (!busy) onClose();
      }}
    >
      <h2>
        {t(`file.${direction}`)} · {name}
      </h2>
      <p>{endpoint}</p>
      <p className="file-hint">{t("file.sshHint")}</p>
      <form onSubmit={submit}>
        <fieldset disabled={busy}>
          <label>
            {t("file.protocol")}
            <select
              value={protocol}
              onChange={(e) => {
                setProtocol(e.target.value === "scp" ? "scp" : "sftp");
                setResume(false);
              }}
            >
              <option value="sftp">SFTP</option>
              <option value="scp">SCP</option>
            </select>
          </label>
          <label>
            {t("file.remotePath")}
            <input
              name="remotePath"
              required
              maxLength={4096}
              placeholder="/tmp/firmware.bin"
            />
          </label>
          <div className="file-selection">
            <button
              type="button"
              onClick={() =>
                void choose(direction === "send" ? "file" : "directory")
              }
            >
              {t(direction === "send" ? "file.chooseFile" : "file.saveFolder")}
            </button>
            {direction === "send" && (
              <button type="button" onClick={() => void choose("directory")}>
                {t("file.chooseFolder")}
              </button>
            )}
            <span>{local?.name}</span>
          </div>
          {direction === "receive" && (
            <label className="file-resumable">
              <input
                type="checkbox"
                checked={recursive}
                onChange={(e) => {
                  setRecursive(e.target.checked);
                  setResume(false);
                }}
              />
              {t("file.recursive")}
            </label>
          )}
          <label className="file-resumable">
            <input
              type="checkbox"
              checked={resume}
              disabled={protocol !== "sftp" || recursive}
              onChange={(e) => setResume(e.target.checked)}
            />
            {t("file.sftpResume")}
          </label>
          <p className="file-hint">{t("file.sftpResumeHint")}</p>
          <label>
            {t("file.localIdentities")}
            <textarea name="identityFiles" rows={2} maxLength={8192} />
          </label>
          <label>
            {t("connection.password")}
            <input
              type="password"
              name="password"
              autoComplete="new-password"
              maxLength={4096}
            />
          </label>
          <label>
            {t("connection.passphrase")}
            <input
              type="password"
              name="keyPassphrase"
              autoComplete="new-password"
              maxLength={4096}
            />
          </label>
          <label>
            {t("file.localKnownHosts")}
            <input name="knownHostsFile" maxLength={4096} />
          </label>
          <p className="file-hint">{t("file.localTrust")}</p>
        </fieldset>
        {error && <p role="alert">{error}</p>}
        <footer>
          <button type="button" disabled={busy} onClick={onClose}>
            {t("connection.cancel")}
          </button>
          <button type="submit" className="primary" disabled={busy || !local}>
            {t(`file.${direction}`)}
          </button>
        </footer>
      </form>
    </dialog>
  );
}
