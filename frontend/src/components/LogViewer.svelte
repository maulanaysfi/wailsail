<script lang="ts">
  import { tick, untrack } from "svelte";
  import { Kube, Logs, newStreamId, on, topics, errorMessage, type ContainerInfo, type LogEvent } from "../lib/api";
  import { formatLogTimestamp } from "../lib/format";
  import { computeLogWindow, entryOffset, type LogEntryItem } from "../lib/logWindow.ts";
  import { app, type DockStatus } from "../lib/store.svelte";

  let {
    context,
    namespace,
    pod,
    initialContainer = "",
    workload,
    onStatus,
  }: {
    context: string;
    namespace: string;
    pod: string;
    initialContainer?: string;
    workload?: { kind: string; uid: string; name: string };
    onStatus?: (s: DockStatus) => void;
  } = $props();

  const MAX_LINES = 10_000;
  const POD_COLOR_COUNT = 10;
  const K8S_TS_RE = /^(\d{4}-\d{2}-\d{2}T\d{2}:\d{2}:\d{2}(?:\.\d+)?(?:Z|[+-]\d{2}:\d{2}))(\s+)(.*)$/;

  let containers = $state<ContainerInfo[]>([]);
  let container = $state(untrack(() => initialContainer));
  let tailLines = $state(untrack(() => app.settings.logTailLines || 500));
  let timestamps = $state(untrack(() => app.settings.logTimestamps));
  let previous = $state(false);
  let follow = $state(true);
  let wrap = $state(false);
  let paused = $state(false);
  let search = $state("");
  let podCount = $state(0);
  let totalPods = $state(0);
  let entries: LogEntryItem[] = [];
  let version = $state(0);
  let measureVersion = $state(0);
  let ended = $state(false);
  let error = $state("");
  let restartNonce = $state(0);
  let scroller: HTMLDivElement | undefined = $state();
  let scrollTop = $state(0);
  let viewportH = $state(320);
  let viewportW = $state(640);
  let stickBottom = $state(true);
  let lineSeq = 0;
  const podColors = new Map<string, number>();
  const measuredHeights = new Map<number, number>();
  let lastMeasureWidth = 0;
  let pinPending = false;
  let adjustingScroll = false;
  let lastScrollTop = 0;
  let anchor: { id: number; relTop: number } | null = null;

  function colorForPod(name: string): number {
    if (!name) return 0;
    const existing = podColors.get(name);
    if (existing !== undefined) return existing;
    const idx = podColors.size % POD_COLOR_COUNT;
    podColors.set(name, idx);
    return idx;
  }

  function makeEntry(line: string, withTs: boolean, podName = ""): LogEntryItem {
    let ts = "";
    let body = line;
    if (withTs) {
      const m = K8S_TS_RE.exec(line);
      if (m) {
        ts = m[1] + m[2];
        body = m[3];
      }
    }
    const lower = podName ? `${podName} ${line}`.toLowerCase() : line.toLowerCase();
    return {
      id: ++lineSeq,
      lower,
      pod: podName,
      podColor: podName ? colorForPod(podName) : 0,
      ts,
      body,
    };
  }

  // Load containers of the pod once (single-pod mode only).
  $effect(() => {
    const ctx = context,
      ns = namespace,
      p = pod,
      wl = workload;
    if (wl || !p) return;
    Kube.PodContainers(ctx, ns, p)
      .then((cs) => {
        containers = cs ?? [];
        if (!container || !containers.some((c) => c.name === container)) {
          container = containers.find((c) => c.type === "main")?.name ?? containers[0]?.name ?? "";
        }
      })
      .catch((e) => {
        error = errorMessage(e);
        onStatus?.("disconnected");
      });
  });

  function resetEntries(clearPods = false) {
    entries = [];
    measuredHeights.clear();
    if (clearPods) {
      podColors.clear();
      podCount = 0;
      totalPods = 0;
    }
    version++;
    scrollTop = 0;
    lastScrollTop = 0;
    stickBottom = true;
    pinPending = false;
    anchor = null;
  }

  // Start / restart the log stream whenever options change.
  $effect(() => {
    const ctx = context,
      ns = namespace,
      p = pod,
      wl = workload,
      c = container,
      tail = tailLines,
      ts = timestamps,
      prev = previous,
      flw = follow;
    restartNonce;
    if (!wl && !c) return;
    untrack(() => {
      resetEntries(true);
      ended = false;
      error = "";
      onStatus?.("connecting");
    });
    const id = newStreamId();
    const off = on<LogEvent>(topics.logs(id), (ev) => {
      if (ev.connected) onStatus?.("connected");
      if (ev.podCount !== undefined) podCount = ev.podCount;
      if (ev.totalPods !== undefined) totalPods = ev.totalPods;
      const hasEntries = (ev.entries?.length ?? 0) > 0;
      const hasLines = (ev.lines?.length ?? 0) > 0;
      if (!paused && (hasEntries || hasLines)) {
        onStatus?.("connected");
        if (hasEntries) {
          for (const item of ev.entries!) {
            entries.push(makeEntry(item.text, ts, item.pod ?? ""));
          }
        }
        if (hasLines) {
          for (const line of ev.lines!) entries.push(makeEntry(line, ts));
        }
        if (entries.length > MAX_LINES) {
          const trimmed = entries.splice(0, entries.length - MAX_LINES);
          for (const item of trimmed) measuredHeights.delete(item.id);
        }
        if (stickBottom) pinPending = true;
        version++;
      }
      if (ev.error) {
        error = ev.error;
        onStatus?.("disconnected");
      }
      if (ev.ended) {
        ended = true;
        onStatus?.("disconnected");
      }
    });
    const startPromise = wl
      ? Logs.StartWorkload(
          ctx,
          ns,
          wl.kind,
          wl.uid,
          wl.name,
          { container: "", tailLines: tail, timestamps: ts, previous: false, follow: flw },
          id,
        )
      : Logs.Start(ctx, ns, p, { container: c, tailLines: tail, timestamps: ts, previous: prev, follow: flw }, id);
    startPromise.catch((e) => {
      error = errorMessage(e);
      ended = true;
      onStatus?.("disconnected");
    });
    return () => {
      off();
      Logs.Stop(id);
    };
  });

  function filteredEntries(): LogEntryItem[] {
    const q = search.trim().toLowerCase();
    return q ? entries.filter((e) => e.lower.includes(q)) : entries;
  }

  const view = $derived.by(() => {
    version;
    measureVersion;
    return computeLogWindow(filteredEntries(), {
      wrap,
      stickBottom,
      scrollTop,
      viewportHeight: viewportH,
      viewportWidth: viewportW,
      measured: measuredHeights,
    });
  });

  function clearLogs() {
    resetEntries(false);
  }

  function captureAnchor(): { id: number; relTop: number } | null {
    if (!scroller) return null;
    const scrollerTop = scroller.getBoundingClientRect().top;
    const nodes = scroller.querySelectorAll<HTMLElement>(".line[data-id]");
    for (const el of nodes) {
      const rect = el.getBoundingClientRect();
      if (rect.bottom > scrollerTop + 1) {
        const id = Number(el.dataset.id);
        if (!Number.isNaN(id)) return { id, relTop: rect.top - scrollerTop };
      }
    }
    return null;
  }

  function applyDomScrollTop(next: number) {
    if (!scroller) return;
    adjustingScroll = true;
    scroller.scrollTop = next;
    const actual = scroller.scrollTop;
    lastScrollTop = actual;
    if (Math.abs(scrollTop - actual) >= 1) scrollTop = actual;
    requestAnimationFrame(() => {
      adjustingScroll = false;
      pinPending = false;
    });
  }

  async function toggleWrap(nextWrap: boolean) {
    const captured = !stickBottom ? captureAnchor() : null;
    if (nextWrap) measuredHeights.clear();
    wrap = nextWrap;
    await tick();
    if (!scroller) return;
    if (stickBottom) {
      applyDomScrollTop(scroller.scrollHeight);
      return;
    }
    if (captured) {
      const offset = entryOffset(filteredEntries(), captured.id, wrap, measuredHeights, viewportW);
      if (offset >= 0) applyDomScrollTop(Math.max(0, offset - captured.relTop));
    }
  }

  function onWheel(e: WheelEvent) {
    if (e.deltaY < 0) {
      stickBottom = false;
      pinPending = false;
    }
  }

  function onScroll() {
    if (!scroller || adjustingScroll) return;
    const nextTop = scroller.scrollTop;
    const scrolledUp = nextTop < lastScrollTop - 2;
    scrollTop = nextTop;
    lastScrollTop = nextTop;
    if (scrolledUp) {
      stickBottom = false;
      pinPending = false;
    } else if (!pinPending) {
      stickBottom = scroller.scrollHeight - nextTop - scroller.clientHeight < 24;
    }
  }

  $effect.pre(() => {
    view;
    wrap;
    if (!scroller || stickBottom) {
      anchor = null;
      return;
    }
    anchor = captureAnchor();
  });

  $effect(() => {
    view;
    wrap;
    const w = viewportW;
    viewportH;
    if (!scroller) return;

    if (w > 0 && Math.abs(w - lastMeasureWidth) >= 2) {
      lastMeasureWidth = w;
      if (wrap && measuredHeights.size > 0) measuredHeights.clear();
    }

    if (wrap) {
      let changed = false;
      const nodes = scroller.querySelectorAll<HTMLElement>(".line[data-id]");
      for (const el of nodes) {
        const id = Number(el.dataset.id);
        if (Number.isNaN(id)) continue;
        const h = el.getBoundingClientRect().height;
        const prev = measuredHeights.get(id);
        if (h > 0 && (prev === undefined || Math.abs(h - prev) >= 0.5)) {
          measuredHeights.set(id, h);
          changed = true;
        }
      }
      if (changed) measureVersion++;
    }

    if (stickBottom) {
      if (scroller.scrollHeight - scroller.scrollTop - scroller.clientHeight > 1 || pinPending) {
        applyDomScrollTop(scroller.scrollHeight);
      }
      return;
    }

    if (anchor) {
      const target = scroller.querySelector<HTMLElement>(`.line[data-id="${anchor.id}"]`);
      if (target) {
        const newRelTop = target.getBoundingClientRect().top - scroller.getBoundingClientRect().top;
        const delta = newRelTop - anchor.relTop;
        if (Math.abs(delta) >= 1) {
          applyDomScrollTop(Math.max(0, scroller.scrollTop + delta));
        }
      }
      anchor = null;
    }
  });
