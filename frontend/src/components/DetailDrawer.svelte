<script lang="ts">
  import { untrack } from "svelte";
  import {
    Kube,
    newStreamId,
    on,
    topics,
    errorMessage,
    type Column,
    type ContainerMetrics,
    type EventInfo,
    type HelmReleaseDetail,
    type ResourceRef,
    type Row,
    type WatchEvent,
  } from "../lib/api";
  import { app, cssZoom, refOf, type Selection } from "../lib/store.svelte";
  import {
    age,
    buildConfigDataEntries,
    SECRET_VALUE_MASK,
    toneFor,
    type ResourceMeter,
  } from "../lib/format";
  import {
    buildContainersList,
    buildControlledPodsPlan,
    buildSelectorEntries,
    buildSpecRows,
    filterControlledPods,
    podSummaryColumns,
    unreadyWorkloadCells,
  } from "../lib/specs";
  import { kindKey, restartableKinds, scalableKinds } from "../lib/resources";
  import YamlEditor from "./YamlEditor.svelte";

  let { sel }: { sel: Selection } = $props();

  let tab = $state<"overview" | "yaml" | "manifest">("overview");
  let obj = $state<Record<string, any> | null>(null);
  let helmDetail = $state<HelmReleaseDetail | null>(null);
  let helmUserOnly = $state(false);
  let revPage = $state(0);
  let rollingBackRev = $state<number | null>(null);
  let events = $state<EventInfo[]>([]);
  let metrics = $state<Record<string, ContainerMetrics>>({});
  let yaml = $state("");
  let neatYaml = $state("");
  let neat = $state(false);
  let draft = $state("");
  let editing = $state(false);
  let loading = $state(false);
  function defaultDrawerWidth(): number {
    if (typeof window === "undefined") return 544;
    return Math.min(1100, Math.max(384, Math.round((window.innerWidth / cssZoom()) * 0.4)));
  }

  let saving = $state(false);
  let error = $state("");
  let width = $state(defaultDrawerWidth());
  let expandedMeta = $state<Record<string, boolean>>({});
  let revealedKeys = $state<Record<string, boolean>>({});
  let revealedVals = $state<Record<string, boolean>>({});
  let copiedId = $state("");

  const kk = $derived(kindKey(sel.type));
  const isPod = $derived(kk === "/Pod");
  const isHelmRelease = $derived(kk === "helm.sh/Release");
  const canJumpOwner = $derived(kk === "/Pod" || kk === "apps/ReplicaSet" || kk === "batch/Job");
  const canForward = $derived(kk === "/Pod" || kk === "/Service");
  const canScale = $derived(scalableKinds.has(kk));
  const canRestart = $derived(restartableKinds.has(kk));
  const activeYaml = $derived(
    isHelmRelease
      ? helmUserOnly
        ? (helmDetail?.userValues ?? "")
        : (helmDetail?.mergedValues ?? "")
      : neat
        ? neatYaml
        : yaml,
  );
  const dirty = $derived(editing && draft !== activeYaml);
  const REV_PAGE_SIZE = 5;
  const helmHistory = $derived(helmDetail?.history ?? []);
  const revTotalPages = $derived(Math.max(1, Math.ceil(helmHistory.length / REV_PAGE_SIZE)));
  const safeRevPage = $derived(Math.min(revPage, revTotalPages - 1));
  const pagedRevisions = $derived(helmHistory.slice(safeRevPage * REV_PAGE_SIZE, (safeRevPage + 1) * REV_PAGE_SIZE));

  let loadSeq = 0;

  async function load() {
    const seq = ++loadSeq;
    const ctx = app.context;
    const ref = refOf(sel.type);
    const ns = sel.namespace;
    const name = sel.name;
    loading = true;
    error = "";
    try {
      if (ref.group === "helm.sh" && ref.resource === "releases") {
        const hd = await Kube.GetHelmRelease(ctx, ns, name);
        if (seq !== loadSeq) return;
        helmDetail = hd;
        yaml = hd.userValues ?? "";
        neatYaml = hd.mergedValues ?? "";
        if (!editing) draft = helmUserOnly ? yaml : neatYaml;
        return;
      }
      const detail = await Kube.GetDetail(ctx, ref, ns, name);
      if (seq !== loadSeq) return;
      obj = (detail.object as Record<string, any>) ?? null;
      events = detail.events ?? [];
      metrics = (detail.metrics as Record<string, ContainerMetrics>) ?? {};
      yaml = detail.yaml ?? "";
      neatYaml = detail.neatYaml ?? yaml;
      if (!editing) draft = neat ? neatYaml : yaml;
    } catch (e) {
      if (seq !== loadSeq) return;
      error = errorMessage(e);
    } finally {
      if (seq === loadSeq) loading = false;
    }
  }

  let loadedKey = "";

  $effect(() => {
    // Reload only when the selected object or active cluster actually changes.
    const key = `${app.context}\0${sel.type.group}\0${sel.type.resource}\0${sel.namespace}\0${sel.name}`;
    if (key === loadedKey) return;
    loadedKey = key;
    untrack(() => {
      editing = false;
      replicas = null;
      obj = null;
      helmDetail = null;
      rollingBackRev = null;
      if (tab === "manifest" && !(sel.type.group === "helm.sh" && sel.type.resource === "releases")) {
        tab = "overview";
      }
      events = [];
      metrics = {};
      yaml = "";
      neatYaml = "";
      draft = "";
      expandedMeta = {};
      revealedKeys = {};
      revealedVals = {};
      podPage = 0;
      revPage = 0;
      load();
    });
  });

  function startEdit() {
    draft = activeYaml;
    editing = true;
    tab = "yaml";
  }

  function cancelEdit() {
    editing = false;
    draft = activeYaml;
  }

  $effect(() => {
    if (!editing) return;
    app.cancelYamlEdit = cancelEdit;
    return () => {
      if (app.cancelYamlEdit === cancelEdit) app.cancelYamlEdit = null;
    };
  });

  async function copyValue(value: string, id: string, label = "value") {
    if (value == null) return;
    try {
      await navigator.clipboard.writeText(value);
      copiedId = id;
      setTimeout(() => {
        if (copiedId === id) copiedId = "";
      }, 1200);
    } catch (e) {
      app.toast(`Could not copy ${label}: ${errorMessage(e)}`, "error");
    }
  }

  function toggleExpand(key: string) {
    if (window.getSelection()?.toString()) return;
    expandedMeta[key] = !expandedMeta[key];
  }

  async function save() {
    saving = true;
    try {
      if (isHelmRelease) {
        await Kube.UpgradeHelmRelease(app.context, sel.namespace, sel.name, draft);
        app.toast(`Release ${sel.name} upgraded`, "ok");
      } else {
        await Kube.Update(app.context, refOf(sel.type), sel.namespace, sel.name, draft);
        app.toast(`${sel.type.kind} ${sel.name} applied`, "ok");
      }
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
    const verb = isHelmRelease ? "Uninstall" : "Delete";
    if (!(await app.confirm(`${verb} ${sel.type.kind}?`, `${sel.name}${where} will be ${verb.toLowerCase()}ed.`, true))) return;
    try {
      await Kube.Delete(app.context, refOf(sel.type), sel.namespace, sel.name);
      app.toast(`${sel.type.kind} ${sel.name} ${verb.toLowerCase()}ed`, "ok");
      app.selection = null;
    } catch (e) {
      app.toast(errorMessage(e), "error");
    }
  }

  async function rollbackHelm(rev: number) {
    if (
      !(await app.confirm(
        `Rollback ${sel.name}?`,
        `Roll back release ${sel.name} in namespace ${sel.namespace} to revision ${rev}?`,
      ))
    )
      return;
    rollingBackRev = rev;
    try {
      await Kube.RollbackHelmRelease(app.context, sel.namespace, sel.name, rev);
      app.toast(`Rolled back ${sel.name} to revision ${rev}`, "ok");
      await load();
    } catch (e) {
      app.toast(errorMessage(e), "error");
    } finally {
      rollingBackRev = null;
    }
  }

  let replicas = $state<number | null>(null);

  function startScale() {
    const specReplicas = obj?.spec?.replicas;
    if (typeof specReplicas === "number") {
      replicas = specReplicas;
      return;
    }
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
      load();
    } catch (e) {
      app.toast(errorMessage(e), "error");
    }
  }

  function openDock(kind: "logs" | "shell", container = "") {
    app.openDock({ kind, title: sel.name, context: app.context, namespace: sel.namespace, pod: sel.name, container });
  }

  function onKey(e: KeyboardEvent) {
    if ((e.ctrlKey || e.metaKey) && e.key === "s" && editing) {
      e.preventDefault();
      save();
    }
  }

  function startResize(e: PointerEvent) {
    const z = cssZoom();
    const startX = e.clientX;
    const startW = width;
    const move = (ev: PointerEvent) => (width = Math.min(1100, Math.max(384, startW + (startX - ev.clientX) / z)));
    const up = () => {
      window.removeEventListener("pointermove", move);
      window.removeEventListener("pointerup", up);
    };
    window.addEventListener("pointermove", move);
    window.addEventListener("pointerup", up);
  }

  // Derived structured sections from `obj`.
  const metadata = $derived((obj?.metadata ?? {}) as Record<string, any>);
  const status = $derived((obj?.status ?? {}) as Record<string, any>);
  const labels = $derived(Object.entries((metadata.labels ?? {}) as Record<string, string>));
  const annotations = $derived(Object.entries((metadata.annotations ?? {}) as Record<string, string>));
  const owners = $derived((metadata.ownerReferences ?? []) as Array<Record<string, any>>);
  const primaryOwner = $derived(owners.find((o) => o.controller) ?? owners[0] ?? null);
  const finalizers = $derived((metadata.finalizers ?? []) as string[]);
  const conditions = $derived((status.conditions ?? []) as Array<Record<string, any>>);

  const specRows = $derived.by(() => buildSpecRows(sel.type.kind, obj));
  const isConfigResource = $derived(sel.type.kind === "ConfigMap" || sel.type.kind === "Secret");
  const isSecret = $derived(sel.type.kind === "Secret");
  const configEntries = $derived.by(() => (isConfigResource ? buildConfigDataEntries(obj, isSecret) : []));
  const serviceFqdn = $derived.by(() => {
    if (!obj || sel.type.kind !== "Service") return "";
    const name = metadata.name ?? sel.name;
    const ns = metadata.namespace ?? sel.namespace;
    return name && ns ? `${name}.${ns}` : "";
  });

  function toggleRevealKey(id: string) {
    revealedKeys[id] = !revealedKeys[id];
  }

  function toggleRevealVal(id: string) {
    revealedVals[id] = !revealedVals[id];
  }

  const selectorEntries = $derived.by(() => buildSelectorEntries(obj));
  const containersList = $derived.by(() => buildContainersList(sel.type.kind, obj, metrics));

  const POD_PAGE_SIZE = 5;
  const podRowsMap = new Map<string, Row>();
  const childRowsMap = new Map<string, Row>();
  let podVersion = $state(0);
  let podCols = $state<Column[]>([]);
  let podLoading = $state(false);
  let podError = $state("");
  let podPage = $state(0);

  const podsPlan = $derived.by(() => buildControlledPodsPlan(kk, obj, sel.namespace));
  const podsPlanKey = $derived(
    podsPlan
      ? `${podsPlan.mode}\0${podsPlan.uid}\0${podsPlan.namespace}\0${podsPlan.labelSelector}\0${podsPlan.fieldSelector}\0${podsPlan.emptyHint}`
      : "",
  );

  function resourceRefOrDefault(group: string, kind: string, fallback: ResourceRef): ResourceRef {
    const hit = app.resources.find((r) => r.group === group && r.kind === kind);
    return hit ? refOf(hit) : fallback;
  }

  $effect(() => {
    const ctx = app.context;
    const planKey = podsPlanKey;
    const plan = untrack(() => podsPlan);
    if (!ctx || !planKey || !plan || plan.emptyHint) {
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
      plan.mode === "deployment"
        ? untrack(() =>
            resourceRefOrDefault("apps", "ReplicaSet", {
              group: "apps",
              version: "v1",
              resource: "replicasets",
              kind: "ReplicaSet",
              namespaced: true,
            }),
          )
        : plan.mode === "cronjob"
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
      Kube.WatchFiltered(ctx, ref, plan.namespace, labelSelector, fieldSelector, id).catch((e) => {
        podError = errorMessage(e);
        podLoading = false;
      });
      cleanups.push(() => {
        off();
        Kube.StopWatch(id);
      });
    };

    startSubWatch(podRef, podRowsMap, true, plan.labelSelector, plan.fieldSelector);
    if (childRef) {
      startSubWatch(childRef, childRowsMap, false, "", "");
    }

    return () => {
      for (const stop of cleanups) stop();
    };
  });

  const controlledPods = $derived.by(() => {
    podVersion;
    return filterControlledPods(podsPlan, podRowsMap.values(), childRowsMap.values());
  });
  const podColIdx = $derived.by(() => podSummaryColumns(podCols));
  const podTotalPages = $derived(Math.max(1, Math.ceil(controlledPods.length / POD_PAGE_SIZE)));
  const safePodPage = $derived(Math.min(podPage, podTotalPages - 1));
  const pagedPods = $derived(controlledPods.slice(safePodPage * POD_PAGE_SIZE, (safePodPage + 1) * POD_PAGE_SIZE));
