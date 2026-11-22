import { useEffect, useState } from "react";
import type { DeviceSelection, DeviceDecision } from "../../shared/devices";
import { clientErrorMessageId } from "../../shared/host-recovery";
import { host, unwrap } from "../host";
import { observeDevices, type DeviceObservation } from "./observations";

/** Read-only polling exists only while this view is mounted; selecting a row never opens a port. */
export function DeviceDiscovery({
  selection,
  onSelect,
  onValidity,
  onClose,
  t,
}: {
  selection?: DeviceSelection;
  onSelect: (selected: DeviceSelection) => void;
  onValidity: (current: boolean) => void;
  onClose: () => void;
  t: (id: string) => string;
}) {
  const [snapshot, setSnapshot] = useState<{
    rows: DeviceObservation[];
    truncated: boolean;
  }>({ rows: [], truncated: false });
  const [error, setError] = useState("");
  const [stale, setStale] = useState(true);
  const [refreshing, setRefreshing] = useState(false);
  const [revision, setRevision] = useState(0);
  const [decision, setDecision] = useState<DeviceDecision>();
  useEffect(() => {
    let disposed = false;
    let timer: ReturnType<typeof setTimeout> | undefined;
    setDecision(undefined);
    if (selection) onValidity(false);
    async function refresh() {
      setRefreshing(true);
      try {
        const discovery = await unwrap(host.devices());
        const devices = discovery.devices;
        if (disposed) return;
        let nextDecision: DeviceDecision | undefined;
        let issue = "";
        let current =
          discovery.state === "current" &&
          (!selection ||
            devices.some(
              (device) =>
                device.id === selection.deviceId &&
                device.endpoint === selection.endpoint &&
                device.transport === "serial",
            ));
        if (selection && current) {
          try {
            nextDecision = await unwrap(host.deviceDecision(selection));
            current = nextDecision.present;
          } catch (error) {
            issue = clientErrorMessageId(error);
            current = false;
          }
        }
        if (disposed) return;
        if (discovery.state === "current")
          setSnapshot((previous) => observeDevices(previous.rows, devices));
        else
          setSnapshot((previous) =>
            previous.rows.length ? previous : observeDevices([], devices),
          );
        setDecision(nextDecision);
        setError(
          issue ||
            (discovery.state === "current" ? "" : "recovery.discovery_stale"),
        );
        setStale(discovery.state !== "current");
        onValidity(current);
      } catch (error) {
        if (!disposed) {
          setError(clientErrorMessageId(error));
          setStale(true);
          setDecision(undefined);
          onValidity(!selection);
        }
      } finally {
        if (!disposed) {
          setRefreshing(false);
          // Schedule after completion, so a slow Host cannot accumulate reads.
          timer = setTimeout(() => void refresh(), 2000);
        }
      }
    }
    void refresh();
    return () => {
      disposed = true;
      clearTimeout(timer);
    };
  }, [selection?.deviceId, selection?.endpoint, revision, onValidity]);
  return (
    <section className="device-discovery" aria-label={t("devices.title")}>
      <div className="device-toolbar">
        <strong>{t("devices.title")}</strong>
        <button
          type="button"
          disabled={refreshing}
          onClick={() => setRevision((value) => value + 1)}
        >
          {t("devices.refresh")}
        </button>
        <button type="button" onClick={onClose}>
          {t("devices.hide")}
        </button>
      </div>
      {error && <p role="alert">{t(error)}</p>}
      {stale && <p>{t("devices.stale")}</p>}
      {!stale && !snapshot.rows.length && <p>{t("devices.empty")}</p>}
      {snapshot.truncated && <p>{t("devices.limit")}</p>}
      <ul className="device-observations">
        {snapshot.rows.map((device) => (
          <li
            key={
              device.id || JSON.stringify([device.transport, device.endpoint])
            }
          >
            <div className="device-heading">
              <strong>{device.endpoint}</strong>
              <span className="device-presence">
                {t(
                  stale
                    ? "devices.unknown"
                    : device.present
                      ? "devices.present"
                      : "devices.absent",
                )}
              </span>
              <button
                type="button"
                disabled={
                  stale ||
                  !device.present ||
                  device.transport !== "serial" ||
                  !device.id
                }
                onClick={() =>
                  onSelect({ deviceId: device.id, endpoint: device.endpoint })
                }
              >
                {t("devices.select")}
              </button>
            </div>
            <p>{device.product || device.manufacturer || device.transport}</p>
            {device.previousEndpoint && (
              <p>
                {t("devices.previous")}: {device.previousEndpoint}
              </p>
            )}
            <details>
              <summary>{t("devices.details")}</summary>
              <p>
                {t(`devices.identity.${device.identity}`)} ?{" "}
                {t(
                  device.persistent
                    ? "devices.persistent"
                    : "devices.runtimeOnly",
                )}
              </p>
              <code>{device.id || t("devices.unknown")}</code>
              <dl>
                <dt>VID / PID</dt>
                <dd>
                  {device.vid || "—"} / {device.pid || "—"}
                </dd>
                <dt>{t("devices.usbSerial")}</dt>
                <dd>{device.usbSerial || "—"}</dd>
                <dt>{t("devices.usbPath")}</dt>
                <dd>{device.usbPath || "—"}</dd>
                <dt>{t("devices.lastSeen")}</dt>
                <dd>{device.lastSeen}</dd>
              </dl>
            </details>
          </li>
        ))}
      </ul>
      {selection && (
        <div className="device-decision">
          <strong>{selection.endpoint}</strong>
          <code>{selection.deviceId}</code>
          {decision ? (
            <>
              <p>
                {t("devices.policy")}: {t(`devices.policy.${decision.policy}`)}{" "}
                · {t("devices.action")}:{" "}
                {t(`devices.action.${decision.action}`)}
              </p>
              <p>
                {t(
                  !decision.present
                    ? "devices.absent"
                    : decision.connected
                      ? "devices.connected"
                      : "devices.externalUnknown",
                )}
              </p>
              {decision.sessionId && (
                <p>
                  <code>{decision.sessionId}</code> · {decision.sessionRef}
                </p>
              )}
            </>
          ) : (
            <p>{t("devices.reselect")}</p>
          )}
          <p className="connection-hint">{t("devices.explicit")}</p>
        </div>
      )}
      <p className="connection-hint">{t("devices.boundary")}</p>
    </section>
  );
}
