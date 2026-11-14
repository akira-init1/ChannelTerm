import { useEffect, useRef, useState } from "react";
import type { SessionSummary } from "../../shared/api";
import type { ControlRequest, ControlOutcome } from "../../shared/control";
import { host, unwrap } from "../host";
import { OperationSource } from "../components/OperationSource";

/** A single captured request survives selection changes and disconnection. Host alone grants input. */
export function ControlPanel({
  current,
  connected,
  t,
  refresh,
  pauseInput,
}: {
  current?: SessionSummary;
  connected: boolean;
  t: (id: string) => string;
  refresh: () => Promise<void>;
  pauseInput: (sessionId: string, version: string, paused: boolean) => void;
}) {
  const [draft, setDraft] = useState<ControlRequest>();
  const [last, setLast] = useState<ControlRequest>();
  const [outcome, setOutcome] = useState<ControlOutcome>();
  const [message, setMessage] = useState("");
  const [pending, setPending] = useState(false);
  const mounted = useRef(true);
  const busy = useRef(false);
  useEffect(() => {
    mounted.current = true;
    return () => {
      mounted.current = false;
    };
  }, []);
  // Polling is deliberately explicit. An uncertain request is only queried by its
  // original key; reconnect and tab changes never repeat a mutation.
  async function run(request: ControlRequest) {
    if (busy.current) return;
    busy.current = true;
    pauseInput(request.session_id, request.version, true);
    setPending(true);
    setDraft(undefined);
    setLast(request);
    setOutcome(undefined);
    setMessage("");
    try {
      const reply = await unwrap(host.control(request));
      if (!mounted.current) return;
      if ("result" in reply) setOutcome(reply.result);
      else setMessage("control.allowed");
      await refresh();
      if (
        mounted.current &&
        ("control" in reply || reply.result.status !== "stopping")
      )
        pauseInput(request.session_id, request.version, false);
    } catch (error) {
      if (mounted.current)
        setMessage(
          typeof error === "object" &&
            error !== null &&
            "code" in error &&
            error.code === "control_stale"
            ? "control.stale"
            : "control.unknown",
        );
    } finally {
      busy.current = false;
      if (mounted.current) setPending(false);
    }
  }
  function capture(action: "takeover" | "allow") {
    const control = current?.control;
    if (!control || !connected || pending) return;
    setDraft({
      action,
      session_id: control.session_id,
      version: control.version,
      operation_id: control.operation_id,
    });
  }
  if (!current?.control && !last && !draft) return null;
  return (
    <section className="control-panel" aria-label={t("control.title")}>
      <div className="control-actions">
        <button
          data-testid="control-takeover"
          disabled={
            !connected ||
            pending ||
            !current?.control ||
            current.state !== "open"
          }
          onClick={() => capture("takeover")}
        >
          {t(
            current?.control?.source?.kind === "agent"
              ? "control.interrupt"
              : "control.takeover",
          )}
        </button>
        <button
          data-testid="control-allow"
          disabled={
            !connected ||
            pending ||
            !current?.control?.ai_paused ||
            current.state !== "open"
          }
          onClick={() => capture("allow")}
        >
          {t("control.allow")}
        </button>
        {current?.control?.ai_paused && <span>{t("control.paused")}</span>}
      </div>
      {draft && (
        <div role="alert" data-testid="control-confirm">
          <div>
            {t(
              draft.action === "allow"
                ? "control.allowWarning"
                : "control.warning",
            )}
          </div>
          <code>
            {draft.session_id} · {draft.operation_id || "—"} · {draft.version}
          </code>
          <div>
            <button
              disabled={!connected || pending}
              onClick={() => void run(draft)}
            >
              {t("control.confirm")}
            </button>
            <button onClick={() => setDraft(undefined)}>
              {t("control.dismiss")}
            </button>
          </div>
        </div>
      )}
      {last && (
        <div role="status" data-testid="control-result">
          <code>
            {last.session_id} · {last.operation_id || "—"} · {last.version}
          </code>
          <div>
            {t(
              pending
                ? "control.processing"
                : outcome
                  ? `control.${outcome.status}`
                  : message || "control.unknown",
            )}
          </div>
          <button
            disabled={pending}
            onClick={() => {
              setLast(undefined);
              setOutcome(undefined);
            }}
          >
            {t("control.dismiss")}
          </button>
          {outcome && <OperationSource source={outcome.source} t={t} />}
          {last.action !== "allow" && (
            <button
              disabled={!connected || pending}
              onClick={() => void run({ ...last, action: "result" })}
            >
              {t("control.query")}
            </button>
          )}
        </div>
      )}
    </section>
  );
}