</script>

{#snippet copyBtn(value: string, id: string, label: string)}
  <button
    class="copy-btn"
    class:copied={copiedId === id}
    onclick={(e) => {
      e.stopPropagation();
      copyValue(value, id, label);
    }}
    title={copiedId === id ? `Copied ${label}` : `Copy ${label}`}
    aria-label={`Copy ${label}`}
  >
    {#if copiedId === id}
      <svg viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="2.2" stroke-linecap="round" stroke-linejoin="round" aria-hidden="true">
        <polyline points="20 6 9 17 4 12" />
      </svg>
    {:else}
      <svg viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="2" stroke-linecap="round" stroke-linejoin="round" aria-hidden="true">
        <rect x="9" y="9" width="13" height="13" rx="2" ry="2" />
        <path d="M5 15H4a2 2 0 0 1-2-2V4a2 2 0 0 1 2-2h9a2 2 0 0 1 2 2v1" />
      </svg>
    {/if}
  </button>
{/snippet}

{#snippet eyeBtn(revealed: boolean, onToggle: () => void, targetLabel: string)}
  <button
    class="eye-btn"
    class:revealed
    onclick={(e) => {
      e.stopPropagation();
      onToggle();
    }}
    title={revealed ? `Hide ${targetLabel}` : `Reveal ${targetLabel}`}
    aria-label={revealed ? `Hide ${targetLabel}` : `Reveal ${targetLabel}`}
  >
    {#if revealed}
      <svg viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="2" stroke-linecap="round" stroke-linejoin="round" aria-hidden="true">
        <path d="M17.94 17.94A10.07 10.07 0 0 1 12 20c-7 0-11-8-11-8a18.45 18.45 0 0 1 5.06-5.94" />
        <path d="M9.9 4.24A9.12 9.12 0 0 1 12 4c7 0 11 8 11 8a18.5 18.5 0 0 1-2.16 3.19" />
        <path d="M14.12 14.12a3 3 0 1 1-4.24-4.24" />
        <line x1="1" y1="1" x2="23" y2="23" />
      </svg>
    {:else}
      <svg viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="2" stroke-linecap="round" stroke-linejoin="round" aria-hidden="true">
        <path d="M1 12s4-8 11-8 11 8 11 8-4 8-11 8-11-8-11-8z" />
        <circle cx="12" cy="12" r="3" />
      </svg>
    {/if}
  </button>
{/snippet}

{#snippet truncText(text: string, expandKey: string, preWrap = false)}
  {@const long = text.length > 100}
  {@const open = !!expandedMeta[expandKey]}
  <span class="mono val-text" class:pre-val={preWrap}>
    {long && !open ? text.slice(0, 100) : text}{#if long}<button
        class="ellipsis-btn"
        onclick={(e) => {
          e.stopPropagation();
          toggleExpand(expandKey);
        }}
        title={open ? "Collapse value" : "Expand full value"}
      >{open ? " (less)" : "…"}</button>{/if}
  </span>
{/snippet}

{#snippet resourceBox(meter: ResourceMeter)}
  <div class="meter-box">
    {#if meter.showBar}
      <div class="meter-track" title={meter.hoverText}>
        <div class="meter-clip">
          <div class={`meter-fill tone-bg-${meter.tone}`} style={`width:${meter.usedPct}%`}></div>
        </div>
        {#if meter.requestMarkPct !== null}
          <span class="meter-req-mark" class:warn={meter.requestMarkWarn} style={`left:${meter.requestMarkPct}%`}></span>
        {/if}
      </div>
      <div class="meter-caption">
        <span class="meter-label">{meter.label}</span>
        <span class="meter-val">{meter.usedText} / {meter.limitText !== "none" ? meter.limitText : meter.scaleMaxText}</span>
      </div>
    {:else}
      <div class="meter-caption">
        <span class="meter-label">{meter.label}</span>
        <span class="meter-plain">{meter.usedText}</span>
      </div>
    {/if}
  </div>
{/snippet}

<aside class="drawer" style={`width:${width}px`}>
  <div class="resizer" onpointerdown={startResize} role="separator" aria-orientation="vertical"></div>
  <header>
    <div class="title">
      <span class="kind">{sel.type.kind}</span>
      <strong>{sel.name}</strong>
      {#if sel.namespace}<span class="ns">{sel.namespace}</span>{/if}
    </div>
    <button class="ghost close" onclick={() => (app.selection = null)} title="Close detail panel (Esc)">✕</button>
  </header>

  <div class="actions">
    <div class="tabs" role="tablist">
      <button
        role="tab"
        aria-selected={tab === "overview"}
        class:active={tab === "overview"}
        onclick={() => (tab = "overview")}
        title="Show object metadata, specification, and events"
      >
        Overview
      </button>
      <button
        role="tab"
        aria-selected={tab === "yaml"}
        class:active={tab === "yaml"}
        onclick={() => (tab = "yaml")}
        title={isHelmRelease ? "View or edit Helm release values" : "View or edit raw YAML manifest"}
      >
        {isHelmRelease ? "Values" : "YAML"}{dirty ? " *" : ""}
      </button>
      {#if isHelmRelease}
        <button
          role="tab"
          aria-selected={tab === "manifest"}
          class:active={tab === "manifest"}
          onclick={() => (tab = "manifest")}
          title="View rendered Kubernetes manifest for this release"
        >
          Manifest
        </button>
      {/if}
    </div>

    {#if canJumpOwner || owners.length > 0}
      <button
        disabled={!primaryOwner}
        onclick={() =>
          primaryOwner &&
          app.jumpToOwner(
            { apiVersion: primaryOwner.apiVersion, kind: primaryOwner.kind, name: primaryOwner.name },
            sel.namespace,
          )}
        title={primaryOwner ? `Jump to Owner (${primaryOwner.kind}/${primaryOwner.name})` : "Jump to Owner (no owner reference)"}
      >
        Jump to Owner
      </button>
    {/if}
    {#if canForward}
      <button onclick={() => (app.forwardTarget = { ...sel })} title={`Port forward (${sel.type.kind} ${sel.name})`}>
        Port forward
      </button>
    {/if}
    {#if canScale}
      {#if replicas === null}
        <button onclick={startScale} title={`Scale (${sel.type.kind} ${sel.name})`}>Scale</button>
      {:else}
        <span class="scale">
          <input type="number" min="0" bind:value={replicas} onkeydown={(e) => e.key === "Enter" && scale()} />
          <button class="primary" onclick={scale} title={`Set replicas to ${replicas ?? 0}`}>Set</button>
          <button class="ghost" onclick={() => (replicas = null)} title="Cancel scaling">✕</button>
        </span>
      {/if}
    {/if}
    {#if canRestart}
      <button onclick={restart} title={`Restart (${sel.type.kind} ${sel.name})`}>Restart</button>
    {/if}
    <span class="grow"></span>
    {#if editing}
      <button onclick={cancelEdit} disabled={saving} title="Cancel YAML edits">Cancel</button>
      <button class="primary" onclick={save} disabled={saving || !dirty} title="Apply YAML changes (Ctrl+S)">
        {saving ? "Applying…" : "Apply"}
      </button>
    {:else}
      {#if isPod}
        <button class="icon" onclick={() => openDock("logs")} title={`Logs (${sel.name})`} aria-label="Logs">
          <svg viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="2" stroke-linecap="round" stroke-linejoin="round" aria-hidden="true">
            <path d="M14 2H6a2 2 0 0 0-2 2v16a2 2 0 0 0 2 2h12a2 2 0 0 0 2-2V8z" />
            <polyline points="14 2 14 8 20 8" />
            <line x1="8" y1="13" x2="16" y2="13" />
            <line x1="8" y1="17" x2="16" y2="17" />
            <line x1="8" y1="9" x2="10" y2="9" />
          </svg>
        </button>
        <button class="icon" onclick={() => openDock("shell")} title={`Shell (${sel.name})`} aria-label="Shell">
          <svg viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="2" stroke-linecap="round" stroke-linejoin="round" aria-hidden="true">
            <polyline points="4 17 10 11 4 5" />
            <line x1="12" y1="19" x2="20" y2="19" />
          </svg>
        </button>
      {/if}
      <button class="icon" onclick={load} disabled={loading} title={`Refresh (${sel.type.kind} ${sel.name})`} aria-label="Refresh">
        <svg viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="2" stroke-linecap="round" stroke-linejoin="round" aria-hidden="true">
          <polyline points="23 4 23 10 17 10" />
          <polyline points="1 20 1 14 7 14" />
          <path d="M3.51 9a9 9 0 0 1 14.85-3.36L23 10M1 14l4.64 4.36A9 9 0 0 0 20.49 15" />
        </svg>
      </button>
      <button
        class="icon"
        onclick={startEdit}
        disabled={isHelmRelease ? !helmDetail : !yaml}
        title={isHelmRelease ? `Edit values (${sel.name})` : `Edit YAML (${sel.type.kind} ${sel.name})`}
        aria-label={isHelmRelease ? "Edit values" : "Edit YAML"}
      >
        <svg viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="2" stroke-linecap="round" stroke-linejoin="round" aria-hidden="true">
          <path d="M14 2H6a2 2 0 0 0-2 2v16a2 2 0 0 0 2 2h12a2 2 0 0 0 2-2v-7" />
          <path d="M18.5 2.5a2.121 2.121 0 0 1 3 3L12 15l-4 1 1-4 9.5-9.5z" />
          <line x1="8" y1="9" x2="11" y2="9" />
          <line x1="8" y1="13" x2="10" y2="13" />
        </svg>
      </button>
      <button
        class="icon danger"
        onclick={remove}
        title={`${isHelmRelease ? "Uninstall" : "Delete"} (${sel.type.kind} ${sel.name})`}
        aria-label={isHelmRelease ? "Uninstall" : "Delete"}
      >
        <svg viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="2" stroke-linecap="round" stroke-linejoin="round" aria-hidden="true">
          <polyline points="3 6 5 6 21 6" />
          <path d="M19 6v14a2 2 0 0 1-2 2H7a2 2 0 0 1-2-2V6m3 0V4a2 2 0 0 1 2-2h4a2 2 0 0 1 2 2v2" />
          <line x1="10" y1="11" x2="10" y2="17" />
          <line x1="14" y1="11" x2="14" y2="17" />
        </svg>
      </button>
    {/if}
  </div>

  {#if error}<div class="banner error">{error}</div>{/if}

  {#if tab === "overview"}
    <div class="overview">
      {#if isHelmRelease}
        {#if loading && !helmDetail}
          <p class="empty">Loading…</p>
        {:else if helmDetail}
          {@const fullChart = helmDetail.chartVersion ? `${helmDetail.chart}-${helmDetail.chartVersion}` : helmDetail.chart}
          <section class="section">
            <h4>Release info</h4>
            <dl class="kv-grid">
              <dt>Name</dt>
              <dd class="val-copy">
                {@render copyBtn(helmDetail.name, "helm:name", "release name")}
                <span class="mono val-text">{helmDetail.name}</span>
              </dd>
              <dt>Namespace</dt>
              <dd class="val-copy">
                {@render copyBtn(helmDetail.namespace, "helm:ns", "namespace")}
                <span class="mono val-text">{helmDetail.namespace}</span>
              </dd>
              <dt>Revision</dt>
              <dd class="mono val-text">{helmDetail.revision}</dd>
              <dt>Status</dt>
              <dd class={`val-text tone-${toneFor(helmDetail.status)}`}>{helmDetail.status}</dd>
              <dt>Chart</dt>
              <dd class="val-copy">
                {@render copyBtn(fullChart, "helm:chart", "chart")}
                <span class="mono val-text">{fullChart || "—"}</span>
              </dd>
              {#if helmDetail.appVersion}
                <dt>App version</dt>
                <dd class="val-copy">
                  {@render copyBtn(helmDetail.appVersion, "helm:appver", "app version")}
                  <span class="mono val-text">{helmDetail.appVersion}</span>
                </dd>
              {/if}
              {#if helmDetail.firstDeployed}
                <dt>First deployed</dt>
                <dd class="val-text">{age(helmDetail.firstDeployed, app.now)} ago <span class="muted">({helmDetail.firstDeployed})</span></dd>
              {/if}
              {#if helmDetail.lastDeployed}
                <dt>Last deployed</dt>
                <dd class="val-text">{age(helmDetail.lastDeployed, app.now)} ago <span class="muted">({helmDetail.lastDeployed})</span></dd>
              {/if}
              {#if helmDetail.description}
                <dt>Description</dt>
                <dd class="val-text">{helmDetail.description}</dd>
              {/if}
            </dl>
          </section>

          <section class="section">
            <div class="section-head">
              <h4>Revisions ({helmHistory.length})</h4>
              {#if revTotalPages > 1}
                <div class="pager">
                  <span class="muted pager-range">
                    {safeRevPage * REV_PAGE_SIZE + 1}–{Math.min((safeRevPage + 1) * REV_PAGE_SIZE, helmHistory.length)} of {helmHistory.length}
                  </span>
                  <button
                    class="mini"
                    disabled={safeRevPage === 0}
                    onclick={() => (revPage = Math.max(0, safeRevPage - 1))}
                    title="Previous revisions page"
                  >
                    ‹
                  </button>
                  <button
                    class="mini"
                    disabled={safeRevPage >= revTotalPages - 1}
                    onclick={() => (revPage = Math.min(revTotalPages - 1, safeRevPage + 1))}
                    title="Next revisions page"
                  >
                    ›
                  </button>
                </div>
              {/if}
            </div>
            {#if helmHistory.length === 0}
              <p class="muted empty-inline">No revision history.</p>
            {:else}
              <table class="ledger-table revisions-table">
                <thead>
                  <tr>
                    <th class="col-rev-num">Rev</th>
                    <th class="col-rev-status">Status</th>
                    <th>Chart</th>
                    <th class="col-rev-age">Updated</th>
                    <th>Description</th>
                    <th class="col-rev-act"></th>
                  </tr>
                </thead>
                <tbody>
                  {#each pagedRevisions as h (h.revision)}
                    <tr>
                      <td class="col-rev-num mono val-text">{h.revision}</td>
                      <td class={`col-rev-status val-text tone-${toneFor(h.status)}`}>{h.status}</td>
                      <td class="mono val-text">{h.chart || "—"}</td>
                      <td class="col-rev-age mono muted val-text">{h.updated ? age(h.updated, app.now) : "—"}</td>
                      <td class="val-text">{h.description || "—"}</td>
                      <td class="col-rev-act">
                        {#if h.revision !== helmDetail.revision}
                          <button
                            class="mini"
                            disabled={rollingBackRev !== null}
                            onclick={() => rollbackHelm(h.revision)}
                            title={`Roll back ${sel.name} to revision ${h.revision}`}
                          >
                            {rollingBackRev === h.revision ? "…" : "Rollback"}
                          </button>
                        {/if}
                      </td>
                    </tr>
                  {/each}
                </tbody>
              </table>
            {/if}
          </section>

          {#if helmDetail.notes}
            <section class="section">
              <h4>Notes</h4>
              <pre class="helm-notes mono val-text">{helmDetail.notes}</pre>
            </section>
          {/if}
        {/if}
      {:else if loading && !obj}
        <p class="empty">Loading…</p>
      {:else if obj}
        <section class="section">
          <h4>Metadata</h4>
          <dl class="kv-grid">
            <dt>Name</dt>
            <dd class="val-copy">
              {@render copyBtn(metadata.name ?? sel.name, "meta:name", "name")}
              <span class="mono val-text">{metadata.name ?? sel.name}</span>
            </dd>
            {#if metadata.namespace}
              <dt>Namespace</dt>
              <dd class="val-copy">
                {@render copyBtn(metadata.namespace, "meta:ns", "namespace")}
                <span class="mono val-text">{metadata.namespace}</span>
              </dd>
            {/if}
            <dt>API version</dt>
            <dd class="mono val-text">{obj.apiVersion ?? sel.type.version} ({obj.kind ?? sel.type.kind})</dd>
            {#if metadata.creationTimestamp}
              <dt>Created</dt>
              <dd class="val-text">{age(metadata.creationTimestamp, app.now)} ago <span class="muted">({metadata.creationTimestamp})</span></dd>
            {/if}
            {#if metadata.uid}
              <dt>UID</dt>
              <dd class="val-copy">
                {@render copyBtn(metadata.uid, "meta:uid", "UID")}
                <span class="mono val-text">{metadata.uid}</span>
              </dd>
            {/if}
            {#if metadata.resourceVersion}
              <dt>Resource version</dt>
              <dd class="val-copy">
                {@render copyBtn(metadata.resourceVersion, "meta:rv", "resource version")}
                <span class="mono val-text">{metadata.resourceVersion}{metadata.generation ? ` (gen ${metadata.generation})` : ""}</span>
              </dd>
            {/if}
            {#if owners.length > 0}
              <dt>Controlled by</dt>
              <dd class="chips">
                {#each owners as o (`${o.kind}/${o.name}`)}
                  <span class="chip mono val-text">{o.kind}/{o.name}</span>
                {/each}
              </dd>
            {/if}
            {#if finalizers.length > 0}
              <dt>Finalizers</dt>
              <dd class="chips">
                {#each finalizers as f (f)}
                  <span class="chip mono val-text">{f}</span>
                {/each}
              </dd>
            {/if}
            <dt>Labels</dt>
            <dd>
              {#if labels.length === 0}
                <span class="muted">None</span>
              {:else}
                <div class="chips">
                  {#each labels as [k, v] (k)}
                    {@const ek = `lbl:${k}`}
                    {@const long = v.length > 100}
                    {@const open = !!expandedMeta[ek]}
                    <span class="chip mono val-text" class:expanded={open}>
                      <span>{k}=</span><span
                        class="chip-v"
                        class:expandable={long}
                        role={long ? "button" : undefined}
                        onclick={long ? () => toggleExpand(ek) : undefined}
                        onkeydown={long ? (e) => e.key === "Enter" && toggleExpand(ek) : undefined}
                      >{long && !open ? v.slice(0, 100) : v}</span>{#if long}<button
                          class="ellipsis-btn"
                          onclick={() => toggleExpand(ek)}
                          title={open ? "Collapse label value" : "Expand full label value"}
                        >{open ? " (less)" : "…"}</button>{/if}
                    </span>
                  {/each}
                </div>
              {/if}
            </dd>
            <dt>Annotations</dt>
            <dd>
              {#if annotations.length === 0}
                <span class="muted">None</span>
              {:else}
                <div class="ann-list">
                  {#each annotations as [k, v] (k)}
                    {@const ek = `ann:${k}`}
                    {@const long = v.length > 100}
                    {@const open = !!expandedMeta[ek]}
                    <div class="ann-row">
                      <span class="mono ann-k val-text">{k}</span>
                      <span
                        class="mono ann-v val-text"
                        class:expandable={long}
                        role={long ? "button" : undefined}
                        onclick={long ? () => toggleExpand(ek) : undefined}
                        onkeydown={long ? (e) => e.key === "Enter" && toggleExpand(ek) : undefined}
                      >{long && !open ? v.slice(0, 100) : v}{#if long}<button
                            class="ellipsis-btn"
                            onclick={(e) => {
                              e.stopPropagation();
                              toggleExpand(ek);
                            }}
                            title={open ? "Collapse annotation value" : "Expand full annotation value"}
                          >{open ? " (less)" : "…"}</button>{/if}</span>
                    </div>
                  {/each}
                </div>
              {/if}
            </dd>
          </dl>
        </section>

        {#if specRows.length > 0 || selectorEntries.length > 0 || serviceFqdn}
          <section class="section">
            <h4>Specification &amp; status</h4>
            <dl class="kv-grid">
              {#each specRows as r (r.label)}
                <dt>{r.label}</dt>
                {#if r.copy}
                  <dd class="val-copy">
                    {@render copyBtn(r.value, `spec:${r.label}`, r.label)}
                    <span class={`val-text ${r.mono ? "mono " : ""}${r.tone ? `tone-${r.tone}` : ""}`}>{r.value}</span>
                  </dd>
                {:else}
                  <dd class={`val-text ${r.mono ? "mono " : ""}${r.tone ? `tone-${r.tone}` : ""}`}>{r.value}</dd>
                {/if}
              {/each}
              {#if selectorEntries.length > 0}
                <dt>Selector</dt>
                <dd class="chips">
                  {#each selectorEntries as [k, v] (k)}
                    <span class="chip mono val-text"><span class="chip-k">{k}=</span>{v}</span>
                  {/each}
                </dd>
              {/if}
              {#if serviceFqdn}
                <dt>Cluster FQDN</dt>
                <dd class="val-copy">
                  {@render copyBtn(serviceFqdn, "spec:Cluster FQDN", "Cluster FQDN")}
                  <span class="mono val-text">{serviceFqdn}</span>
                </dd>
              {/if}
            </dl>
          </section>
        {/if}

        {#if isConfigResource}
          <section class="section">
            <h4>Data ({configEntries.length})</h4>
            {#if configEntries.length === 0}
              <p class="muted empty-inline">No data entries.</p>
            {:else}
              <table class="data-table">
                <thead>
                  <tr>
                    <th class="data-key-col">Key</th>
                    <th>Value</th>
                  </tr>
                </thead>
                <tbody>
                  {#each configEntries as entry (entry.id)}
                    {@const keyOpen = !isSecret || !!revealedKeys[entry.id]}
                    {@const valOpen = !isSecret || !!revealedVals[entry.id]}
                    <tr>
                      <td class="data-key-col">
                        <div class="val-copy">
                          {#if isSecret}
                            {@render eyeBtn(
                              !!revealedKeys[entry.id],
                              () => toggleRevealKey(entry.id),
                              "secret key",
                            )}
                          {/if}
                          <span class={`mono ${keyOpen ? "val-text" : "muted"}`}>
                            {keyOpen ? entry.key : entry.maskedKey}
                          </span>
                          {#if entry.source === "binaryData"}
                            <span class="pill">binary</span>
                          {/if}
                        </div>
                      </td>
                      <td class="data-val-col">
                        <div class="val-copy">
                          {#if isSecret}
                            {@render eyeBtn(
                              !!revealedVals[entry.id],
                              () => toggleRevealVal(entry.id),
                              `value for ${keyOpen ? entry.key : entry.maskedKey}`,
                            )}
                          {/if}
                          {@render copyBtn(
                            entry.value,
                            `data:${entry.id}`,
                            isSecret && !keyOpen ? "secret value" : `value for ${entry.key}`,
                          )}
                          {#if !valOpen}
                            <span class="mono muted">{SECRET_VALUE_MASK}</span>
                          {:else}
                            {#if entry.binary && isSecret}
                              <span class="pill">binary</span>
                            {/if}
                            {#if entry.value === ""}
                              <span class="muted">(empty)</span>
                            {:else}
                              {@render truncText(entry.value, `data:${entry.id}`, true)}
                            {/if}
                          {/if}
                        </div>
                      </td>
                    </tr>
                  {/each}
                </tbody>
              </table>
            {/if}
          </section>
        {/if}

        {#if containersList.length > 0}
          <section class="section">
            <h4>Containers ({containersList.length})</h4>
            <div class="container-list">
              {#each containersList as c (`${c.role}:${c.name}`)}
                <div class="container-row">
                  <div class="container-head">
                    <strong class="mono val-text">{c.name}</strong>
                    {#if c.role !== "main"}<span class="pill">{c.role}</span>{/if}
                    {#if c.state}<span class={`val-text tone-${c.tone}`}>{c.state}</span>{/if}
                    {#if c.restarts != null}<span class="muted">restarts: <span class="val-text">{c.restarts}</span></span>{/if}
                    <span class="grow"></span>
                    {#if isPod}
                      <button class="mini" onclick={() => openDock("logs", c.name)} title={`View logs for container ${c.name}`}>
                        Logs
                      </button>
                      <button class="mini" onclick={() => openDock("shell", c.name)} title={`Open shell in container ${c.name}`}>
                        Shell
                      </button>
                    {/if}
                  </div>
                  <div class="val-copy muted">
                    {#if c.image}
                      {@render copyBtn(c.image, `img:${c.role}:${c.name}`, "image")}
                    {/if}
                    <span class="mono val-text img-text">{c.image}{c.ports ? ` (${c.ports})` : ""}</span>
                  </div>
                  <div class="meters">
                    {@render resourceBox(c.cpu)}
                    {@render resourceBox(c.memory)}
                  </div>
                </div>
              {/each}
            </div>
          </section>
        {/if}

        {#if conditions.length > 0}
          <section class="section">
            <h4>Conditions</h4>
            <table class="ledger-table">
              <thead>
                <tr>
                  <th>Type</th>
                  <th>Status</th>
                  <th>Reason</th>
                  <th>Message</th>
                </tr>
              </thead>
              <tbody>
                {#each conditions as cond, idx (cond.type ?? idx)}
                  <tr>
                    <td class="mono val-text">{cond.type ?? "—"}</td>
                    <td class={`val-text ${cond.status === "True" ? "tone-ok" : cond.status === "False" ? "tone-muted" : "tone-warn"}`}>
                      {cond.status ?? "—"}
                    </td>
                    <td class="mono val-text">{cond.reason ?? "—"}</td>
                    <td class="val-text">{cond.message ?? "—"}</td>
                  </tr>
                {/each}
              </tbody>
            </table>
          </section>
        {/if}

        {#if podsPlan}
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
            {#if podsPlan.emptyHint}
              <p class="muted empty-inline">{podsPlan.emptyHint}</p>
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
                      onclick={() => {
                        if (!window.getSelection()?.toString()) {
                          app.jumpToResource({ kind: "Pod", name: p.name }, p.namespace);
                        }
                      }}
                    >
                      <td class="col-pod-name">
                        <button
                          class="pod-link mono val-text"
                          onclick={(e) => {
                            e.stopPropagation();
                            if (!window.getSelection()?.toString()) {
                              app.jumpToResource({ kind: "Pod", name: p.name }, p.namespace);
                            }
                          }}
                          title={`Open Pod ${p.namespace ? `${p.namespace}/` : ""}${p.name}`}
                        >
                          {p.name}
                        </button>
                        {#if podsPlan.mode === "node" && p.namespace}
                          <div class="mono muted pod-ns val-text">{p.namespace}</div>
                        {/if}
                      </td>
                      <td class="col-pod-ready mono val-text" class:tone-warn={podUnready}>{podReady}</td>
                      <td class={`col-pod-status val-text tone-${toneFor(podStatus)}`}>{podStatus}</td>
                      <td class="col-pod-age mono muted val-text">{p.created ? age(p.created, app.now) : "—"}</td>
                      <td class="col-pod-restarts mono val-text">{podRestarts}</td>
                    </tr>
                  {/each}
                </tbody>
              </table>
            {/if}
          </section>
        {/if}

        <section class="section">
          <h4>Events ({events.length})</h4>
          {#if events.length === 0}
            <p class="muted empty-inline">No events recorded for this {sel.type.kind}.</p>
          {:else}
            <table class="ledger-table events-table">
              <thead>
                <tr>
                  <th class="col-reason">Reason</th>
                  <th>Message</th>
                  <th class="col-age">Age</th>
                </tr>
              </thead>
              <tbody>
                {#each events as ev, i (i)}
                  <tr>
                    <td class="col-reason">
                      <span class={`val-text ${ev.type === "Warning" ? "tone-warn reason-name" : "reason-name"}`}>{ev.reason || ev.type}</span>
                      {#if ev.count > 1}<span class="event-count">×{ev.count}</span>{/if}
                    </td>
                    <td>
                      <div class="event-msg val-text">{ev.message}</div>
                      {#if ev.source || ev.subObject}
                        <div class="mono muted event-meta val-text">
                          {ev.source}{ev.source && ev.subObject ? " / " : ""}{ev.subObject}
                        </div>
                      {/if}
                    </td>
                    <td class="col-age mono muted val-text">{ev.lastSeen ? age(ev.lastSeen, app.now) : "—"}</td>
                  </tr>
                {/each}
              </tbody>
            </table>
          {/if}
        </section>
      {/if}
    </div>
  {:else if tab === "manifest" && isHelmRelease}
    <div class="yaml-bar">
      <span class="muted yaml-hint">Rendered manifest (revision {helmDetail?.revision ?? "—"})</span>
      <span class="grow"></span>
      {@render copyBtn(helmDetail?.manifest ?? "", "helm:manifest", "rendered manifest")}
    </div>
    <YamlEditor readonly value={helmDetail?.manifest ?? ""} placeholder={loading ? "Loading…" : "No manifest."} />
  {:else}
    <div class="yaml-bar">
      {#if isHelmRelease}
        <label class="neat-toggle">
          <input type="checkbox" bind:checked={helmUserOnly} disabled={editing} />
          User values
        </label>
      {:else}
        <label class="neat-toggle">
          <input type="checkbox" bind:checked={neat} disabled={editing} />
          Neat
        </label>
      {/if}
      <span class="grow"></span>
      {@render copyBtn(
        editing ? draft : activeYaml,
        "yaml:doc",
        isHelmRelease ? (helmUserOnly ? "user values" : "merged values") : neat ? "neated YAML" : "raw YAML",
      )}
    </div>
    {#if editing}
      <YamlEditor bind:value={draft} onkeydown={onKey} placeholder={loading ? "Loading…" : ""} />
    {:else}
      <YamlEditor
        readonly
        value={activeYaml}
        placeholder={loading ? "Loading…" : isHelmRelease ? "# No user-supplied values" : ""}
      />
    {/if}
  {/if}
</aside>

<style>
  .drawer {
    position: relative;
    display: flex;
    flex-direction: column;
    border-left: 1px solid var(--border);
    background: var(--panel);
    min-width: 0;
    min-height: 0;
    overflow: hidden;
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
    user-select: text;
    -webkit-user-select: text;
  }
  .val-text {
    user-select: text;
    -webkit-user-select: text;
  }
  .val-copy {
    display: flex;
    align-items: baseline;
    gap: var(--space-1);
    min-width: 0;
  }
  .copy-btn,
  .eye-btn {
    display: inline-flex;
    align-items: center;
    justify-content: center;
    flex-shrink: 0;
    width: 18px;
    height: 18px;
    min-height: 0;
    padding: 0;
    border: 0;
    border-radius: var(--radius);
    background: transparent;
    color: var(--muted);
    align-self: center;
  }
  .copy-btn:hover:not(:disabled),
  .eye-btn:hover:not(:disabled) {
    color: var(--text-strong);
    background: var(--hover);
  }
  .copy-btn:focus-visible,
  .eye-btn:focus-visible,
  .ellipsis-btn:focus-visible {
    outline: 1px solid var(--accent);
    outline-offset: 1px;
  }
  .copy-btn.copied {
    color: var(--ok);
  }
  .eye-btn.revealed {
    color: var(--accent);
  }
  .copy-btn svg,
  .eye-btn svg {
    width: 12px;
    height: 12px;
  }
  .kind {
    font-size: 12px;
    font-weight: 600;
    color: var(--accent);
  }
  .ns {
    font-size: 12px;
    color: var(--muted);
  }
  .actions {
    display: flex;
    flex-wrap: wrap;
    align-items: center;
    gap: var(--space-2);
    padding: 0 var(--space-3) var(--space-3);
    border-bottom: 1px solid var(--border);
  }
  .actions button.icon {
    width: var(--control-h-sm);
    height: var(--control-h-sm);
  }
  .actions button.icon svg {
    width: 14px;
    height: 14px;
  }
  .tabs {
    display: inline-flex;
    border: 1px solid var(--border);
    border-radius: var(--radius);
    overflow: hidden;
    margin-right: var(--space-1);
  }
  .tabs button {
    border: 0;
    border-radius: 0;
    background: var(--bg);
    padding: var(--space-1) var(--space-2);
  }
  .tabs button + button {
    border-left: 1px solid var(--border);
  }
  .tabs button.active {
    background: var(--accent);
    color: #fff;
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
  .overview {
    flex: 1;
    overflow-y: auto;
    overflow-x: hidden;
    padding: 0 var(--space-3) var(--space-4);
    display: flex;
    flex-direction: column;
    min-width: 0;
    min-height: 0;
  }
  .section {
    padding: var(--space-3) 0;
    border-bottom: 1px solid var(--border);
    min-width: 0;
  }
  .section:last-child {
    border-bottom: 0;
  }
  h4 {
    margin: 0 0 var(--space-2);
    font-size: 13px;
    color: var(--text-strong);
    font-weight: 600;
  }
  .kv-grid {
    display: grid;
    grid-template-columns: 136px 1fr;
    row-gap: var(--space-2);
    column-gap: var(--space-3);
    margin: 0;
    font-size: 12.5px;
    align-items: baseline;
  }
  .kv-grid dt {
    color: var(--muted);
    font-size: 12px;
  }
  .kv-grid dd {
    margin: 0;
    color: var(--text-strong);
    word-break: break-word;
    min-width: 0;
  }
  .mono {
    font-family: var(--mono);
    font-size: 12px;
  }
  .muted {
    color: var(--muted);
  }
  .chips {
    display: flex;
    flex-wrap: wrap;
    gap: var(--space-1);
  }
  .chip {
    display: inline-block;
    padding: 0 var(--space-2);
    line-height: 20px;
    background: var(--bg);
    border: 1px solid var(--border);
    border-radius: var(--radius);
    color: var(--text);
    max-width: 100%;
    overflow: hidden;
    text-overflow: ellipsis;
    white-space: nowrap;
  }
  .chip.expanded {
    white-space: normal;
    word-break: break-all;
  }
  .chip-k {
    color: var(--muted);
  }
  .expandable {
    cursor: pointer;
  }
  .ellipsis-btn {
    display: inline;
    min-height: 0;
    padding: 0 var(--space-1);
    margin-left: 2px;
    border: 0;
    background: transparent;
    color: var(--accent);
    font: inherit;
    font-size: 11px;
    line-height: inherit;
  }
  .ellipsis-btn:hover:not(:disabled) {
    background: var(--hover);
  }
  .ann-list {
    display: flex;
    flex-direction: column;
    gap: var(--space-1);
  }
  .ann-row {
    display: flex;
    flex-direction: column;
    padding: var(--space-1) 0;
    border-bottom: 1px solid var(--border-soft);
  }
  .ann-row:last-child {
    border-bottom: 0;
  }
  .ann-k {
    color: var(--accent);
    word-break: break-all;
  }
  .ann-v {
    color: var(--text);
    word-break: break-all;
  }
  .container-list {
    display: flex;
    flex-direction: column;
    min-width: 0;
  }
  .container-row {
    display: flex;
    flex-direction: column;
    gap: var(--space-1);
    padding: var(--space-2) 0;
    border-bottom: 1px solid var(--border-soft);
    min-width: 0;
    overflow: hidden;
  }
  .img-text {
    flex: 1;
    min-width: 0;
    overflow: hidden;
    text-overflow: ellipsis;
    white-space: nowrap;
  }
  .container-row:first-child {
    padding-top: 0;
  }
  .container-row:last-child {
    border-bottom: 0;
    padding-bottom: 0;
  }
  .container-head {
    display: flex;
    align-items: center;
    gap: var(--space-2);
  }
  .meters {
    display: grid;
    grid-template-columns: minmax(0, 1fr) minmax(0, 1fr);
    gap: var(--space-2);
    margin-top: var(--space-1);
  }
  .meter-box {
    display: flex;
    flex-direction: column;
    align-items: center;
    justify-content: center;
    gap: var(--space-1);
    padding: var(--space-2);
    background: transparent;
    border: 1px solid var(--border);
    border-radius: var(--radius);
    text-align: center;
    min-width: 0;
  }
  .meter-box .meter-track {
    width: 100%;
  }
  .meter-caption {
    display: inline-flex;
    align-items: baseline;
    justify-content: center;
    gap: var(--space-1);
    flex-wrap: wrap;
  }
  .meter-label {
    font-size: 11px;
    color: var(--muted);
  }
  .meter-val,
  .meter-plain {
    font-family: var(--sans);
    font-size: 12px;
    text-align: center;
    color: var(--text-strong);
    font-variant-numeric: tabular-nums;
    user-select: text;
    -webkit-user-select: text;
  }
  .yaml-bar {
    display: flex;
    align-items: center;
    gap: var(--space-2);
    padding: var(--space-1) var(--space-3);
    border-bottom: 1px solid var(--border);
    background: var(--code-bg);
  }
  .neat-toggle {
    display: inline-flex;
    align-items: center;
    gap: var(--space-1);
    font-size: 12px;
    color: var(--muted);
    cursor: pointer;
  }
  .ledger-table {
    width: 100%;
    border-collapse: collapse;
    font-size: 12px;
  }
  .ledger-table th,
  .ledger-table td {
    text-align: left;
    padding: var(--space-2) var(--space-2) var(--space-2) 0;
    border-bottom: 1px solid var(--border-soft);
    vertical-align: top;
  }
  .ledger-table th {
    padding-top: 0;
    color: var(--muted);
    font-weight: 500;
  }
  .ledger-table tbody tr:last-child td {
    border-bottom: 0;
  }
  .data-table {
    width: 100%;
    border-collapse: collapse;
    font-size: 12px;
    table-layout: fixed;
  }
  .data-table th,
  .data-table td {
    text-align: left;
    padding: var(--space-1) var(--space-3) var(--space-1) 0;
    vertical-align: top;
  }
  .data-table th {
    padding-top: 0;
    color: var(--muted);
    font-weight: 500;
  }
  .data-table td.data-key-col {
    width: 36%;
    word-break: break-word;
  }
  .data-table td.data-val-col {
    padding-right: 0;
    word-break: break-all;
  }
  .data-table .copy-btn,
  .data-table .eye-btn {
    align-self: flex-start;
  }
  .data-table .val-copy > span:last-child {
    flex: 1;
    min-width: 0;
  }
  .pre-val {
    white-space: pre-wrap;
    word-break: break-all;
  }
  .events-table .col-reason {
    width: 128px;
    white-space: nowrap;
  }
  .events-table .col-age {
    width: 56px;
    text-align: right;
    padding-right: 0;
    white-space: nowrap;
    font-variant-numeric: tabular-nums;
  }
  .reason-name {
    font-family: var(--mono);
    font-weight: 600;
    color: var(--text-strong);
  }
  .reason-name.tone-warn {
    color: var(--warn);
  }
  .event-count {
    margin-left: var(--space-1);
    font-family: var(--mono);
    font-size: 11px;
    color: var(--muted);
    font-variant-numeric: tabular-nums;
  }
  .event-msg {
    color: var(--text);
    line-height: 16px;
    word-break: break-word;
  }
  .event-meta {
    margin-top: var(--space-1);
    font-size: 11px;
  }
  .section-head {
    display: flex;
    align-items: center;
    justify-content: space-between;
    gap: var(--space-2);
    margin-bottom: var(--space-2);
  }
  .section-head h4 {
    margin: 0;
  }
  .pager {
    display: inline-flex;
    align-items: center;
    gap: var(--space-1);
    font-size: 11px;
    font-variant-numeric: tabular-nums;
  }
  .pager-range {
    margin-right: var(--space-1);
  }
  .pods-table {
    table-layout: fixed;
  }
  .pods-table .col-pod-name {
    min-width: 0;
  }
  .pods-table .col-pod-ready {
    width: 50px;
    white-space: nowrap;
    font-variant-numeric: tabular-nums;
  }
  .pods-table .col-pod-status {
    width: 92px;
    white-space: nowrap;
    overflow: hidden;
    text-overflow: ellipsis;
  }
  .pods-table .col-pod-age {
    width: 44px;
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
  .pod-link {
    all: unset;
    display: block;
    width: 100%;
    overflow: hidden;
    text-overflow: ellipsis;
    white-space: nowrap;
    color: var(--accent);
    cursor: pointer;
  }
  .pod-link:hover {
    text-decoration: underline;
  }
  .pod-link:focus-visible {
    outline: 1px solid var(--accent);
    outline-offset: 1px;
    border-radius: 2px;
  }
  .pod-ns {
    font-size: 11px;
    overflow: hidden;
    text-overflow: ellipsis;
    white-space: nowrap;
  }
  .yaml-hint {
    font-size: 12px;
  }
  .revisions-table .col-rev-num {
    width: 44px;
    white-space: nowrap;
    font-variant-numeric: tabular-nums;
  }
  .revisions-table .col-rev-status {
    width: 88px;
    white-space: nowrap;
  }
  .revisions-table .col-rev-age {
    width: 60px;
    white-space: nowrap;
    font-variant-numeric: tabular-nums;
  }
  .revisions-table .col-rev-act {
    width: 72px;
    text-align: right;
    padding-right: 0;
    white-space: nowrap;
  }
  .helm-notes {
    margin: 0;
    padding: var(--space-2);
    background: var(--code-bg);
    border: 1px solid var(--border);
    border-radius: var(--radius);
    white-space: pre-wrap;
    word-break: break-word;
    line-height: 16px;
  }
  .inline-banner {
    margin: 0;
  }
  .empty,
  .empty-inline {
    margin: 0;
  }
  .banner {
    margin: var(--space-2) var(--space-3);
  }
</style>
