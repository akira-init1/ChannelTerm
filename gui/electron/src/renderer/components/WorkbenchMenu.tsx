import { useRef } from "react";
import { Icon } from "./Icon";

/**
 * WorkbenchMenu exposes local view controls without adding privileged bridge APIs.
 * Native auto popovers handle outside clicks, Escape, and focus restoration.
 */
export function WorkbenchMenu({
  sidebarVisible,
  onToggleSidebar,
}: {
  sidebarVisible: boolean;
  onToggleSidebar: () => void;
}) {
  const view = useRef<HTMLDivElement>(null);
  return (
    <header className="topbar">
      <nav className="app-menu" aria-label="Application menu">
        <button popoverTarget="view-options">
          View <Icon name="chevron" />
        </button>
        <button
          disabled
          title="Settings are not available yet"
          aria-label="Settings (not available yet)"
        >
          Settings
        </button>
        <button popoverTarget="help-panel">
          Help <Icon name="chevron" />
        </button>
      </nav>
      <div
        ref={view}
        id="view-options"
        className="menu-popup view-options"
        popover="auto"
        aria-label="View options"
      >
        <button
          onClick={() => {
            onToggleSidebar();
            view.current?.hidePopover();
          }}
        >
          {sidebarVisible ? "Hide Session sidebar" : "Show Session sidebar"}
        </button>
      </div>
      <div
        id="help-panel"
        className="menu-popup help-panel"
        popover="auto"
        aria-label="Quick help"
      >
        <h2>Using ChannelTerm</h2>
        <p>
          Connect to an existing Host using the Host panel, then select a
          Session.
        </p>
        <p>
          Open new serial or SSH Sessions with the CLI. This desktop client
          attaches to existing Sessions.
        </p>
        <p>
          Type directly in the terminal. Detaching or closing this window leaves
          shared Sessions running.
        </p>
      </div>
    </header>
  );
}
