import { useEffect, useState, type ReactNode } from "react";
import { SidebarResizer } from "./SidebarResizer";
import { Icon } from "./Icon";

/** Hidden tabs stay mounted so transfer confirmation and progress keep their lifetime. */
export function BottomPanel({
  visible,
  tab,
  onTab,
  onClose,
  t,
  children,
  status,
  activity,
}: {
  visible: boolean;
  tab: "transfers" | "status" | "activity";
  onTab: (tab: "transfers" | "status" | "activity") => void;
  onClose: () => void;
  t: (id: string) => string;
  children: ReactNode;
  status: ReactNode;
  activity: ReactNode;
}) {
  const [height, setHeight] = useState(220);
  const [viewportHeight, setViewportHeight] = useState(innerHeight);
  useEffect(() => {
    const resize = () => setViewportHeight(innerHeight);
    window.addEventListener("resize", resize);
    return () => window.removeEventListener("resize", resize);
  }, []);
  const maximum = Math.max(100, Math.floor((viewportHeight - 56) * 0.45));
  return (
    <section
      id="bottom-panel"
      className="bottom-panel"
      hidden={!visible}
      style={{ height: Math.min(height, maximum) }}
      aria-label={t("view.panel")}
    >
      {visible && (
        <SidebarResizer
          width={Math.min(height, maximum)}
          minimum={100}
          maximum={maximum}
          defaultWidth={220}
          edge="top"
          controls="bottom-panel"
          label={t("panel.resize")}
          hint={t("view.resizeSidebarHelp")}
          onResize={setHeight}
        />
      )}
      <div className="panel-header">
        <div role="tablist" aria-label={t("view.panel")}>
          {(["transfers", "status", "activity"] as const).map((id) => (
            <button
              key={id}
              role="tab"
              id={`panel-tab-${id}`}
              aria-controls={`panel-content-${id}`}
              aria-selected={tab === id}
              tabIndex={tab === id ? 0 : -1}
              onClick={() => onTab(id)}
              onKeyDown={(event) => {
                if (
                  event.nativeEvent.isComposing ||
                  event.keyCode === 229 ||
                  event.altKey ||
                  event.ctrlKey ||
                  event.metaKey
                )
                  return;
                if (
                  !["ArrowLeft", "ArrowRight", "Home", "End"].includes(
                    event.key,
                  )
                )
                  return;
                event.preventDefault();
                const tabs = ["transfers", "status", "activity"] as const;
                const index = tabs.indexOf(id);
                const next =
                  tabs[
                    event.key === "Home"
                      ? 0
                      : event.key === "End"
                        ? 2
                        : (index + (event.key === "ArrowLeft" ? 2 : 1)) %
                          tabs.length
                  ]!;
                onTab(next);
                document.getElementById(`panel-tab-${next}`)?.focus();
              }}
            >
              {t(
                id === "transfers"
                  ? "file.transfers"
                  : id === "activity"
                    ? "activity.title"
                    : "view.status",
              )}
            </button>
          ))}
        </div>
        <button
          className="quiet icon-button"
          aria-label={t("panel.close")}
          onClick={onClose}
        >
          <Icon name="close" />
        </button>
      </div>
      <div
        role="tabpanel"
        id="panel-content-transfers"
        aria-labelledby="panel-tab-transfers"
        hidden={tab !== "transfers"}
        tabIndex={0}
      >
        {children}
      </div>
      <div
        role="tabpanel"
        id="panel-content-status"
        aria-labelledby="panel-tab-status"
        hidden={tab !== "status"}
        tabIndex={0}
      >
        {visible && tab === "status" ? status : null}
      </div>
      <div
        role="tabpanel"
        id="panel-content-activity"
        aria-labelledby="panel-tab-activity"
        hidden={tab !== "activity"}
        tabIndex={0}
      >
        {visible && tab === "activity" ? activity : null}
      </div>
    </section>
  );
}
