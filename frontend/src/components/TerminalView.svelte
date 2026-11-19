<script lang="ts">
  import { onMount, untrack } from "svelte";
  import { Terminal } from "@xterm/xterm";
  import { FitAddon } from "@xterm/addon-fit";
  import "@xterm/xterm/css/xterm.css";
  import { Exec, Kube, newStreamId, on, topics, errorMessage, type ContainerInfo, type ExecEvent } from "../lib/api";
  import type { DockStatus } from "../lib/store.svelte";

  let {
    context,
    namespace,
    pod,
    initialContainer = "",
    onStatus,
  }: {
    context: string;
    namespace: string;
    pod: string;
    initialContainer?: string;
    onStatus?: (s: DockStatus) => void;
  } = $props();

  let containers = $state<ContainerInfo[]>([]);
  let container = $state(untrack(() => initialContainer));
  let shell = $state("");
  let exited = $state(false);
  let error = $state("");
  let reconnectNonce = $state(0);
  let hostEl: HTMLDivElement | undefined = $state();

  let term: Terminal | null = null;
  let fit: FitAddon | null = null;
  let activeSession = "";

  onMount(() => {
    const t = new Terminal({
      cursorBlink: true,
      fontSize: 13,
      fontFamily: "ui-monospace, SFMono-Regular, Menlo, Monaco, Consolas, monospace",
      theme: { background: "#0f1117", foreground: "#e6edf3" },
    });
    const f = new FitAddon();
    t.loadAddon(f);
    if (hostEl) t.open(hostEl);
    f.fit();
    term = t;
    fit = f;

    t.onData((data) => {
      if (activeSession && !exited) Exec.Write(activeSession, data);
    });
    t.onResize(({ cols, rows }) => {
      if (activeSession && !exited) Exec.Resize(activeSession, cols, rows);
    });

    const ro = new ResizeObserver(() => f.fit());
    if (hostEl) ro.observe(hostEl);

    Kube.PodContainers(context, namespace, pod)
      .then((cs) => {
        containers = cs ?? [];
        if (!container || !containers.some((c) => c.name === container)) {
          container = containers.find((c) => c.type === "main")?.name ?? containers[0]?.name ?? "";
        }
      })
      .catch((e) => {
        error = errorMessage(e);
        onStatus?.("disconnected");
      });

    return () => {
      ro.disconnect();
      t.dispose();
    };
  });

  $effect(() => {
    const ctx = context,
      ns = namespace,
      p = pod,
      c = container,
      sh = shell;
    reconnectNonce;
    if (!c || !term) return;
    untrack(() => {
      exited = false;
      error = "";
      onStatus?.("connecting");
    });
    term.reset();
    fit?.fit();

    const id = newStreamId();
    activeSession = id;
    const off = on<ExecEvent>(topics.exec(id), (ev) => {
      if (ev.connected) onStatus?.("connected");
      if (ev.data) {
        onStatus?.("connected");
        const bin = atob(ev.data);
        const bytes = new Uint8Array(bin.length);
        for (let i = 0; i < bin.length; i++) bytes[i] = bin.charCodeAt(i);
        term?.write(bytes);
      }
      if (ev.error) {
        error = ev.error;
        onStatus?.("disconnected");
      }
      if (ev.exited) {
        exited = true;
        onStatus?.("disconnected");
        term?.writeln("\r\n\x1b[90m[session ended]\x1b[0m");
      }
    });
    const cmd = sh ? [sh] : [];
    Exec.Start(ctx, ns, p, c, cmd, term.cols || 80, term.rows || 24, id)
      .then(() => term?.focus())
      .catch((e) => {
        error = errorMessage(e);
        exited = true;
        onStatus?.("disconnected");
      });
    return () => {
      off();
      Exec.Close(id);
      if (activeSession === id) activeSession = "";
    };
  });
</script>

<div class="term-pane">
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
      Shell
      <select bind:value={shell}>
        <option value="">auto (bash / sh)</option>
        <option value="/bin/bash">/bin/bash</option>
        <option value="/bin/sh">/bin/sh</option>
        <option value="/bin/ash">/bin/ash</option>
      </select>
    </label>
    <button onclick={() => reconnectNonce++} title="Reconnect interactive shell session">Reconnect</button>
    {#if exited}<span class="pill">exited</span>{/if}
  </div>
  {#if error}<div class="banner error">{error}</div>{/if}
  <div class="host" bind:this={hostEl}></div>
</div>

<style>
  .term-pane {
    display: flex;
    flex-direction: column;
    flex: 1;
    min-height: 0;
  }
  .bar {
    display: flex;
    align-items: center;
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
  .host {
    flex: 1;
    min-height: 0;
    padding: var(--space-1) var(--space-2);
    background: #0f1117;
  }
  .banner {
    margin: var(--space-2) var(--space-3) 0;
  }
</style>
