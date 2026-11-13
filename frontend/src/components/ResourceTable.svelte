<script lang="ts">
  import { untrack } from "svelte";
  import { Kube, newStreamId, on, topics, errorMessage, type Column, type Row, type WatchEvent } from "../lib/api";
  import { app, refOf } from "../lib/store.svelte";
  import { age, compareCells, toneFor } from "../lib/format";
  import { kindKey } from "../lib/resources";

  const ROW_H = 28;

  const rows = new Map<string, Row>();
  let version = $state(0);
  let totalRows = $state(0);
  let columns = $state<Column[]>([]);
  let loading = $state(true);
  let error = $state("");
  let filter = $state("");
  let sortKey = $state("name");
  let sortAsc = $state(true);
  let scrollTop = $state(0);
  let viewportH = $state(600);

  const allNamespaces = $derived(!!app.current?.namespaced && app.namespace === "");
  const isPod = $derived(app.current ? kindKey(app.current) === "/Pod" : false);

  // Start a watch whenever the context, resource type or namespace changes.
  $effect(() => {
    const ctx = app.context;
    const r = app.current;
    const ns = r?.namespaced ? app.namespace : "";
    if (!ctx || !r) return;
    untrack(() => {
      rows.clear();
      totalRows = 0;
      columns = [];
      loading = true;
      error = "";
      scrollTop = 0;
      version++;
    });
    const id = newStreamId();
    const off = on<WatchEvent>(topics.watch(id), (ev) => {
      if (ev.reset) {
        rows.clear();
        if (ev.columns) columns = ev.columns;
        loading = false;
      }
      for (const r of ev.upserts ?? []) rows.set(r.key, r);
      for (const k of ev.deletes ?? []) rows.delete(k);
      totalRows = rows.size;
      error = ev.error ?? (ev.reset ? "" : error);
      if (ev.error) loading = false;
      version++;
    });
    Kube.Watch(ctx, refOf(r), ns, id).catch((e) => {
      error = errorMessage(e);
      loading = false;
    });
    return () => {
      off();
      Kube.StopWatch(id);
    };
  });

  interface DisplayCol {
    key: string;
    label: string;
    title: string;
    get: (r: Row) => string;
    status?: boolean;
  }

  const displayCols = $derived.by<DisplayCol[]>(() => {
    const out: DisplayCol[] = [];
    columns.forEach((c, i) => {
      if (c.priority > 0 && !app.settings.showWide) return;
      out.push({
        key: `c${i}`,
        label: c.name,
        title: c.description,
        get: (r) => r.cells[i] ?? "",
        status: c.name === "Status" || c.name === "Phase",
      });
      if (i === 0 && allNamespaces) {
        out.push({ key: "namespace", label: "Namespace", title: "", get: (r) => r.namespace });
      }
    });
    if (columns.length === 0) out.push({ key: "name", label: "Name", title: "", get: (r) => r.name });
    out.push({ key: "age", label: "Age", title: "Time since creation", get: (r) => age(r.created, app.now) });
    return out;
  });

  const view = $derived.by(() => {
    version; // re-run on row changes
    const q = filter.trim().toLowerCase();
    let list = [...rows.values()];
    if (q) {
      list = list.filter(
        (r) => r.name.toLowerCase().includes(q) || r.namespace.toLowerCase().includes(q) || r.cells.some((c) => c.toLowerCase().includes(q)),
      );
    }
    const dir = sortAsc ? 1 : -1;
    const key = sortKey;
    let cmp: (a: Row, b: Row) => number;
    if (key === "age") cmp = (a, b) => b.created.localeCompare(a.created);
    else if (key === "namespace") cmp = (a, b) => a.namespace.localeCompare(b.namespace) || a.name.localeCompare(b.name);
    else if (key === "name" || key === "c0") cmp = (a, b) => compareCells(a.name, b.name) || a.namespace.localeCompare(b.namespace);
    else {
      const i = Number(key.slice(1));
      cmp = (a, b) => compareCells(a.cells[i] ?? "", b.cells[i] ?? "") || a.name.localeCompare(b.name);
    }
    list.sort((a, b) => dir * cmp(a, b));
    return list;
  });

  const first = $derived(Math.max(0, Math.floor(scrollTop / ROW_H) - 10));
  const last = $derived(Math.min(view.length, Math.ceil((scrollTop + viewportH) / ROW_H) + 10));
  const visible = $derived(view.slice(first, last));

  function sortBy(key: string) {
    if (sortKey === key || (key === "c0" && sortKey === "name")) sortAsc = !sortAsc;
    else {
      sortKey = key;
      sortAsc = true;
    }
  }

  function select(r: Row) {
    if (!app.current) return;
    app.selection = { type: app.current, namespace: r.namespace, name: r.name };
  }

  function isSelected(r: Row) {
    const s = app.selection;
    return !!s && s.name === r.name && s.namespace === r.namespace;
  }

  function openPod(kind: "logs" | "shell", r: Row, e: MouseEvent) {
    e.stopPropagation();
    app.openDock({ kind, title: r.name, context: app.context, namespace: r.namespace, pod: r.name });
  }
