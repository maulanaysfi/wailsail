<script lang="ts">
  import { untrack } from "svelte";
  import {
    Kube,
    newStreamId,
    on,
    topics,
    errorMessage,
    type Column,
    type ContainerMetrics,
    type Row,
    type WatchEvent,
  } from "../lib/api";
  import { app, cssZoom, refOf } from "../lib/store.svelte";
  import { age, buildResourceMeter, compareCells, toneFor, type ResourceMeter } from "../lib/format";
  import { kindKey, resourceKey } from "../lib/resources";

  const ROW_H = 32;
  const MIN_COL_W = 48;

  const ownableKinds = new Set(["/Pod", "apps/ReplicaSet", "batch/Job"]);

  const rows = new Map<string, Row>();
  let version = $state(0);
  let totalRows = $state(0);
  let columns = $state<Column[]>([]);
  let podMetrics = $state<Record<string, ContainerMetrics>>({});
  let loading = $state(true);
  let error = $state("");
  let filter = $state("");
  let sortKey = $state("name");
  let sortAsc = $state(true);
  let scrollTop = $state(0);
  let viewportH = $state(600);

  let colWidths = $state<Record<string, number>>({});
  let resizingKey = $state("");
  let draggingKey = $state("");
  let dropTargetKey = $state("");
  let dropAfter = $state(false);
  let headerRowEl: HTMLTableRowElement | undefined = $state();

  const allNamespaces = $derived(!!app.current?.namespaced && app.namespace === "");
  const curKindKey = $derived(app.current ? kindKey(app.current) : "");
  const isPod = $derived(curKindKey === "/Pod");
  const hasOwnerAction = $derived(ownableKinds.has(curKindKey));
  const hasActionsCol = $derived(isPod || hasOwnerAction);
  const actionsColW = $derived(isPod ? 208 : 120);
  const resScopeKey = $derived(app.current ? resourceKey(app.current) : "_");

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

  // Poll live Pod metrics while on the Pod page.
  $effect(() => {
    const ctx = app.context;
    const ns = app.namespace;
    const podView = isPod;
    if (!ctx || !podView) {
      podMetrics = {};
      return;
    }
    let cancelled = false;
    const pull = () => {
      Kube.PodMetrics(ctx, ns)
        .then((m) => {
          if (!cancelled) podMetrics = (m as Record<string, ContainerMetrics>) ?? {};
        })
        .catch(() => {});
    };
    pull();
    const timer = setInterval(pull, 5000);
    return () => {
      cancelled = true;
      clearInterval(timer);
    };
  });

  function podUsageFor(r: Row): ContainerMetrics | undefined {
    const base = `${r.namespace}/${r.name}`;
    const mainName = r.podResources?.container;
    if (mainName && podMetrics[`${base}/${mainName}`]) {
      return podMetrics[`${base}/${mainName}`];
    }
    return podMetrics[base];
  }

  function podMeterFor(r: Row, kind: "cpu" | "memory"): ResourceMeter {
    const u = podUsageFor(r);
    const pr = r.podResources;
    if (kind === "cpu") {
      return buildResourceMeter("cpu", u ? u.cpuMilli : null, pr?.cpuRequest, pr?.cpuLimit);
    }
    return buildResourceMeter("memory", u ? u.memoryBytes : null, pr?.memoryRequest, pr?.memoryLimit);
  }

  interface DisplayCol {
    key: string;
    id: string;
    label: string;
    defaultWidth: number;
    get: (r: Row) => string;
    meter?: "cpu" | "memory";
    status?: boolean;
  }

  function defaultWidthFor(name: string, index: number): number {
    const n = name.toLowerCase();
    if (index === 0 || n === "name") return 240;
    if (n === "namespace") return 136;
    if (n === "cpu usage" || n === "memory usage") return 168;
    if (n === "ready" || n === "restarts" || n === "age" || n === "desired" || n === "current" || n === "up-to-date" || n === "available") return 88;
    if (n === "status" || n === "phase" || n === "type" || n === "roles") return 120;
    if (n === "ip" || n === "cluster-ip" || n === "external-ip" || n === "internal-ip") return 136;
    if (n === "node" || n === "nominated node") return 168;
    if (n === "containers" || n === "images" || n === "selector" || n === "ports") return 200;
    return 144;
  }

  const baseCols = $derived.by<DisplayCol[]>(() => {
    const out: DisplayCol[] = [];
    const podUsageCols: DisplayCol[] = isPod
      ? [
          {
            key: "cpu-usage",
            id: "CPU Usage",
            label: "CPU Usage",
            defaultWidth: 168,
            meter: "cpu",
            get: (r) => podMeterFor(r, "cpu").usedText,
          },
          {
            key: "memory-usage",
            id: "Memory Usage",
            label: "Memory Usage",
            defaultWidth: 168,
            meter: "memory",
            get: (r) => podMeterFor(r, "memory").usedText,
          },
        ]
      : [];
    let insertedUsage = false;

    columns.forEach((c, i) => {
      const isStatus = c.name === "Status" || c.name === "Phase";
      out.push({
        key: `c${i}`,
        id: c.name,
        label: c.name,
        defaultWidth: defaultWidthFor(c.name, i),
        get: (r) => r.cells[i] ?? "",
        status: isStatus,
      });
      if (i === 0 && allNamespaces) {
        out.push({
          key: "namespace",
          id: "Namespace",
          label: "Namespace",
          defaultWidth: defaultWidthFor("Namespace", -1),
          get: (r) => r.namespace,
        });
      }
      if (isStatus && !insertedUsage && podUsageCols.length) {
        out.push(...podUsageCols);
        insertedUsage = true;
      }
    });
    if (columns.length === 0) {
      out.push({ key: "name", id: "Name", label: "Name", defaultWidth: 240, get: (r) => r.name });
    }
    if (!insertedUsage && podUsageCols.length) {
      out.push(...podUsageCols);
    }
    out.push({
      key: "age",
      id: "Age",
      label: "Age",
      defaultWidth: 88,
      get: (r) => age(r.created, app.now),
    });
    return out;
  });

  const displayCols = $derived.by<DisplayCol[]>(() => {
    const order = app.settings.columnOrders?.[resScopeKey];
    if (!order || order.length === 0) return baseCols;
    const byId = new Map<string, DisplayCol>();
    for (const c of baseCols) {
      byId.set(c.id, c);
      byId.set(c.key, c);
    }
    const used = new Set<string>();
    const ordered: DisplayCol[] = [];
    for (const token of order) {
      const col = byId.get(token);
      if (col && !used.has(col.key)) {
        ordered.push(col);
        used.add(col.key);
      }
    }
    for (const col of baseCols) {
      if (!used.has(col.key)) ordered.push(col);
    }
    return ordered;
  });

  function widthKey(c: DisplayCol): string {
    return `${resScopeKey}:${c.id}`;
  }

  function colWidth(c: DisplayCol): number {
    const wk = widthKey(c);
    return colWidths[wk] ?? app.settings.columnWidths?.[wk] ?? c.defaultWidth;
  }

  const totalTableW = $derived(
    displayCols.reduce((sum, c) => sum + colWidth(c), 0) + (hasActionsCol ? actionsColW : 0),
  );

  function startColResize(e: PointerEvent, c: DisplayCol) {
    e.stopPropagation();
    e.preventDefault();
    const wk = widthKey(c);
    const z = cssZoom();
    const startX = e.clientX;
    const startW = colWidth(c);
    resizingKey = c.key;

    const move = (ev: PointerEvent) => {
      const next = Math.max(MIN_COL_W, Math.round(startW + (ev.clientX - startX) / z));
      colWidths[wk] = next;
    };
    const up = () => {
      resizingKey = "";
      window.removeEventListener("pointermove", move);
      window.removeEventListener("pointerup", up);
      if (colWidths[wk] !== undefined) {
        app.saveSettings({
          columnWidths: {
            ...(app.settings.columnWidths ?? {}),
            [wk]: colWidths[wk],
          },
        });
      }
    };
    window.addEventListener("pointermove", move);
    window.addEventListener("pointerup", up);
  }

  function resetColWidth(e: MouseEvent, c: DisplayCol) {
    e.stopPropagation();
    const wk = widthKey(c);
    delete colWidths[wk];
    const nextWidths = { ...(app.settings.columnWidths ?? {}) };
    if (wk in nextWidths) {
      delete nextWidths[wk];
      app.saveSettings({ columnWidths: nextWidths });
    }
  }

  function startHeaderPointer(e: PointerEvent, c: DisplayCol) {
    if (e.button !== 0) return;
    if ((e.target as HTMLElement)?.closest(".col-resizer")) return;
    const startX = e.clientX;
    let moved = false;

    const move = (ev: PointerEvent) => {
      if (!moved && Math.abs(ev.clientX - startX) > 5) {
        moved = true;
        draggingKey = c.key;
      }
      if (!moved || !headerRowEl) return;
      const ths = Array.from(headerRowEl.querySelectorAll<HTMLTableCellElement>("th[data-col-key]"));
      let target = "";
      let after = false;
      for (const th of ths) {
        const k = th.dataset.colKey ?? "";
        const rect = th.getBoundingClientRect();
        if (ev.clientX >= rect.left && ev.clientX <= rect.right) {
          target = k;
          after = ev.clientX > rect.left + rect.width / 2;
          break;
        }
      }
      dropTargetKey = target && target !== c.key ? target : "";
      dropAfter = after;
    };

    const up = () => {
      window.removeEventListener("pointermove", move);
      window.removeEventListener("pointerup", up);
      if (moved) {
        if (dropTargetKey && dropTargetKey !== c.key) {
          const cur = displayCols.filter((col) => col.key !== c.key);
          const idx = cur.findIndex((col) => col.key === dropTargetKey);
          if (idx !== -1) {
            cur.splice(dropAfter ? idx + 1 : idx, 0, c);
            app.saveSettings({
              columnOrders: {
                ...(app.settings.columnOrders ?? {}),
                [resScopeKey]: cur.map((col) => col.id),
              },
            });
          }
        }
        draggingKey = "";
        dropTargetKey = "";
      } else {
        sortBy(c.key);
      }
    };

    window.addEventListener("pointermove", move);
    window.addEventListener("pointerup", up);
  }

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
    else if (key === "cpu-usage") cmp = (a, b) => (podUsageFor(a)?.cpuMilli ?? 0) - (podUsageFor(b)?.cpuMilli ?? 0) || a.name.localeCompare(b.name);
    else if (key === "memory-usage") cmp = (a, b) => (podUsageFor(a)?.memoryBytes ?? 0) - (podUsageFor(b)?.memoryBytes ?? 0) || a.name.localeCompare(b.name);
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
    if (isSelected(r)) {
      app.selection = null;
      return;
    }
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

  function jumpOwner(r: Row, e: MouseEvent) {
    e.stopPropagation();
    app.jumpToOwner(r.owner, r.namespace);
  }
