<script lang="ts">
  import { app, cssZoom } from "../lib/store.svelte";
  import LogViewer from "./LogViewer.svelte";

  let height = $state(320);
  let terminalPromise: Promise<typeof import("./TerminalView.svelte")> | null = null;

  function loadTerminalView() {
    return (terminalPromise ??= import("./TerminalView.svelte"));
  }

  function startResize(e: PointerEvent) {
    const z = cssZoom();
    const startY = e.clientY;
    const startH = height;
    const move = (ev: PointerEvent) => (height = Math.min(640, Math.max(160, startH + (startY - ev.clientY) / z)));
    const up = () => {
      window.removeEventListener("pointermove", move);
      window.removeEventListener("pointerup", up);
    };
    window.addEventListener("pointermove", move);
    window.addEventListener("pointerup", up);
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
    <div
      class="tab-strip"
      onwheel={(e) => {
        if (e.deltaY && !e.deltaX && e.currentTarget.scrollWidth > e.currentTarget.clientWidth) {
          e.currentTarget.scrollLeft += e.deltaY;
          e.preventDefault();
        }
      }}
    >
      {#each app.dock as t (t.id)}
        {@const status = app.dockStatuses[t.id] ?? "connecting"}
        <div class="tab" class:active={app.activeDock === t.id}>
          <button
            class="label"
            onclick={() => (app.activeDock = t.id)}
            title={`${t.kind === "logs" ? "Logs" : "Shell"} (${status}) for ${t.title}`}
          >
            <span class={`tab-icon ${status}`} aria-hidden="true">
              {#if t.kind === "logs"}
                <svg viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="2" stroke-linecap="round" stroke-linejoin="round">
                  <path d="M14 2H6a2 2 0 0 0-2 2v16a2 2 0 0 0 2 2h12a2 2 0 0 0 2-2V8z" />
                  <polyline points="14 2 14 8 20 8" />
                  <line x1="8" y1="13" x2="16" y2="13" />
                  <line x1="8" y1="17" x2="16" y2="17" />
                  <line x1="8" y1="9" x2="10" y2="9" />
                </svg>
              {:else}
                <svg viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="2" stroke-linecap="round" stroke-linejoin="round">
                  <polyline points="4 17 10 11 4 5" />
                  <line x1="12" y1="19" x2="20" y2="19" />
                </svg>
              {/if}
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
  .tab-icon svg {
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