</script>

<section class="table-pane">
  <div class="toolbar">
    <h2>{app.current?.kind ?? "—"}</h2>
    <span class="count">{view.length}{filter ? ` / ${totalRows}` : ""}</span>
    <input class="filter" placeholder="Filter…" bind:value={filter} />
    <label class="check">
      <input type="checkbox" checked={app.settings.showWide} onchange={(e) => app.saveSettings({ showWide: e.currentTarget.checked })} />
      Wide
    </label>
  </div>

  {#if error}
    <div class="banner error">{error}</div>
  {/if}

  <div class="grid-wrap" bind:clientHeight={viewportH} onscroll={(e) => (scrollTop = e.currentTarget.scrollTop)}>
    <table style={`--cols:${displayCols.length}`}>
      <thead>
        <tr>
          {#each displayCols as c (c.key)}
            <th title={c.title} onclick={() => sortBy(c.key)}>
              {c.label}
              {#if sortKey === c.key || (c.key === "c0" && sortKey === "name")}<span class="arrow">{sortAsc ? "▲" : "▼"}</span>{/if}
            </th>
          {/each}
          {#if isPod}<th class="actions-h"></th>{/if}
        </tr>
      </thead>
      <tbody>
        {#if first > 0}<tr class="spacer" style={`height:${first * ROW_H}px`}></tr>{/if}
        {#each visible as r (r.key)}
          <tr class:selected={isSelected(r)} onclick={() => select(r)}>
            {#each displayCols as c (c.key)}
              {@const v = c.get(r)}
              <td class={c.status ? `tone-${toneFor(v)}` : ""} title={v}>{v}</td>
            {/each}
            {#if isPod}
              <td class="actions">
                <button class="mini" onclick={(e) => openPod("logs", r, e)}>Logs</button>
                <button class="mini" onclick={(e) => openPod("shell", r, e)}>Shell</button>
              </td>
            {/if}
          </tr>
        {/each}
        {#if last < view.length}<tr class="spacer" style={`height:${(view.length - last) * ROW_H}px`}></tr>{/if}
      </tbody>
    </table>
    {#if loading}
      <p class="empty">Loading…</p>
    {:else if view.length === 0 && !error}
      <p class="empty">{filter ? "No matches" : "No resources"}</p>
    {/if}
  </div>
</section>

<style>
  .table-pane {
    display: flex;
    flex-direction: column;
    min-width: 0;
    min-height: 0;
  }
  .toolbar {
    display: flex;
    align-items: center;
    gap: 10px;
    padding: 10px 14px;
  }
  h2 {
    margin: 0;
    font-size: 16px;
    color: var(--text-strong);
  }
  .count {
    color: var(--muted);
    font-size: 12px;
    font-variant-numeric: tabular-nums;
  }
  .filter {
    margin-left: auto;
    width: 240px;
  }
  .check {
    display: flex;
    align-items: center;
    gap: 4px;
    font-size: 12px;
    color: var(--muted);
  }
  .grid-wrap {
    flex: 1;
    overflow: auto;
    min-height: 0;
    position: relative;
  }
  table {
    width: 100%;
    border-collapse: collapse;
    font-size: 12.5px;
  }
  thead th {
    position: sticky;
    top: 0;
    z-index: 1;
    background: var(--bg);
    text-align: left;
    font-weight: 600;
    color: var(--muted);
    padding: 6px 10px;
    border-bottom: 1px solid var(--border);
    cursor: pointer;
    user-select: none;
    white-space: nowrap;
  }
  .arrow {
    font-size: 9px;
    margin-left: 4px;
  }
  tbody tr {
    height: 28px;
    cursor: pointer;
  }
  tbody tr:not(.spacer):hover {
    background: var(--hover);
  }
  tbody tr.selected {
    background: var(--selected);
  }
  td {
    padding: 0 10px;
    border-bottom: 1px solid var(--border-soft);
    white-space: nowrap;
    overflow: hidden;
    text-overflow: ellipsis;
    max-width: 420px;
  }
  td:first-child {
    color: var(--text-strong);
  }
  .actions-h,
  .actions {
    width: 1%;
    text-align: right;
  }
  .actions .mini {
    visibility: hidden;
  }
  tr:hover .actions .mini {
    visibility: visible;
  }
  .empty {
    text-align: center;
    color: var(--muted);
    margin-top: 40px;
  }
  .banner {
    margin: 0 14px 8px;
  }
</style>
