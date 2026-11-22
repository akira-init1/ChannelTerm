import type { KeyboardEvent, ReactNode } from "react";
import { Icon, type IconName } from "./Icon";

const protocols = [
  ["serial", "serial", "Serial"],
  ["ssh", "monitor", "SSH"],
  ["shell", "shell", "Shell"],
  ["tcp", "tcp", "TCP"],
  ["sftp", "sftp", "SFTP"],
  ["telnet", "telnet", "Telnet"],
  ["wsl", "wsl", "WSL"],
] as const satisfies readonly (readonly [string, IconName, string])[];

export type Protocol = (typeof protocols)[number][0];

export function ProtocolPanel({
  selected,
  notice,
  disabled,
  children,
}: {
  selected: Protocol;
  notice?: string;
  disabled: boolean;
  children: ReactNode;
}) {
  return (
    <div
      role="tabpanel"
      id="protocol-panel"
      aria-labelledby={`protocol-${selected}`}
      tabIndex={notice ? 0 : undefined}
    >
      {notice && <p className="protocol-notice">{notice}</p>}
      <fieldset hidden={!!notice} disabled={disabled || !!notice}>
        {children}
      </fieldset>
    </div>
  );
}

// Arrow keys move focus. Activation is explicit because a protocol switch clears credentials.
function navigate(event: KeyboardEvent<HTMLDivElement>) {
  if (
    event.nativeEvent.isComposing ||
    !(event.target instanceof HTMLButtonElement)
  )
    return;
  const tabs = Array.from(
    event.currentTarget.querySelectorAll<HTMLButtonElement>("button:enabled"),
  );
  const index = tabs.indexOf(event.target);
  let next: number;
  switch (event.key) {
    case "ArrowRight":
      next = (index + 1) % tabs.length;
      break;
    case "ArrowLeft":
      next = (index - 1 + tabs.length) % tabs.length;
      break;
    case "Home":
      next = 0;
      break;
    case "End":
      next = tabs.length - 1;
      break;
    default:
      return;
  }
  event.preventDefault();
  tabs[next]?.focus();
}

export function ProtocolTabs({
  selected,
  sshAvailable,
  onSelect,
  t,
}: {
  selected: Protocol;
  sshAvailable: boolean;
  onSelect: (protocol: Protocol) => void;
  t: (id: string) => string;
}) {
  return (
    <>
      <span id="protocol-label">{t("connection.protocol")}</span>
      <div
        className="protocol-tabs"
        role="tablist"
        aria-labelledby="protocol-label"
        onKeyDown={navigate}
      >
        {protocols.map(([id, icon, label]) =>
          id === "wsl" && !/^Win/i.test(navigator.platform) ? null : (
            <button
              key={id}
              id={`protocol-${id}`}
              role="tab"
              type="button"
              aria-selected={selected === id}
              aria-controls="protocol-panel"
              tabIndex={selected === id ? 0 : -1}
              className={
                id === "serial" || id === "ssh" ? undefined : "planned-protocol"
              }
              disabled={id === "ssh" && !sshAvailable}
              onClick={() => onSelect(id)}
            >
              <Icon name={icon} />
              {id === "serial" ? t("connection.serialTab") : label}
            </button>
          ),
        )}
      </div>
    </>
  );
}
