import { useEffect, useRef, useState, type FormEvent } from "react";
import type {
  LocalSelection,
  SSHTransferRequest,
} from "../../shared/transfers";
import type { ClientError } from "../../shared/api";
import { host, unwrap } from "../host";
import { SSHBrowser } from "./SSHBrowser";

/** Credentials stay in the live form for explicit browsing and are cleared before transfer dispatch. */
export function SSHFileDialog({
  sessionId,
  name,
  endpoint,
  direction,
  t,
  onStart,
  onClose,
  initial,
}: {
  sessionId: string;
  name: string;
  endpoint: string;
  direction: "send" | "receive";
  t: (id: string) => string;
  onStart: (r: SSHTransferRequest[]) => void;
  onClose: () => void;
  initial?: LocalSelection[];
}) {
  const dialog = useRef<HTMLDialogElement>(null);
  const formRef = useRef<HTMLFormElement>(null);
  const choosing = useRef(false);
  const [locals, setLocals] = useState<LocalSelection[]>(initial ?? []);
  const local = locals[0];
  const [dropped, setDropped] = useState(!!initial);
  const [busy, setBusy] = useState(false);
  const [error, setError] = useState("");
  const [protocol, setProtocol] = useState<"sftp" | "scp">("sftp");
  const [recursive, setRecursive] = useState(
    initial?.some((item) => item.kind === "directory") ?? false,
  );
  const [resume, setResume] = useState(false);
  const [browserOpen, setBrowserOpen] = useState(false);
  const [browsing, setBrowsing] = useState(false);
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
        setLocals([selection]);
        setDropped(false);
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
  function readConnection(
    form: HTMLFormElement,
  ): SSHTransferRequest["connection"] {
    const data = new FormData(form);
    const value = (name: string) => String(data.get(name) ?? "");
    const at = endpoint.lastIndexOf("@");
    const colon = endpoint.lastIndexOf(":");
    if (at < 1 || colon < at) {
      throw new Error(t("connection.invalid"));
    }
    return {
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
    };
  }
  function submit(event: FormEvent<HTMLFormElement>) {
    event.preventDefault();
    if (!local || busy || browsing) return;
    const form = event.currentTarget;
    let connection: SSHTransferRequest["connection"];
    try {
      connection = readConnection(form);
    } catch (error) {
      setError((error as Error).message);
      return;
    }
    const request: SSHTransferRequest = {
      sessionId,
      direction,
      selectionId: local.id,
      protocol,
      resume,
      recursive,
      remotePath: String(new FormData(form).get("remotePath") ?? ""),
      connection,
    };
    form
      .querySelectorAll<HTMLInputElement>('input[type="password"]')
      .forEach((input) => {
        input.value = "";
      });
    onStart(
      locals.map((item) => ({
        ...request,
        selectionId: item.id,
        recursive: direction === "send" ? item.kind === "directory" : recursive,
        resume: locals.length === 1 && resume,
        remotePath:
          direction === "send" && dropped
            ? `${request.remotePath.replace(/\/$/, "")}/${item.name}`
            : request.remotePath,
      })),
    );
    onClose();
  }
  async function drop(files: File[], remoteDirectory?: string) {
    if (
      direction !== "send" ||
      choosing.current ||
      busy ||
      browsing ||
      !files.length
    )
      return;
    choosing.current = true;
    setBusy(true);
    setError("");
    try {
      const items = await unwrap(host.selectDroppedFiles(sessionId, files));
      setLocals(items);
      setDropped(true);
      setRecursive(items.some((item) => item.kind === "directory"));
      setResume(false);
      const input = formRef.current?.elements.namedItem("remotePath");
      if (remoteDirectory && input instanceof HTMLInputElement)
        input.value = remoteDirectory;
    } catch (e) {
      setError((e as ClientError).message);
    } finally {
      choosing.current = false;
      setBusy(false);
    }
  }
  return (
    <dialog
      ref={dialog}
      className="connection-dialog file-dialog ssh-file-dialog"
      aria-label={t(`file.${direction}`)}
      onDragOver={(e) => {
        if (e.dataTransfer.types.includes("Files")) {
          e.preventDefault();
          e.stopPropagation();
          e.dataTransfer.dropEffect =
            direction === "send" && !busy && !browsing ? "copy" : "none";
        }
      }}
      onDrop={(e) => {
        e.preventDefault();
        e.stopPropagation();
        void drop(Array.from(e.dataTransfer.files));
      }}
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
      {dropped && <p>{t("file.dropDestination")}</p>}
      <form ref={formRef} onSubmit={submit}>
        <fieldset disabled={busy || browsing}>
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
            <span>{locals.map((item) => item.name).join(", ")}</span>
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
              disabled={protocol !== "sftp" || recursive || locals.length > 1}
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
          {protocol === "sftp" && (
            <button
              type="button"
              onClick={() => setBrowserOpen((value) => !value)}
            >
              {t(browserOpen ? "sshBrowser.hide" : "sshBrowser.title")}
            </button>
          )}
        </fieldset>
        {browserOpen && protocol === "sftp" && (
          <SSHBrowser
            onDrop={(files, path) => {
              void drop(files, path);
            }}
            t={t}
            onBusy={setBrowsing}
            query={(operation, path) =>
              unwrap(
                host.browseSSH({
                  sessionId,
                  operation,
                  path,
                  connection: readConnection(formRef.current!),
                }),
              )
            }
            onSelect={(path, kind) => {
              const field = formRef.current?.elements.namedItem("remotePath");
              if (field instanceof HTMLInputElement) field.value = path;
              if (direction === "receive") {
                setRecursive(kind === "directory");
                if (kind === "directory") setResume(false);
              }
            }}
          />
        )}
        {error && <p role="alert">{error}</p>}
        <footer>
          <button type="button" disabled={busy} onClick={onClose}>
            {t("connection.cancel")}
          </button>
          <button
            type="submit"
            className="primary"
            disabled={busy || browsing || !local}
          >
            {t(`file.${direction}`)}
          </button>
        </footer>
      </form>
    </dialog>
  );
}
