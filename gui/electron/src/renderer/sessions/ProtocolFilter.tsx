import { useEffect, useRef, useState } from "react";
import {
  presetEndpoint,
  type ConnectionRecord,
} from "../../shared/connection-history";
import type { ConnectionRequest } from "../../shared/connections";
import { Icon } from "../components/Icon";

type Protocol = ConnectionRequest["kind"];

/** Hover previews saved presets without changing the current directory selection. */
export function ProtocolFilter({
  history,
  protocols,
  selected,
  onSelect,
  t,
}: {
  history: ConnectionRecord[];
  protocols: Protocol[];
  selected?: Protocol;
  onSelect: (protocol?: Protocol) => void;
  t: (id: string) => string;
}) {
  const [open, setOpen] = useState(false);
  const root = useRef<HTMLDivElement>(null);
  const trigger = useRef<HTMLButtonElement>(null);
  const label = (kind: Protocol) =>
    kind === "ssh" ? "SSH" : t("debug.serial");
  useEffect(() => {
    if (!open) return;
    const dismiss = (e: PointerEvent) => {
      if (e.target instanceof Node && !root.current?.contains(e.target))
        setOpen(false);
    };
    document.addEventListener("pointerdown", dismiss, true);
    return () => document.removeEventListener("pointerdown", dismiss, true);
  }, [open]);
  function select(kind?: Protocol) {
    onSelect(kind);
    setOpen(false);
    trigger.current?.focus();
  }
  return (
    <div
      className="directory-protocol"
      ref={root}
      onPointerEnter={(e) => {
        if (e.pointerType === "mouse") setOpen(true);
      }}
      onPointerLeave={() => {
        if (
          !root.current
            ?.querySelector(".protocol-options")
            ?.contains(document.activeElement)
        )
          setOpen(false);
      }}
      onBlur={(e) => {
        if (!e.currentTarget.contains(e.relatedTarget)) setOpen(false);
      }}
      onKeyDown={(e) => {
        if (e.key === "Escape" && open) {
          e.stopPropagation();
          setOpen(false);
          trigger.current?.focus();
        }
      }}
    >
      <button
        ref={trigger}
        type="button"
        aria-expanded={open}
        onClick={() => setOpen(true)}
      >
        {t("connection.protocol")}
        {selected && " · " + label(selected)} <Icon name="chevron" />
      </button>
      {open && (
        <div
          className="protocol-options"
          role="group"
          aria-label={t("connection.protocol")}
        >
          <button
            type="button"
            aria-pressed={!selected}
            onClick={() => select()}
          >
            {t("directory.allProtocols")}
          </button>
          {protocols.map((kind) => {
            const entries = history.filter(
              (entry) => entry.request.kind === kind,
            );
            return (
              <section key={kind}>
                <button
                  type="button"
                  aria-pressed={kind === selected}
                  onClick={() => select(kind)}
                >
                  {label(kind)} ({entries.length})
                </button>
                {entries.length > 0 && (
                  <ul>
                    {entries.map((entry, i) => {
                      const endpoint = presetEndpoint(entry.request);
                      return (
                        <li key={entry.id ?? i}>
                          {[entry.request.label, endpoint]
                            .filter(Boolean)
                            .join(" · ")}
                        </li>
                      );
                    })}
                  </ul>
                )}
              </section>
            );
          })}
          {!history.length && <p>{t("directory.empty")}</p>}
        </div>
      )}
    </div>
  );
}
