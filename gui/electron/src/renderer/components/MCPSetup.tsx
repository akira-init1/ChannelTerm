import { useEffect, useRef, useState } from "react";
import type {
  MCPSetupClient,
  MCPSetupRequest,
  MCPSetupResult,
} from "../../shared/mcp-setup";
import { clientErrorMessageId } from "../../shared/host-recovery";
import { host, unwrap } from "../host";

/** Client setup exposes intent and public state; native Main dialogs own file previews. */
export function MCPSetupDialog({
  endpoint,
  t,
  onClose,
}: {
  endpoint: string;
  t: (id: string) => string;
  onClose: () => void;
}) {
  const dialog = useRef<HTMLDialogElement>(null);
  const discovery = useRef<Promise<MCPSetupClient[]> | undefined>(undefined);
  const [clients, setClients] = useState<MCPSetupClient[]>([]);
  const [client, setClient] = useState<MCPSetupRequest["client"]>("codex");
  const [scope, setScope] = useState<MCPSetupRequest["scope"]>("user");
  const [replace, setReplace] = useState(false);
  const [busy, setBusy] = useState(true);
  const [error, setError] = useState("");
  const [result, setResult] = useState<MCPSetupResult>();
  useEffect(() => {
    dialog.current?.showModal();
    let active = true;
    discovery.current ??= unwrap(host.mcpSetupClients());
    void discovery.current
      .then((value) => {
        if (active) setClients(value);
      })
      .catch((error) => {
        if (active) setError(clientErrorMessageId(error));
      })
      .finally(() => {
        if (active) setBusy(false);
      });
    return () => {
      active = false;
    };
  }, []);
  const run = async (action: MCPSetupRequest["action"] | "recover") => {
    if (busy) return;
    setBusy(true);
    setError("");
    setResult(undefined);
    try {
      setResult(
        await unwrap(
          action === "recover"
            ? host.mcpSetupRecover()
            : host.mcpSetup({ client, scope, replace, action }),
        ),
      );
    } catch (error) {
      setError(clientErrorMessageId(error));
    } finally {
      setBusy(false);
    }
  };
  return (
    <dialog
      ref={dialog}
      className="connection-dialog recording-dialog mcp-setup-dialog"
      aria-labelledby="mcp-setup-title"
      onClose={onClose}
      onCancel={(event) => {
        if (busy) event.preventDefault();
      }}
    >
      <h2 id="mcp-setup-title">{t("setup.open")}</h2>
      <div className="recording-content">
        <p className="connection-hint">{t("setup.intro")}</p>
        <p>
          <code>{endpoint}</code>
        </p>
        {!busy &&
          clients.length > 0 &&
          !clients.some((entry) => entry.detected) && (
            <p role="status">{t("setup.noneDetected")}</p>
          )}
        <fieldset disabled={busy}>
          <label>
            {t("setup.client")}
            <select
              value={client}
              onChange={(event) => {
                const value = event.target.value as MCPSetupRequest["client"];
                setClient(value);
                if (value === "codex") setScope("user");
                setResult(undefined);
              }}
            >
              {(["codex", "claude", "opencode", "zoo"] as const).map((id) => {
                const current = clients.find((entry) => entry.id === id);
                return (
                  <option key={id} value={id}>
                    {
                      {
                        codex: "Codex",
                        claude: "Claude Code",
                        opencode: "OpenCode",
                        zoo: "Zoo Code",
                      }[id]
                    }{" "}
                    · {current?.version ?? t("workbench.unknown")} ·{" "}
                    {t(
                      current?.detected
                        ? "setup.detected"
                        : "setup.notDetected",
                    )}
                  </option>
                );
              })}
            </select>
          </label>
          <label>
            {t("setup.scope")}
            <select
              value={scope}
              onChange={(event) => {
                setScope(event.target.value as MCPSetupRequest["scope"]);
                setResult(undefined);
              }}
            >
              <option value="user">{t("setup.user")}</option>
              <option value="project" disabled={client === "codex"}>
                {t("setup.project")}
              </option>
            </select>
          </label>
          <label className="connection-check">
            <input
              type="checkbox"
              checked={replace}
              onChange={(event) => setReplace(event.target.checked)}
            />
            {t("setup.replace")}
          </label>
          <p className="connection-hint">{t("setup.fallback")}</p>
          <p className="connection-hint">{t("setup.newCredential")}</p>
          <div className="setup-actions">
            <button type="button" onClick={() => void run("inspect")}>
              {t("setup.inspect")}
            </button>
            <button type="button" onClick={() => void run("verify")}>
              {t("setup.verify")}
            </button>
            <button type="button" onClick={() => void run("configure")}>
              {t("setup.configure")}
            </button>
          </div>
        </fieldset>
        {busy && <p role="status">{t("setup.working")}</p>}
        {result && (
          <div role="status">
            <p>
              {t(
                result.state === "failed"
                  ? `setup.error.${result.code}`
                  : `setup.result.${result.state}`,
              )}
            </p>
            {result.clientId && (
              <p>
                <code>{result.clientId}</code>
              </p>
            )}
            {result.permissions && (
              <p>{t(`setup.permissions.${result.permissions}`)}</p>
            )}
          </div>
        )}
        <p className="connection-hint">{t("setup.reload")}</p>
        {error && <p role="alert">{t(error)}</p>}
      </div>
      <footer>
        <button
          type="button"
          disabled={busy}
          onClick={() => void run("recover")}
        >
          {t("setup.recover")}
        </button>
        <button type="button" disabled={busy} onClick={onClose}>
          {t("diagnostics.close")}
        </button>
      </footer>
    </dialog>
  );
}
