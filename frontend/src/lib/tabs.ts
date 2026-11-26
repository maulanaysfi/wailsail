import { pushNavEntry, type NavEntry, type NavState } from "./nav.ts";
import { abbreviateKind, resourceKey } from "./resources.ts";
import type { ResourceType } from "./api";

export interface Selection {
  type: ResourceType;
  namespace: string;
  name: string;
}

export interface PageTab {
  id: string;
  selection: Selection;
  nav: NavState;
}

/** Stable identity key for deduplicating opened object tabs. */
export function selectionKey(sel: Selection): string {
  return `${resourceKey(sel.type)}\0${sel.namespace}\0${sel.name}`;
}

/** Format tab label with abbreviated resource kind prefix (e.g. Pod/web-0, PVC/data). */
export function formatPageTabTitle(sel: Selection): string {
  return `${abbreviateKind(sel.type.kind)}/${sel.name}`;
}

/** Find an existing opened tab currently showing the exact selection. */
export function findTabBySelection(tabs: PageTab[], sel: Selection): PageTab | undefined {
  const key = selectionKey(sel);
  return tabs.find((t) => selectionKey(t.selection) === key);
}

/** Build a navigation history entry representing an opened object. */
export function navEntryFromSelection(sel: Selection): NavEntry {
  const rk = resourceKey(sel.type);
  return {
    resourceKey: rk,
    selection: { resourceKey: rk, namespace: sel.namespace, name: sel.name },
  };
}

/** Focus an existing matching tab or append and activate a new object tab. */
export function openOrFocusTab(
  tabs: PageTab[],
  sel: Selection,
  makeId: () => string,
): { tabs: PageTab[]; activeId: string } {
  const existing = findTabBySelection(tabs, sel);
  if (existing) {
    return { tabs, activeId: existing.id };
  }
  const tab: PageTab = {
    id: makeId(),
    selection: { ...sel },
    nav: { entries: [navEntryFromSelection(sel)], index: 0 },
  };
  return { tabs: [...tabs, tab], activeId: tab.id };
}

/** Navigate a specific page tab to a new object selection and record its history step. */
export function navigatePageTab(tabs: PageTab[], tabId: string, sel: Selection): PageTab[] {
  return tabs.map((t) =>
    t.id === tabId
      ? {
          ...t,
          selection: { ...sel },
          nav: pushNavEntry(t.nav, navEntryFromSelection(sel)),
        }
      : t,
  );
}

/** Close a page tab by id and choose the next active tab (or null for main tab). */
export function closePageTabState(
  tabs: PageTab[],
  activeId: string | null,
  id: string,
): { tabs: PageTab[]; activeId: string | null } {
  const idx = tabs.findIndex((t) => t.id === id);
  if (idx === -1) return { tabs, activeId };
  const next = tabs.filter((t) => t.id !== id);
  if (activeId !== id) return { tabs: next, activeId };
  return { tabs: next, activeId: next[Math.max(0, idx - 1)]?.id ?? null };
}
