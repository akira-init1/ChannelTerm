import { clientErrorMessageId } from "../../shared/host-recovery";
import {
  connectionHistoryKey,
  restoreConnections,
  recordConnection,
  findConnection,
  connectionHighlightsChanged,
  maxConnectionHistoryLength,
  type ConnectionRecord,
} from "../../shared/connection-history";
import { DeviceDiscovery } from "../devices/DeviceDiscovery";
import type { DeviceSelection } from "../../shared/devices";
import { SSHTrustConfirmation } from "./SSHTrustConfirmation";
import { SerialProfilesFields } from "./SerialProfiles";
import type { SerialProfile } from "../../shared/serial-profiles";
import { useEffect, useRef, useState, type FormEvent } from "react";
import {
  isConnectionRequest,
  defaultSSHAdvanced,
  type ConnectionRequest,
} from "../../shared/connections";
import { host, unwrap } from "../host";
import {
  TerminalSettingsFields,
  readTerminalSettings,
} from "./TerminalSettings";
import type { TerminalSettings } from "../../shared/terminal-settings";
import { commonBaudRates } from "../../shared/serial-settings";
import { HighlightSettings } from "./HighlightSettings";
import type { HighlightRule } from "../../shared/highlight-rules";

/** Collect explicit user intent. Credentials live in password inputs, never preferences. */
export function ConnectionDialog({
  kind: initialKind,
  terminalDefaults,
  sshAvailable,
  sshAdvanced,
  t,
  onOpen,
  onStorageError,
  onClose,
}: {
  kind: ConnectionRequest["kind"];
  terminalDefaults: TerminalSettings;
  sshAvailable: boolean;
  sshAdvanced: boolean;
  t: (id: string) => string;
  onOpen: (
    request: ConnectionRequest,
    settings?: TerminalSettings,
  ) => Promise<void>;
  onStorageError: () => void;
  onClose: () => void;
}) {
  const [history, setHistory] = useState(() => {
    try {
      return restoreConnections(localStorage.getItem(connectionHistoryKey));
    } catch {
      return [];
    }
  });
  const [preset, setPreset] = useState<ConnectionRequest>();
  const [highlightTarget, setHighlightTarget] = useState<ConnectionRecord>();
  function editHighlights(record: ConnectionRecord) {
    try {
      const latest = restoreConnections(
        localStorage.getItem(connectionHistoryKey),
      );
      const current = findConnection(latest, record.request);
      if (!current) throw new Error("Saved connection was removed.");
      const identified = { ...current, id: current.id ?? crypto.randomUUID() };
      if (
        saveHistory(
          latest.map((entry) => (entry === current ? identified : entry)),
        )
      )
        setHighlightTarget(identified);
    } catch {
      setError("history.storageError");
      onStorageError();
    }
  }
  function saveHighlights(rules: HighlightRule[]) {
    try {
      const latest = restoreConnections(
        localStorage.getItem(connectionHistoryKey),
      );
      if (
        !highlightTarget?.id ||
        !latest.some((entry) => entry.id === highlightTarget.id)
      )
        return false;
      return saveHistory(
        latest.map((entry) =>
          entry.id === highlightTarget.id
            ? { ...entry, highlights: rules.map((rule) => ({ ...rule })) }
            : entry,
        ),
      );
    } catch {
      onStorageError();
      return false;
    }
  }
  const [revision, setRevision] = useState(0);
  const [favorite, setFavorite] = useState(false);
  function saveHistory(entries: ConnectionRecord[]) {
    try {
      const raw = JSON.stringify(entries);
      if (raw.length > maxConnectionHistoryLength)
        throw new RangeError("Connection storage limit exceeded.");
      localStorage.setItem(connectionHistoryKey, raw);
      setHistory(entries);
      window.dispatchEvent(new Event(connectionHighlightsChanged));
      return true;
    } catch {
      setError("history.storageError");
      onStorageError();
      return false;
    }
  }
  function applyPreset(request: ConnectionRequest) {
    setKind(request.kind);
    setPreset(request);
    setRevision((value) => value + 1);
    setAuth("key");
    setFavorite(false);
    if (request.kind === "serial") applySerial({ ...request, wake: false });
  }
  const sshPreset = preset?.kind === "ssh" ? preset : undefined;
  const formRef = useRef<HTMLFormElement>(null);
  const [serialDefaults, setSerialDefaults] = useState<SerialProfile>();
  const [serialRevision, setSerialRevision] = useState(0);
  const dialog = useRef<HTMLDialogElement>(null);
  const [kind, setKind] = useState(initialKind);
  const submitting = useRef(false);
  const [busy, setBusy] = useState(false);
  const [scanning, setScanning] = useState(false);
  const [ports, setPorts] = useState<string[]>([]);
  const [discoveryOpen, setDiscoveryOpen] = useState(false);
  const [discovery, setDiscovery] = useState<DeviceSelection>();
  const [discoveryCurrent, setDiscoveryCurrent] = useState(true);
  const [error, setError] = useState("");
  const [auth, setAuth] = useState("key");
  const [baudChoice, setBaudChoice] = useState("115200");
  useEffect(() => {
    dialog.current?.showModal();
  }, []);
  function readSerial(): SerialProfile {
    const data = new FormData(formRef.current!);
    const value = (name: string) => String(data.get(name) ?? "");
    return {
      port: value("port").trim(),
      baud: Number(value("baud")),
      dataBits: Number(value("dataBits")),
      parity: value("parity"),
      stopBits: value("stopBits"),
      flowControl: value("flowControl"),
      wake: data.get("wake") === "on",
    };
  }
  function applySerial(profile: SerialProfile) {
    setDiscovery(undefined);
    setDiscoveryCurrent(true);
    setSerialDefaults(profile);
    setSerialRevision((version) => version + 1);
    setBaudChoice(
      commonBaudRates.some((rate) => rate === profile.baud)
        ? String(profile.baud)
        : "custom",
    );
  }
  async function scan() {
    setScanning(true);
    setError("");
    try {
      setPorts(await unwrap(host.serialPorts()));
      setDiscoveryOpen(true);
    } catch (error) {
      setError(clientErrorMessageId(error));
    } finally {
      setScanning(false);
    }
  }
  async function submit(event: FormEvent<HTMLFormElement>) {
    event.preventDefault();
    if (
      submitting.current ||
      (kind === "serial" && discovery && !discoveryCurrent)
    )
      return;
    if (kind === "ssh" && !sshAvailable) return;
    const form = event.currentTarget;
    const data = new FormData(form);
    const value = (name: string) => String(data.get(name) ?? "");
    const request: ConnectionRequest =
      kind === "serial"
        ? {
            ...readSerial(),
            kind,
            ...(discovery ? { discovery } : {}),
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
            confirmHostKey: true,
            host: value("host").trim(),
            user: value("user").trim(),
            port: Number(value("port")),
            label: value("label").trim(),
            knownHostsFile: value("knownHostsFile").trim(),
            ...(sshAdvanced
              ? {
                  advanced: {
                    timeoutSeconds: Number(value("timeoutSeconds")),
                    keepAliveSeconds: Number(value("keepAliveSeconds")),
                    keepAliveCount: Number(value("keepAliveCount")),
                    writeTimeoutSeconds: Number(value("writeTimeoutSeconds")),
                    term: value("term"),
                    columns: Number(value("columns")),
                    rows: Number(value("rows")),
                  },
                }
              : {}),
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
    const settings = kind === "serial" ? readTerminalSettings(data) : undefined;
    if (!isConnectionRequest(request) || (kind === "serial" && !settings)) {
      setError("connection.invalid");
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
      let latest = history;
      try {
        latest = restoreConnections(localStorage.getItem(connectionHistoryKey));
      } catch {
        onStorageError();
      }
      const existing = findConnection(latest, request);
      const planned = recordConnection(
        existing ? [existing] : [],
        request,
        favorite,
        Date.now(),
        crypto.randomUUID(),
      )[0];
      if (!planned?.id)
        throw new Error("Cannot prepare local connection identity.");
      await onOpen(request, {
        ...(settings ?? terminalDefaults),
        highlightProfile: planned.id,
      });
      try {
        const updated = recordConnection(
          restoreConnections(localStorage.getItem(connectionHistoryKey)),
          request,
          favorite,
          Date.now(),
          planned.id,
        );
        saveHistory(updated);
        if (!updated.some((entry) => entry.id === planned.id)) onStorageError();
      } catch {
        onStorageError();
      }
      onClose();
    } catch (error) {
      setError(clientErrorMessageId(error));
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
          <option key={value} value={value}>
            {name === "flowControl" && value !== "none"
              ? value === "software"
                ? "XON/XOFF"
                : "RTS/CTS"
              : value}
          </option>
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
      <form
        ref={formRef}
        onSubmit={(event) => void submit(event)}
        autoComplete="off"
      >
        <h2 id="connection-title">{t("connection.new")}</h2>
        <fieldset disabled={busy || scanning}>
          <details className="connection-history">
            <summary>{t("history.title")}</summary>
            <p className="connection-hint">{t("history.hint")}</p>
            {history.map((entry, index) => (
              <div key={index}>
                <button
                  type="button"
                  disabled={entry.request.kind === "ssh" && !sshAvailable}
                  onClick={() => applyPreset(entry.request)}
                >
                  {entry.favorite ? "★ " : ""}
                  {entry.request.label ||
                    (entry.request.kind === "ssh"
                      ? `${entry.request.user}@${entry.request.host}:${entry.request.port}`
                      : entry.request.port)}
                </button>
                <small>
                  {" "}
                  {new Date(entry.lastUsed).toLocaleString()} · {entry.uses}
                </small>
                <button type="button" onClick={() => editHighlights(entry)}>
                  {t("highlight.title")}
                </button>
                {entry.request.kind === "ssh" && (
                  <button
                    type="button"
                    onClick={() =>
                      saveHistory(
                        history.map((item, i) =>
                          i === index
                            ? { ...item, favorite: !item.favorite }
                            : item,
                        ),
                      )
                    }
                  >
                    {t(
                      entry.favorite
                        ? "history.unfavorite"
                        : "history.favorite",
                    )}
                  </button>
                )}
                <button
                  type="button"
                  onClick={() =>
                    saveHistory(history.filter((_, i) => i !== index))
                  }
                >
                  {t("history.remove")}
                </button>
              </div>
            ))}
          </details>
          <label>
            {t("connection.protocol")}
            <select
              value={kind}
              onChange={(event) => {
                const next = event.target.value;
                if (next !== "serial" && (next !== "ssh" || !sshAvailable))
                  return;
                setKind(next);
                setDiscovery(undefined);
                setDiscoveryCurrent(true);
                setDiscoveryOpen(false);
                setPreset(undefined);
                setFavorite(false);
                setError("");
                setAuth("key");
                setBaudChoice("115200");
                setSerialDefaults(undefined);
              }}
            >
              <option value="serial">{t("debug.serial")}</option>
              <option value="ssh" disabled={!sshAvailable}>
                SSH
              </option>
            </select>
          </label>
        </fieldset>
        {/* Remount protocol fields so secrets and overlapping field names cannot cross forms. */}
        <fieldset key={`${kind}-${revision}`} disabled={busy || scanning}>
          {kind === "serial" ? (
            <>
              <SerialProfilesFields
                t={t}
                read={readSerial}
                apply={applySerial}
                lock={(value) => {
                  submitting.current = value;
                  setBusy(value);
                }}
              />
              <div key={serialRevision}>
                <label>
                  {t("connection.port")}
                  <div className="connection-port">
                    <input
                      name="port"
                      onChange={() => {
                        setDiscovery(undefined);
                        setDiscoveryCurrent(true);
                      }}
                      defaultValue={serialDefaults?.port ?? ""}
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
                {discoveryOpen && (
                  <DeviceDiscovery
                    selection={discovery}
                    onSelect={(selected) => {
                      const input = formRef.current?.elements.namedItem("port");
                      if (!(input instanceof HTMLInputElement)) return;
                      input.value = selected.endpoint;
                      setDiscoveryCurrent(false);
                      setDiscovery(selected);
                    }}
                    onValidity={setDiscoveryCurrent}
                    onClose={() => setDiscoveryOpen(false)}
                    t={t}
                  />
                )}
                {discovery && !discoveryCurrent && (
                  <p>{t("devices.reselect")}</p>
                )}
                <div className="connection-grid">
                  <label>
                    {t("connection.baud")}
                    <select
                      name={baudChoice === "custom" ? "baudPreset" : "baud"}
                      value={baudChoice}
                      onChange={(event) => setBaudChoice(event.target.value)}
                    >
                      {commonBaudRates.map((rate) => (
                        <option key={rate} value={rate}>
                          {rate}
                        </option>
                      ))}
                      <option value="custom">{t("connection.custom")}</option>
                    </select>
                  </label>
                  {baudChoice === "custom" && (
                    <label>
                      {t("connection.customBaud")}
                      <input
                        name="baud"
                        type="number"
                        min={1}
                        max={4000000}
                        step={1}
                        defaultValue={serialDefaults?.baud ?? 115200}
                        required
                      />
                    </label>
                  )}
                </div>
                <div className="connection-grid">
                  {select(
                    "dataBits",
                    "connection.dataBits",
                    ["5", "6", "7", "8"],
                    String(serialDefaults?.dataBits ?? 8),
                  )}
                  {select(
                    "parity",
                    "connection.parity",
                    ["none", "odd", "even", "mark", "space"],
                    serialDefaults?.parity ?? "none",
                  )}
                  {select(
                    "stopBits",
                    "connection.stopBits",
                    ["1", "1.5", "2"],
                    serialDefaults?.stopBits ?? "1",
                  )}
                  {select(
                    "flowControl",
                    "connection.flowControl",
                    ["none", "software", "hardware"],
                    serialDefaults?.flowControl ?? "none",
                  )}
                </div>
                <label className="display-checkbox">
                  <input type="checkbox" name="wake" />
                  {t("profiles.wake")}
                </label>
                <p className="connection-hint">{t("profiles.wakeHint")}</p>
              </div>
            </>
          ) : (
            <>
              <div className="connection-grid">
                <label>
                  {t("connection.host")}
                  <input
                    name="host"
                    defaultValue={sshPreset?.host}
                    required
                    maxLength={253}
                  />
                </label>
                <label>
                  {t("connection.port")}
                  <input
                    name="port"
                    type="number"
                    min={1}
                    max={65535}
                    defaultValue={sshPreset?.port ?? 22}
                    required
                  />
                </label>
              </div>
              <label>
                {t("connection.user")}
                <input
                  name="user"
                  defaultValue={sshPreset?.user}
                  required
                  maxLength={128}
                />
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
              {sshAdvanced && (
                <details>
                  <summary>{t("ssh.advanced")}</summary>
                  <p className="connection-hint">{t("ssh.advancedHint")}</p>
                  <div className="connection-grid">
                    {(
                      [
                        ["timeoutSeconds", 1, 120],
                        ["keepAliveSeconds", 0, 3600],
                        ["keepAliveCount", 1, 10],
                        ["writeTimeoutSeconds", 0, 3600],
                        ["columns", 1, 1000],
                        ["rows", 1, 1000],
                      ] as const
                    ).map(([name, min, max]) => (
                      <label key={name}>
                        {t(`ssh.${name}`)}
                        <input
                          name={name}
                          type="number"
                          min={min}
                          max={max}
                          step={1}
                          required
                          defaultValue={
                            sshPreset?.advanced?.[name] ??
                            defaultSSHAdvanced[name]
                          }
                        />
                      </label>
                    ))}
                  </div>
                  <label>
                    {t("ssh.term")}
                    <input
                      name="term"
                      maxLength={64}
                      pattern={"[a-zA-Z0-9_.+\\-]+"}
                      required
                      defaultValue={
                        sshPreset?.advanced?.term ?? defaultSSHAdvanced.term
                      }
                    />
                  </label>
                </details>
              )}
            </>
          )}
          <label>
            {t("connection.label")}
            <input name="label" defaultValue={preset?.label} maxLength={128} />
          </label>
          {kind === "ssh" && (
            <label className="display-checkbox">
              <input
                type="checkbox"
                checked={favorite}
                onChange={(event) => setFavorite(event.target.checked)}
              />
              {t("history.favorite")}
            </label>
          )}
          {kind === "serial" && (
            <TerminalSettingsFields t={t} value={terminalDefaults} />
          )}
        </fieldset>
        {error && <p role="alert">{t(error)}</p>}
        {kind === "ssh" && busy && <SSHTrustConfirmation t={t} />}
        <footer>
          <button type="button" disabled={busy} onClick={onClose}>
            {t("connection.cancel")}
          </button>
          <button
            className="primary"
            disabled={
              busy ||
              scanning ||
              (kind === "serial" && !!discovery && !discoveryCurrent)
            }
            type="submit"
          >
            {t(busy ? "connection.opening" : "connection.open")}
          </button>
        </footer>
      </form>
      {highlightTarget && (
        <HighlightSettings
          label={
            highlightTarget.request.label ||
            (highlightTarget.request.kind === "serial"
              ? highlightTarget.request.port
              : highlightTarget.request.host)
          }
          initial={highlightTarget.highlights ?? []}
          t={t}
          onSave={saveHighlights}
          onClose={() => setHighlightTarget(undefined)}
        />
      )}
    </dialog>
  );
}
