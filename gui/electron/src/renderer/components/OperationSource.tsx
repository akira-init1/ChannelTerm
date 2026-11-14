import type { OperationSource as Source } from "../../shared/provenance";

/** Public correlation only. Actor and client-declared executors never determine identity. */
export function OperationSource({
  source,
  t,
}: {
  source?: Source;
  t: (id: string) => string;
}) {
  return (
    <details className="operation-source">
      <summary>
        {t(`source.${source?.kind ?? "unknown"}`)}
        {source?.name ? ` · ${source.name}` : ""}
        {source?.instanceId && (
          <span title={source.instanceId}>
            {" "}
            · {source.instanceId.slice(0, 11)}…
          </span>
        )}
        {source?.declaredEntry
          ? ` → ${source.declaredEntry} (${t("source.declared")})`
          : ""}
      </summary>
      <div>{t(`source.${source?.presence ?? "unknown"}Presence`)}</div>
      {source && (
        <dl>
          {[
            ["client_id", source.clientId],
            ["instance_id", source.instanceId],
            ["parent_instance_id", source.parentInstanceId],
            ["operation_id", source.operationId],
            ["entry", source.entry],
          ]
            .filter(([, value]) => value)
            .map(([key, value]) => (
              <div key={key}>
                <dt>{key}</dt>
                <dd>{value}</dd>
              </div>
            ))}
        </dl>
      )}
    </details>
  );
}
