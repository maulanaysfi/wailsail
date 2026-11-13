<script lang="ts">
  import { onMount } from "svelte";
  import { Kube, PortForward, on, topics, errorMessage, type ContainerPort, type Forward } from "../lib/api";
  import { app } from "../lib/store.svelte";

  let forwards = $state<Forward[]>([]);
  let ports = $state<ContainerPort[]>([]);
  let remotePort = $state(80);
  let localPort = $state(0);
  let busy = $state(false);
  let error = $state("");

  const target = $derived(app.forwardTarget);

  onMount(() => {
    PortForward.List().then((f) => (forwards = f ?? []));
    return on<Forward[]>(topics.portForwards, (f) => (forwards = f ?? []));
  });

  $effect(() => {
    const t = target;
    if (!t) return;
    ports = [];
    error = "";
    const p =
      t.type.kind === "Service"
        ? PortForward.ServicePorts(app.context, t.namespace, t.name)
        : Kube.PodContainers(app.context, t.namespace, t.name).then((cs) => (cs ?? []).flatMap((c) => c.ports ?? []));
    p.then((list) => {
      ports = list ?? [];
      if (ports.length) remotePort = ports[0].port;
    }).catch((e) => (error = errorMessage(e)));
  });

  async function start() {
    if (!target) return;
    busy = true;
    error = "";
    try {
      const fw = await PortForward.Start({
        context: app.context,
        namespace: target.namespace,
        kind: target.type.kind,
        name: target.name,
        remotePort: Number(remotePort),
        localPort: Number(localPort) || 0,
      });
      app.toast(`Forwarding 127.0.0.1:${fw.localPort} → ${fw.pod}:${fw.podPort}`, "ok");
      app.forwardTarget = null;
      app.showForwards = true;
    } catch (e) {
      error = errorMessage(e);
    } finally {
      busy = false;
    }
  }

  function close() {
    app.forwardTarget = null;
    app.showForwards = false;
  }
</script>

<div class="backdrop" role="presentation" onclick={(e) => e.target === e.currentTarget && close()}>
  <div class="modal" role="dialog" aria-modal="true">
    <header>
      <h3>{target ? `Port Forward · ${target.type.kind} ${target.name}` : "Port Forwards"}</h3>
      <button class="ghost" onclick={close}>✕</button>
    </header>

    {#if target}
      <div class="form">
        <label>
          Remote port
          {#if ports.length}
            <select bind:value={remotePort}>
              {#each ports as p (p.port)}
                <option value={p.port}>{p.port}{p.name ? ` (${p.name})` : ""}</option>
              {/each}
            </select>
          {/if}
          <input type="number" min="1" max="65535" bind:value={remotePort} />
        </label>
        <label>
          Local port <span class="hint">(0 = auto)</span>
          <input type="number" min="0" max="65535" bind:value={localPort} />
        </label>
        <button class="primary" onclick={start} disabled={busy}>{busy ? "Starting…" : "Start"}</button>
      </div>
    {/if}

    {#if error}<div class="banner error">{error}</div>{/if}

    {#if forwards.length === 0}
      <p class="empty">No active port forwards.</p>
    {:else}
      <table>
        <thead>
          <tr><th>Local</th><th>Target</th><th>Pod</th><th>Status</th><th></th></tr>
        </thead>
        <tbody>
          {#each forwards as f (f.id)}
            <tr>
              <td class="mono">127.0.0.1:{f.localPort}</td>
              <td>{f.kind}/{f.name}:{f.remotePort} <span class="hint">({f.namespace})</span></td>
              <td class="mono">{f.pod}:{f.podPort}</td>
              <td class={f.status === "active" ? "tone-ok" : "tone-bad"} title={f.error ?? ""}>{f.status}</td>
              <td><button class="mini danger" onclick={() => PortForward.Stop(f.id)}>Stop</button></td>
            </tr>
          {/each}
        </tbody>
      </table>
    {/if}
  </div>
</div>

<style>
  .form {
    display: flex;
    align-items: flex-end;
    gap: 12px;
    margin-bottom: 14px;
  }
  .form label {
    display: flex;
    flex-direction: column;
    gap: 4px;
    font-size: 12px;
  }
  .form input {
    width: 110px;
  }
  .hint {
    color: var(--muted);
    font-size: 11px;
  }
  .mono {
    font-family: var(--mono);
  }
  table {
    width: 100%;
    border-collapse: collapse;
    font-size: 12.5px;
  }
  th,
  td {
    text-align: left;
    padding: 6px 8px;
    border-bottom: 1px solid var(--border-soft);
  }
  th {
    color: var(--muted);
  }
  .empty {
    color: var(--muted);
    text-align: center;
    margin: 20px 0;
  }
</style>
