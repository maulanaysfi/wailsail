<script lang="ts">
  import { app } from "../lib/store.svelte";

  const themeLabel: Record<string, string> = { system: "System", light: "Light", dark: "Dark" };
  const theme = $derived(themeLabel[app.settings.theme] ? app.settings.theme : "system");
  const nextTheme: Record<string, string> = { system: "Light", light: "Dark", dark: "System" };
</script>

<header class="topbar">
  <div class="nav-btns">
    <button
      class="ghost icon"
      disabled={!app.canGoBack}
      onclick={() => app.goBack()}
      title="Back"
      aria-label="Back"
    >
      <svg viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="2" stroke-linecap="round" stroke-linejoin="round" aria-hidden="true">
        <polyline points="15 18 9 12 15 6" />
      </svg>
    </button>
    <button
      class="ghost icon"
      disabled={!app.canGoForward}
      onclick={() => app.goForward()}
      title="Forward"
      aria-label="Forward"
    >
      <svg viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="2" stroke-linecap="round" stroke-linejoin="round" aria-hidden="true">
        <polyline points="9 18 15 12 9 6" />
      </svg>
    </button>
  </div>

  <select
    class="ns-select"
    aria-label="Namespace"
    value={app.namespace}
    disabled={!app.current?.namespaced}
    onchange={(e) => app.selectNamespace(e.currentTarget.value)}
  >
    <option value="">All namespaces</option>
    {#if app.namespace && !app.namespaces.includes(app.namespace)}
      <option value={app.namespace}>{app.namespace}</option>
    {/if}
    {#each app.namespaces as ns (ns)}
      <option value={ns}>{ns}</option>
    {/each}
  </select>

  <div class="spacer"></div>

  <button
    class="ghost icon"
    onclick={() => app.reload()}
    title="Reload kubeconfig and refresh current resource view"
    aria-label="Reload"
  >
    <svg viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="2" stroke-linecap="round" stroke-linejoin="round" aria-hidden="true">
      <polyline points="23 4 23 10 17 10" />
      <polyline points="1 20 1 14 7 14" />
      <path d="M3.51 9a9 9 0 0 1 14.85-3.36L23 10M1 14l4.64 4.36A9 9 0 0 0 20.49 15" />
    </svg>
  </button>

  <button
    class="ghost icon"
    onclick={() => app.cycleTheme()}
    title={`Theme: ${themeLabel[theme]} (click to switch to ${nextTheme[theme]})`}
    aria-label={`Theme: ${themeLabel[theme]}`}
  >
    <svg viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="2" stroke-linecap="round" stroke-linejoin="round" aria-hidden="true">
      {#if theme === "light"}
        <circle cx="12" cy="12" r="4" />
        <path d="M12 2v2M12 20v2M4.93 4.93l1.41 1.41M17.66 17.66l1.41 1.41M2 12h2M20 12h2M6.34 17.66l-1.41 1.41M19.07 4.93l-1.41 1.41" />
      {:else if theme === "dark"}
        <path d="M21 12.79A9 9 0 1 1 11.21 3 7 7 0 0 0 21 12.79z" />
      {:else}
        <rect x="2" y="3" width="20" height="14" rx="2" />
        <path d="M8 21h8M12 17v4" />
      {/if}
    </svg>
  </button>
  <button class="ghost icon" onclick={() => (app.showSettings = true)} title="Open application settings" aria-label="Settings">
    <svg viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="2" stroke-linecap="round" stroke-linejoin="round" aria-hidden="true">
      <circle cx="12" cy="12" r="3" />
      <path
        d="M19.4 15a1.65 1.65 0 0 0 .33 1.82l.06.06a2 2 0 1 1-2.83 2.83l-.06-.06a1.65 1.65 0 0 0-1.82-.33 1.65 1.65 0 0 0-1 1.51V21a2 2 0 1 1-4 0v-.09A1.65 1.65 0 0 0 9 19.4a1.65 1.65 0 0 0-1.82.33l-.06.06a2 2 0 1 1-2.83-2.83l.06-.06A1.65 1.65 0 0 0 4.68 15a1.65 1.65 0 0 0-1.51-1H3a2 2 0 1 1 0-4h.09A1.65 1.65 0 0 0 4.6 9a1.65 1.65 0 0 0-.33-1.82l-.06-.06a2 2 0 1 1 2.83-2.83l.06.06A1.65 1.65 0 0 0 9 4.68a1.65 1.65 0 0 0 1-1.51V3a2 2 0 1 1 4 0v.09a1.65 1.65 0 0 0 1 1.51 1.65 1.65 0 0 0 1.82-.33l.06-.06a2 2 0 1 1 2.83 2.83l-.06.06A1.65 1.65 0 0 0 19.4 9a1.65 1.65 0 0 0 1.51 1H21a2 2 0 1 1 0 4h-.09a1.65 1.65 0 0 0-1.51 1z"
      />
    </svg>
  </button>
</header>

<style>
  .topbar {
    display: flex;
    align-items: center;
    gap: var(--space-2);
    height: 48px;
    padding: 0 var(--space-3);
    border-bottom: 1px solid var(--border);
    background: var(--panel);
  }
  .nav-btns {
    display: inline-flex;
    align-items: center;
    gap: var(--space-1);
  }
  .ns-select {
    min-width: 184px;
    max-width: 280px;
    height: var(--control-h);
    margin-right: var(--space-2);
  }
  .topbar > button {
    height: var(--control-h);
  }
  .spacer {
    flex: 1;
  }
</style>
