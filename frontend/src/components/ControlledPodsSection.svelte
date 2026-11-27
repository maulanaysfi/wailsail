<script lang="ts">
  import { untrack } from "svelte";
  import {
    Kube,
    errorMessage,
    newStreamId,
    on,
    topics,
    type Column,
    type ResourceRef,
    type ResourceType,
    type Row,
    type WatchEvent,
  } from "../lib/api";
  import { age, toneFor } from "../lib/format";
  import { kindKey } from "../lib/resources";
  import {
    filterControlledPods,
    podSummaryColumns,
    unreadyWorkloadCells,
    type ControlledPodsPlan,
  } from "../lib/specs";
  import { app, cssZoom, refOf } from "../lib/store.svelte";
  import RowContextMenu, { buildRowMenuItems } from "./RowContextMenu.svelte";

  let {
    plan,
    resetKey,
    scrollToken,
  }: {
    plan: ControlledPodsPlan;
    resetKey: string;
    scrollToken: number;
  } = $props();

  const POD_PAGE_SIZE = 5;
  const podRowsMap = new Map<string, Row>();
  const childRowsMap = new Map<string, Row>();
  let podVersion = $state(0);
  let podCols = $state<Column[]>([]);
  let podLoading = $state(false);
  let podError = $state("");
  let podPage = $state(0);
  let focusedPodKey = $state("");
  let podMenu = $state<{ row: Row; x: number; y: number } | null>(null);

  const podResourceType: ResourceType = $derived(
    app.resources.find((x) => kindKey(x) === "/Pod") ?? {
      group: "",
      version: "v1",
      resource: "pods",
      kind: "Pod",
      namespaced: true,
      shortNames: ["po"],
      verbs: [],
    },
  );

  const planKey = $derived(
    `${plan.mode}\0${plan.uid}\0${plan.namespace}\0${plan.labelSelector}\0${plan.fieldSelector}\0${plan.emptyHint}`,
  );

  $effect(() => {
    resetKey;
    podPage = 0;
    focusedPodKey = "";
    podMenu = null;
  });

  $effect(() => {
    scrollToken;
    podMenu = null;
  });

  function openPodMenu(e: MouseEvent, p: Row) {
    e.preventDefault();
    focusedPodKey = p.key;
    const z = cssZoom();
    podMenu = { row: p, x: Math.round(e.clientX / z), y: Math.round(e.clientY / z) };
  }

  function resourceRefOrDefault(group: string, kind: string, fallback: ResourceRef): ResourceRef {
    const hit = app.resources.find((r) => r.group === group && r.kind === kind);
    return hit ? refOf(hit) : fallback;
  }

  $effect(() => {
    const ctx = app.context;
    const key = planKey;
    app.reconnectNonce;
    const curPlan = untrack(() => plan);
    if (!ctx || !key || curPlan.emptyHint) {
      untrack(() => {
        podRowsMap.clear();
        childRowsMap.clear();
        podCols = [];
        podError = "";
        podLoading = false;
        podVersion++;
      });
      return;
    }

    const podRef = untrack(() =>
      resourceRefOrDefault("", "Pod", { group: "", version: "v1", resource: "pods", kind: "Pod", namespaced: true }),
    );
    const childRef =
      curPlan.mode === "deployment"
        ? untrack(() =>
            resourceRefOrDefault("apps", "ReplicaSet", {
              group: "apps",
              version: "v1",
              resource: "replicasets",
              kind: "ReplicaSet",
              namespaced: true,
            }),
          )
        : curPlan.mode === "cronjob"
          ? untrack(() =>
              resourceRefOrDefault("batch", "Job", {
                group: "batch",
                version: "v1",
                resource: "jobs",
                kind: "Job",
                namespaced: true,
              }),
            )
          : null;

    untrack(() => {
      podRowsMap.clear();
      childRowsMap.clear();
      podCols = [];
      podError = "";
      podLoading = true;
      podPage = 0;
      podVersion++;
    });

    let cancelled = false;
    let podReady = false;
    let childReady = childRef === null;
    const syncReady = () => {
      if (podReady && childReady) podLoading = false;
    };

    const cleanups: (() => void)[] = [];
    const startSubWatch = (
      ref: ResourceRef,
      targetMap: Map<string, Row>,
      isPodWatch: boolean,
      labelSelector: string,
      fieldSelector: string,
    ) => {
      const id = newStreamId();
      const off = on<WatchEvent>(topics.watch(id), (ev) => {
        if (cancelled) return;
        if (ev.reset) {
          targetMap.clear();
          if (isPodWatch) {
            if (ev.columns) podCols = ev.columns;
            podReady = true;
          } else {
            childReady = true;
          }
          syncReady();
        }
        for (const r of ev.upserts ?? []) targetMap.set(r.key, r);
        for (const k of ev.deletes ?? []) targetMap.delete(k);
        if (ev.error) {
          podError = ev.error;
          podLoading = false;
        } else if (ev.reset) {
          podError = "";
        }
        podVersion++;
      });
      Kube.WatchFiltered(ctx, ref, curPlan.namespace, labelSelector, fieldSelector, id).catch((e) => {
        if (cancelled) return;
        podError = errorMessage(e);
        podLoading = false;
      });
      cleanups.push(() => {
        off();
        Kube.StopWatch(id);
      });
    };

    startSubWatch(podRef, podRowsMap, true, curPlan.labelSelector, curPlan.fieldSelector);
    if (childRef) {
      startSubWatch(childRef, childRowsMap, false, "", "");
    }

    return () => {
      cancelled = true;
      for (const stop of cleanups) stop();
    };
  });

  const controlledPods = $derived.by(() => {
    podVersion;
    return filterControlledPods(plan, podRowsMap.values(), childRowsMap.values());
  });
  const podColIdx = $derived.by(() => podSummaryColumns(podCols));
  const podTotalPages = $derived(Math.max(1, Math.ceil(controlledPods.length / POD_PAGE_SIZE)));
  const safePodPage = $derived(Math.min(podPage, podTotalPages - 1));
  const pagedPods = $derived(controlledPods.slice(safePodPage * POD_PAGE_SIZE, (safePodPage + 1) * POD_PAGE_SIZE));
