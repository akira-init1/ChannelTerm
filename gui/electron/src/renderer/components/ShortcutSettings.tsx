import { useEffect, useRef, useState } from "react";
import {
  defaultShortcuts,
  isShortcutBindings,
  layoutActions,
  shortcutFromEvent,
  shortcutLabel,
  type ShortcutBindings,
} from "../../shared/shortcuts";

/** Captures a draft locally; only Apply persists bindings and changes dispatch. */
export function ShortcutSettings({
  value,
  mac,
  t,
  onApply,
  onClose,
}: {
  value: ShortcutBindings;
  mac: boolean;
  t: (id: string) => string;
  onApply: (bindings: ShortcutBindings) => boolean;
  onClose: () => void;
}) {
  const dialog = useRef<HTMLDialogElement>(null);
  const [draft, setDraft] = useState(value);
  const [error, setError] = useState("");
  useEffect(() => {
    dialog.current?.showModal();
  }, []);
  return (
    <dialog
      ref={dialog}
      className="connection-dialog shortcut-settings"
      aria-labelledby="shortcuts-title"
      onClose={onClose}
    >
      <h2 id="shortcuts-title">{t("settings.shortcuts")}</h2>
      <p className="connection-hint">{t("shortcuts.hint")}</p>
      {layoutActions.map((id) => (
        <div className="shortcut-row" key={id}>
          <label>
            {t(id)}
            <input
              readOnly
              aria-label={t(id)}
              value={shortcutLabel(draft[id])}
              placeholder={t("shortcuts.unassigned")}
              onKeyDown={(event) => {
                if (
                  event.nativeEvent.isComposing ||
                  event.keyCode === 229 ||
                  event.key === "Tab" ||
                  event.key === "Escape"
                )
                  return;
                event.preventDefault();
                event.stopPropagation();
                if (["Control", "Meta", "Alt", "Shift"].includes(event.key))
                  return;
                const binding = shortcutFromEvent(event.nativeEvent);
                if (!binding) {
                  setError("shortcuts.invalid");
                  return;
                }
                setDraft({ ...draft, [id]: binding });
                setError("");
              }}
            />
          </label>
          <button
            onClick={() => {
              setDraft({ ...draft, [id]: "" });
              setError("");
            }}
          >
            {t("shortcuts.remove")}
          </button>
        </div>
      ))}
      {(!isShortcutBindings(draft) || error) && (
        <p role="alert">{t(error || "shortcuts.conflict")}</p>
      )}
      <footer>
        <button
          onClick={() => {
            setDraft(defaultShortcuts(mac));
            setError("");
          }}
        >
          {t("shortcuts.reset")}
        </button>
        <button onClick={onClose}>{t("connection.cancel")}</button>
        <button
          className="primary"
          disabled={!isShortcutBindings(draft)}
          onClick={() => {
            if (onApply(draft)) onClose();
            else setError("shortcuts.storageError");
          }}
        >
          {t("terminal.apply")}
        </button>
      </footer>
    </dialog>
  );
}
