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
  import { age, buildResourceMeter, clearMeasureTextCache, compareCells, measureTextWidth, toneFor, type ResourceMeter } from "../lib/format";
  import { kindKey, resourceKey, restartableKinds, scalableKinds } from "../lib/resources";
  import { unreadyWorkloadCells, workloadColumnIndices } from "../lib/specs";

  type MenuIcon = "owner" | "forward" | "logs" | "shell" | "scale" | "restart" | "edit" | "delete";

  interface MenuItem {
    id: string;
    label: string;
    icon: MenuIcon;
    danger?: boolean;
    disabled?: boolean;
    sepBefore?: boolean;
    run: () => void;
  }

  const WORKLOAD_LOG_KINDS = new Set([
    "apps/Deployment",
    "apps/ReplicaSet",
    "apps/StatefulSet",
    "apps/DaemonSet",
    "batch/Job",
    "batch/CronJob",
  ]);

  const ROW_H = 32;
  const MIN_COL_W = 48;

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
  let fontGen = $state(0);
  let focusedKey = $state("");
  let menu = $state<{ row: Row; x: number; y: number } | null>(null);
  let menuEl: HTMLDivElement | undefined = $state();

  $effect(() => {
    if (typeof document === "undefined" || !document.fonts) return;
    const invalidate = () => {
      clearMeasureTextCache();
      fontGen++;
    };
    document.fonts.ready.then(invalidate);
    document.fonts.addEventListener?.("loadingdone", invalidate);
    return () => {
      document.fonts.removeEventListener?.("loadingdone", invalidate);
    };
  });

  const allNamespaces = $derived(!!app.current?.namespaced && app.namespace === "");
  const curKindKey = $derived(app.current ? kindKey(app.current) : "");
  const isPod = $derived(curKindKey === "/Pod");
  const resScopeKey = $derived(app.current ? resourceKey(app.current) : "_");

  // Start a watch whenever the context, resource type, namespace or reconnect state changes.
  $effect(() => {
    const ctx = app.context;
    const r = app.current;
    const ns = r?.namespaced ? app.namespace : "";
    app.reconnectNonce;
    if (!ctx || !r) return;
    untrack(() => {
      rows.clear();
      totalRows = 0;
      columns = [];
      loading = true;
      error = "";
      scrollTop = 0;
      focusedKey = "";
      menu = null;
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

  const maxMeterPx = $derived.by<{ cpu: number; memory: number }>(() => {
    version;
    podMetrics;
    fontGen;
    if (!isPod) return { cpu: 0, memory: 0 };
    const font = '12px "Google Sans Flex", -apple-system, BlinkMacSystemFont, "Segoe UI", Roboto, sans-serif';
    let cpu = 0;
    let memory = 0;
    for (const r of rows.values()) {
      const ct = podMeterFor(r, "cpu").usedText;
      const mt = podMeterFor(r, "memory").usedText;
      cpu = Math.max(cpu, measureTextWidth(ct, font, true));
      memory = Math.max(memory, measureTextWidth(mt, font, true));
    }
    return { cpu, memory };
  });

  const workloadColIdx = $derived.by(() => workloadColumnIndices(columns));

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
    if (n === "ready" || n === "restarts" || n === "age" || n === "revision" || n === "desired" || n === "current" || n === "up-to-date" || n === "available") return 88;
    if (n === "status" || n === "phase" || n === "type" || n === "roles" || n === "app version") return 120;
    if (n === "ip" || n === "cluster-ip" || n === "external-ip" || n === "internal-ip") return 136;
    if (n === "node" || n === "nominated node") return 168;
    if (n === "containers" || n === "images" || n === "selector" || n === "ports" || n === "chart") return 200;
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
    displayCols.reduce((sum, c) => sum + colWidth(c), 0),
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
      app.closeSelection();
      return;
    }
    app.selectObject({ type: app.current, namespace: r.namespace, name: r.name });
  }

  function isSelected(r: Row) {
    const s = app.selection;
    return !!s && s.name === r.name && s.namespace === r.namespace;
  }

  function inferReplicas(r: Row): number {
    const dIdx = workloadColIdx.desiredIdx;
    if (dIdx >= 0 && /^\d+$/.test((r.cells[dIdx] ?? "").trim())) {
      return Number(r.cells[dIdx].trim());
    }
    const rIdx = workloadColIdx.readyIdx;
    if (rIdx >= 0) {
      const m = /^\d+\s*\/\s*(\d+)$/.exec((r.cells[rIdx] ?? "").trim());
      if (m) return Number(m[1]);
    }
    return 1;
  }

  function openContextMenu(e: MouseEvent, r: Row) {
    if (!app.current) return;
    e.preventDefault();
    focusedKey = r.key;
    const z = cssZoom();
    menu = { row: r, x: Math.round(e.clientX / z), y: Math.round(e.clientY / z) };
  }

  function menuItemsFor(r: Row): MenuItem[] {
    const type = app.current;
    if (!type) return [];
    const sel = { type, namespace: r.namespace, name: r.name };
    const isHelm = curKindKey === "helm.sh/Release";
    const items: MenuItem[] = [];

    if (curKindKey === "/Pod" || curKindKey === "apps/ReplicaSet" || curKindKey === "batch/Job" || r.owner) {
      items.push({
        id: "owner",
        label: "Jump to Owner",
        icon: "owner",
        disabled: !r.owner,
        run: () => r.owner && app.jumpToOwner(r.owner, r.namespace),
      });
    }
    if (isPod || curKindKey === "/Service") {
      items.push({
        id: "forward",
        label: "Port forward",
        icon: "forward",
        run: () => (app.forwardTarget = sel),
      });
    }
    if (scalableKinds.has(curKindKey)) {
      items.push({
        id: "scale",
        label: "Scale",
        icon: "scale",
        run: () => (app.scaleTarget = { sel, replicas: inferReplicas(r) }),
      });
    }
    if (restartableKinds.has(curKindKey)) {
      items.push({
        id: "restart",
        label: "Restart",
        icon: "restart",
        run: () => void app.restartResource(sel),
      });
    }
    if (isPod) {
      items.push({
        id: "logs",
        label: "View logs",
        icon: "logs",
        run: () => app.openDock({ kind: "logs", title: r.name, context: app.context, namespace: r.namespace, pod: r.name }),
      });
      items.push({
        id: "shell",
        label: "Exec shell",
        icon: "shell",
        run: () => app.openDock({ kind: "shell", title: r.name, context: app.context, namespace: r.namespace, pod: r.name }),
      });
    } else if (WORKLOAD_LOG_KINDS.has(curKindKey)) {
      items.push({
        id: "logs",
        label: "View logs",
        icon: "logs",
        disabled: !r.uid,
        run: () =>
          r.uid &&
          app.openDock({
            kind: "logs",
            title: `${type.kind}/${r.name}`,
            context: app.context,
            namespace: r.namespace,
            pod: "",
            workload: { kind: type.kind, uid: r.uid, name: r.name },
          }),
      });
    }

    items.push({
      id: "edit",
      label: isHelm ? "Edit values" : "Edit YAML",
      icon: "edit",
      sepBefore: items.length > 0,
      run: () => app.openEdit(sel),
    });
    items.push({
      id: "delete",
      label: isHelm ? "Uninstall" : "Delete",
      icon: "delete",
      danger: true,
      run: () => void app.deleteResource(sel),
    });
    return items;
  }

  $effect(() => {
    const m = menu;
    if (!m) return;
    if (menuEl) {
      const z = cssZoom();
      const maxX = Math.max(8, window.innerWidth / z - menuEl.offsetWidth - 8);
      const maxY = Math.max(8, window.innerHeight / z - menuEl.offsetHeight - 8);
      const nx = Math.min(Math.max(8, m.x), maxX);
      const ny = Math.min(Math.max(8, m.y), maxY);
      if (nx !== m.x || ny !== m.y) {
        menu = { ...m, x: nx, y: ny };
      }
    }
    const onPointerDown = (e: PointerEvent) => {
      if (!menuEl || !menuEl.contains(e.target as Node)) menu = null;
    };
    const onKeyDown = (e: KeyboardEvent) => {
      if (e.key === "Escape") {
        e.stopPropagation();
        menu = null;
      }
    };
    const close = () => (menu = null);
    window.addEventListener("pointerdown", onPointerDown, true);
    window.addEventListener("keydown", onKeyDown, true);
    window.addEventListener("blur", close);
    return () => {
      window.removeEventListener("pointerdown", onPointerDown, true);
      window.removeEventListener("keydown", onKeyDown, true);
      window.removeEventListener("blur", close);
    };
  });
</script>

{#snippet menuSvg(icon: MenuIcon)}
  <svg viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="2" stroke-linecap="round" stroke-linejoin="round" aria-hidden="true">
    {#if icon === "owner"}
      <path d="M7 17L17 7" />
      <polyline points="8 7 17 7 17 16" />
    {:else if icon === "forward"}
      <path d="M18 13v6a2 2 0 0 1-2 2H5a2 2 0 0 1-2-2V8a2 2 0 0 1 2-2h6" />
      <polyline points="15 3 21 3 21 9" />
      <line x1="10" y1="14" x2="21" y2="3" />
    {:else if icon === "scale"}
      <line x1="12" y1="3" x2="12" y2="21" />
      <polyline points="8 7 12 3 16 7" />
      <polyline points="8 17 12 21 16 17" />
    {:else if icon === "restart"}
      <polyline points="23 4 23 10 17 10" />
      <polyline points="1 20 1 14 7 14" />
      <path d="M3.51 9a9 9 0 0 1 14.85-3.36L23 10M1 14l4.64 4.36A9 9 0 0 0 20.49 15" />
    {:else if icon === "logs"}
      <path d="M14 2H6a2 2 0 0 0-2 2v16a2 2 0 0 0 2 2h12a2 2 0 0 0 2-2V8z" />
      <polyline points="14 2 14 8 20 8" />
      <line x1="8" y1="13" x2="16" y2="13" />
      <line x1="8" y1="17" x2="16" y2="17" />
      <line x1="8" y1="9" x2="10" y2="9" />
    {:else if icon === "shell"}
      <polyline points="4 17 10 11 4 5" />
      <line x1="12" y1="19" x2="20" y2="19" />
    {:else if icon === "edit"}
      <path d="M14 2H6a2 2 0 0 0-2 2v16a2 2 0 0 0 2 2h12a2 2 0 0 0 2-2v-7" />
      <path d="M18.5 2.5a2.121 2.121 0 0 1 3 3L12 15l-4 1 1-4 9.5-9.5z" />
      <line x1="8" y1="9" x2="11" y2="9" />
      <line x1="8" y1="13" x2="10" y2="13" />
    {:else if icon === "delete"}
      <polyline points="3 6 5 6 21 6" />
      <path d="M19 6v14a2 2 0 0 1-2 2H7a2 2 0 0 1-2-2V6m3 0V4a2 2 0 0 1 2-2h4a2 2 0 0 1 2 2v2" />
      <line x1="10" y1="11" x2="10" y2="17" />
      <line x1="14" y1="11" x2="14" y2="17" />
    {/if}
  </svg>
{/snippet}

{#snippet cellMeter(meter: ResourceMeter, maxPx: number)}
  {#if meter.showBar}
    <div class="cell-meter" style={`--text-w:${maxPx}px`}>
      <span class="cell-meter-text">{meter.usedText}</span>
      <div class="meter-track" title={meter.hoverText}>
        <div class="meter-clip">
          <div class={`meter-fill tone-bg-${meter.tone}`} style={`width:${meter.usedPct}%`}></div>
        </div>
        {#if meter.requestMarkPct !== null}
          <span class="meter-req-mark" class:warn={meter.requestMarkWarn} style={`left:${meter.requestMarkPct}%`}></span>
        {/if}
      </div>
    </div>
  {:else}
    <span class="cell-meter-plain">{meter.usedText}</span>
  {/if}
{/snippet}

<section class="table-pane" class:resizing={resizingKey !== ""} class:dragging={draggingKey !== ""}>
  <div class="toolbar">
    <h2>{app.current?.kind ?? "—"}</h2>
    <span class="count">{view.length}{filter ? ` / ${totalRows}` : ""}</span>
    <div class="filter-wrap">
      <input class="filter" placeholder="Filter…" bind:value={filter} />
      {#if filter}
        <button class="clear-btn" onclick={() => (filter = "")} title="Clear filter" aria-label="Clear filter">✕</button>
      {/if}
    </div>
  </div>

  {#if error}
    <div class="banner error">{error}</div>
  {/if}

  <div
    class="grid-wrap"
    bind:clientHeight={viewportH}
    onscroll={(e) => {
      scrollTop = e.currentTarget.scrollTop;
      if (menu) menu = null;
    }}
  >
    <table style={`width:${totalTableW}px`}>
      <colgroup>
        {#each displayCols as c (c.key)}
          <col style={`width:${colWidth(c)}px`} />
        {/each}
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
        </tr>
      </thead>
      <tbody>
        {#if first > 0}<tr class="spacer" style={`height:${first * ROW_H}px`}></tr>{/if}
        {#each visible as r (r.key)}
          {@const unready = unreadyWorkloadCells(curKindKey, r.cells, workloadColIdx)}
          <tr
            class:selected={isSelected(r)}
            class:focused={focusedKey === r.key}
            onclick={() => (focusedKey = r.key)}
            ondblclick={() => {
              focusedKey = r.key;
              select(r);
            }}
            oncontextmenu={(e) => openContextMenu(e, r)}
          >
            {#each displayCols as c (c.key)}
              {#if c.meter}
                <td>{@render cellMeter(podMeterFor(r, c.meter), maxMeterPx[c.meter])}</td>
              {:else}
                {@const v = c.get(r)}
                {@const isName = c.key === "c0" || c.key === "name"}
                {@const isYellow =
                  (isName && unready.name) ||
                  (c.id.toLowerCase() === "ready" && unready.ready) ||
                  (c.id.toLowerCase() === "completions" && unready.completions)}
                <td
                  class={`${c.status ? `tone-${toneFor(v)}` : ""}${isYellow ? " tone-warn" : ""}`.trim()}
                  class:name-col={isName}
                >{v}</td>
              {/if}
            {/each}
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

  {#if menu}
    {@const items = menuItemsFor(menu.row)}
    <div
      class="ctx-menu"
      bind:this={menuEl}
      role="menu"
      style={`left:${menu.x}px;top:${menu.y}px`}
    >
      {#each items as item (item.id)}
        {#if item.sepBefore}
          <div class="ctx-sep" role="separator"></div>
        {/if}
        <button
          class="ctx-item"
          class:danger={item.danger}
          role="menuitem"
          disabled={item.disabled}
          title={item.label}
          onclick={() => {
            menu = null;
            item.run();
          }}
        >
          {@render menuSvg(item.icon)}
          <span>{item.label}</span>
        </button>
      {/each}
    </div>
  {/if}
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
  .filter-wrap {
    position: relative;
    display: flex;
    align-items: center;
    margin-left: auto;
  }
  .filter {
    width: 240px;
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
    z-index: 4;
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
    z-index: 5;
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
  tbody tr.focused,
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
  td.name-col.tone-warn {
    color: var(--warn);
  }
  .cell-meter {
    display: grid;
    grid-template-columns: var(--text-w, auto) minmax(48px, 1fr);
    align-items: center;
    gap: var(--space-2);
  }
  .cell-meter-text,
  .cell-meter-plain {
    font-family: var(--sans);
    font-size: 12px;
    font-variant-numeric: tabular-nums;
    color: var(--text);
    white-space: nowrap;
  }
  .cell-meter-text {
    text-align: left;
  }
  .empty {
    text-align: center;
    color: var(--muted);
    margin-top: var(--space-6);
  }
  .banner {
    margin: 0 var(--space-3) var(--space-2);
  }
  .ctx-menu {
    position: fixed;
    z-index: 40;
    min-width: 176px;
    padding: var(--space-1);
    background: var(--panel);
    border: 1px solid var(--border);
    border-radius: var(--radius);
    box-shadow: 0 6px 20px rgba(0, 0, 0, 0.38);
    display: flex;
    flex-direction: column;
    gap: 2px;
  }
  .ctx-sep {
    height: 1px;
    margin: var(--space-1) 0;
    background: var(--border-soft);
  }
  .ctx-item {
    display: flex;
    align-items: center;
    gap: var(--space-2);
    width: 100%;
    min-height: 28px;
    padding: var(--space-1) var(--space-2);
    border: 0;
    border-radius: var(--radius);
    background: transparent;
    color: var(--text-strong);
    font-size: 12.5px;
    text-align: left;
  }
  .ctx-item:hover:not(:disabled) {
    background: var(--hover);
  }
  .ctx-item:focus-visible {
    outline: 2px solid var(--accent);
    outline-offset: -2px;
  }
  .ctx-item.danger {
    color: var(--bad);
  }
  .ctx-item svg {
    width: 14px;
    height: 14px;
    flex-shrink: 0;
  }
</style>
