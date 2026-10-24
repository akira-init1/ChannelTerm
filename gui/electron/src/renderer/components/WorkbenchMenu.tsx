import {
  useEffect,
  useLayoutEffect,
  useRef,
  useState,
  type KeyboardEvent,
} from "react";
import type { Action, MenuEntry, MenuState } from "../../shared/menus";
import { applicationMenu } from "../../shared/menus";
import { Icon } from "./Icon";

export interface MenuPopupRequest {
  entries: MenuEntry[];
  x: number;
  y: number;
  opener: HTMLElement;
}

/** Menu keyboard handling is scoped to the popup, never document or xterm. */
function navigateMenu(event: KeyboardEvent<HTMLDivElement>) {
  if (event.nativeEvent.isComposing || event.keyCode === 229) return;
  if (!["ArrowDown", "ArrowUp", "Home", "End"].includes(event.key)) return;
  const items = Array.from(
    event.currentTarget.querySelectorAll<HTMLButtonElement>(
      "button:not(:disabled)",
    ),
  ).filter((item) => item.closest('[role="menu"]') === event.currentTarget);
  if (!items.length) return;
  const index = items.indexOf(document.activeElement as HTMLButtonElement);
  const next =
    event.key === "Home"
      ? 0
      : event.key === "End"
        ? items.length - 1
        : (index + (event.key === "ArrowUp" ? -1 : 1) + items.length) %
          items.length;
  items[next]?.focus();
  event.preventDefault();
}

