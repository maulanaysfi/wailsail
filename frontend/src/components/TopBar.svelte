<script lang="ts">
  import { app } from "../lib/store.svelte";

  const themeLabel: Record<string, string> = { system: "Auto", light: "Light", dark: "Dark" };
</script>

<header class="topbar">
  <div class="brand">
    <img class="logo" src="/icon.png" alt="wailsail" width="22" height="22" />
    <span>wailsail</span>
  </div>

  <label class="field">
    <span>Context</span>
    <select value={app.context} onchange={(e) => app.selectContext(e.currentTarget.value)}>
      {#if app.contexts.length === 0}<option value="">No contexts</option>{/if}
      {#each app.contexts as c (c.name)}
        <option value={c.name}>{c.name}</option>
      {/each}
    </select>
  </label>

  <label class="field">
    <span>Namespace</span>
    <select
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
  </label>

  {#if app.serverVersion}
    <span class="pill" title="Kubernetes server version">{app.serverVersion}</span>
  {/if}

  <div class="spacer"></div>

  <button class="ghost" onclick={() => (app.showApply = true)} disabled={!app.context} title="Apply YAML">
    + Apply
  </button>
  <button class="ghost" onclick={() => (app.showForwards = true)} title="Port forwards">Forwards</button>
  <button class="ghost" onclick={() => (app.dockOpen = !app.dockOpen)} disabled={app.dock.length === 0}>
    Dock{app.dock.length ? ` (${app.dock.length})` : ""}
  </button>
  <button class="ghost" onclick={() => app.reload()} title="Reload kubeconfig">Reload</button>
  <button class="ghost" onclick={() => app.cycleTheme()} title="Theme">{themeLabel[app.settings.theme] ?? "Auto"}</button>
</header>

<style>
  .topbar {
    display: flex;
    align-items: center;
    gap: 12px;
    padding: 8px 12px;
    border-bottom: 1px solid var(--border);
    background: var(--panel);
  }
  .brand {
    display: flex;
    align-items: center;
    gap: 8px;
    font-weight: 700;
    letter-spacing: 0.02em;
    margin-right: 8px;
  }
  .logo {
    border-radius: 5px;
    object-fit: cover;
    display: block;
  }
  .field {
    display: flex;
    align-items: center;
    gap: 6px;
    font-size: 12px;
    color: var(--muted);
  }
  .field select {
    min-width: 180px;
    max-width: 280px;
  }
  .spacer {
    flex: 1;
  }
</style>
