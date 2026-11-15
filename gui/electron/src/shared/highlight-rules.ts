/** Ordered, literal, client-only rules. Earlier enabled rules own overlapping text. */
export interface HighlightRule {
  text: string;
  color: string;
  enabled: boolean;
}

/** A reusable local template; importing rules always copies their values. */
export interface HighlightTemplate {
  id: string;
  name: string;
  rules: HighlightRule[];
}

export const maxHighlightRules = 32;
export const highlightTemplatesKey = "channelterm.highlight-templates.v1";

/** Opaque local profile identity, independent of Session references and physical endpoints. */
export function isConnectionIdentity(value: unknown): value is string {
  return (
    typeof value === "string" &&
    /^[0-9a-f]{8}-[0-9a-f]{4}-4[0-9a-f]{3}-[89ab][0-9a-f]{3}-[0-9a-f]{12}$/i.test(
      value,
    )
  );
}

function plainText(value: unknown, maximum: number): value is string {
  return (
    typeof value === "string" &&
    value.length > 0 &&
    value.length <= maximum &&
    !/[\x00-\x1f\x7f-\x9f\uD800-\uDFFF]/u.test(value)
  );
}

/** Validate and copy untrusted stored rules; colors cannot inject terminal controls. */
export function readHighlightRules(
  value: unknown,
): HighlightRule[] | undefined {
  if (!Array.isArray(value) || value.length > maxHighlightRules) return;
  const rules: HighlightRule[] = [];
  for (const entry of value) {
    if (!entry || typeof entry !== "object" || Array.isArray(entry)) return;
    const rule = entry as Record<string, unknown>;
    if (
      !plainText(rule.text, 128) ||
      typeof rule.enabled !== "boolean" ||
      typeof rule.color !== "string" ||
      !/^#[0-9a-f]{6}$/i.test(rule.color)
    )
      return;
    rules.push({
      text: rule.text,
      color: rule.color.toLowerCase(),
      enabled: rule.enabled,
    });
  }
  return rules;
}

/** Bounded templates contain no executable patterns, connection identity or shared mutable lists. */
export function restoreHighlightTemplates(
  raw: string | null,
): HighlightTemplate[] {
  if (!raw || raw.length > 65536) return [];
  try {
    const value: unknown = JSON.parse(raw);
    if (!Array.isArray(value) || value.length > 10) return [];
    const templates: HighlightTemplate[] = [];
    const ids = new Set<string>();
    for (const entry of value) {
      if (!entry || typeof entry !== "object" || Array.isArray(entry))
        return [];
      const template = entry as Record<string, unknown>;
      const rules = readHighlightRules(template.rules);
      if (
        !isConnectionIdentity(template.id) ||
        ids.has(template.id) ||
        !plainText(template.name, 64) ||
        !rules
      )
        return [];
      ids.add(template.id);
      templates.push({ id: template.id, name: template.name, rules });
    }
    return templates;
  } catch {
    return [];
  }
}