/** Renderer and native application menus consume the same finite descriptors. */
export function WorkbenchMenu({
  state,
  t,
  execute,
  enabled,
  context,
  onContextClose,
  helpOpen,
  onHelpClose,
}: {
  state: MenuState;
  t: (id: string) => string;
  execute: (action: Action) => void;
  enabled: (action: Action) => boolean;
  context?: MenuPopupRequest;
  onContextClose: () => void;
  helpOpen: boolean;
  onHelpClose: () => void;
}) {
  const [opened, setOpened] = useState<MenuPopupRequest>();
  const [expanded, setExpanded] = useState<string>();
  const popup = useRef<HTMLDivElement>(null);
  const submenu = useRef<HTMLDivElement>(null);
  const focusSubmenu = useRef(false);
  const help = useRef<HTMLDialogElement>(null);
  const request = context ?? opened;
  const requestRef = useRef(request);
  requestRef.current = request;

  function close(restoreFocus = false) {
    if (restoreFocus) requestRef.current?.opener.focus();
    popup.current?.hidePopover();
    setOpened(undefined);
    setExpanded(undefined);
    onContextClose();
  }
  useLayoutEffect(() => {
    if (!request || !popup.current) return;
    const element = popup.current;
    element.showPopover();
    const position = () => {
      const bounds = element.getBoundingClientRect();
      element.style.left = `${Math.max(4, Math.min(request.x, innerWidth - bounds.width - 4))}px`;
      element.style.top = `${Math.max(4, Math.min(request.y, innerHeight - bounds.height - 4))}px`;
    };
    position();
    element.querySelector<HTMLButtonElement>("button:not(:disabled)")?.focus();
    window.addEventListener("resize", position);
    return () => window.removeEventListener("resize", position);
  }, [request, state.locale]);
  useLayoutEffect(() => {
    const parent = popup.current?.querySelector<HTMLButtonElement>(
      '[aria-expanded="true"]',
    );
    const child = submenu.current;
    if (!expanded || !parent || !child) return;
    // Anchor to the parent row, flipping at the right edge instead of changing
    // the top-level popup's width. Fixed positioning keeps terminal layout stable.
    const position = () => {
      const row = parent.getBoundingClientRect();
      const panel = popup.current!.getBoundingClientRect();
      const bounds = child.getBoundingClientRect();
      const left =
        panel.right + bounds.width <= innerWidth - 4
          ? panel.right - 1
          : panel.left - bounds.width + 1;
      child.style.left = `${Math.max(4, left)}px`;
      child.style.top = `${Math.max(4, Math.min(row.top, innerHeight - bounds.height - 4))}px`;
    };
    position();
    if (focusSubmenu.current) {
      child.querySelector<HTMLButtonElement>("button:not(:disabled)")?.focus();
      focusSubmenu.current = false;
    }
    window.addEventListener("resize", position);
    return () => window.removeEventListener("resize", position);
  }, [expanded, request, state.locale]);
  useEffect(() => {
    if (helpOpen) help.current?.showModal();
    else help.current?.close();
  }, [helpOpen]);

  function renderItems(entries: MenuEntry[], child = false) {
    return entries.map((entry) => {
      const checked =
        entry.id === "view.sidebar"
          ? state.sidebarVisible
          : entry.action?.id === "language.change"
            ? state.locale === entry.action.locale
            : entry.checked;
      return (
        <div key={entry.id}>
          <button
            role={
              checked === undefined
                ? "menuitem"
                : entry.action?.id === "language.change"
                  ? "menuitemradio"
                  : "menuitemcheckbox"
            }
            aria-checked={checked}
            aria-haspopup={entry.children ? "menu" : undefined}
            aria-expanded={entry.children ? expanded === entry.id : undefined}
            disabled={
              entry.action ? !enabled(entry.action) : entry.enabled === false
            }
            data-action={entry.action?.id}
            onPointerEnter={() => {
              if (!child) setExpanded(entry.children ? entry.id : undefined);
            }}
            onKeyDown={(event) => {
              if (event.nativeEvent.isComposing || event.keyCode === 229)
                return;
              if (entry.children && event.key === "ArrowRight") {
                if (expanded === entry.id) {
                  submenu.current
                    ?.querySelector<HTMLButtonElement>("button:not(:disabled)")
                    ?.focus();
                } else {
                  focusSubmenu.current = true;
                  setExpanded(entry.id);
                }
                event.preventDefault();
              }
            }}
            onClick={() => {
              if (entry.children) {
                setExpanded(entry.id);
                return;
              }
              close(true);
              if (entry.action) execute(entry.action);
            }}
          >
            <span className="menu-check" aria-hidden="true">
              {checked ? "✓" : ""}
            </span>
            {t(entry.label)}
            {entry.children && (
              <span className="submenu-arrow">
                <Icon name="chevron" />
              </span>
            )}
          </button>
          {entry.children && expanded === entry.id && (
            <div
              ref={submenu}
              role="menu"
              className="menu-popup menu-submenu"
              aria-label={t(entry.label)}
              onKeyDown={(event) => {
                if (event.nativeEvent.isComposing || event.keyCode === 229)
                  return;
                if (event.key === "ArrowLeft") {
                  popup.current
                    ?.querySelector<HTMLButtonElement>('[aria-expanded="true"]')
                    ?.focus();
                  setExpanded(undefined);
                  event.preventDefault();
                  event.stopPropagation();
                } else if (
                  ["ArrowDown", "ArrowUp", "Home", "End"].includes(event.key)
                ) {
                  navigateMenu(event);
                  event.stopPropagation();
                }
              }}
            >
              {renderItems(entry.children, true)}
            </div>
          )}
        </div>
      );
    });
  }

  return (
    <header className="topbar">
      <nav
        className="app-menu"
        aria-label="Application menu"
        onKeyDown={(event) => {
          if (event.nativeEvent.isComposing || event.keyCode === 229) return;
          const buttons = Array.from(
            event.currentTarget.querySelectorAll("button"),
          );
          const index = buttons.indexOf(
            document.activeElement as HTMLButtonElement,
          );
          if (event.key === "ArrowRight" || event.key === "ArrowLeft") {
            buttons[
              (index + (event.key === "ArrowRight" ? 1 : -1) + buttons.length) %
                buttons.length
            ]?.focus();
            event.preventDefault();
          } else if (event.key === "ArrowDown") {
            buttons[index]?.click();
            event.preventDefault();
          }
        }}
      >
        {applicationMenu(state).map((menu) => (
          <button
            key={menu.id}
            aria-haspopup="menu"
            aria-expanded={request?.opener.dataset.menu === menu.id}
            aria-controls="workbench-menu"
            data-menu={menu.id}
            onClick={(event) => {
              const opener = event.currentTarget;
              const rect = opener.getBoundingClientRect();
              onContextClose();
              setExpanded(undefined);
              setOpened({
                entries: menu.children!,
                x: rect.left,
                y: rect.bottom,
                opener,
              });
            }}
          >
            {t(menu.label)} <Icon name="chevron" />
          </button>
        ))}
      </nav>
      <div
        ref={popup}
        id="workbench-menu"
        popover="auto"
        className="menu-popup"
        role="menu"
        aria-label="Workbench menu"
        onToggle={(event) => {
          if (event.newState === "closed") {
            setOpened(undefined);
            setExpanded(undefined);
            onContextClose();
          }
        }}
        onKeyDown={(event) => {
          if (event.nativeEvent.isComposing || event.keyCode === 229) return;
          if (event.key === "Escape") {
            event.preventDefault();
            close(true);
          } else if (event.key === "Tab") close();
          else if (event.key === "ArrowLeft" && expanded) {
            setExpanded(undefined);
            event.preventDefault();
          } else navigateMenu(event);
        }}
      >
        {request && renderItems(request.entries)}
      </div>
      <dialog
        ref={help}
        className="help-dialog"
        aria-labelledby="help-title"
        onClose={onHelpClose}
      >
        <h2 id="help-title">{t("help.title")}</h2>
        <p>{t("help.connect")}</p>
        <p>{t("help.detach")}</p>
        <p>{t("help.clear")}</p>
        <button onClick={onHelpClose}>{t("help.close")}</button>
      </dialog>
    </header>
  );
}
