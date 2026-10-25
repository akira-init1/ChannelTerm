import { useEffect, useRef, useState } from "react";
import type { SSHBrowseResult, SSHFileEntry } from "../../shared/ssh-browser";
import type { ClientError } from "../../shared/api";
import { host } from "../host";

/** A read-only directory view; choosing a path does not start a transfer. */
export function SSHBrowser({
  query,
  onSelect,
  onBusy,
  t,
}: {
  query: (operation: "ls" | "stat", path: string) => Promise<SSHBrowseResult>;
  onSelect: (path: string, kind: SSHFileEntry["kind"]) => void;
  onBusy: (busy: boolean) => void;
  t: (id: string) => string;
}) {
  const [path, setPath] = useState("/"),
    [result, setResult] = useState<SSHBrowseResult>();
  const [stat, setStat] = useState<SSHFileEntry>(),
    [busy, setBusy] = useState(false),
    [error, setError] = useState("");
  const alive = useRef(true),
    pending = useRef(false);
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
      if (alive.current) setError((error as ClientError).message);
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
    <section className="ssh-browser" aria-label={t("sshBrowser.title")}>
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
      {error && <p role="alert">{error}</p>}
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