</script>

<section class="section">
  <div class="section-head">
    <h4>Pods ({controlledPods.length})</h4>
    {#if podTotalPages > 1}
      <div class="pager">
        <span class="muted pager-range">
          {safePodPage * POD_PAGE_SIZE + 1}–{Math.min((safePodPage + 1) * POD_PAGE_SIZE, controlledPods.length)} of {controlledPods.length}
        </span>
        <button
          class="mini"
          disabled={safePodPage === 0}
          onclick={() => (podPage = Math.max(0, safePodPage - 1))}
          title="Previous pods page"
        >
          ‹
        </button>
        <button
          class="mini"
          disabled={safePodPage >= podTotalPages - 1}
          onclick={() => (podPage = Math.min(podTotalPages - 1, safePodPage + 1))}
          title="Next pods page"
        >
          ›
        </button>
      </div>
    {/if}
  </div>
  {#if plan.emptyHint}
    <p class="muted empty-inline">{plan.emptyHint}</p>
  {:else if podError}
    <div class="banner error inline-banner">{podError}</div>
  {:else if podLoading && controlledPods.length === 0}
    <p class="muted empty-inline">Loading…</p>
  {:else if controlledPods.length === 0}
    <p class="muted empty-inline">No pods.</p>
  {:else}
    <table class="ledger-table pods-table">
      <thead>
        <tr>
          <th class="col-pod-name">Name</th>
          <th class="col-pod-ready">Ready</th>
          <th class="col-pod-status">Status</th>
          <th class="col-pod-age">Age</th>
          <th class="col-pod-restarts">Restarts</th>
        </tr>
      </thead>
      <tbody>
        {#each pagedPods as p (p.key)}
          {@const podStatus = podColIdx.statusIdx >= 0 ? (p.cells[podColIdx.statusIdx] ?? "—") : "—"}
          {@const podReady = podColIdx.readyIdx >= 0 ? (p.cells[podColIdx.readyIdx] ?? "—") : "—"}
          {@const podRestarts = podColIdx.restartsIdx >= 0 ? (p.cells[podColIdx.restartsIdx] ?? "—") : "—"}
          {@const podUnready = unreadyWorkloadCells("/Pod", p.cells, {
            readyIdx: podColIdx.readyIdx,
            desiredIdx: -1,
            completionsIdx: -1,
            statusIdx: podColIdx.statusIdx,
          }).ready}
          <tr
            class="pod-row"
            class:focused={focusedPodKey === p.key}
            onclick={() => (focusedPodKey = p.key)}
            ondblclick={() => {
              focusedPodKey = p.key;
              app.jumpToResource({ kind: "Pod", name: p.name }, p.namespace);
            }}
            oncontextmenu={(e) => openPodMenu(e, p)}
          >
            <td class="col-pod-name">
              <span class="pod-name mono">{p.name}</span>
              {#if plan.mode === "node" && p.namespace}
                <div class="mono muted pod-ns">{p.namespace}</div>
              {/if}
            </td>
            <td class="col-pod-ready mono" class:tone-warn={podUnready}>{podReady}</td>
            <td class={`col-pod-status tone-${toneFor(podStatus)}`}>{podStatus}</td>
            <td class="col-pod-age mono muted">{p.created ? age(p.created, app.now) : "—"}</td>
            <td class="col-pod-restarts mono">{podRestarts}</td>
          </tr>
        {/each}
      </tbody>
    </table>
  {/if}
</section>
{#if podMenu}
  <RowContextMenu
    items={buildRowMenuItems(podResourceType, podMenu.row)}
    x={podMenu.x}
    y={podMenu.y}
    onClose={() => (podMenu = null)}
  />
{/if}

<style>
  .pods-table {
    table-layout: fixed;
    user-select: none;
    -webkit-user-select: none;
  }
  .pods-table td {
    vertical-align: middle;
  }
  .pods-table .col-pod-name {
    min-width: 0;
    padding-right: var(--space-3);
  }
  .pods-table .col-pod-ready {
    width: 52px;
    padding-right: var(--space-3);
    white-space: nowrap;
    font-variant-numeric: tabular-nums;
  }
  .pods-table .col-pod-status {
    width: 88px;
    padding-right: var(--space-3);
    white-space: nowrap;
    overflow: hidden;
    text-overflow: ellipsis;
  }
  .pods-table .col-pod-age {
    width: 48px;
    padding-right: var(--space-3);
    text-align: right;
    white-space: nowrap;
    font-variant-numeric: tabular-nums;
  }
  .pods-table .col-pod-restarts {
    width: 60px;
    text-align: right;
    padding-right: 0;
    white-space: nowrap;
    font-variant-numeric: tabular-nums;
  }
  .pods-table tbody tr.pod-row {
    cursor: pointer;
  }
  .pods-table tbody tr.pod-row:hover {
    background: var(--hover);
  }
  .pods-table tbody tr.pod-row.focused {
    background: var(--selected);
  }
  .pod-name {
    display: block;
    width: 100%;
    overflow: hidden;
    text-overflow: ellipsis;
    white-space: nowrap;
    color: var(--text-strong);
  }
  .pod-ns {
    font-size: 11px;
    overflow: hidden;
    text-overflow: ellipsis;
    white-space: nowrap;
  }
</style>
