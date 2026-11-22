// Manual positions are scoped to the window's Host and stored separately from connection history.
export function readDirectoryOrder(raw: string | null): string[] {
  if (!raw || raw.length > 262144) return [];
  try {
    const keys: unknown = JSON.parse(raw);
    if (
      Array.isArray(keys) &&
      keys.length <= 512 &&
      keys.every((k) => typeof k === "string" && k.length <= 2048)
    )
      return [...new Set<string>(keys)];
  } catch {
    /* Invalid preferences fall back to favorite/recency ordering. */
  }
  return [];
}

export function orderedKeys(keys: string[], order: string[]): string[] {
  const remaining = new Set(keys);
  return [...order.filter((key) => remaining.delete(key)), ...remaining];
}

// Move among visible positions, preserving the slots of filtered-out connections.
export function moveDirectoryRow(
  keys: string[],
  visible: string[],
  source: string,
  target: string,
): string[] {
  const from = visible.indexOf(source),
    to = visible.indexOf(target);
  if (from < 0 || to < 0 || from === to) return keys;
  const moved = [...visible];
  moved.splice(from, 1);
  moved.splice(to, 0, source);
  const slots = new Set(visible);
  let index = 0;
  return keys.map((key) => (slots.has(key) ? moved[index++]! : key));
}
