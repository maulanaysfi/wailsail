<script lang="ts">
  import { Dialogs } from "@wailsio/runtime";
  import { errorMessage } from "../lib/api";
  import { APP_VERSION, app, scaleOptions } from "../lib/store.svelte";

  let draft = $state("");
  let busy = $state(false);

  const paths = $derived(app.settings.kubeconfigPaths ?? []);
  const isDefault = $derived(paths.length === 1 && paths[0] === app.defaultKubeconfigPath);

  const themes = [
    { value: "system", label: "System" },
    { value: "light", label: "Light" },
    { value: "dark", label: "Dark" },
  ];

  async function run(fn: () => Promise<void> | void) {
    busy = true;
    try {
      await fn();
    } catch (e) {
      app.toast(errorMessage(e), "error");
    } finally {
      busy = false;
    }
  }

  function addDraft() {
    const p = draft.trim();
    if (!p) return;
    if (paths.includes(p)) {
      app.toast("That path is already in the list", "info");
      return;
    }
    draft = "";
    run(() => app.addKubeconfigPath(p));
  }

  async function pick(directory: boolean) {
    let chosen: string | string[];
    try {
      chosen = await Dialogs.OpenFile({
        Title: directory ? "Add kubeconfig folder" : "Add kubeconfig file",
        CanChooseFiles: !directory,
        CanChooseDirectories: directory,
        ShowHiddenFiles: true,
        AllowsMultipleSelection: false,
      });
    } catch (e) {
      app.toast(`Could not open picker: ${errorMessage(e)}`, "error");
      return;
    }
    const p = Array.isArray(chosen) ? chosen[0] : chosen;
    if (p) run(() => app.addKubeconfigPath(p));
  }

  function close() {
    app.showSettings = false;
  }
</script>

<div class="backdrop" role="presentation" onclick={(e) => e.target === e.currentTarget && close()}>
  <div class="modal wide" role="dialog" aria-modal="true" aria-labelledby="settings-title">
    <header>
      <div class="head-title">
        <h3 id="settings-title">Settings</h3>
        <span class="pill">{APP_VERSION}</span>
      </div>
      <button class="ghost" onclick={close} title="Close settings (Esc)">✕</button>
    </header>

    <section>
      <h4>Appearance</h4>
      <div class="row">
        <span class="label">Screen scale</span>
        <div class="segmented" role="radiogroup" aria-label="Screen scale">
          {#each scaleOptions as s (s)}
            <button
              role="radio"
              aria-checked={app.settings.scale === s}
              class:on={app.settings.scale === s}
              onclick={() => app.saveSettings({ scale: s })}
              title={`Set screen scale to ${s}%`}
            >
              {s}%
            </button>
          {/each}
        </div>
      </div>
      <div class="row">
        <span class="label">Theme</span>
        <div class="segmented" role="radiogroup" aria-label="Theme">
          {#each themes as t (t.value)}
            <button
              role="radio"
              aria-checked={app.settings.theme === t.value}
              class:on={app.settings.theme === t.value}
              onclick={() => app.saveSettings({ theme: t.value })}
              title={`Set theme to ${t.label}`}
            >
              {t.label}
            </button>
          {/each}
        </div>
      </div>
    </section>

    <section>
      <h4>Kubeconfig</h4>
      <p class="hint">
        Files are merged in order; folders are scanned for kubeconfig files. <code>~</code> and environment variables are
        expanded. Entries in <code>$KUBECONFIG</code> are also loaded, after these.
      </p>

      {#if paths.length === 0}
        <p class="empty">No paths configured — using <code>$KUBECONFIG</code> or the default kubeconfig.</p>
      {:else}
        <ul class="paths">
          {#each paths as p (p)}
            <li>
              <span class="path">{p}</span>
              {#if p === app.defaultKubeconfigPath}<span class="pill">default</span>{/if}
              <button
                class="mini"
                disabled={busy}
                onclick={() => run(() => app.removeKubeconfigPath(p))}
                title={`Remove ${p} from kubeconfig paths`}
              >
                Remove
              </button>
            </li>
          {/each}
        </ul>
      {/if}

      <div class="add">
        <input
          placeholder="e.g. ~/.kube/config or ~/.kube/clusters"
          bind:value={draft}
          onkeydown={(e) => e.key === "Enter" && addDraft()}
        />
        <button onclick={addDraft} disabled={busy || !draft.trim()} title="Add entered kubeconfig path">Add</button>
      </div>
      <div class="actions">
        <button onclick={() => pick(false)} disabled={busy} title="Browse and add a kubeconfig file">Add file…</button>
        <button onclick={() => pick(true)} disabled={busy} title="Browse and add a folder of kubeconfig files">Add folder…</button>
        <span class="grow"></span>
        <button
          onclick={() => run(() => app.resetKubeconfigPaths())}
          disabled={busy || isDefault}
          title="Restore the default kubeconfig path"
        >
          Reset to default
        </button>
      </div>
    </section>
  </div>
</div>

<style>
  .head-title {
    display: flex;
    align-items: center;
    gap: var(--space-2);
  }
  section + section {
    margin-top: var(--space-4);
    padding-top: var(--space-3);
    border-top: 1px solid var(--border);
  }
  h4 {
    margin: 0 0 var(--space-2);
    font-size: 11px;
    text-transform: uppercase;
    letter-spacing: 0.06em;
    color: var(--muted);
    font-weight: 600;
  }
  .row {
    display: flex;
    align-items: center;
    gap: var(--space-3);
    min-height: var(--control-h);
    margin-bottom: var(--space-2);
  }
  .label {
    width: 112px;
    color: var(--text);
  }
  .segmented {
    display: inline-flex;
    border: 1px solid var(--border);
    border-radius: var(--radius);
    overflow: hidden;
  }
  .segmented button {
    border: 0;
    border-radius: 0;
    min-width: 56px;
    height: var(--control-h-sm);
    padding: 0 var(--space-2);
    background: var(--bg);
  }
  .segmented button + button {
    border-left: 1px solid var(--border);
  }
  .segmented button.on {
    background: var(--accent);
    color: #fff;
  }
  .hint {
    margin: 0 0 var(--space-2);
    font-size: 12px;
    color: var(--muted);
    line-height: 16px;
  }
  code {
    font-family: var(--mono);
    font-size: 11px;
  }
  .empty {
    margin: var(--space-2) 0;
    color: var(--muted);
  }
  .paths {
    list-style: none;
    margin: 0 0 var(--space-2);
    padding: 0;
    border: 1px solid var(--border);
    border-radius: var(--radius);
    background: var(--bg);
  }
  .paths li {
    display: flex;
    align-items: center;
    gap: var(--space-2);
    min-height: var(--control-h);
    padding: 0 var(--space-2);
  }
  .paths li + li {
    border-top: 1px solid var(--border-soft);
  }
  .path {
    flex: 1;
    min-width: 0;
    font-family: var(--mono);
    font-size: 12px;
    overflow: hidden;
    text-overflow: ellipsis;
    white-space: nowrap;
    color: var(--text-strong);
  }
  .add {
    display: flex;
    gap: var(--space-2);
    margin-bottom: var(--space-2);
  }
  .add input {
    flex: 1;
  }
  .actions {
    display: flex;
    gap: var(--space-2);
  }
  .grow {
    flex: 1;
  }
</style>
