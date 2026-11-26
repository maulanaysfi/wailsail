<script module lang="ts">
  import type { ResourceType, Row } from "../lib/api";
  import { kindKey, restartableKinds, scalableKinds } from "../lib/resources";
  import { app } from "../lib/store.svelte";

  export type MenuIcon = "owner" | "forward" | "logs" | "shell" | "scale" | "restart" | "newtab" | "edit" | "delete";

  export interface MenuItem {
    id: string;
    label: string;
    icon: MenuIcon;
    danger?: boolean;
    disabled?: boolean;
    sepBefore?: boolean;
    run: () => void;
  }

  const WORKLOAD_LOG_KINDS = new Set([
    "apps/Deployment",
    "apps/ReplicaSet",
    "apps/StatefulSet",
    "apps/DaemonSet",
    "batch/Job",
    "batch/CronJob",
  ]);

  export function buildRowMenuItems(
    type: ResourceType,
    r: Row,
    inferReplicas?: (r: Row) => number,
  ): MenuItem[] {
    const sel = { type, namespace: r.namespace, name: r.name };
    const kk = kindKey(type);
    const isPod = kk === "/Pod";
    const isHelm = kk === "helm.sh/Release";
    const items: MenuItem[] = [];

    if (kk === "/Pod" || kk === "apps/ReplicaSet" || kk === "batch/Job" || r.owner) {
      items.push({
        id: "owner",
        label: "Jump to Owner",
        icon: "owner",
        disabled: !r.owner,
        run: () => r.owner && app.jumpToOwner(r.owner, r.namespace),
      });
    }
    if (isPod || kk === "/Service") {
      items.push({
        id: "forward",
        label: "Port forward",
        icon: "forward",
        run: () => (app.forwardTarget = sel),
      });
    }
    if (scalableKinds.has(kk)) {
      items.push({
        id: "scale",
        label: "Scale",
        icon: "scale",
        run: () => (app.scaleTarget = { sel, replicas: inferReplicas ? inferReplicas(r) : 1 }),
      });
    }
    if (restartableKinds.has(kk)) {
      items.push({
        id: "restart",
        label: "Restart",
        icon: "restart",
        run: () => void app.restartResource(sel),
      });
    }
    if (isPod) {
      items.push({
        id: "logs",
        label: "View logs",
        icon: "logs",
        run: () => app.openDock({ kind: "logs", title: r.name, context: app.context, namespace: r.namespace, pod: r.name }),
      });
      items.push({
        id: "shell",
        label: "Exec shell",
        icon: "shell",
        run: () => app.openDock({ kind: "shell", title: r.name, context: app.context, namespace: r.namespace, pod: r.name }),
      });
    } else if (WORKLOAD_LOG_KINDS.has(kk)) {
      items.push({
        id: "logs",
        label: "View logs",
        icon: "logs",
        disabled: !r.uid,
        run: () =>
          r.uid &&
          app.openDock({
            kind: "logs",
            title: `${type.kind}/${r.name}`,
            context: app.context,
            namespace: r.namespace,
            pod: "",
            workload: { kind: type.kind, uid: r.uid, name: r.name },
          }),
      });
    }

    items.push({
      id: "newtab",
      label: "View in new tab",
      icon: "newtab",
      run: () => app.openPageTab(sel),
    });

    items.push({
      id: "edit",
      label: isHelm ? "Edit values" : "Edit YAML",
      icon: "edit",
      sepBefore: items.length > 0,
      run: () => app.openEdit(sel),
    });
    items.push({
      id: "delete",
      label: isHelm ? "Uninstall" : "Delete",
      icon: "delete",
      danger: true,
      run: () => void app.deleteResource(sel),
    });
    return items;
  }
</script>

<script lang="ts">
  import { cssZoom } from "../lib/store.svelte";

  let {
    items,
    x,
    y,
    onClose,
  }: {
    items: MenuItem[];
    x: number;
    y: number;
    onClose: () => void;
  } = $props();

  let menuEl: HTMLDivElement | undefined = $state();
  let pos = $state({ x: 0, y: 0 });

  $effect(() => {
    if (!menuEl) {
      pos = { x, y };
      return;
    }
    const z = cssZoom();
    const maxX = Math.max(8, window.innerWidth / z - menuEl.offsetWidth - 8);
    const maxY = Math.max(8, window.innerHeight / z - menuEl.offsetHeight - 8);
    pos = {
      x: Math.min(Math.max(8, x), maxX),
      y: Math.min(Math.max(8, y), maxY),
    };
  });

  $effect(() => {
    const onPointerDown = (e: PointerEvent) => {
      if (!menuEl || !menuEl.contains(e.target as Node)) onClose();
    };
    const onKeyDown = (e: KeyboardEvent) => {
      if (e.key === "Escape") {
        e.stopPropagation();
        onClose();
      }
    };
    window.addEventListener("pointerdown", onPointerDown, true);
    window.addEventListener("keydown", onKeyDown, true);
    window.addEventListener("blur", onClose);
    return () => {
      window.removeEventListener("pointerdown", onPointerDown, true);
      window.removeEventListener("keydown", onKeyDown, true);
      window.removeEventListener("blur", onClose);
    };
  });
