<script lang="ts">
  import { app, cssZoom } from "../lib/store.svelte";
  import { abbreviateKind, defaultExpandedGroups, groupResources, resourceKey } from "../lib/resources";
  import { measureTextWidth } from "../lib/format";
  import type { ResourceType } from "../lib/api";

  const DEFAULT_W = 216;
  const MIN_W = 144;

  let query = $state("");
  let toggled = $state<Record<string, boolean>>({});
  let width = $state(DEFAULT_W);
  let resizing = $state(false);

  const groups = $derived(groupResources(app.resources));
  const pinned = $derived(app.resources.filter((r) => app.isPinned(r)));

  const maxWidth = $derived.by(() => {
    let maxText = 0;
    for (const r of app.resources) {
      maxText = Math.max(maxText, measureTextWidth(abbreviateKind(r.kind), '13px "Google Sans Flex", sans-serif'));
    }
    for (const g of groups) {
      maxText = Math.max(maxText, measureTextWidth(g.name.toUpperCase(), '600 11px "Google Sans Flex", sans-serif'));
    }
    // Account for left/right padding (24+16), pin slot (24), and scrollbar (12).
    return Math.max(DEFAULT_W, maxText + 76);
  });

  $effect(() => {
    if (width > maxWidth) width = maxWidth;
  });

  function startResize(e: PointerEvent) {
    e.preventDefault();
    const z = cssZoom();
    const startX = e.clientX;
    const startW = width;
    resizing = true;
    const move = (ev: PointerEvent) => {
      width = Math.min(maxWidth, Math.max(MIN_W, Math.round(startW + (ev.clientX - startX) / z)));
    };
    const up = () => {
      resizing = false;
      window.removeEventListener("pointermove", move);
      window.removeEventListener("pointerup", up);
    };
    window.addEventListener("pointermove", move);
    window.addEventListener("pointerup", up);
  }

  function isCollapsed(name: string): boolean {
    if (name in toggled) return toggled[name];
    return !defaultExpandedGroups.has(name);
  }

  function toggleGroup(name: string) {
    toggled[name] = !isCollapsed(name);
  }

  function matches(r: ResourceType, q: string) {
    if (!q) return true;
    q = q.toLowerCase();
    return (
      r.kind.toLowerCase().includes(q) ||
      abbreviateKind(r.kind).toLowerCase().includes(q) ||
      r.resource.includes(q) ||
      (r.shortNames ?? []).some((s) => s === q) ||
      r.group.includes(q)
    );
  }

  const filtered = $derived(
    groups
      .map((g) => ({ ...g, items: g.items.filter((r) => matches(r, query.trim())) }))
      .filter((g) => g.items.length > 0),
  );

  function isActive(r: ResourceType) {
    return !app.activePageTab && app.current != null && resourceKey(app.current) === resourceKey(r);
  }
</script>

