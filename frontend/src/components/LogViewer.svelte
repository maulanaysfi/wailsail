<script lang="ts">
  import { tick, untrack } from "svelte";
  import { Kube, Logs, newStreamId, on, topics, errorMessage, type ContainerInfo, type LogEvent } from "../lib/api";
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
  const LINE_H = 16;
  const OVERSCAN = 60;
  const K8S_TS_RE = /^(\d{4}-\d{2}-\d{2}T\d{2}:\d{2}:\d{2}(?:\.\d+)?(?:Z|[+-]\d{2}:\d{2}))(\s+)(.*)$/;

  interface LogEntry {
    id: number;
    lower: string;
    pod: string;
    podColor: number;
    ts: string;
    body: string;
  }

  let containers = $state<ContainerInfo[]>([]);
  let container = $state(untrack(() => initialContainer));
  let tailLines = $state(untrack(() => app.settings.logTailLines || 500));
  let timestamps = $state(untrack(() => app.settings.logTimestamps));
  let previous = $state(false);
  let follow = $state(true);
  let paused = $state(false);
  let search = $state("");
  let podCount = $state(0);
  let totalPods = $state(0);
  let entries: LogEntry[] = [];
  let version = $state(0);
  let ended = $state(false);
  let error = $state("");
  let restartNonce = $state(0);
  let scroller: HTMLDivElement | undefined = $state();
  let scrollTop = $state(0);
  let viewportH = $state(320);
  let stickBottom = $state(true);
  let lineSeq = 0;
  const podColors = new Map<string, number>();

  function colorForPod(name: string): number {
    if (!name) return 0;
    const existing = podColors.get(name);
    if (existing !== undefined) return existing;
    const idx = podColors.size % POD_COLOR_COUNT;
    podColors.set(name, idx);
    return idx;
  }

  function makeEntry(line: string, withTs: boolean, podName = ""): LogEntry {
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
      entries = [];
      podColors.clear();
      podCount = 0;
      totalPods = 0;
      version++;
      scrollTop = 0;
      stickBottom = true;
      ended = false;
      error = "";
      onStatus?.("connecting");
    });
    const id = newStreamId();
    const off = on<LogEvent>(topics.logs(id), async (ev) => {
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
          entries.splice(0, entries.length - MAX_LINES);
        }
        version++;
        if (stickBottom && scroller) {
          await tick();
          scroller.scrollTop = scroller.scrollHeight;
          scrollTop = scroller.scrollTop;
        }
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

  // shortcut: fixed 16px row height estimate for spacers; wrapped lines render cleanly inside the 60-line overscan window.
  const view = $derived.by(() => {
    version;
    const q = search.trim().toLowerCase();
    const list = q ? entries.filter((e) => e.lower.includes(q)) : entries;
    const total = list.length;
    const page = Math.max(1, Math.ceil(viewportH / LINE_H));
    let first: number;
    let last: number;
    if (stickBottom) {
      last = total;
      first = Math.max(0, last - page - OVERSCAN * 2);
    } else {
      first = Math.max(0, Math.floor(scrollTop / LINE_H) - OVERSCAN);
      last = Math.min(total, Math.ceil((scrollTop + viewportH) / LINE_H) + OVERSCAN);
    }
    return { total, first, last, items: list.slice(first, last) };
  });

  function clearLogs() {
    entries = [];
    version++;
    scrollTop = 0;
    stickBottom = true;
  }

  function onScroll() {
    if (!scroller) return;
    scrollTop = scroller.scrollTop;
    stickBottom = scroller.scrollHeight - scroller.scrollTop - scroller.clientHeight < 24;
  }
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
    <button onclick={() => (paused = !paused)} title={paused ? "Resume live log stream" : "Pause live log stream"}>
      {paused ? "Resume" : "Pause"}
    </button>
    <button onclick={clearLogs} title="Clear displayed log lines">Clear</button>
    <button onclick={() => restartNonce++} title="Reconnect container log stream">Reconnect</button>
    <input class="search" placeholder="Filter lines…" bind:value={search} />
    {#if ended}<span class="pill">EOF</span>{/if}
  </div>

  {#if error}<div class="banner error">{error}</div>{/if}

  <div class="out" bind:this={scroller} bind:clientHeight={viewportH} onscroll={onScroll}>
    {#if view.first > 0}<div aria-hidden="true" style={`height:${view.first * LINE_H}px`}></div>{/if}
    {#each view.items as entry (entry.id)}
      <div class="line">{#if entry.pod}<span class="pod-prefix" style={`color:var(--log-pod-${entry.podColor})`}>{entry.pod}</span>{" "}{/if}{#if entry.ts}<span class="ts">{entry.ts}</span>{/if}{entry.body}</div>
    {/each}
    {#if view.last < view.total}<div aria-hidden="true" style={`height:${(view.total - view.last) * LINE_H}px`}></div>{/if}
  </div>
</div>

<style>
  .logs {
    display: flex;
    flex-direction: column;
    flex: 1;
    min-height: 0;
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
  }
  .bar label {
    display: inline-flex;
    align-items: center;
    gap: var(--space-1);
  }
  .search {
    margin-left: auto;
    width: 200px;
  }
  .out {
    flex: 1;
    overflow: auto;
    padding: var(--space-2) var(--space-3);
    font-family: var(--mono);
    font-size: 12px;
    line-height: 16px;
    background: var(--code-bg);
    white-space: pre-wrap;
    word-break: break-all;
    user-select: text;
    -webkit-user-select: text;
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
