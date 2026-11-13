<script lang="ts">
  import { app } from "../lib/store.svelte";
  import LogViewer from "./LogViewer.svelte";
  import TerminalView from "./TerminalView.svelte";

  let height = $state(300);

  function startResize(e: PointerEvent) {
    const startY = e.clientY;
    const startH = height;
    const move = (ev: PointerEvent) => (height = Math.min(640, Math.max(160, startH + startY - ev.clientY)));
    const up = () => {
      window.removeEventListener("pointermove", move);
      window.removeEventListener("pointerup", up);
    };
    window.addEventListener("pointermove", move);
    window.addEventListener("pointerup", up);
  }
</script>

<section class="dock" style={`height:${height}px`}>
  <div class="resizer" onpointerdown={startResize} role="separator" aria-orientation="horizontal"></div>
  <header class="tabs">
    {#each app.dock as t (t.id)}
      <div class="tab" class:active={app.activeDock === t.id}>
        <button class="label" onclick={() => (app.activeDock = t.id)}>
          <span class="badge">{t.kind}</span>
          {t.title}
        </button>
        <button class="x" onclick={() => app.closeDock(t.id)} title="Close tab">✕</button>
      </div>
    {/each}
    <span class="grow"></span>
    <button class="ghost" onclick={() => (app.dockOpen = false)} title="Minimize dock">▾</button>
  </header>

  {#each app.dock as t (t.id)}
    <div class="body" class:hidden={app.activeDock !== t.id}>
      {#if t.kind === "logs"}
        <LogViewer context={t.context} namespace={t.namespace} pod={t.pod} initialContainer={t.container} />
      {:else}
        <TerminalView context={t.context} namespace={t.namespace} pod={t.pod} initialContainer={t.container} />
      {/if}
    </div>
  {/each}
</section>

<style>
  .dock {
    position: relative;
    display: flex;
    flex-direction: column;
    border-top: 1px solid var(--border);
    background: var(--panel);
    min-height: 0;
  }
  .resizer {
    position: absolute;
    top: -3px;
    left: 0;
    right: 0;
    height: 6px;
    cursor: row-resize;
    z-index: 2;
  }
  .tabs {
    display: flex;
    align-items: center;
    gap: 2px;
    padding: 0 8px;
    border-bottom: 1px solid var(--border);
    overflow-x: auto;
  }
  .tab {
    display: inline-flex;
    align-items: center;
    border-bottom: 2px solid transparent;
    font-size: 12px;
  }
  .tab.active {
    border-bottom-color: var(--accent);
    color: var(--text-strong);
  }
  .label,
  .x {
    all: unset;
    cursor: pointer;
    padding: 6px 6px;
  }
  .badge {
    font-size: 10px;
    text-transform: uppercase;
    color: var(--accent);
    margin-right: 4px;
  }
  .x {
    color: var(--muted);
  }
  .x:hover {
    color: var(--text-strong);
  }
  .grow {
    flex: 1;
  }
  .body {
    display: flex;
    flex: 1;
    min-height: 0;
  }
  .body.hidden {
    display: none;
  }
</style>
