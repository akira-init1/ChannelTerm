import { useEffect, useRef, useState, type MouseEvent } from "react";
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
  onReorder,
}: {
  tabs: TabSummary[];
  selected?: string;
  disabled: boolean;
  t: (id: string) => string;
  onSelect: (id: string) => void;
  onClose: (id: string) => void;
  onNew: () => void;
  onContextMenu: (event: MouseEvent<HTMLElement>, id: string) => void;
  onReorder: (
    source: string,
    target: string,
    position: "before" | "after",
  ) => void;
}) {
  const strip = useRef<HTMLDivElement>(null);
  const dragging = useRef<string | undefined>(undefined);
  const [drop, setDrop] = useState<{
    id: string;
    position: "before" | "after";
  }>();
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
            className={`terminal-tab ${tab.id === selected ? "active attached" : ""} ${drop?.id === tab.id ? `drop-${drop.position}` : ""}`}
            data-tab-id={tab.id}
            draggable={!disabled && !tab.moving}
            onDragStart={(event) => {
              if (disabled || tab.moving) {
                event.preventDefault();
                return;
              }
              dragging.current = tab.id;
              event.dataTransfer.effectAllowed = "move";
              event.dataTransfer.setData(
                "application/x-channelterm-local-tab",
                tab.id,
              );
            }}
            onDragOver={(event) => {
              // A DataTransfer string alone never authorizes a cross-window or external move.
              if (!dragging.current || disabled || tab.moving) return;
              event.preventDefault();
              event.stopPropagation();
              event.dataTransfer.dropEffect = "move";
              const bounds = event.currentTarget.getBoundingClientRect();
              const position =
                event.clientX < bounds.left + bounds.width / 2
                  ? "before"
                  : "after";
              setDrop((previous) =>
                previous?.id === tab.id && previous.position === position
                  ? previous
                  : { id: tab.id, position },
              );
            }}
            onDrop={(event) => {
              const source = dragging.current;
              if (!source || disabled || tab.moving) return;
              event.preventDefault();
              event.stopPropagation();
              dragging.current = undefined;
              setDrop(undefined);
              if (
                event.dataTransfer.getData(
                  "application/x-channelterm-local-tab",
                ) !== source
              )
                return;
              const bounds = event.currentTarget.getBoundingClientRect();
              onReorder(
                source,
                tab.id,
                event.clientX < bounds.left + bounds.width / 2
                  ? "before"
                  : "after",
              );
            }}
            onDragEnd={() => {
              dragging.current = undefined;
              setDrop(undefined);
            }}
            onContextMenu={(event) => onContextMenu(event, tab.id)}
          >
            <button
              role="tab"
              aria-selected={tab.id === selected}
              tabIndex={tab.id === selected ? 0 : -1}
              title={`${tab.label}\n${t("tab.reorderHint")}`}
              aria-description={t("tab.reorderHint")}
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
                // Reordering keys are scoped to the focused tab button, never terminal input or IME.
                if (
                  event.shiftKey &&
                  (event.key === "ArrowLeft" || event.key === "ArrowRight")
                ) {
                  event.preventDefault();
                  event.stopPropagation();
                  const target =
                    tabs[index + (event.key === "ArrowLeft" ? -1 : 1)];
                  if (!disabled && !tab.moving && target && !target.moving)
                    onReorder(
                      tab.id,
                      target.id,
                      event.key === "ArrowLeft" ? "before" : "after",
                    );
                  return;
                }
                if (event.shiftKey) return;
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
