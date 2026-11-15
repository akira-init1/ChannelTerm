import { useEffect, useRef, useState, type FormEvent } from "react";
import {
  highlightTemplatesKey,
  maxHighlightRules,
  readHighlightRules,
  restoreHighlightTemplates,
  type HighlightRule,
  type HighlightTemplate,
} from "../../shared/highlight-rules";

/** Edit one local connection's rules. Template operations copy values and never execute input. */
export function HighlightSettings({
  label,
  initial,
  t,
  onSave,
  onClose,
}: {
  label: string;
  initial: readonly HighlightRule[];
  t: (id: string) => string;
  onSave: (rules: HighlightRule[]) => boolean;
  onClose: () => void;
}) {
  const dialog = useRef<HTMLDialogElement>(null);
  const [rules, setRules] = useState(() =>
    initial.map((rule) => ({ ...rule })),
  );
  const [loaded] = useState(() => {
    try {
      return {
        templates: restoreHighlightTemplates(
          localStorage.getItem(highlightTemplatesKey),
        ),
        failed: false,
      };
    } catch {
      return { templates: [], failed: true };
    }
  });
  const [templates, setTemplates] = useState<HighlightTemplate[]>(
    loaded.templates,
  );
  const [templateId, setTemplateId] = useState("");
  const [name, setName] = useState("");
  const [error, setError] = useState(
    loaded.failed ? "highlight.saveError" : "",
  );
  const selected = templates.find((entry) => entry.id === templateId);
  useEffect(() => {
    dialog.current?.showModal();
  }, []);

  function update(index: number, patch: Partial<HighlightRule>) {
    setRules((previous) =>
      previous.map((rule, i) => (i === index ? { ...rule, ...patch } : rule)),
    );
  }
  function move(index: number, direction: -1 | 1) {
    const next = [...rules];
    const target = index + direction;
    if (target < 0 || target >= next.length) return;
    [next[index], next[target]] = [next[target]!, next[index]!];
    setRules(next);
  }
  function saveTemplates(next: HighlightTemplate[]) {
    const raw = JSON.stringify(next);
    if (restoreHighlightTemplates(raw).length !== next.length) {
      setError("highlight.invalid");
      return false;
    }
    try {
      localStorage.setItem(highlightTemplatesKey, raw);
      setTemplates(next);
      setError("");
      return true;
    } catch {
      setError("highlight.saveError");
      return false;
    }
  }
  function saveTemplate(replace: boolean) {
    const checked = readHighlightRules(rules);
    if (!checked || !name.trim() || (replace && !selected)) {
      setError("highlight.invalid");
      return;
    }
    const id = replace ? selected!.id : crypto.randomUUID();
    const next = { id, name: name.trim(), rules: checked };
    // Refuse stale template replacement/deletion rather than recreating another window's removed item.
    try {
      const latest = restoreHighlightTemplates(
        localStorage.getItem(highlightTemplatesKey),
      );
      if (replace && !latest.some((entry) => entry.id === id)) {
        setError("highlight.saveError");
        return;
      }
      if (
        saveTemplates(
          replace
            ? latest.map((entry) => (entry.id === id ? next : entry))
            : [...latest, next],
        )
      )
        setTemplateId(id);
    } catch {
      setError("highlight.saveError");
    }
  }
  function submit(event: FormEvent) {
    event.preventDefault();
    const checked = readHighlightRules(rules);
    if (!checked) {
      setError("highlight.invalid");
      return;
    }
    if (onSave(checked)) onClose();
    else setError("highlight.saveError");
  }
  return (
    <dialog
      ref={dialog}
      className="connection-dialog highlight-dialog"
      aria-labelledby="highlight-title"
      onCancel={(event) => {
        event.preventDefault();
        event.stopPropagation();
        onClose();
      }}
    >
      <form onSubmit={submit}>
        <h2 id="highlight-title">
          {t("highlight.title")} · {label}
        </h2>
        <p>{t("highlight.hint")}</p>
        <div className="highlight-rules">
          {rules.map((rule, index) => (
            <div className="highlight-rule" key={index}>
              <span>{index + 1}</span>
              <label>
                <input
                  type="checkbox"
                  checked={rule.enabled}
                  onChange={(event) =>
                    update(index, { enabled: event.target.checked })
                  }
                />
                {t("highlight.enabled")}
              </label>
              <label>
                {t("highlight.text")}
                <input
                  value={rule.text}
                  maxLength={128}
                  required
                  onChange={(event) =>
                    update(index, { text: event.target.value })
                  }
                />
              </label>
              <label>
                {t("highlight.color")}
                <input
                  type="color"
                  value={rule.color}
                  onChange={(event) =>
                    update(index, { color: event.target.value })
                  }
                />
              </label>
              <div className="highlight-order">
                <button
                  type="button"
                  disabled={index === 0}
                  aria-label={t("highlight.up")}
                  onClick={() => move(index, -1)}
                >
                  ↑
                </button>
                <button
                  type="button"
                  disabled={index === rules.length - 1}
                  aria-label={t("highlight.down")}
                  onClick={() => move(index, 1)}
                >
                  ↓
                </button>
                <button
                  type="button"
                  onClick={() => setRules(rules.filter((_, i) => i !== index))}
                >
                  {t("highlight.remove")}
                </button>
              </div>
            </div>
          ))}
        </div>
        <div className="highlight-actions">
          <button
            type="button"
            disabled={rules.length >= maxHighlightRules}
            onClick={() =>
              setRules([
                ...rules,
                { text: "", color: "#ffcc66", enabled: true },
              ])
            }
          >
            {t("highlight.add")}
          </button>
          <button
            type="button"
            disabled={!rules.length}
            onClick={() => setRules([])}
          >
            {t("highlight.clear")}
          </button>
          <span>
            {rules.length}/{maxHighlightRules}
          </span>
        </div>
        <details className="highlight-templates">
          <summary>{t("highlight.templates")}</summary>
          <p>{t("highlight.templateHint")}</p>
          <label>
            {t("highlight.template")}
            <select
              value={templateId}
              onChange={(event) => {
                setTemplateId(event.target.value);
                setName(
                  templates.find((entry) => entry.id === event.target.value)
                    ?.name ?? "",
                );
              }}
            >
              <option value="">—</option>
              {templates.map((entry) => (
                <option key={entry.id} value={entry.id}>
                  {entry.name}
                </option>
              ))}
            </select>
          </label>
          <label>
            {t("highlight.templateName")}
            <input
              value={name}
              maxLength={64}
              onChange={(event) => setName(event.target.value)}
            />
          </label>
          <div className="highlight-actions">
            <button
              type="button"
              disabled={
                !selected ||
                rules.length + selected.rules.length > maxHighlightRules
              }
              onClick={() =>
                selected &&
                setRules([
                  ...rules,
                  ...selected.rules.map((rule) => ({ ...rule })),
                ])
              }
            >
              {t("highlight.import")}
            </button>
            <button
              type="button"
              disabled={templates.length >= 10}
              onClick={() => saveTemplate(false)}
            >
              {t("highlight.saveTemplate")}
            </button>
            <button
              type="button"
              disabled={!selected}
              onClick={() => saveTemplate(true)}
            >
              {t("highlight.replaceTemplate")}
            </button>
            <button
              type="button"
              disabled={!selected}
              onClick={() => {
                try {
                  const latest = restoreHighlightTemplates(
                    localStorage.getItem(highlightTemplatesKey),
                  );
                  if (
                    saveTemplates(
                      latest.filter((entry) => entry.id !== templateId),
                    )
                  ) {
                    setTemplateId("");
                    setName("");
                  }
                } catch {
                  setError("highlight.saveError");
                }
              }}
            >
              {t("highlight.deleteTemplate")}
            </button>
          </div>
        </details>
        {error && <p role="alert">{t(error)}</p>}
        <footer>
          <button type="button" onClick={onClose}>
            {t("connection.cancel")}
          </button>
          <button type="submit">{t("highlight.save")}</button>
        </footer>
      </form>
    </dialog>
  );
}
