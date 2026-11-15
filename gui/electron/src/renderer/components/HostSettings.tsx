import { useEffect, useRef, useState } from "react";
import type { HostInfo, HostSettingsInfo } from "../../shared/api";
import { clientErrorMessageId } from "../../shared/host-recovery";
import { host, unwrap } from "../host";

/** Explicit local connection settings; credentials and persistence remain in Main. */
export function HostSettingsDialog({
  t,
  onApply,
  onClose,
}: {
  t: (id: string) => string;
  onApply: (endpoint: string) => Promise<HostInfo>;
  onClose: () => void;
}) {
  const dialog = useRef<HTMLDialogElement>(null);
  const [info, setInfo] = useState<HostSettingsInfo>();
  const [endpoint, setEndpoint] = useState("");
  const [error, setError] = useState("");
  const [busy, setBusy] = useState(false);
  useEffect(() => {
    dialog.current?.showModal();
    let active = true;
    void unwrap(host.hostSettings())
      .then((value) => {
        if (!active) return;
        setInfo(value);
        setEndpoint(value.host.endpoint);
      })
      .catch((e) => {
        if (active) setError(clientErrorMessageId(e));
      });
    return () => {
      active = false;
    };
  }, []);
  const unknown = t("workbench.unknown");
  const capability = (value?: boolean) =>
    !info?.connected || value === undefined
      ? unknown
      : t(value ? "host.settings.available" : "host.settings.unavailable");
  return (
    <dialog
      ref={dialog}
      className="connection-dialog host-settings-dialog"
      aria-labelledby="host-settings-title"
      onClose={onClose}
      onCancel={(event) => {
        if (busy) event.preventDefault();
      }}
    >
      <form
        onSubmit={(event) => {
          event.preventDefault();
          if (busy || !info || info.environmentOverride) return;
          setBusy(true);
          setError("");
          void onApply(endpoint)
            .then(onClose)
            .catch((e) => setError(clientErrorMessageId(e)))
            .finally(() => setBusy(false));
        }}
      >
        <h2 id="host-settings-title">{t("host.settings")}</h2>
        <div className="host-settings-content">
          {info && (
            <>
              <p role="status">
                {t(
                  info.connected
                    ? "workbench.connected"
                    : "workbench.disconnected",
                )}
              </p>
              <dl className="host-versions">
                <dt>{t("host.settings.desktop")}</dt>
                <dd>{info.desktopVersion ?? unknown}</dd>
                <dt>Electron</dt>
                <dd>{info.electronVersion ?? unknown}</dd>
                <dt>Host</dt>
                <dd>
                  {info.connected
                    ? (info.host.hostVersion ?? unknown)
                    : unknown}
                </dd>
                <dt>{t("host.settings.cli")}</dt>
                <dd>{info.cliVersion ?? unknown}</dd>
                <dt>SSH</dt>
                <dd>{capability(info.host.sshAvailable)}</dd>
                <dt>{t("host.settings.resize")}</dt>
                <dd>{capability(info.host.remoteSize)}</dd>
                <dt>{t("host.settings.transfers")}</dt>
                <dd>{capability(info.host.managedTransfers)}</dd>
              </dl>
              <label>
                {t("host.settings.endpoint")}
                <input
                  name="host-endpoint"
                  value={endpoint}
                  maxLength={2048}
                  disabled={busy || info.environmentOverride}
                  onChange={(event) => setEndpoint(event.target.value)}
                  required
                />
              </label>
              <p className="connection-hint">
                {t(
                  info.environmentOverride
                    ? "host.settings.override"
                    : "host.settings.hint",
                )}
              </p>
              <p className="connection-hint">{t("host.settings.versions")}</p>
            </>
          )}
          {error && <p role="alert">{t(error)}</p>}
        </div>
        <footer>
          <button type="button" disabled={busy} onClick={onClose}>
            {t("diagnostics.close")}
          </button>
          <button
            type="submit"
            disabled={busy || !info || info.environmentOverride}
          >
            {t("host.settings.apply")}
          </button>
        </footer>
      </form>
    </dialog>
  );
}