</script>

<div class="logs">
  <div class="bar">
    {#if workload}
      <span class={`pill ${totalPods > podCount ? "cap-warn" : ""}`}>
        {totalPods > podCount ? `${podCount}/${totalPods} pods (capped)` : `${podCount} pod${podCount === 1 ? "" : "s"}`}
      </span>
    {:else}
      <label>
        Container
        <select bind:value={container}>
          {#each containers as c (c.name)}
            <option value={c.name}>{c.name}{c.type !== "main" ? ` (${c.type})` : ""}</option>
          {/each}
        </select>
      </label>
    {/if}
    <label>
      Tail
      <select
        value={tailLines}
        onchange={(e) => {
          tailLines = Number(e.currentTarget.value);
          app.saveSettings({ logTailLines: tailLines });
        }}
      >
        <option value={100}>100</option>
        <option value={500}>500</option>
        <option value={2000}>2000</option>
        <option value={5000}>5000</option>
      </select>
    </label>
    <label class="check">
      <input
        type="checkbox"
        checked={timestamps}
        onchange={(e) => {
          timestamps = e.currentTarget.checked;
          app.saveSettings({ logTimestamps: timestamps });
        }}
      />
      Timestamps
    </label>
    {#if !workload}
      <label class="check"><input type="checkbox" bind:checked={previous} />Previous</label>
    {/if}
    <label class="check"><input type="checkbox" bind:checked={follow} />Follow</label>
    <label class="check">
      <input type="checkbox" checked={wrap} onchange={(e) => toggleWrap(e.currentTarget.checked)} />
      Wrap
    </label>
    <button onclick={() => (paused = !paused)} title={paused ? "Resume live log stream" : "Pause live log stream"}>
      {paused ? "Resume" : "Pause"}
    </button>
    <button onclick={clearLogs} title="Clear displayed log lines">Clear</button>
    <button onclick={() => restartNonce++} title="Reconnect container log stream">Reconnect</button>
    <input class="search" placeholder="Filter lines…" bind:value={search} />
    {#if ended}<span class="pill">EOF</span>{/if}
  </div>

  {#if error}<div class="banner error">{error}</div>{/if}

  <div
    class="out"
    class:wrap
    bind:this={scroller}
    bind:clientHeight={viewportH}
    bind:clientWidth={viewportW}
    onscroll={onScroll}
    onwheel={onWheel}
  >
    {#if view.topPad > 0}<div aria-hidden="true" style={`height:${view.topPad}px`}></div>{/if}
    {#each view.items as entry (entry.id)}
      <div class="line" data-id={entry.id}>{#if entry.pod}<span class="pod-prefix" style={`color:var(--log-pod-${entry.podColor})`}>{entry.pod}</span>{" "}{/if}{#if entry.ts}<span class="ts">{formatLogTimestamp(entry.ts, app.settings.logTimezone)}</span>{/if}{entry.body}</div>
    {/each}
    {#if view.bottomPad > 0}<div aria-hidden="true" style={`height:${view.bottomPad}px`}></div>{/if}
  </div>
</div>

<style>
  .logs {
    display: flex;
    flex-direction: column;
    flex: 1;
    min-width: 0;
    min-height: 0;
    overflow: hidden;
  }
  .bar {
    display: flex;
    align-items: center;
    flex-wrap: wrap;
    gap: var(--space-2);
    padding: var(--space-2) var(--space-3);
    border-bottom: 1px solid var(--border);
    font-size: 12px;
    color: var(--muted);
    min-width: 0;
  }
  .bar label {
    display: inline-flex;
    align-items: center;
    gap: var(--space-1);
  }
  .search {
    margin-left: auto;
    width: 200px;
    min-width: 120px;
    max-width: 100%;
  }
  .out {
    flex: 1;
    min-width: 0;
    overflow: auto;
    padding: var(--space-2) var(--space-3);
    font-family: var(--mono);
    font-size: 12px;
    line-height: 16px;
    background: var(--code-bg);
    white-space: pre;
    user-select: text;
    -webkit-user-select: text;
  }
  .out.wrap {
    white-space: pre-wrap;
    word-break: break-all;
  }
  .out:not(.wrap) .line {
    width: max-content;
    min-width: 100%;
  }
  .ts {
    color: var(--accent);
    font-weight: 500;
    user-select: none;
    -webkit-user-select: none;
  }
  .pod-prefix {
    font-weight: 600;
    user-select: none;
    -webkit-user-select: none;
  }
  .cap-warn {
    color: var(--warn);
    border-color: var(--warn);
  }
  .line:hover {
    background: var(--hover);
  }
  .banner {
    margin: var(--space-2) var(--space-3) 0;
  }
</style>
