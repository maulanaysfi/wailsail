<script lang="ts">
  import { horizontalWheelScroll, startPointerDrag } from "../lib/dom";
  import { app } from "../lib/store.svelte";
  import Icon from "./Icon.svelte";
  import LogViewer from "./LogViewer.svelte";

  let height = $state(320);
  let terminalPromise: Promise<typeof import("./TerminalView.svelte")> | null = null;

  function loadTerminalView() {
    return (terminalPromise ??= import("./TerminalView.svelte"));
  }

  function startResize(e: PointerEvent) {
    const startH = height;
    startPointerDrag(e, (_, dy) => {
      height = Math.min(640, Math.max(160, startH - dy));
    });
  }

  async function closeAll() {
    if (
      await app.confirm(
        "Close all tabs?",
        `Close all ${app.dock.length} open dock session${app.dock.length === 1 ? "" : "s"}?`,
        true,
      )
    ) {
      app.closeAllDock();
    }
  }
</script>

<section class="dock" class:minimized={!app.dockOpen} style={`height:${height}px`}>
  <div class="resizer" onpointerdown={startResize} role="separator" aria-orientation="horizontal"></div>
  <header class="dock-bar">
    <div class="tab-strip" onwheel={horizontalWheelScroll}>
      {#each app.dock as t (t.id)}
        {@const status = app.dockStatuses[t.id] ?? "connecting"}
        <div class="tab" class:active={app.activeDock === t.id}>
          <button
            class="label"
            onclick={() => (app.activeDock = t.id)}
            title={`${t.kind === "logs" ? "Logs" : "Shell"} (${status}) for ${t.title}`}
          >
            <span class={`tab-icon ${status}`} aria-hidden="true">
              <Icon name={t.kind === "logs" ? "logs" : "shell"} />
            </span>
            <span class="tab-title">{t.title}</span>
          </button>
          <button class="x" onclick={() => app.closeDock(t.id)} title={`Close ${t.kind} tab for ${t.title}`}>✕</button>
        </div>
      {/each}
    </div>
    <div class="dock-actions">
      <button class="ghost" onclick={closeAll} title="Close all dock tabs">Close all</button>
      <button class="ghost" onclick={() => (app.dockOpen = false)} title="Hide dock">▾</button>
    </div>
  </header>

  {#each app.dock as t (t.id)}
    <div class="body" class:hidden={app.activeDock !== t.id}>
      {#if t.kind === "logs"}
        <LogViewer
          context={t.context}
          namespace={t.namespace}
          pod={t.pod}
          initialContainer={t.container}
          workload={t.workload}
          onStatus={(s) => app.setDockStatus(t.id, s)}
        />
      {:else}
        {#await loadTerminalView()}
          <p class="muted loading-term">Loading terminal…</p>
        {:then mod}
          {@const TerminalView = mod.default}
          <TerminalView
            context={t.context}
            namespace={t.namespace}
            pod={t.pod}
            initialContainer={t.container}
            onStatus={(s) => app.setDockStatus(t.id, s)}
          />
        {:catch err}
          <div class="banner error loading-err">Failed to load terminal: {String(err)}</div>
        {/await}
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
    min-width: 0;
    min-height: 0;
  }
  .dock.minimized {
    display: none;
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
  .dock-bar {
    display: flex;
    align-items: center;
    gap: var(--space-2);
    padding: 0 var(--space-2);
    border-bottom: 1px solid var(--border);
    min-width: 0;
  }
  .tab-strip {
    display: flex;
    align-items: center;
    gap: var(--space-1);
    flex: 1;
    min-width: 0;
    overflow-x: auto;
    scrollbar-width: none;
  }
  .tab-strip::-webkit-scrollbar {
    display: none;
  }
  .tab {
    display: inline-flex;
    align-items: center;
    flex-shrink: 0;
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
    display: inline-flex;
    align-items: center;
    gap: var(--space-1);
    padding: var(--space-2);
    white-space: nowrap;
  }
  .tab-icon {
    display: inline-flex;
    align-items: center;
    justify-content: center;
    color: var(--warn);
  }
  .tab-icon :global(svg) {
    width: 14px;
    height: 14px;
  }
  .tab-icon.connected {
    color: var(--ok);
  }
  .tab-icon.connecting {
    color: var(--warn);
  }
  .tab-icon.disconnected {
    color: var(--bad);
  }
  .tab-title {
    max-width: 180px;
    overflow: hidden;
    text-overflow: ellipsis;
    white-space: nowrap;
  }
  .x {
    color: var(--muted);
  }
  .x:hover {
    color: var(--text-strong);
  }
  .dock-actions {
    display: inline-flex;
    align-items: center;
    gap: var(--space-1);
    flex-shrink: 0;
  }
  .body {
    display: flex;
    flex: 1;
    min-width: 0;
    min-height: 0;
  }
  .body.hidden {
    display: none;
  }
  .loading-term,
  .loading-err {
    margin: var(--space-3);
  }
</style>
