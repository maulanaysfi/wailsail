<script lang="ts">
  import { Kube, errorMessage } from "../lib/api";
  import { app } from "../lib/store.svelte";

  let manifest = $state(
    `apiVersion: v1\nkind: ConfigMap\nmetadata:\n  name: example\ndata:\n  hello: world\n`,
  );
  let busy = $state(false);
  let error = $state("");

  async function apply() {
    busy = true;
    error = "";
    try {
      const res = (await Kube.Apply(app.context, manifest, app.namespace)) ?? [];
      app.toast(`Applied ${res.map((r) => `${r.kind}/${r.name}`).join(", ")}`, "ok");
      app.showApply = false;
    } catch (e) {
      error = errorMessage(e);
    } finally {
      busy = false;
    }
  }
</script>

<div class="backdrop" role="presentation" onclick={(e) => e.target === e.currentTarget && (app.showApply = false)}>
  <div class="modal wide" role="dialog" aria-modal="true">
    <header>
      <h3>Apply YAML / JSON ({app.context}{app.namespace ? ` · ${app.namespace}` : ""})</h3>
      <button class="ghost" onclick={() => (app.showApply = false)} title="Close apply dialog (Esc)">✕</button>
    </header>
    {#if error}<div class="banner error">{error}</div>{/if}
    <textarea class="editor" spellcheck="false" bind:value={manifest}></textarea>
    <footer>
      <button onclick={() => (app.showApply = false)} title="Cancel and close dialog (Esc)">Cancel</button>
      <button class="primary" onclick={apply} disabled={busy || !manifest.trim()} title="Server-side apply manifest to cluster">
        {busy ? "Applying…" : "Apply"}
      </button>
    </footer>
  </div>
</div>

<style>
  .editor {
    width: 100%;
    height: 344px;
    padding: var(--space-2);
    font-family: var(--mono);
    font-size: 12px;
    line-height: 16px;
    background: var(--code-bg);
    resize: vertical;
  }
  footer {
    display: flex;
    justify-content: flex-end;
    gap: var(--space-2);
    margin-top: var(--space-3);
  }
</style>
