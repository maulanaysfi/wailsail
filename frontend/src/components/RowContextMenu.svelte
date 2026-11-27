<script module lang="ts">
  import type { ResourceType, Row } from "../lib/api";
  import { kindKey, ownerJumpKinds, restartableKinds, scalableKinds, workloadLogKinds } from "../lib/resources";
  import { app } from "../lib/store.svelte";
  import Icon, { type IconName } from "./Icon.svelte";

  export type MenuIcon = IconName;

  export interface MenuItem {
    id: string;
    label: string;
    icon: MenuIcon;
    danger?: boolean;
    disabled?: boolean;
    sepBefore?: boolean;
    run: () => void;
  }

  export function buildRowMenuItems(
    type: ResourceType,
    r: Row,
    inferReplicas?: (r: Row) => number,
  ): MenuItem[] {
    const sel = { type, namespace: r.namespace, name: r.name };
    const kk = kindKey(type);
    const isPod = kk === "/Pod";
    const isHelm = kk === "helm.sh/Release";
    const items: MenuItem[] = [
      {
        id: "newtab",
        label: "View in new tab",
        icon: "newtab",
        run: () => app.openPageTab(sel),
      },
    ];

    if (ownerJumpKinds.has(kk) || r.owner) {
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
    } else if (workloadLogKinds.has(kk)) {
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
      <Icon name={item.icon} />
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
  .ctx-item :global(svg) {
    width: 14px;
    height: 14px;
    flex-shrink: 0;
  }
</style>
