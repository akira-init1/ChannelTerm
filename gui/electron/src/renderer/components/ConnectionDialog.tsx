import { useEffect, useRef, useState, type FormEvent } from "react";
import {
  isConnectionRequest,
  type ConnectionRequest,
} from "../../shared/connections";
import type { ClientError } from "../../shared/api";
import { host, unwrap } from "../host";

/** Collect explicit user intent. Credentials live in password inputs, never preferences. */
export function ConnectionDialog({
  kind,
  t,
  onOpen,
  onClose,
}: {
  kind: ConnectionRequest["kind"];
  t: (id: string) => string;
  onOpen: (request: ConnectionRequest) => Promise<void>;
  onClose: () => void;
}) {
  const dialog = useRef<HTMLDialogElement>(null);
  const submitting = useRef(false);
  const [busy, setBusy] = useState(false);
  const [scanning, setScanning] = useState(false);
  const [ports, setPorts] = useState<string[]>([]);
  const [error, setError] = useState("");
  const [auth, setAuth] = useState("key");
  useEffect(() => {
    dialog.current?.showModal();
  }, []);
  async function scan() {
    setScanning(true);
    setError("");
    try {
      setPorts(await unwrap(host.serialPorts()));
    } catch (error) {
      setError((error as ClientError).message);
    } finally {
      setScanning(false);
    }
  }
  async function submit(event: FormEvent<HTMLFormElement>) {
    event.preventDefault();
    if (submitting.current) return;
    const form = event.currentTarget;
    const data = new FormData(form);
    const value = (name: string) => String(data.get(name) ?? "");
    const request: ConnectionRequest =
      kind === "serial"
        ? {
            kind,
            port: value("port").trim(),
            baud: Number(value("baud")),
            dataBits: Number(value("dataBits")),
            parity: value("parity"),
            stopBits: value("stopBits"),
            flowControl: value("flowControl"),
            label: value("label").trim(),
          }
        : {
            kind,
            host: value("host").trim(),
            user: value("user").trim(),
            port: Number(value("port")),
            label: value("label").trim(),
            knownHostsFile: value("knownHostsFile").trim(),
            identityFiles:
              auth === "key"
                ? value("identityFiles")
                    .split(/\r?\n/)
                    .map((line) => line.trim())
                    .filter(Boolean)
                : [],
            ...(auth === "password"
              ? { password: value("password") }
              : value("keyPassphrase")
                ? { keyPassphrase: value("keyPassphrase") }
                : {}),
          };
    if (!isConnectionRequest(request)) {
      setError(t("connection.invalid"));
      return;
    }
    submitting.current = true;
    setBusy(true);
    setError("");
    // Clear DOM secrets immediately after capture, including failed attempts.
    form
      .querySelectorAll<HTMLInputElement>('input[type="password"]')
      .forEach((input) => {
        input.value = "";
      });
    try {
      await onOpen(request);
      onClose();
    } catch (error) {
      setError((error as ClientError).message);
    } finally {
      if (request.kind === "ssh") {
        request.password = undefined;
        request.keyPassphrase = undefined;
      }
      submitting.current = false;
      setBusy(false);
    }
  }
  const select = (
    name: string,
    id: string,
    options: string[],
    defaultValue: string,
  ) => (
    <label>
      {t(id)}
      <select name={name} defaultValue={defaultValue}>
        {options.map((value) => (
          <option key={value}>{value}</option>
        ))}
      </select>
    </label>
  );
  return (
    <dialog
      ref={dialog}
      className="connection-dialog"
      aria-labelledby="connection-title"
      onCancel={(event) => {
        if (submitting.current) event.preventDefault();
      }}
      onClose={onClose}
    >
      <form onSubmit={(event) => void submit(event)} autoComplete="off">
        <h2 id="connection-title">
          {t(kind === "serial" ? "connection.serial" : "connection.ssh")}
        </h2>
        <fieldset disabled={busy || scanning}>
          {kind === "serial" ? (
            <>
              <label>
                {t("connection.port")}
                <div className="connection-port">
                  <input
                    name="port"
                    list="serial-ports"
                    required
                    maxLength={512}
                  />
                  <button type="button" onClick={() => void scan()}>
                    {t(scanning ? "connection.scanning" : "connection.scan")}
                  </button>
                </div>
              </label>
              <datalist id="serial-ports">
                {ports.map((port) => (
                  <option key={port} value={port} />
                ))}
              </datalist>
              <label>
                {t("connection.baud")}
                <input
                  name="baud"
                  type="number"
                  min={1}
                  max={4000000}
                  defaultValue={115200}
                  required
                />
              </label>
              <div className="connection-grid">
                {select(
                  "dataBits",
                  "connection.dataBits",
                  ["5", "6", "7", "8"],
                  "8",
                )}
                {select(
                  "parity",
                  "connection.parity",
                  ["none", "odd", "even", "mark", "space"],
                  "none",
                )}
                {select(
                  "stopBits",
                  "connection.stopBits",
                  ["1", "1.5", "2"],
                  "1",
                )}
                {select(
                  "flowControl",
                  "connection.flowControl",
                  ["none", "software", "hardware"],
                  "none",
                )}
              </div>
            </>
          ) : (
            <>
              <div className="connection-grid">
                <label>
                  {t("connection.host")}
                  <input name="host" required maxLength={253} />
                </label>
                <label>
                  {t("connection.port")}
                  <input
                    name="port"
                    type="number"
                    min={1}
                    max={65535}
                    defaultValue={22}
                    required
                  />
                </label>
              </div>
              <label>
                {t("connection.user")}
                <input name="user" required maxLength={128} />
              </label>
              <label>
                {t("connection.auth")}
                <select
                  value={auth}
                  onChange={(event) => setAuth(event.target.value)}
                >
                  <option value="key">{t("connection.key")}</option>
                  <option value="password">{t("connection.password")}</option>
                </select>
              </label>
              {auth === "password" ? (
                <label>
                  {t("connection.password")}
                  <input
                    name="password"
                    type="password"
                    autoComplete="new-password"
                    maxLength={4096}
                  />
                </label>
              ) : (
                <>
                  <label>
                    {t("connection.identities")}
                    <textarea name="identityFiles" rows={2} maxLength={8192} />
                  </label>
                  <label>
                    {t("connection.passphrase")}
                    <input
                      name="keyPassphrase"
                      type="password"
                      autoComplete="new-password"
                      maxLength={4096}
                    />
                  </label>
                </>
              )}
              <label>
                {t("connection.knownHosts")}
                <input name="knownHostsFile" maxLength={4096} />
              </label>
              <p className="connection-hint">{t("connection.trust")}</p>
            </>
          )}
          <label>
            {t("connection.label")}
            <input name="label" maxLength={128} />
          </label>
        </fieldset>
        {error && <p role="alert">{error}</p>}
        <footer>
          <button type="button" disabled={busy} onClick={onClose}>
            {t("connection.cancel")}
          </button>
          <button className="primary" disabled={busy || scanning} type="submit">
            {t(busy ? "connection.opening" : "connection.open")}
          </button>
        </footer>
      </form>
    </dialog>
  );
}
