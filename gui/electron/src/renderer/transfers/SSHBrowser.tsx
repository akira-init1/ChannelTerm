import { clientErrorMessageId } from "../../shared/host-recovery";
import { useEffect, useRef, useState } from "react";
import type {
  SSHBrowseResult,
  SSHFileEntry,
  SSHManageOperation,
  SSHManageResult,
} from "../../shared/ssh-browser";
import { host } from "../host";

/** Browse snapshots and explicit management intent; Main confirms every mutation. */
export function SSHBrowser({
  query,
  manage,
  onSelect,
  onBusy,
  t,
  onDrop,
}: {
  query: (operation: "ls" | "stat", path: string) => Promise<SSHBrowseResult>;
  manage: (
    operation: SSHManageOperation,
    path: string,
    target?: string,
  ) => Promise<SSHManageResult>;
  onSelect: (path: string, kind: SSHFileEntry["kind"]) => void;
  onBusy: (busy: boolean) => void;
  t: (id: string) => string;
  onDrop?: (files: File[], directory: string) => void;
}) {
  const [path, setPath] = useState("/"),
    [result, setResult] = useState<SSHBrowseResult>();
  const [stat, setStat] = useState<SSHFileEntry>(),
    [busy, setBusy] = useState(false),
    [error, setError] = useState("");
  const alive = useRef(true),
    pending = useRef(false);
  const [mutation, setMutation] = useState<SSHManageOperation>();
  const [mutationPath, setMutationPath] = useState("");
  const [renameTarget, setRenameTarget] = useState("");
  const [receipt, setReceipt] = useState<SSHManageResult>();
  function prepare(operation: SSHManageOperation, target: string) {
    setMutation(operation);
    setMutationPath(target);
    setRenameTarget("");
    setReceipt(undefined);
  }
  async function apply() {
    if (pending.current || !mutation) return;
    pending.current = true;
    setBusy(true);
    onBusy(true);
    setError("");
    try {
      const result = await manage(
        mutation,
        mutationPath,
        mutation === "rename" ? renameTarget : undefined,
      );
      if (alive.current) {
        setReceipt(result);
        setMutation(undefined);
        if (result.outcome !== "not_started") {
          setResult(undefined);
          setStat(undefined);
        }
      }
    } catch (error) {
      if (alive.current) {
        setError(clientErrorMessageId(error));
        setResult(undefined);
        setStat(undefined);
      }
    } finally {
      pending.current = false;
      if (alive.current) {
        setBusy(false);
        onBusy(false);
      }
    }
  }
  useEffect(() => {
    alive.current = true;
    return () => {
      alive.current = false;
      if (pending.current) void host.cancelSSHBrowse();
      onBusy(false);
    };
  }, [onBusy]);
  async function read(operation: "ls" | "stat", target: string) {
    if (pending.current) return;
    pending.current = true;
    setBusy(true);
    onBusy(true);
    setError("");
    setStat(undefined);
    try {
      const next = await query(operation, target);
      if (alive.current) {
        if (operation === "ls") {
          setResult(next);
          setPath(next.path);
          setStat(undefined);
        } else setStat(next.entries[0]);
      }
    } catch (error) {
      if (alive.current) setError(clientErrorMessageId(error));
    } finally {
      pending.current = false;
      if (alive.current) {
        setBusy(false);
        onBusy(false);
      }
    }
  }
  const child = (name: string) => `${result!.path.replace(/\/$/, "")}/${name}`;
  return (
    <section
      className="ssh-browser"
      aria-label={t("sshBrowser.title")}
      onDrop={(e) => {
        e.preventDefault();
        e.stopPropagation();
        if (!busy && result && onDrop)
          onDrop(Array.from(e.dataTransfer.files), result.path);
      }}
    >
      <h3>{t("sshBrowser.title")}</h3>
      <p className="file-hint">{t("sshBrowser.hint")}</p>
      <label>
        {t("sshBrowser.path")}
        <input
          value={path}
          onChange={(event) => setPath(event.target.value)}
          maxLength={4096}
          disabled={busy}
          onKeyDown={(event) => {
            if (
              event.key === "Enter" &&
              !event.nativeEvent.isComposing &&
              event.keyCode !== 229
            ) {
              event.preventDefault();
              if (!busy && path.trim()) void read("ls", path);
            }
          }}
        />
      </label>
      <div className="file-selection">
        <button
          type="button"
          disabled={busy}
          onClick={() =>
            prepare("mkdir", `${(result?.path ?? path).replace(/\/$/, "")}/`)
          }
        >
          {t("sshManage.mkdir")}
        </button>
        {busy && (
          <button type="button" onClick={() => void host.cancelSSHBrowse()}>
            {t("connection.cancel")}
          </button>
        )}
        <button
          type="button"
          disabled={busy || !path.trim()}
          onClick={() => void read("ls", path)}
        >
          {t(busy ? "sshBrowser.loading" : "sshBrowser.list")}
        </button>
        <button
          type="button"
          disabled={busy || !path.trim()}
          onClick={() => void read("stat", path)}
        >
          {t("sshBrowser.stat")}
        </button>
        {result && (
          <button
            type="button"
            disabled={busy}
            onClick={() => onSelect(result.path, "directory")}
          >
            {t("sshBrowser.useDirectory")}
          </button>
        )}
      </div>
      {mutation && (
        <fieldset
          disabled={busy}
          onKeyDown={(event) => {
            // This editor is inside the transfer form; Enter must not submit that form.
            if (
              event.key === "Enter" &&
              !event.nativeEvent.isComposing &&
              event.keyCode !== 229
            )
              event.preventDefault();
          }}
        >
          <legend>{t(`sshManage.${mutation}`)}</legend>
          <label>
            {t("sshBrowser.path")}
            <input
              name="sshManagePath"
              value={mutationPath}
              maxLength={4096}
              onChange={(event) => setMutationPath(event.target.value)}
            />
          </label>
          {mutation === "rename" && (
            <label>
              {t("sshManage.target")}
              <input
                name="sshManageTarget"
                value={renameTarget}
                maxLength={4096}
                onChange={(event) => setRenameTarget(event.target.value)}
              />
            </label>
          )}
          <p className="file-hint">{t("sshManage.boundary")}</p>
          <button
            type="button"
            disabled={!mutationPath || (mutation === "rename" && !renameTarget)}
            onClick={() => void apply()}
          >
            {t("sshManage.review")}
          </button>
          <button type="button" onClick={() => setMutation(undefined)}>
            {t("connection.cancel")}
          </button>
        </fieldset>
      )}
      {receipt && (
        <p role="status">
          {t(`sshManage.${receipt.operation}`)} · {receipt.path}
          {receipt.target ? ` → ${receipt.target}` : ""}
          <br />
          {t(`sshManage.${receipt.outcome}`)}
          {receipt.reason && <> {t(`sshManage.${receipt.reason}`)}</>}
        </p>
      )}
      {error && <p role="alert">{t(error)}</p>}
      {stat && (
        <p role="status">
          {stat.name} · {t(`sshBrowser.${stat.kind}`)} · {stat.size} B ·{" "}
          {stat.mode} · {stat.modified}
        </p>
      )}
      {result && (
        <>
          <p>
            {t("sshBrowser.location")}: {result.path}
          </p>
          {result.entries.length === 0 && <p>{t("sshBrowser.empty")}</p>}
          <ul>
            {result.entries.map((entry) => (
              <li key={entry.name}>
                <span>{entry.name}</span>
                <small>
                  {t(`sshBrowser.${entry.kind}`)} · {entry.size} B ·{" "}
                  {entry.mode}
                </small>
                <div>
                  <button
                    type="button"
                    disabled={busy}
                    onClick={() => prepare("rename", child(entry.name))}
                  >
                    {t("sshManage.rename")}
                  </button>
                  <button
                    type="button"
                    disabled={busy || entry.kind === "other"}
                    onClick={() =>
                      prepare(
                        entry.kind === "directory" ? "rmdir" : "rm",
                        child(entry.name),
                      )
                    }
                  >
                    {t(
                      entry.kind === "directory"
                        ? "sshManage.rmdir"
                        : "sshManage.rm",
                    )}
                  </button>
                  {entry.kind === "directory" && (
                    <button
                      type="button"
                      disabled={busy}
                      onClick={() => void read("ls", child(entry.name))}
                    >
                      {t("sshBrowser.open")}
                    </button>
                  )}
                  <button
                    type="button"
                    disabled={busy}
                    onClick={() => void read("stat", child(entry.name))}
                  >
                    {t("sshBrowser.stat")}
                  </button>
                  <button
                    type="button"
                    disabled={
                      busy ||
                      (entry.kind !== "file" && entry.kind !== "directory")
                    }
                    onClick={() => onSelect(child(entry.name), entry.kind)}
                  >
                    {t("sshBrowser.use")}
                  </button>
                </div>
              </li>
            ))}
          </ul>
        </>
      )}
    </section>
  );
}
