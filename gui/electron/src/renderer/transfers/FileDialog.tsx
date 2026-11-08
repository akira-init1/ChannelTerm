import { useEffect, useRef, useState } from "react";
import type {
  LocalSelection,
  RemoteDirectory,
  TransferRequest,
  DirectoryPreference,
} from "../../shared/transfers";
import type { ClientError } from "../../shared/api";
import { host, unwrap } from "../host";
import { DirectoryQuestion } from "./DirectoryQuestion";

/** Browse a snapshot in a child shell. UI navigation never changes the interactive cwd. */
export function FileDialog({
  sessionId,
  name,
  direction,
  t,
  onStart,
  onClose,
  initial,
}: {
  sessionId: string;
  name: string;
  direction: "send" | "receive";
  t: (id: string) => string;
  onStart: (requests: TransferRequest[]) => void;
  onClose: () => void;
  initial?: LocalSelection[];
}) {
  const dialog = useRef<HTMLDialogElement>(null);
  const inFlight = useRef(false);
  const [busy, setBusy] = useState(false);
  const [directory, setDirectory] = useState<RemoteDirectory>();
  const [path, setPath] = useState(".");
  const [selected, setSelected] = useState("");
  const [locals, setLocals] = useState<LocalSelection[]>(initial ?? []);
  const local = locals[0];
  const [preference, setPreference] = useState<DirectoryPreference>();
  const [question, setQuestion] = useState(false);
  const [automatic, setAutomatic] = useState(!!initial);
  const submitted = useRef(false);
  const alive = useRef(true);
  const [error, setError] = useState("");
  const [wire, setWire] = useState<"base64" | "raw">("base64");
  const [resumable, setResumable] = useState(false);
  useEffect(() => {
    dialog.current?.showModal();
    void browse(".");
    alive.current = true;
    void unwrap(host.transferPreference())
      .then((value) => {
        if (alive.current) setPreference(value);
      })
      .catch((e: ClientError) => {
        if (alive.current) setError(e.message);
      });
    return () => {
      alive.current = false;
    };
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
      setAutomatic(false);
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
      if (result) setLocals([result]);
    } catch (failure) {
      setError((failure as ClientError).message);
    } finally {
      inFlight.current = false;
      setBusy(false);
    }
  }
  const join = (name: string) =>
    `${directory?.path.replace(/\/$/, "")}/${name}`;
  const hasDirectory =
    direction === "send"
      ? locals.some((item) => item.kind === "directory")
      : !selected ||
        directory?.entries.find((item) => item.name === selected)?.kind ===
          "directory";
  function start(mode: "tar" | "files") {
    if (!local || !directory || submitted.current) return;
    if (hasDirectory && mode === "files" && resumable) {
      setError(t("file.modeResumeConflict"));
      setQuestion(false);
      return;
    }
    submitted.current = true;
    onStart(
      locals.map((item) => ({
        wire,
        resumable,
        directoryMode: hasDirectory ? mode : "tar",
        sessionId,
        direction,
        selectionId: item.id,
        remotePath:
          direction === "send"
            ? join(item.name)
            : selected
              ? join(selected)
              : directory.path,
      })),
    );
    onClose();
  }
  function prepare() {
    if (!preference || !local || !directory) return;
    if (hasDirectory && preference === "ask") setQuestion(true);
    else start(preference === "files" ? "files" : "tar");
  }
  useEffect(() => {
    if (automatic && !busy && directory && preference && locals.length) {
      setAutomatic(false);
      prepare();
    }
  }, [automatic, busy, directory, preference, locals]);
  return (
    <dialog
      ref={dialog}
      className="connection-dialog file-dialog"
      aria-label={t(`file.${direction}`)}
      onDragOver={(event) => {
        if (event.dataTransfer.types.includes("Files")) {
          event.preventDefault();
          event.stopPropagation();
          event.dataTransfer.dropEffect =
            direction === "send" && !busy ? "copy" : "none";
        }
      }}
      onDrop={(event) => {
        event.preventDefault();
        event.stopPropagation();
        if (direction !== "send" || inFlight.current || question) return;
        const files = Array.from(event.dataTransfer.files);
        if (!files.length) return;
        inFlight.current = true;
        setBusy(true);
        setError("");
        void unwrap(host.selectDroppedFiles(sessionId, files))
          .then((items) => {
            if (alive.current) {
              setLocals(items);
              setAutomatic(true);
            }
          })
          .catch((e: ClientError) => {
            if (alive.current) setError(e.message);
          })
          .finally(() => {
            inFlight.current = false;
            if (alive.current) setBusy(false);
          });
      }}
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
        <span>{locals.map((item) => item.name).join(", ")}</span>
      </div>
      {error && <p role="alert">{error}</p>}
      <label>
        {t("file.wire")}
        <select
          value={wire}
          disabled={busy}
          onChange={(event) =>
            setWire(event.target.value === "raw" ? "raw" : "base64")
          }
        >
          <option value="base64">Base64</option>
          <option value="raw">Raw</option>
        </select>
      </label>
      <p className="file-hint">{t("file.wireHint")}</p>
      <label className="file-resumable">
        <input
          type="checkbox"
          checked={resumable}
          disabled={busy}
          onChange={(event) => setResumable(event.target.checked)}
        />
        {t("file.resumable")}
      </label>
      {resumable && <p className="file-hint">{t("file.resumeHint")}</p>}
      <footer>
        <button disabled={busy} onClick={onClose}>
          {t("connection.cancel")}
        </button>
        <button
          className="primary"
          disabled={busy || !local || !directory || !preference}
          onClick={prepare}
        >
          {direction === "receive" && !selected
            ? t("file.receiveFolder")
            : t(`file.${direction}`)}
        </button>
      </footer>
      {question && (
        <DirectoryQuestion
          t={t}
          resumable={resumable}
          onChoose={start}
          onCancel={() => {
            if (initial) onClose();
            else setQuestion(false);
          }}
        />
      )}
    </dialog>
  );
}
