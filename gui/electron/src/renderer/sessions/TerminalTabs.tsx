import { useEffect, useRef, type MouseEvent } from "react";
import { Icon } from "../components/Icon";

export interface TabSummary {
  id: string;
  label: string;
  transport?: string;
  moving?: boolean;
}

/** Tab navigation is local presentation; callbacks retain the clicked tab's identity. */
export function TerminalTabs({
  tabs,
  selected,
  disabled,
  t,
  onSelect,
  onClose,
  onNew,
  onContextMenu,
}: {
  tabs: TabSummary[];
  selected?: string;
  disabled: boolean;
  t: (id: string) => string;
  onSelect: (id: string) => void;
  onClose: (id: string) => void;
  onNew: () => void;
  onContextMenu: (event: MouseEvent<HTMLElement>, id: string) => void;
}) {
  const strip = useRef<HTMLDivElement>(null);
  useEffect(() => {
    strip.current
      ?.querySelector('[aria-selected="true"]')
      ?.scrollIntoView({ block: "nearest", inline: "nearest" });
  }, [selected]);
  return (
    <>
      <div
        ref={strip}
        className="terminal-tabs"
        role="tablist"
        aria-label={t("tab.list")}
      >
        {tabs.map((tab, index) => (
          <div
            key={tab.id}
            className={`terminal-tab ${tab.id === selected ? "active attached" : ""}`}
            data-tab-id={tab.id}
            onContextMenu={(event) => onContextMenu(event, tab.id)}
          >
            <button
              role="tab"
              aria-selected={tab.id === selected}
              tabIndex={tab.id === selected ? 0 : -1}
              title={tab.label}
              disabled={disabled}
              onClick={() => onSelect(tab.id)}
              onKeyDown={(event) => {
                if (
                  event.nativeEvent.isComposing ||
                  event.keyCode === 229 ||
                  event.ctrlKey ||
                  event.altKey ||
                  event.metaKey
                )
                  return;
                const next =
                  event.key === "ArrowRight"
                    ? (index + 1) % tabs.length
                    : event.key === "ArrowLeft"
                      ? (index + tabs.length - 1) % tabs.length
                      : event.key === "Home"
                        ? 0
                        : event.key === "End"
                          ? tabs.length - 1
                          : undefined;
                if (next === undefined) return;
                event.preventDefault();
                onSelect(tabs[next]!.id);
                strip.current
                  ?.querySelectorAll<HTMLButtonElement>('[role="tab"]')
                  [next]?.focus();
              }}
            >
              <Icon
                name={
                  tab.transport === "serial"
                    ? "serial"
                    : tab.transport === "ssh"
                      ? "monitor"
                      : "terminal"
                }
              />
              <span>{tab.moving ? t("tab.moving") : tab.label}</span>
            </button>
            <button
              className="quiet icon-button"
              aria-label={t("tab.close")}
              title={t("tab.close")}
              disabled={disabled || tab.moving}
              onClick={() => onClose(tab.id)}
            >
              <Icon name="close" />
            </button>
          </div>
        ))}
      </div>
      <button
        className="quiet new-tab-button"
        aria-label={t("tab.new")}
        title={t("tab.new")}
        disabled={disabled || tabs.length >= 32}
        onClick={onNew}
      >
        +
      </button>
    </>
  );
}
