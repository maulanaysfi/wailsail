<script lang="ts">
  import { Kube, errorMessage } from "../lib/api";
  import { app, cssZoom, refOf, type Selection } from "../lib/store.svelte";
  import { kindKey, restartableKinds, scalableKinds } from "../lib/resources";

  let { sel }: { sel: Selection } = $props();

  let yaml = $state("");
  let draft = $state("");
  let editing = $state(false);
  let loading = $state(false);
  let saving = $state(false);
  let error = $state("");
  let width = $state(560);

  const kk = $derived(kindKey(sel.type));
  const isPod = $derived(kk === "/Pod");
  const canForward = $derived(kk === "/Pod" || kk === "/Service");
  const canScale = $derived(scalableKinds.has(kk));
  const canRestart = $derived(restartableKinds.has(kk));
  const dirty = $derived(editing && draft !== yaml);

  async function load() {
    loading = true;
    error = "";
    try {
      yaml = await Kube.GetYAML(app.context, refOf(sel.type), sel.namespace, sel.name);
      if (!editing) draft = yaml;
    } catch (e) {
      error = errorMessage(e);
    } finally {
      loading = false;
    }
  }

  $effect(() => {
    // Reload whenever the selection changes.
    sel.name;
    sel.namespace;
    sel.type;
    editing = false;
    load();
  });

  function startEdit() {
    draft = yaml;
    editing = true;
  }

  async function cancelEdit() {
    if (dirty && !(await app.confirm("Discard changes?", "Your edits to this resource will be lost."))) return;
    editing = false;
    draft = yaml;
  }

  async function save() {
    saving = true;
    try {
      await Kube.Apply(app.context, draft, sel.namespace);
      app.toast(`${sel.type.kind} ${sel.name} applied`, "ok");
      editing = false;
      await load();
    } catch (e) {
      app.toast(errorMessage(e), "error");
    } finally {
      saving = false;
    }
  }

  async function remove() {
    const where = sel.namespace ? ` in namespace ${sel.namespace}` : "";
    if (!(await app.confirm(`Delete ${sel.type.kind}?`, `${sel.name}${where} will be deleted.`, true))) return;
    try {
      await Kube.Delete(app.context, refOf(sel.type), sel.namespace, sel.name);
      app.toast(`${sel.type.kind} ${sel.name} deleted`, "ok");
      app.selection = null;
    } catch (e) {
      app.toast(errorMessage(e), "error");
    }
  }

  let replicas = $state<number | null>(null);

  function startScale() {
    const m = /\n\s*replicas:\s*(\d+)/.exec(yaml.split("\nstatus:")[0]);
    replicas = Number(m?.[1] ?? 1);
  }

  async function scale() {
    const n = replicas;
    if (n == null || !Number.isInteger(n) || n < 0) {
      app.toast("Replicas must be a non-negative integer", "error");
      return;
    }
    try {
      await Kube.Scale(app.context, refOf(sel.type), sel.namespace, sel.name, n);
      app.toast(`Scaled ${sel.name} to ${n}`, "ok");
      replicas = null;
      load();
    } catch (e) {
      app.toast(errorMessage(e), "error");
    }
  }

  async function restart() {
    if (!(await app.confirm(`Restart ${sel.name}?`, "Pods will be replaced following the rollout strategy."))) return;
    try {
      await Kube.Restart(app.context, refOf(sel.type), sel.namespace, sel.name);
      app.toast(`Restart of ${sel.name} triggered`, "ok");
    } catch (e) {
      app.toast(errorMessage(e), "error");
    }
  }

  function openDock(kind: "logs" | "shell") {
    app.openDock({ kind, title: sel.name, context: app.context, namespace: sel.namespace, pod: sel.name });
  }

  function onKey(e: KeyboardEvent) {
    if ((e.ctrlKey || e.metaKey) && e.key === "s" && editing) {
      e.preventDefault();
      save();
    }
    if (e.key === "Tab" && editing) {
      e.preventDefault();
      const ta = e.currentTarget as HTMLTextAreaElement;
      const { selectionStart: s, selectionEnd: en } = ta;
      draft = draft.slice(0, s) + "  " + draft.slice(en);
      requestAnimationFrame(() => ta.setSelectionRange(s + 2, s + 2));
    }
  }

  function startResize(e: PointerEvent) {
    const z = cssZoom();
    const startX = e.clientX;
    const startW = width;
    const move = (ev: PointerEvent) => (width = Math.min(1100, Math.max(360, startW + (startX - ev.clientX) / z)));
    const up = () => {
      window.removeEventListener("pointermove", move);
      window.removeEventListener("pointerup", up);
    };
    window.addEventListener("pointermove", move);
    window.addEventListener("pointerup", up);
  }
