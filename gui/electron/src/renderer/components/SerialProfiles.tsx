import { useRef, useState } from "react";
import type { ClientError } from "../../shared/api";
import {
  isProfileName,
  isSerialProfile,
  type SerialProfile,
  type SerialProfiles,
  type SerialProfileChange,
} from "../../shared/serial-profiles";
import { host, unwrap } from "../host";

/** Profile edits use the Host's CLI configuration; no browser persistence. */
export function SerialProfilesFields({
  t,
  read,
  apply,
  lock,
}: {
  t: (id: string) => string;
  read: () => SerialProfile;
  apply: (profile: SerialProfile) => void;
  lock: (busy: boolean) => void;
}) {
  const [snapshot, setSnapshot] = useState<SerialProfiles>();
  const [selected, setSelected] = useState("");
  const [name, setName] = useState("");
  const [replace, setReplace] = useState(false);
  const [notice, setNotice] = useState("");
  const pending = useRef(false);
  async function run(change?: SerialProfileChange) {
    if (pending.current) return;
    pending.current = true;
    lock(true);
    setNotice("");
    try {
      if (change) await unwrap(host.changeSerialProfile(change));
      setSnapshot(await unwrap(host.serialProfiles()));
      setReplace(false);
      if (change) setNotice(t("profiles.saved"));
    } catch (error) {
      setNotice((error as ClientError).message);
    } finally {
      pending.current = false;
      lock(false);
    }
  }
  return (
    <details className="serial-profiles">
      <summary>{t("profiles.title")}</summary>
      <p className="connection-hint">{t("profiles.hint")}</p>
      <button type="button" onClick={() => void run()}>
        {t("profiles.load")}
      </button>
      {snapshot && (
        <>
          <label>
            {t("profiles.select")}
            <select
              aria-label={t("profiles.select")}
              value={selected}
              onChange={(event) => {
                const name = event.target.value;
                const entry = snapshot.profiles.find(
                  (profile) => profile.name === name,
                );
                setSelected(name);
                setName(name);
                setReplace(false);
                if (entry) apply(entry.profile);
              }}
            >
              <option value="">—</option>
              {snapshot.profiles.map((entry) => (
                <option key={entry.name} value={entry.name}>
                  {entry.name}
                  {entry.name === snapshot.default
                    ? ` (${t("profiles.default")})`
                    : ""}
                </option>
              ))}
            </select>
          </label>
          <button
            type="button"
            disabled={!selected || selected === snapshot.default}
            onClick={() => void run({ action: "default", name: selected })}
          >
            {t("profiles.setDefault")}
          </button>
        </>
      )}
      <label>
        {t("profiles.name")}
        <input
          aria-label={t("profiles.name")}
          maxLength={128}
          value={name}
          onChange={(event) => {
            setName(event.target.value);
            setReplace(false);
          }}
        />
      </label>
      <label className="display-checkbox">
        <input
          type="checkbox"
          checked={replace}
          onChange={(event) => setReplace(event.target.checked)}
        />
        {t("profiles.replace")}
      </label>
      <button
        type="button"
        onClick={() => {
          const profile = read();
          if (!isProfileName(name) || !isSerialProfile(profile)) {
            setNotice(t("connection.invalid"));
            return;
          }
          void run({ action: "save", name, profile, replace });
        }}
      >
        {t("profiles.save")}
      </button>
      {notice && <p role="status">{notice}</p>}
    </details>
  );
}
