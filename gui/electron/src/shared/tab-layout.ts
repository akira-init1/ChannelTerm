/** Two visible views reference local tab identities, never Session IDs or Host authority. */
export interface SplitLayout {
  axis: "columns" | "rows";
  first: string;
  second: string;
}

/** Reorder only existing local tabs; the records, attachments and selected ID stay intact. */
export function reorderTabs<T extends { id: string }>(
  tabs: T[],
  source: string,
  target: string,
  position: "before" | "after",
): T[] {
  const moving = tabs.find((tab) => tab.id === source);
  if (!moving || source === target || !tabs.some((tab) => tab.id === target))
    return tabs;
  const next = tabs.filter((tab) => tab.id !== source);
  const index =
    next.findIndex((tab) => tab.id === target) + (position === "after" ? 1 : 0);
  next.splice(index, 0, moving);
  return next;
}

/** Focusing a visible pane preserves its position; a new tab replaces only the focused slot. */
export function focusSplit(
  split: SplitLayout | undefined,
  previous: string | undefined,
  selected: string | undefined,
): SplitLayout | undefined {
  if (!selected) return undefined;
  if (!split || selected === split.first || selected === split.second)
    return split;
  return previous === split.second
    ? { ...split, second: selected }
    : { ...split, first: selected };
}

/** Closing/moving either split view collapses layout without disposing the surviving tab. */
export function retainSplit(
  split: SplitLayout | undefined,
  tabs: readonly { id: string }[],
): SplitLayout | undefined {
  if (!split || split.first === split.second) return undefined;
  return tabs.some((tab) => tab.id === split.first) &&
    tabs.some((tab) => tab.id === split.second)
    ? split
    : undefined;
}