</script>

{#snippet cellMeter(meter: ResourceMeter)}
  {#if meter.showBar}
    <div class="cell-meter">
      <div class="meter-track" title={meter.hoverText}>
        <div class={`meter-fill tone-bg-${meter.tone}`} style={`width:${meter.usedPct}%`}></div>
        {#if meter.requestMarkPct !== null}
          <span class="meter-req-mark" class:warn={meter.requestMarkWarn} style={`left:${meter.requestMarkPct}%`}></span>
        {/if}
      </div>
      <span class="cell-meter-text">{meter.usedText}</span>
    </div>
  {:else}
    <span class="cell-meter-plain">{meter.usedText}</span>
  {/if}
{/snippet}

<section class="table-pane" class:resizing={resizingKey !== ""} class:dragging={draggingKey !== ""}>
  <div class="toolbar">
    <h2>{app.current?.kind ?? "—"}</h2>
    <span class="count">{view.length}{filter ? ` / ${totalRows}` : ""}</span>
    <input class="filter" placeholder="Filter…" bind:value={filter} />
  </div>

  {#if error}
    <div class="banner error">{error}</div>
  {/if}

  <div class="grid-wrap" bind:clientHeight={viewportH} onscroll={(e) => (scrollTop = e.currentTarget.scrollTop)}>
    <table style={`width:${totalTableW}px`}>
      <colgroup>
        {#each displayCols as c (c.key)}
          <col style={`width:${colWidth(c)}px`} />
        {/each}
        {#if hasActionsCol}<col style={`width:${actionsColW}px`} />{/if}
      </colgroup>
      <thead>
        <tr bind:this={headerRowEl}>
          {#each displayCols as c (c.key)}
            <th
              data-col-key={c.key}
              class:is-dragging={draggingKey === c.key}
              class:drop-before={dropTargetKey === c.key && !dropAfter}
              class:drop-after={dropTargetKey === c.key && dropAfter}
              onpointerdown={(e) => startHeaderPointer(e, c)}
            >
              <span class="th-label">
                {c.label}
                {#if sortKey === c.key || (c.key === "c0" && sortKey === "name")}<span class="arrow">{sortAsc ? "▲" : "▼"}</span>{/if}
              </span>
              <span
                class="col-resizer"
                class:active={resizingKey === c.key}
                role="separator"
                aria-orientation="vertical"
                onpointerdown={(e) => startColResize(e, c)}
                ondblclick={(e) => resetColWidth(e, c)}
              ></span>
            </th>
          {/each}
          {#if hasActionsCol}<th class="actions-h"></th>{/if}
        </tr>
      </thead>
      <tbody>
        {#if first > 0}<tr class="spacer" style={`height:${first * ROW_H}px`}></tr>{/if}
        {#each visible as r (r.key)}
          <tr class:selected={isSelected(r)} onclick={() => select(r)}>
            {#each displayCols as c (c.key)}
              {#if c.meter}
                <td>{@render cellMeter(podMeterFor(r, c.meter))}</td>
              {:else}
                {@const v = c.get(r)}
                <td class={c.status ? `tone-${toneFor(v)}` : ""} class:name-col={c.key === "c0" || c.key === "name"}>{v}</td>
              {/if}
            {/each}
            {#if hasActionsCol}
              <td class="actions">
                {#if hasOwnerAction}
                  <button
                    class="mini"
                    disabled={!r.owner}
                    onclick={(e) => jumpOwner(r, e)}
                    title={r.owner ? `Jump to owner ${r.owner.kind}/${r.owner.name}` : "No owner reference"}
                  >
                    Jump to Owner
                  </button>
                {/if}
                {#if isPod}
                  <button class="mini" onclick={(e) => openPod("logs", r, e)} title={`Stream container logs for pod ${r.name}`}>
                    Logs
                  </button>
                  <button class="mini" onclick={(e) => openPod("shell", r, e)} title={`Open interactive shell in pod ${r.name}`}>
                    Shell
                  </button>
                {/if}
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
  .table-pane.resizing {
    cursor: col-resize;
    user-select: none;
  }
  .table-pane.dragging {
    cursor: grabbing;
    user-select: none;
  }
  .toolbar {
    display: flex;
    align-items: center;
    gap: var(--space-2);
    min-height: 48px;
    padding: var(--space-2) var(--space-3);
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
    height: var(--control-h);
  }
  .grid-wrap {
    flex: 1;
    overflow: auto;
    min-height: 0;
    position: relative;
  }
  table {
    table-layout: fixed;
    min-width: 100%;
    border-collapse: collapse;
    font-size: 12.5px;
  }
  thead th {
    position: sticky;
    top: 0;
    z-index: 2;
    background: var(--bg);
    text-align: left;
    font-weight: 600;
    color: var(--muted);
    height: var(--control-h);
    padding: 0 var(--space-2);
    border-bottom: 1px solid var(--border);
    border-right: 1px solid var(--border-soft);
    cursor: grab;
    user-select: none;
    white-space: nowrap;
    overflow: hidden;
    text-overflow: clip;
  }
  thead th:active {
    cursor: grabbing;
  }
  thead th.is-dragging {
    opacity: 0.55;
    background: var(--hover);
  }
  thead th.drop-before {
    box-shadow: inset 2px 0 0 var(--accent);
  }
  thead th.drop-after {
    box-shadow: inset -2px 0 0 var(--accent);
  }
  .th-label {
    display: block;
    overflow: hidden;
    text-overflow: clip;
    white-space: nowrap;
    padding-right: var(--space-1);
  }
  .col-resizer {
    position: absolute;
    top: 0;
    right: -3px;
    bottom: 0;
    width: 7px;
    cursor: col-resize;
    z-index: 3;
  }
  .col-resizer:hover,
  .col-resizer.active {
    background: var(--accent);
  }
  .arrow {
    font-size: 9px;
    margin-left: var(--space-1);
  }
  tbody tr {
    height: 32px;
    cursor: pointer;
    background: var(--bg);
  }
  tbody tr:not(.spacer):hover {
    background: var(--hover);
  }
  tbody tr.selected {
    background: var(--selected);
  }
  td {
    position: relative;
    background: inherit;
    padding: 0 var(--space-2);
    border-bottom: 1px solid var(--border-soft);
    white-space: nowrap;
    overflow: hidden;
    text-overflow: clip;
  }
  td.name-col {
    color: var(--text-strong);
  }
  .cell-meter {
    display: grid;
    grid-template-columns: minmax(48px, 1fr) 56px;
    align-items: center;
    gap: var(--space-2);
  }
  .cell-meter-text,
  .cell-meter-plain {
    font-family: var(--mono);
    font-size: 11px;
    font-variant-numeric: tabular-nums;
    color: var(--text);
  }
  .cell-meter-text {
    text-align: right;
    overflow: hidden;
    text-overflow: clip;
  }
  .actions-h,
  .actions {
    text-align: right;
    cursor: default;
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
    margin-top: var(--space-6);
  }
  .banner {
    margin: 0 var(--space-3) var(--space-2);
  }
</style>
