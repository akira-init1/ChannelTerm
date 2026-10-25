import { useEffect, useState } from "react";
import type { ActivitySnapshot } from "../../shared/activity";
import type { ClientError } from "../../shared/api";
import { host, unwrap } from "../host";

/** Mounted only for the visible panel; old reads cannot populate a different attachment. */
export function ActivityPanel({
  attachmentId,
  t,
}: {
  attachmentId?: string;
  t: (id: string) => string;
}) {
  const [snapshot, setSnapshot] = useState<ActivitySnapshot>();
  const [error, setError] = useState("");
  useEffect(() => {
    let alive = true;
    let timer: ReturnType<typeof setTimeout>;
    setSnapshot(undefined);
    setError("");
    if (!attachmentId) return;
    const id = attachmentId;
    async function poll() {
      try {
        const next = await unwrap(host.activity(id));
        if (alive) {
          setSnapshot(next);
          setError("");
        }
      } catch (error) {
        if (alive && (error as ClientError).code !== "busy")
          setError((error as ClientError).message);
      }
      if (alive) timer = setTimeout(() => void poll(), 1000);
    }
    void poll();
    return () => {
      alive = false;
      clearTimeout(timer);
    };
  }, [attachmentId]);
  if (!attachmentId)
    return <p className="panel-status">{t("activity.select")}</p>;
  return (
    <div className="session-activity">
      <p>{t("activity.hint")}</p>
      {error && <p role="alert">{error}</p>}
      {snapshot?.dropped && <p role="status">{t("activity.dropped")}</p>}
      {snapshot?.truncated && <p role="status">{t("activity.truncated")}</p>}
      {snapshot?.rows.length === 0 && <p>{t("activity.empty")}</p>}
      <table aria-label={t("activity.title")}>
        <thead>
          <tr>
            <th>{t("activity.time")}</th>
            <th>{t("activity.actor")}</th>
            <th>{t("activity.event")}</th>
            <th>{t("activity.details")}</th>
            <th>{t("activity.origin")}</th>
          </tr>
        </thead>
        <tbody>
          {snapshot?.rows.map((row) => (
            <tr key={row.id}>
              <td title={row.timestamp}>
                {row.timestamp.slice(11, 23).replace(/Z$/, "")}
              </td>
              <td>
                {["user", "agent", "system"].includes(row.actor)
                  ? t(`activity.${row.actor}`)
                  : t("activity.other")}
              </td>
              <td>
                {row.bytes !== undefined
                  ? t("activity.write")
                  : row.clientReport
                    ? t("activity.clientReport")
                    : row.type}
              </td>
              <td>
                {row.bytes !== undefined ? `${row.bytes} B` : row.details}
              </td>
              <td>
                {t(row.history ? "activity.history" : "activity.observed")}
              </td>
            </tr>
          ))}
        </tbody>
      </table>
    </div>
  );
}