</script>

{#snippet menuSvg(icon: MenuIcon)}
  <svg viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="2" stroke-linecap="round" stroke-linejoin="round" aria-hidden="true">
    {#if icon === "owner"}
      <path d="M7 17L17 7" />
      <polyline points="8 7 17 7 17 16" />
    {:else if icon === "forward"}
      <path d="M18 13v6a2 2 0 0 1-2 2H5a2 2 0 0 1-2-2V8a2 2 0 0 1 2-2h6" />
      <polyline points="15 3 21 3 21 9" />
      <line x1="10" y1="14" x2="21" y2="3" />
    {:else if icon === "scale"}
      <line x1="12" y1="3" x2="12" y2="21" />
      <polyline points="8 7 12 3 16 7" />
      <polyline points="8 17 12 21 16 17" />
    {:else if icon === "restart"}
      <polyline points="23 4 23 10 17 10" />
      <polyline points="1 20 1 14 7 14" />
      <path d="M3.51 9a9 9 0 0 1 14.85-3.36L23 10M1 14l4.64 4.36A9 9 0 0 0 20.49 15" />
    {:else if icon === "logs"}
      <path d="M14 2H6a2 2 0 0 0-2 2v16a2 2 0 0 0 2 2h12a2 2 0 0 0 2-2V8z" />
      <polyline points="14 2 14 8 20 8" />
      <line x1="8" y1="13" x2="16" y2="13" />
      <line x1="8" y1="17" x2="16" y2="17" />
      <line x1="8" y1="9" x2="10" y2="9" />
    {:else if icon === "shell"}
      <polyline points="4 17 10 11 4 5" />
      <line x1="12" y1="19" x2="20" y2="19" />
    {:else if icon === "newtab"}
      <rect x="3" y="3" width="18" height="18" rx="2" />
      <line x1="12" y1="8" x2="12" y2="16" />
      <line x1="8" y1="12" x2="16" y2="12" />
    {:else if icon === "edit"}
      <path d="M14 2H6a2 2 0 0 0-2 2v16a2 2 0 0 0 2 2h12a2 2 0 0 0 2-2v-7" />
      <path d="M18.5 2.5a2.121 2.121 0 0 1 3 3L12 15l-4 1 1-4 9.5-9.5z" />
      <line x1="8" y1="9" x2="11" y2="9" />
      <line x1="8" y1="13" x2="10" y2="13" />
    {:else if icon === "delete"}
      <polyline points="3 6 5 6 21 6" />
      <path d="M19 6v14a2 2 0 0 1-2 2H7a2 2 0 0 1-2-2V6m3 0V4a2 2 0 0 1 2-2h4a2 2 0 0 1 2 2v2" />
      <line x1="10" y1="11" x2="10" y2="17" />
      <line x1="14" y1="11" x2="14" y2="17" />
    {/if}
  </svg>
{/snippet}

<div class="ctx-menu" bind:this={menuEl} role="menu" style={`left:${pos.x}px;top:${pos.y}px`}>
  {#each items as item (item.id)}
    {#if item.sepBefore}
      <div class="ctx-sep" role="separator"></div>
    {/if}
    <button
      class="ctx-item"
      class:danger={item.danger}
      role="menuitem"
      disabled={item.disabled}
      title={item.label}
      onclick={() => {
        onClose();
        item.run();
      }}
    >
      {@render menuSvg(item.icon)}
      <span>{item.label}</span>
    </button>
  {/each}
</div>

<style>
  .ctx-menu {
    position: fixed;
    z-index: 40;
    min-width: 176px;
    padding: var(--space-1);
    background: var(--panel);
    border: 1px solid var(--border);
    border-radius: var(--radius);
    box-shadow: 0 6px 20px rgba(0, 0, 0, 0.38);
    display: flex;
    flex-direction: column;
    gap: 2px;
  }
  .ctx-sep {
    height: 1px;
    margin: var(--space-1) 0;
    background: var(--border-soft);
  }
  .ctx-item {
    display: flex;
    align-items: center;
    gap: var(--space-2);
    width: 100%;
    min-height: 28px;
    padding: var(--space-1) var(--space-2);
    border: 0;
    border-radius: var(--radius);
    background: transparent;
    color: var(--text-strong);
    font-size: 12.5px;
    text-align: left;
  }
  .ctx-item:hover:not(:disabled) {
    background: var(--hover);
  }
  .ctx-item:focus-visible {
    outline: 2px solid var(--accent);
    outline-offset: -2px;
  }
  .ctx-item.danger {
    color: var(--bad);
  }
  .ctx-item svg {
    width: 14px;
    height: 14px;
    flex-shrink: 0;
  }
</style>
