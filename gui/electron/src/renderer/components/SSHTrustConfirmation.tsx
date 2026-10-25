import { useEffect, useRef, useState } from "react";
import { isSSHTrustPrompt, type SSHTrustPrompt } from "../../shared/ssh-trust";
import type { ClientError } from "../../shared/api";
import { host, unwrap } from "../host";

/** Display a Main-owned pending identity; approval never supplies replacement key material. */
export function SSHTrustConfirmation({ t }: { t: (id: string) => string }) {
  const [prompt, setPrompt] = useState<SSHTrustPrompt>();
  const [busy, setBusy] = useState(false);
  const [error, setError] = useState("");
  const rejectButton = useRef<HTMLButtonElement>(null);
  useEffect(() => {
    rejectButton.current?.focus();
  }, [prompt?.id]);
  useEffect(() => {
    let alive = true;
    let timer: ReturnType<typeof setTimeout>;
    async function poll() {
      try {
        const result = await unwrap(host.sshTrustPrompt());
        if (alive) setPrompt(isSSHTrustPrompt(result) ? result : undefined);
      } catch {
        if (alive) setPrompt(undefined);
      }
      if (alive) timer = setTimeout(() => void poll(), 250);
    }
    void poll();
    return () => {
      alive = false;
      clearTimeout(timer);
    };
  }, []);
  if (!prompt) return null;
  async function decide(accept: boolean) {
    if (!prompt || busy) return;
    setBusy(true);
    setError("");
    try {
      await unwrap(host.resolveSSHTrust(prompt.id, accept));
      setPrompt(undefined);
    } catch (error) {
      setError((error as ClientError).message);
    } finally {
      setBusy(false);
    }
  }
  return (
    <section
      role="alertdialog"
      aria-modal="true"
      aria-label={t("ssh.trustTitle")}
      className="ssh-trust-confirmation"
    >
      <h3>{t("ssh.trustTitle")}</h3>
      <p>{t("ssh.trustHint")}</p>
      <dl>
        <dt>{t("ssh.trustAddress")}</dt>
        <dd>{prompt.address}</dd>
        <dt>{t("ssh.trustAlgorithm")}</dt>
        <dd>{prompt.algorithm}</dd>
        <dt>{t("ssh.trustFingerprint")}</dt>
        <dd>
          <code>{prompt.fingerprint}</code>
        </dd>
      </dl>
      {error && <p role="alert">{error}</p>}
      <button
        ref={rejectButton}
        type="button"
        disabled={busy}
        onClick={() => void decide(false)}
      >
        {t("ssh.trustReject")}
      </button>
      <button type="button" disabled={busy} onClick={() => void decide(true)}>
        {t("ssh.trustAccept")}
      </button>
    </section>
  );
}
