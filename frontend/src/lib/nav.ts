export interface NavEntry {
  resourceKey: string;
  selection: { resourceKey: string; namespace: string; name: string } | null;
}

export interface NavState {
  entries: NavEntry[];
  index: number;
}

/** Return true if two navigation entries point to the same view/object. */
export function navEqual(a: NavEntry | undefined, b: NavEntry | undefined): boolean {
  if (!a || !b) return false;
  if (a.resourceKey !== b.resourceKey) return false;
  if (!a.selection && !b.selection) return true;
  if (!a.selection || !b.selection) return false;
  return (
    a.selection.resourceKey === b.selection.resourceKey &&
    a.selection.namespace === b.selection.namespace &&
    a.selection.name === b.selection.name
  );
}

/** Push a navigation step onto history, dropping any forward branch and skipping duplicates. */
export function pushNavEntry(state: NavState, entry: NavEntry, maxLen = 100): NavState {
  const cur = state.entries[state.index];
  if (navEqual(cur, entry)) return state;
  const trimmed = state.entries.slice(0, state.index + 1);
  trimmed.push(entry);
  if (trimmed.length > maxLen) trimmed.splice(0, trimmed.length - maxLen);
  return { entries: trimmed, index: trimmed.length - 1 };
}
