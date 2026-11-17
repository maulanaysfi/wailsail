<script lang="ts">
  import { app } from "../lib/store.svelte";
  import { groupResources, resourceKey } from "../lib/resources";
  import type { ResourceType } from "../lib/api";

  let query = $state("");
  let collapsed = $state<Record<string, boolean>>({});

  const groups = $derived(groupResources(app.resources));
  const pinned = $derived(app.resources.filter((r) => app.isPinned(r)));

  function matches(r: ResourceType, q: string) {
    if (!q) return true;
    q = q.toLowerCase();
    return (
      r.kind.toLowerCase().includes(q) ||
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
    return app.current != null && resourceKey(app.current) === resourceKey(r);
  }
</script>

{#snippet item(r: ResourceType)}
  <li>
    <button
      class="item"
      class:active={isActive(r)}
      onclick={() => app.selectResource(r)}
      title={`Browse ${r.kind} (${r.resource}.${r.group || "core"}/${r.version})`}
    >
      <span class="kind">{r.kind}</span>
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

<nav class="sidebar">
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
    {#each filtered as g (g.name)}
      <section>
        <h4>
          <button
            class="group"
            onclick={() => (collapsed[g.name] = !collapsed[g.name])}
            title={collapsed[g.name] ? `Expand ${g.name} group` : `Collapse ${g.name} group`}
          >
            <span class="chev" class:closed={collapsed[g.name] && !query}>▾</span>{g.name}
          </button>
        </h4>
        {#if !collapsed[g.name] || query}
          <ul>
            {#each g.items as r (resourceKey(r))}{@render item(r)}{/each}
          </ul>
        {/if}
      </section>
    {/each}
  </div>
</nav>

<style>
  .sidebar {
    display: flex;
    flex-direction: column;
    border-right: 1px solid var(--border);
    background: var(--panel);
    min-height: 0;
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