{#snippet item(r: ResourceType)}
  {@const displayKind = abbreviateKind(r.kind)}
  <li>
    <button
      class="item"
      class:active={isActive(r)}
      onclick={() => app.selectResource(r)}
      title={`Browse ${r.kind} (${r.resource}.${r.group || "core"}/${r.version})`}
    >
      <span class="kind">{displayKind}</span>
      <span
        class="pin"
        class:on={app.isPinned(r)}
        role="button"
        tabindex="-1"
        title={app.isPinned(r) ? `Unpin ${r.kind}` : `Pin ${r.kind} to sidebar`}
        onclick={(e) => {
          e.stopPropagation();
          app.togglePin(r);
        }}
        onkeydown={() => {}}>★</span
      >
    </button>
  </li>
{/snippet}

<nav class="sidebar" class:resizing style={`width:${width}px`}>
  <div class="search-wrap">
    <input class="search" placeholder="Find resource type…" bind:value={query} />
    {#if query}
      <button class="clear-btn" onclick={() => (query = "")} title="Clear search" aria-label="Clear search">✕</button>
    {/if}
  </div>
  {#if app.resourcesLoading}
    <p class="hint">Discovering resources…</p>
  {/if}
  <div class="scroll">
    {#if pinned.length && !query}
      <section>
        <h4>Pinned</h4>
        <ul>
          {#each pinned as r (resourceKey(r))}{@render item(r)}{/each}
        </ul>
      </section>
    {/if}
    {#each filtered as g, i (g.name)}
      {#if g.crd && (i === 0 || !filtered[i - 1].crd)}
        <div class="crd-separator" role="separator">
          <span>CRDs</span>
        </div>
      {/if}
      {@const closed = isCollapsed(g.name)}
      <section>
        <h4>
          <button
            class="group"
            onclick={() => toggleGroup(g.name)}
            title={closed ? `Expand ${g.name} group` : `Collapse ${g.name} group`}
          >
            <span class="chev" class:closed={closed && !query}>▾</span>{g.name}
          </button>
        </h4>
        {#if !closed || query}
          <ul>
            {#each g.items as r (resourceKey(r))}{@render item(r)}{/each}
          </ul>
        {/if}
      </section>
    {/each}
  </div>
  <div
    class="resizer"
    role="separator"
    aria-orientation="vertical"
    onpointerdown={startResize}
    ondblclick={() => (width = DEFAULT_W)}
  ></div>
</nav>

<style>
  .sidebar {
    position: relative;
    display: flex;
    flex-direction: column;
    border-right: 1px solid var(--border);
    background: var(--panel);
    min-height: 0;
  }
  .sidebar.resizing {
    user-select: none;
  }
  .resizer {
    position: absolute;
    top: 0;
    right: -3px;
    bottom: 0;
    width: 6px;
    cursor: col-resize;
    z-index: 2;
  }
  .search-wrap {
    position: relative;
    display: flex;
    align-items: center;
    margin: var(--space-2);
  }
  .search {
    width: 100%;
    height: var(--control-h);
    padding-right: var(--space-4);
  }
  .clear-btn {
    position: absolute;
    right: var(--space-1);
    min-height: 0;
    width: 20px;
    height: 20px;
    padding: 0;
    border: 0;
    background: transparent;
    color: var(--muted);
    font-size: 11px;
    line-height: 1;
  }
  .clear-btn:hover:not(:disabled) {
    color: var(--text-strong);
    background: var(--hover);
  }
  .hint {
    margin: 0 var(--space-3) var(--space-2);
    color: var(--muted);
    font-size: 12px;
  }
  .scroll {
    overflow-y: auto;
    flex: 1;
    padding-bottom: var(--space-3);
  }
  .crd-separator {
    display: flex;
    align-items: center;
    gap: var(--space-2);
    margin: var(--space-2) var(--space-3) var(--space-1);
    color: var(--muted);
    font-size: 11px;
  }
  .crd-separator::after {
    content: "";
    flex: 1;
    height: 1px;
    background: var(--border);
  }
  section {
    margin-bottom: var(--space-1);
  }
  h4 {
    margin: var(--space-2) 0 var(--space-1);
    padding: 0 var(--space-3);
    font-size: 11px;
    text-transform: uppercase;
    letter-spacing: 0.06em;
    color: var(--muted);
    font-weight: 600;
  }
  .group {
    all: unset;
    cursor: pointer;
    display: flex;
    align-items: center;
    gap: var(--space-1);
    width: 100%;
    overflow: hidden;
    text-overflow: ellipsis;
    white-space: nowrap;
  }
  .chev {
    display: inline-block;
    transition: transform 0.15s;
  }
  .chev.closed {
    transform: rotate(-90deg);
  }
  ul {
    list-style: none;
    margin: 0;
    padding: 0;
  }
  .item {
    all: unset;
    box-sizing: border-box;
    display: flex;
    align-items: center;
    width: 100%;
    min-height: var(--control-h-sm);
    padding: 0 var(--space-3) 0 var(--space-4);
    cursor: pointer;
    font-size: 13px;
    border-left: 2px solid transparent;
  }
  .item:hover {
    background: var(--hover);
  }
  .item.active {
    background: var(--selected);
    border-left-color: var(--accent);
    color: var(--text-strong);
  }
  .kind {
    flex: 1;
    overflow: hidden;
    text-overflow: ellipsis;
    white-space: nowrap;
  }
  .pin {
    opacity: 0;
    color: var(--muted);
    font-size: 12px;
    padding: 0 var(--space-1);
  }
  .item:hover .pin,
  .pin.on {
    opacity: 1;
  }
  .pin.on {
    color: var(--accent);
  }
</style>
