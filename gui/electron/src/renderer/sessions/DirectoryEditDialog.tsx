import { useEffect, useRef, useState } from "react";
import {
  presetEndpoint,
  type ConnectionRecord,
} from "../../shared/connection-history";

export type DirectoryDialogAction = "folder" | "copy" | "forget";
export type DirectoryEditor =
  | { action: DirectoryDialogAction; profile: ConnectionRecord }
  | { action: "newFolder"; profile?: never };

export const directoryLabels = {
  newFolder: "directory.newFolder",
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
}: DirectoryEditor & {
  folders: string[];
  onSave: (value: string) => boolean | Promise<boolean>;
  onClose: () => void;
  t: (id: string) => string;
}) {
  const dialog = useRef<HTMLDialogElement>(null);
  const [value, setValue] = useState(
    action === "folder" ? (profile.folder ?? "") : "",
  );
  const [failed, setFailed] = useState(false);
  const [saving, setSaving] = useState(false);
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
        onSubmit={async (event) => {
          event.preventDefault();
          if (saving) return;
          setSaving(true);
          if (await onSave(value.trim())) onClose();
          else setFailed(true);
          setSaving(false);
        }}
      >
        <h2>{title}</h2>
        {profile && (
          <p>
            {[profile.request.label, presetEndpoint(profile.request)]
              .filter(Boolean)
              .join(" · ")}
          </p>
        )}
        {action !== "forget" && (
          <label>
            {t(
              action === "folder"
                ? "directory.folderHint"
                : action === "newFolder"
                  ? "directory.folderName"
                  : "connection.label",
            )}
            <input
              autoFocus
              maxLength={128}
              required={action === "copy" || action === "newFolder"}
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
        {failed && (
          <p role="alert">
            {t(
              action === "newFolder"
                ? "directory.folderFailed"
                : "directory.editFailed",
            )}
          </p>
        )}
        <button type="submit" disabled={saving}>
          {action === "forget" || action === "newFolder"
            ? title
            : t("connection.savePreset")}
        </button>
        <button type="button" autoFocus={action === "forget"} onClick={onClose}>
          {t("connection.cancel")}
        </button>
      </form>
    </dialog>
  );
}
