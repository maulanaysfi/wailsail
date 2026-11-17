<script lang="ts">
  import { tick, untrack } from "svelte";
  import { Kube, Logs, newStreamId, on, topics, errorMessage, type ContainerInfo, type LogEvent } from "../lib/api";
  import { app } from "../lib/store.svelte";

  let {
    context,
    namespace,
    pod,
    initialContainer = "",
  }: { context: string; namespace: string; pod: string; initialContainer?: string } = $props();

  const MAX_LINES = 10_000;

  let containers = $state<ContainerInfo[]>([]);
  let container = $state(untrack(() => initialContainer));
  let tailLines = $state(untrack(() => app.settings.logTailLines || 500));
  let timestamps = $state(untrack(() => app.settings.logTimestamps));
  let previous = $state(false);
  let follow = $state(true);
  let paused = $state(false);
  let search = $state("");
  let lines = $state<string[]>([]);
  let ended = $state(false);
  let error = $state("");
  let restartNonce = $state(0);
  let scroller: HTMLDivElement | undefined = $state();
  let stickBottom = true;

  // Load containers of the pod once.
  $effect(() => {
    const ctx = context,
      ns = namespace,
      p = pod;
    Kube.PodContainers(ctx, ns, p)
      .then((cs) => {
        containers = cs ?? [];
        if (!container || !containers.some((c) => c.name === container)) {
          container = containers.find((c) => c.type === "main")?.name ?? containers[0]?.name ?? "";
        }
      })
      .catch((e) => (error = errorMessage(e)));
  });

  // Start / restart the log stream whenever options change.
  $effect(() => {
    const ctx = context,
      ns = namespace,
      p = pod,
      c = container,
      tail = tailLines,
      ts = timestamps,
      prev = previous,
      flw = follow;
    restartNonce;
    if (!c) return;
    untrack(() => {
      lines = [];
      ended = false;
      error = "";
    });
    const id = newStreamId();
    const off = on<LogEvent>(topics.logs(id), async (ev) => {
      if (!paused && ev.lines?.length) {
        const combined = [...lines, ...ev.lines];
        lines = combined.length > MAX_LINES ? combined.slice(combined.length - MAX_LINES) : combined;
        if (stickBottom && scroller) {
          await tick();
          scroller.scrollTop = scroller.scrollHeight;
        }
      }
      if (ev.ended) ended = true;
      if (ev.error) error = ev.error;
    });
    Logs.Start(ctx, ns, p, { container: c, tailLines: tail, timestamps: ts, previous: prev, follow: flw }, id).catch(
      (e) => {
        error = errorMessage(e);
        ended = true;
      },
    );
    return () => {
      off();
      Logs.Stop(id);
    };
  });

  const visible = $derived.by(() => {
    const q = search.trim().toLowerCase();
    return q ? lines.filter((l) => l.toLowerCase().includes(q)) : lines;
  });

  function onScroll() {
    if (!scroller) return;
    stickBottom = scroller.scrollHeight - scroller.scrollTop - scroller.clientHeight < 24;
  }
</script>

<div class="logs">
  <div class="bar">
    <label>
      Container
      <select bind:value={container}>
        {#each containers as c (c.name)}
          <option value={c.name}>{c.name}{c.type !== "main" ? ` (${c.type})` : ""}</option>
        {/each}
      </select>
    </label>
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
    <label class="check"><input type="checkbox" bind:checked={previous} />Previous</label>
    <label class="check"><input type="checkbox" bind:checked={follow} />Follow</label>
    <button onclick={() => (paused = !paused)} title={paused ? "Resume live log stream" : "Pause live log stream"}>
      {paused ? "Resume" : "Pause"}
    </button>
    <button onclick={() => (lines = [])} title="Clear displayed log lines">Clear</button>
    <button onclick={() => restartNonce++} title="Reconnect container log stream">Reconnect</button>
    <input class="search" placeholder="Filter lines…" bind:value={search} />
    {#if ended}<span class="pill">EOF</span>{/if}
  </div>

  {#if error}<div class="banner error">{error}</div>{/if}

  <div class="out" bind:this={scroller} onscroll={onScroll}>
    {#each visible as line, i (i)}
      <div class="line">{line}</div>
    {/each}
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
  }
  .line:hover {
    background: var(--hover);
  }
  .banner {
    margin: var(--space-2) var(--space-3) 0;
  }
</style>
