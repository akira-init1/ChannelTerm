import { useEffect, useRef, useState } from "react";
import {
  presetEndpoint,
  type ConnectionRecord,
} from "../../shared/connection-history";

export type DirectoryDialogAction = "folder" | "copy" | "forget";

export const directoryLabels = {
  folder: "directory.moveFolder",
  copy: "directory.copy",
  forget: "history.remove",
} as const;

export function DirectoryEditDialog({
  action,
  profile,
  folders,
  onSave,
  onClose,
  t,
}: {
  action: DirectoryDialogAction;
  profile: ConnectionRecord;
  folders: string[];
  onSave: (value: string) => boolean;
  onClose: () => void;
  t: (id: string) => string;
}) {
  const dialog = useRef<HTMLDialogElement>(null);
  const [value, setValue] = useState(
    action === "folder" ? (profile.folder ?? "") : "",
  );
  const [failed, setFailed] = useState(false);
  useEffect(() => {
    const element = dialog.current!;
    element.showModal();
    return () => element.close();
  }, []);
  const title = t(directoryLabels[action]);
  return (
    <dialog
      ref={dialog}
      className="connection-dialog"
      aria-label={title}
      onCancel={onClose}
    >
      <form
        onSubmit={(event) => {
          event.preventDefault();
          if (onSave(value.trim())) onClose();
          else setFailed(true);
        }}
      >
        <h2>{title}</h2>
        <p>
          {[profile.request.label, presetEndpoint(profile.request)]
            .filter(Boolean)
            .join(" · ")}
        </p>
        {action !== "forget" && (
          <label>
            {t(
              action === "folder" ? "directory.folderHint" : "connection.label",
            )}
            <input
              autoFocus
              maxLength={128}
              required={action === "copy"}
              value={value}
              list={action === "folder" ? "directory-folder-names" : undefined}
              onChange={(e) => {
                setValue(e.target.value);
                setFailed(false);
              }}
            />
          </label>
        )}
        {action === "folder" && (
          <datalist id="directory-folder-names">
            {folders.map((folder) => (
              <option key={folder} value={folder} />
            ))}
          </datalist>
        )}
        {failed && <p role="alert">{t("directory.editFailed")}</p>}
        <button type="submit">
          {action === "forget" ? title : t("connection.savePreset")}
        </button>
        <button type="button" autoFocus={action === "forget"} onClick={onClose}>
          {t("connection.cancel")}
        </button>
      </form>
    </dialog>
  );
}
