import { useEffect, useRef, useState } from "react";
import type {
  LocalSelection,
  RemoteDirectory,
  TransferRequest,
} from "../../shared/transfers";
import type { ClientError } from "../../shared/api";
import { host, unwrap } from "../host";

/** Browse a snapshot in a child shell. UI navigation never changes the interactive cwd. */
export function FileDialog({
  sessionId,
  name,
  direction,
  t,
  onStart,
  onClose,
}: {
  sessionId: string;
  name: string;
  direction: "send" | "receive";
  t: (id: string) => string;
  onStart: (request: TransferRequest) => void;
  onClose: () => void;
}) {
  const dialog = useRef<HTMLDialogElement>(null);
  const inFlight = useRef(false);
  const [busy, setBusy] = useState(false);
  const [directory, setDirectory] = useState<RemoteDirectory>();
  const [path, setPath] = useState(".");
  const [selected, setSelected] = useState("");
  const [local, setLocal] = useState<LocalSelection>();
  const [error, setError] = useState("");
  useEffect(() => {
    dialog.current?.showModal();
    void browse(".");
  }, []);
  async function browse(wanted: string) {
    if (inFlight.current) return;
    inFlight.current = true;
    setBusy(true);
    setError("");
    try {
      const result = await unwrap(host.browseDirectory(sessionId, wanted));
      setDirectory(result);
      setPath(result.path);
      setSelected("");
    } catch (failure) {
      setError((failure as ClientError).message);
    } finally {
      inFlight.current = false;
      setBusy(false);
    }
  }
  async function choose(kind: "file" | "directory") {
    if (inFlight.current) return;
    inFlight.current = true;
    setBusy(true);
    setError("");
    try {
      const result = await unwrap(
        host.chooseTransferPath(sessionId, direction, kind),
      );
      if (result) setLocal(result);
    } catch (failure) {
      setError((failure as ClientError).message);
    } finally {
      inFlight.current = false;
      setBusy(false);
    }
  }
  const join = (name: string) =>
    `${directory?.path.replace(/\/$/, "")}/${name}`;
  return (
    <dialog
      ref={dialog}
      className="connection-dialog file-dialog"
      aria-label={t(`file.${direction}`)}
      onCancel={(event) => {
        event.preventDefault();
        if (!busy) onClose();
      }}
    >
      <h2>
        {t(`file.${direction}`)} · {name}
      </h2>
      <p className="file-hint">{t("file.idleHint")}</p>
      <form
        onSubmit={(event) => {
          event.preventDefault();
          void browse(path);
        }}
        className="file-path"
      >
        <button
          type="button"
          disabled={busy || !directory || directory.path === "/"}
          onClick={() =>
            void browse(directory!.path.replace(/\/[^/]+\/?$/, "") || "/")
          }
        >
          {t("file.parent")}
        </button>
        <input
          aria-label={t("file.remotePath")}
          value={path}
          disabled={busy}
          onChange={(event) => setPath(event.target.value)}
        />
        <button disabled={busy}>{t("file.open")}</button>
        <button type="button" disabled={busy} onClick={() => void browse(".")}>
          {t("file.terminalDirectory")}
        </button>
      </form>
      <div
        className="remote-entries"
        role="listbox"
        aria-label={t("file.remotePath")}
        aria-busy={busy}
      >
        {directory?.entries.map((entry) => (
          <button
            key={entry.name}
            type="button"
            role="option"
            aria-selected={selected === entry.name}
            disabled={busy || entry.kind === "other"}
            onClick={() => setSelected(entry.name)}
            onDoubleClick={() => {
              if (entry.kind === "directory") void browse(join(entry.name));
            }}
          >
            <span>{entry.kind === "directory" ? "▸" : "·"}</span>
            <span>{entry.name}</span>
            <small>{t(`file.${entry.kind}`)}</small>
          </button>
        ))}
        {busy && <p>{t("file.running")}</p>}
      </div>
      {directory?.truncated && <p>{t("file.truncated")}</p>}
      <div className="file-selection">
        {direction === "send" ? (
          <>
            <button disabled={busy} onClick={() => void choose("file")}>
              {t("file.chooseFile")}
            </button>
            <button disabled={busy} onClick={() => void choose("directory")}>
              {t("file.chooseFolder")}
            </button>
          </>
        ) : (
          <button disabled={busy} onClick={() => void choose("directory")}>
            {t("file.saveFolder")}
          </button>
        )}
        <span>{local?.name}</span>
      </div>
      {error && <p role="alert">{error}</p>}
      <footer>
        <button disabled={busy} onClick={onClose}>
          {t("connection.cancel")}
        </button>
        <button
          className="primary"
          disabled={busy || !local || !directory}
          onClick={() => {
            if (!local || !directory) return;
            onStart({
              sessionId,
              direction,
              selectionId: local.id,
              remotePath:
                direction === "send"
                  ? join(local.name)
                  : selected
                    ? join(selected)
                    : directory.path,
            });
            onClose();
          }}
        >
          {direction === "receive" && !selected
            ? t("file.receiveFolder")
            : t(`file.${direction}`)}
        </button>
      </footer>
    </dialog>
  );
}