</script>

<aside class="drawer" style={`width:${width}px`}>
  <div class="resizer" onpointerdown={startResize} role="separator" aria-orientation="vertical"></div>
  <header>
    <div class="title">
      <span class="kind">{sel.type.kind}</span>
      <strong title={sel.name}>{sel.name}</strong>
      {#if sel.namespace}<span class="ns">{sel.namespace}</span>{/if}
    </div>
    <button class="ghost close" onclick={() => (app.selection = null)} title="Close">✕</button>
  </header>

  <div class="actions">
    {#if isPod}
      <button onclick={() => openDock("logs")}>Logs</button>
      <button onclick={() => openDock("shell")}>Shell</button>
    {/if}
    {#if canForward}
      <button onclick={() => (app.forwardTarget = { ...sel })}>Port forward</button>
    {/if}
    {#if canScale}
      {#if replicas === null}
        <button onclick={startScale}>Scale</button>
      {:else}
        <span class="scale">
          <input type="number" min="0" bind:value={replicas} onkeydown={(e) => e.key === "Enter" && scale()} />
          <button class="primary" onclick={scale}>Set</button>
          <button class="ghost" onclick={() => (replicas = null)}>✕</button>
        </span>
      {/if}
    {/if}
    {#if canRestart}<button onclick={restart}>Restart</button>{/if}
    <span class="grow"></span>
    {#if editing}
      <button onclick={cancelEdit} disabled={saving}>Cancel</button>
      <button class="primary" onclick={save} disabled={saving || !dirty}>{saving ? "Applying…" : "Apply"}</button>
    {:else}
      <button onclick={load} disabled={loading}>Refresh</button>
      <button onclick={startEdit} disabled={!yaml}>Edit</button>
      <button class="danger" onclick={remove}>Delete</button>
    {/if}
  </div>

  {#if error}<div class="banner error">{error}</div>{/if}

  <textarea
    class="yaml"
    class:editing
    spellcheck="false"
    readonly={!editing}
    value={editing ? draft : yaml}
    oninput={(e) => (draft = e.currentTarget.value)}
    onkeydown={onKey}
    placeholder={loading ? "Loading…" : ""}
  ></textarea>
</aside>

<style>
  .drawer {
    position: relative;
    display: flex;
    flex-direction: column;
    border-left: 1px solid var(--border);
    background: var(--panel);
    min-height: 0;
  }
  .resizer {
    position: absolute;
    left: -3px;
    top: 0;
    bottom: 0;
    width: 6px;
    cursor: col-resize;
    z-index: 2;
  }
  header {
    display: flex;
    align-items: center;
    padding: var(--space-3) var(--space-3) var(--space-2);
    gap: var(--space-2);
  }
  .title {
    display: flex;
    align-items: baseline;
    gap: var(--space-2);
    min-width: 0;
    flex: 1;
  }
  .title strong {
    overflow: hidden;
    text-overflow: ellipsis;
    white-space: nowrap;
    color: var(--text-strong);
  }
  .kind {
    font-size: 11px;
    text-transform: uppercase;
    letter-spacing: 0.05em;
    color: var(--accent);
  }
  .ns {
    font-size: 12px;
    color: var(--muted);
  }
  .actions {
    display: flex;
    flex-wrap: wrap;
    gap: var(--space-2);
    padding: 0 var(--space-3) var(--space-3);
    border-bottom: 1px solid var(--border);
  }
  .grow {
    flex: 1;
  }
  .scale {
    display: inline-flex;
    gap: var(--space-1);
  }
  .scale input {
    width: 64px;
  }
  .yaml {
    flex: 1;
    resize: none;
    border: 0;
    border-radius: 0;
    padding: var(--space-3);
    font-family: var(--mono);
    font-size: 12px;
    line-height: 16px;
    background: var(--code-bg);
    color: var(--text);
    tab-size: 2;
    white-space: pre;
  }
  .yaml.editing {
    box-shadow: inset 2px 0 0 var(--accent);
  }
  .banner {
    margin: var(--space-2) var(--space-3);
  }
</style>
