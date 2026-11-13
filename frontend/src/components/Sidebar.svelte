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
    <button class="item" class:active={isActive(r)} onclick={() => app.selectResource(r)} title={`${r.resource}.${r.group || "core"}/${r.version}`}>
      <span class="kind">{r.kind}</span>
      <span
        class="pin"
        class:on={app.isPinned(r)}
        role="button"
        tabindex="-1"
        title={app.isPinned(r) ? "Unpin" : "Pin"}
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
  <input class="search" placeholder="Find resource type…" bind:value={query} />
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
          <button class="group" onclick={() => (collapsed[g.name] = !collapsed[g.name])}>
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
  .search {
    margin: 10px;
  }
  .hint {
    margin: 0 12px 8px;
    color: var(--muted);
    font-size: 12px;
  }
  .scroll {
    overflow-y: auto;
    flex: 1;
    padding-bottom: 12px;
  }
  section {
    margin-bottom: 4px;
  }
  h4 {
    margin: 8px 0 2px;
    padding: 0 12px;
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
    gap: 4px;
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
    padding: 4px 12px 4px 22px;
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
    padding: 0 2px;
  }
  .item:hover .pin,
  .pin.on {
    opacity: 1;
  }
  .pin.on {
    color: var(--accent);
  }
</style>
