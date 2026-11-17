<script lang="ts">
  import { Kube, errorMessage, type ContainerMetrics, type EventInfo } from "../lib/api";
  import { app, cssZoom, refOf, type Selection } from "../lib/store.svelte";
  import { age, buildResourceMeter, toneFor, type ResourceMeter } from "../lib/format";
  import { kindKey, restartableKinds, scalableKinds } from "../lib/resources";

  let { sel }: { sel: Selection } = $props();

  let tab = $state<"overview" | "yaml">("overview");
  let obj = $state<Record<string, any> | null>(null);
  let events = $state<EventInfo[]>([]);
  let metrics = $state<Record<string, ContainerMetrics>>({});
  let yaml = $state("");
  let draft = $state("");
  let editing = $state(false);
  let loading = $state(false);
  let saving = $state(false);
  let error = $state("");
  let width = $state(600);

  const kk = $derived(kindKey(sel.type));
  const isPod = $derived(kk === "/Pod");
  const canJumpOwner = $derived(kk === "/Pod" || kk === "apps/ReplicaSet" || kk === "batch/Job");
  const canForward = $derived(kk === "/Pod" || kk === "/Service");
  const canScale = $derived(scalableKinds.has(kk));
  const canRestart = $derived(restartableKinds.has(kk));
  const dirty = $derived(editing && draft !== yaml);

  async function load() {
    loading = true;
    error = "";
    try {
      const detail = await Kube.GetDetail(app.context, refOf(sel.type), sel.namespace, sel.name);
      obj = (detail.object as Record<string, any>) ?? null;
      events = detail.events ?? [];
      metrics = (detail.metrics as Record<string, ContainerMetrics>) ?? {};
      yaml = detail.yaml ?? "";
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
    tab = "yaml";
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
  const spec = $derived((obj?.spec ?? {}) as Record<string, any>);
  const status = $derived((obj?.status ?? {}) as Record<string, any>);
  const labels = $derived(Object.entries((metadata.labels ?? {}) as Record<string, string>));
  const annotations = $derived(Object.entries((metadata.annotations ?? {}) as Record<string, string>));
  const owners = $derived((metadata.ownerReferences ?? []) as Array<Record<string, any>>);
  const primaryOwner = $derived(owners.find((o) => o.controller) ?? owners[0] ?? null);
  const finalizers = $derived((metadata.finalizers ?? []) as string[]);
  const conditions = $derived((status.conditions ?? []) as Array<Record<string, any>>);

  interface KV {
    label: string;
    value: string;
    tone?: string;
    mono?: boolean;
  }

  const specRows = $derived.by<KV[]>(() => {
    if (!obj) return [];
    const out: KV[] = [];
    const kind = sel.type.kind;

    const add = (label: string, val: unknown, opts?: { tone?: string; mono?: boolean }) => {
      if (val === undefined || val === null || val === "") return;
      out.push({ label, value: String(val), tone: opts?.tone, mono: opts?.mono });
    };

    if (kind === "Deployment" || kind === "StatefulSet" || kind === "ReplicaSet") {
      const desired = spec.replicas ?? 1;
      const ready = status.readyReplicas ?? 0;
      const updated = status.updatedReplicas ?? 0;
      const available = status.availableReplicas ?? 0;
      add("Replicas", `${ready}/${desired} ready (${updated} updated, ${available} available)`, {
        tone: ready === desired ? "ok" : "warn",
      });
      add("Strategy", spec.strategy?.type ?? spec.updateStrategy?.type);
      if (spec.strategy?.rollingUpdate) {
        const ru = spec.strategy.rollingUpdate;
        add("Rolling update", `maxSurge=${ru.maxSurge ?? "—"}, maxUnavailable=${ru.maxUnavailable ?? "—"}`, { mono: true });
      }
      add("Service name", spec.serviceName, { mono: true });
      add("Pod management", spec.podManagementPolicy);
      add("Min ready seconds", spec.minReadySeconds);
      add("Revision history limit", spec.revisionHistoryLimit);
    } else if (kind === "DaemonSet") {
      const desired = status.desiredNumberScheduled ?? 0;
      const current = status.currentNumberScheduled ?? 0;
      const ready = status.numberReady ?? 0;
      const updated = status.updatedNumberScheduled ?? 0;
      const available = status.numberAvailable ?? 0;
      add("Pods scheduled", `${ready}/${desired} ready (${current} current, ${updated} updated, ${available} available)`, {
        tone: ready === desired && desired > 0 ? "ok" : "warn",
      });
      add("Update strategy", spec.updateStrategy?.type);
    } else if (kind === "Pod") {
      add("Phase", status.phase, { tone: toneFor(status.phase ?? "") });
      add("Reason", status.reason);
      add("QoS class", status.qosClass);
      add("Node", spec.nodeName, { mono: true });
      add("Pod IP", status.podIP, { mono: true });
      add("Host IP", status.hostIP, { mono: true });
      add("Service account", spec.serviceAccountName, { mono: true });
      add("Restart policy", spec.restartPolicy);
      add("Priority class", spec.priorityClassName);
    } else if (kind === "Service") {
      add("Type", spec.type);
      add("Cluster IP", spec.clusterIP, { mono: true });
      if (Array.isArray(spec.externalIPs) && spec.externalIPs.length) {
        add("External IPs", spec.externalIPs.join(", "), { mono: true });
      }
      const lbIng = status.loadBalancer?.ingress;
      if (Array.isArray(lbIng) && lbIng.length) {
        add(
          "LoadBalancer ingress",
          lbIng.map((i: any) => i.ip || i.hostname).filter(Boolean).join(", "),
          { mono: true },
        );
      }
      add("Session affinity", spec.sessionAffinity);
      add("External traffic policy", spec.externalTrafficPolicy);
      if (Array.isArray(spec.ports) && spec.ports.length) {
        add(
          "Ports",
          spec.ports
            .map((p: any) => `${p.name ? p.name + ":" : ""}${p.port}${p.nodePort ? ":" + p.nodePort : ""} -> ${p.targetPort ?? p.port}/${p.protocol ?? "TCP"}`)
            .join(", "),
          { mono: true },
        );
      }
    } else if (kind === "Ingress") {
      add("Ingress class", spec.ingressClassName, { mono: true });
      if (Array.isArray(spec.rules) && spec.rules.length) {
        const rules = spec.rules.flatMap((r: any) =>
          (r.http?.paths ?? []).map(
            (p: any) =>
              `${r.host || "*"}${p.path || "/"} -> ${p.backend?.service?.name ?? "?"}:${p.backend?.service?.port?.number ?? p.backend?.service?.port?.name ?? "?"}`,
          ),
        );
        add("Rules", rules.join("; "), { mono: true });
      }
    } else if (kind === "Job" || kind === "CronJob") {
      add("Schedule", spec.schedule, { mono: true });
      if (spec.suspend !== undefined) add("Suspend", String(spec.suspend));
      add("Concurrency policy", spec.concurrencyPolicy);
      add("Completions", spec.completions);
      add("Parallelism", spec.parallelism);
      add("Backoff limit", spec.backoffLimit);
      if (status.active !== undefined || status.succeeded !== undefined || status.failed !== undefined) {
        add("Job status", `active=${status.active ?? 0}, succeeded=${status.succeeded ?? 0}, failed=${status.failed ?? 0}`);
      }
      add("Last schedule", status.lastScheduleTime);
    } else if (kind === "PersistentVolumeClaim" || kind === "PersistentVolume") {
      add("Phase", status.phase, { tone: toneFor(status.phase ?? "") });
      add("Storage class", spec.storageClassName, { mono: true });
      if (Array.isArray(spec.accessModes)) add("Access modes", spec.accessModes.join(", "));
      add("Capacity", status.capacity?.storage ?? spec.capacity?.storage ?? spec.resources?.requests?.storage, { mono: true });
      add("Volume name", spec.volumeName, { mono: true });
      add("Volume mode", spec.volumeMode);
      add("Reclaim policy", spec.persistentVolumeReclaimPolicy);
    } else if (kind === "Node") {
      const info = status.nodeInfo ?? {};
      add("Kubelet", info.kubeletVersion, { mono: true });
      add("OS / Arch", info.osImage ? `${info.osImage} (${info.architecture ?? ""})` : "");
      add("Kernel", info.kernelVersion, { mono: true });
      add("Runtime", info.containerRuntimeVersion, { mono: true });
      if (spec.unschedulable) add("Schedulable", "No (cordoned)", { tone: "warn" });
      if (Array.isArray(status.addresses)) {
        add(
          "Addresses",
          status.addresses.map((a: any) => `${a.type}=${a.address}`).join(", "),
          { mono: true },
        );
      }
      if (status.allocatable) {
        add(
          "Allocatable",
          `cpu=${status.allocatable.cpu ?? "?"}, memory=${status.allocatable.memory ?? "?"}, pods=${status.allocatable.pods ?? "?"}`,
          { mono: true },
        );
      }
    } else if (kind === "ConfigMap" || kind === "Secret") {
      add("Type", obj.type, { mono: true });
      if (obj.immutable !== undefined) add("Immutable", String(obj.immutable));
      const dataKeys = [...Object.keys(obj.data ?? {}), ...Object.keys(obj.binaryData ?? {})];
      add("Data keys", dataKeys.length ? dataKeys.join(", ") : "None", { mono: true });
    } else {
      for (const [sectionName, sectionObj] of [
        ["spec", spec],
        ["status", status],
      ] as const) {
        for (const [k, v] of Object.entries(sectionObj)) {
          if (k === "conditions" || v === null || v === undefined) continue;
          if (typeof v === "string" || typeof v === "number" || typeof v === "boolean") {
            add(`${sectionName}.${k}`, v);
          } else if (Array.isArray(v)) {
            add(`${sectionName}.${k}`, `${v.length} item(s)`);
          }
        }
      }
    }

    return out;
  });

  const selectorEntries = $derived.by<[string, string][]>(() => {
    const selMap = spec.selector?.matchLabels ?? (typeof spec.selector === "object" && !Array.isArray(spec.selector) ? spec.selector : null);
    if (!selMap) return [];
    return Object.entries(selMap as Record<string, string>).filter(([, v]) => typeof v === "string");
  });

  interface ContainerSummary {
    name: string;
    role: string;
    image: string;
    ports: string;
    state: string;
    tone: string;
    restarts?: number;
    cpu: ResourceMeter;
    memory: ResourceMeter;
  }

  const containersList = $derived.by<ContainerSummary[]>(() => {
    const podSpec = spec.template?.spec ?? (sel.type.kind === "Pod" ? spec : null);
    if (!podSpec) return [];
    const statuses = new Map<string, any>();
    for (const st of [...(status.initContainerStatuses ?? []), ...(status.containerStatuses ?? [])]) {
      if (st?.name) statuses.set(st.name, st);
    }

    const formatContainer = (c: any, role: string): ContainerSummary => {
      const st = statuses.get(c.name);
      let state = "";
      let tone = "muted";
      if (st) {
        if (st.state?.running) {
          state = st.ready ? "Running (Ready)" : "Running (NotReady)";
          tone = st.ready ? "ok" : "warn";
        } else if (st.state?.waiting) {
          state = `Waiting: ${st.state.waiting.reason ?? "Pending"}`;
          tone = toneFor(st.state.waiting.reason ?? "Pending");
        } else if (st.state?.terminated) {
          state = `Terminated: ${st.state.terminated.reason ?? `exit ${st.state.terminated.exitCode ?? 0}`}`;
          tone = st.state.terminated.exitCode === 0 ? "muted" : "bad";
        }
      }
      const ports = Array.isArray(c.ports)
        ? c.ports.map((p: any) => `${p.name ? p.name + ":" : ""}${p.containerPort}/${p.protocol ?? "TCP"}`).join(", ")
        : "";
      const req = c.resources?.requests ?? {};
      const lim = c.resources?.limits ?? {};
      const usage = metrics[c.name];
      const cpu = buildResourceMeter("cpu", usage ? usage.cpuMilli : null, req.cpu, lim.cpu);
      const memory = buildResourceMeter("memory", usage ? usage.memoryBytes : null, req.memory, lim.memory);

      return {
        name: c.name,
        role,
        image: c.image ?? "",
        ports,
        state,
        tone,
        restarts: st?.restartCount,
        cpu,
        memory,
      };
    };

    const out: ContainerSummary[] = [];
    for (const c of podSpec.initContainers ?? []) out.push(formatContainer(c, "init"));
    for (const c of podSpec.containers ?? []) out.push(formatContainer(c, "main"));
    return out;
  });
</script>

{#snippet resourceRow(meter: ResourceMeter)}
  <div class="meter-row">
    <span class="meter-label">{meter.label}</span>
    {#if meter.showBar}
      <span class="meter-val mono">{meter.usedText} / {meter.limitText !== "none" ? meter.limitText : meter.scaleMaxText}</span>
      <div class="meter-track" title={meter.hoverText}>
        <div class={`meter-fill tone-bg-${meter.tone}`} style={`width:${meter.usedPct}%`}></div>
        {#if meter.requestMarkPct !== null}
          <span class="meter-req-mark" class:warn={meter.requestMarkWarn} style={`left:${meter.requestMarkPct}%`}></span>
        {/if}
      </div>
    {:else}
      <span class="meter-plain mono">{meter.usedText}</span>
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
        title="View or edit raw YAML manifest"
      >
        YAML{dirty ? " *" : ""}
      </button>
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
        title={primaryOwner ? `Jump to owner ${primaryOwner.kind}/${primaryOwner.name}` : "No owner reference"}
      >
        Jump to Owner
      </button>
    {/if}
    {#if isPod}
      <button onclick={() => openDock("logs")} title={`Stream container logs for pod ${sel.name}`}>Logs</button>
      <button onclick={() => openDock("shell")} title={`Open interactive shell in pod ${sel.name}`}>Shell</button>
    {/if}
    {#if canForward}
      <button onclick={() => (app.forwardTarget = { ...sel })} title={`Forward a local TCP port to ${sel.type.kind} ${sel.name}`}>
        Port forward
      </button>
    {/if}
    {#if canScale}
      {#if replicas === null}
        <button onclick={startScale} title={`Change replica count for ${sel.type.kind} ${sel.name}`}>Scale</button>
      {:else}
        <span class="scale">
          <input type="number" min="0" bind:value={replicas} onkeydown={(e) => e.key === "Enter" && scale()} />
          <button class="primary" onclick={scale} title={`Apply ${replicas ?? 0} replicas to ${sel.name}`}>Set</button>
          <button class="ghost" onclick={() => (replicas = null)} title="Cancel scaling">✕</button>
        </span>
      {/if}
    {/if}
    {#if canRestart}
      <button onclick={restart} title={`Trigger a rolling restart of ${sel.type.kind} ${sel.name}`}>Restart</button>
    {/if}
    <span class="grow"></span>
    {#if editing}
      <button onclick={cancelEdit} disabled={saving} title="Discard YAML edits">Cancel</button>
      <button class="primary" onclick={save} disabled={saving || !dirty} title="Server-side apply YAML changes (Ctrl+S)">
        {saving ? "Applying…" : "Apply"}
      </button>
    {:else}
      <button onclick={load} disabled={loading} title={`Reload ${sel.type.kind} ${sel.name} details and events`}>Refresh</button>
      <button onclick={startEdit} disabled={!yaml} title={`Edit ${sel.type.kind} ${sel.name} YAML manifest`}>Edit YAML</button>
      <button class="danger" onclick={remove} title={`Delete ${sel.type.kind} ${sel.name} from cluster`}>Delete</button>
    {/if}
  </div>

  {#if error}<div class="banner error">{error}</div>{/if}

  {#if tab === "overview"}
    <div class="overview">
      {#if loading && !obj}
        <p class="empty">Loading…</p>
      {:else if obj}
        <section class="section">
          <h4>Metadata</h4>
          <dl class="kv-grid">
            <dt>Name</dt>
            <dd class="mono">{metadata.name ?? sel.name}</dd>
            {#if metadata.namespace}
              <dt>Namespace</dt>
              <dd class="mono">{metadata.namespace}</dd>
            {/if}
            <dt>API version</dt>
            <dd class="mono">{obj.apiVersion ?? sel.type.version} ({obj.kind ?? sel.type.kind})</dd>
            {#if metadata.creationTimestamp}
              <dt>Created</dt>
              <dd>{age(metadata.creationTimestamp, app.now)} ago <span class="muted">({metadata.creationTimestamp})</span></dd>
            {/if}
            {#if metadata.uid}
              <dt>UID</dt>
              <dd class="mono">{metadata.uid}</dd>
            {/if}
            {#if metadata.resourceVersion}
              <dt>Resource version</dt>
              <dd class="mono">{metadata.resourceVersion}{metadata.generation ? ` (gen ${metadata.generation})` : ""}</dd>
            {/if}
            {#if owners.length > 0}
              <dt>Controlled by</dt>
              <dd class="chips">
                {#each owners as o (`${o.kind}/${o.name}`)}
                  <span class="chip mono">{o.kind}/{o.name}</span>
                {/each}
              </dd>
            {/if}
            {#if finalizers.length > 0}
              <dt>Finalizers</dt>
              <dd class="chips">
                {#each finalizers as f (f)}
                  <span class="chip mono">{f}</span>
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
                    <span class="chip mono">{k}={v}</span>
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
                    <div class="ann-row">
                      <span class="mono ann-k">{k}</span>
                      <span class="mono ann-v">{v}</span>
                    </div>
                  {/each}
                </div>
              {/if}
            </dd>
          </dl>
        </section>

        {#if specRows.length > 0 || selectorEntries.length > 0}
          <section class="section">
            <h4>Specification &amp; status</h4>
            <dl class="kv-grid">
              {#each specRows as r (r.label)}
                <dt>{r.label}</dt>
                <dd class={`${r.mono ? "mono " : ""}${r.tone ? `tone-${r.tone}` : ""}`}>{r.value}</dd>
              {/each}
              {#if selectorEntries.length > 0}
                <dt>Selector</dt>
                <dd class="chips">
                  {#each selectorEntries as [k, v] (k)}
                    <span class="chip mono">{k}={v}</span>
                  {/each}
                </dd>
              {/if}
            </dl>
          </section>
        {/if}

        {#if containersList.length > 0}
          <section class="section">
            <h4>Containers ({containersList.length})</h4>
            <div class="container-list">
              {#each containersList as c (`${c.role}:${c.name}`)}
                <div class="container-row">
                  <div class="container-head">
                    <strong class="mono">{c.name}</strong>
                    {#if c.role !== "main"}<span class="pill">{c.role}</span>{/if}
                    {#if c.state}<span class={`tone-${c.tone}`}>{c.state}</span>{/if}
                    {#if c.restarts != null}<span class="muted">restarts: {c.restarts}</span>{/if}
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
                  <div class="mono muted">{c.image}{c.ports ? ` (${c.ports})` : ""}</div>
                  <div class="meters">
                    {@render resourceRow(c.cpu)}
                    {@render resourceRow(c.memory)}
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
                    <td class="mono">{cond.type ?? "—"}</td>
                    <td class={cond.status === "True" ? "tone-ok" : cond.status === "False" ? "tone-muted" : "tone-warn"}>
                      {cond.status ?? "—"}
                    </td>
                    <td class="mono">{cond.reason ?? "—"}</td>
                    <td>{cond.message ?? "—"}</td>
                  </tr>
                {/each}
              </tbody>
            </table>
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
                      <span class={ev.type === "Warning" ? "tone-warn reason-name" : "reason-name"}>{ev.reason || ev.type}</span>
                      {#if ev.count > 1}<span class="event-count">×{ev.count}</span>{/if}
                    </td>
                    <td>
                      <div class="event-msg">{ev.message}</div>
                      {#if ev.source || ev.subObject}
                        <div class="mono muted event-meta">
                          {ev.source}{ev.source && ev.subObject ? " / " : ""}{ev.subObject}
                        </div>
                      {/if}
                    </td>
                    <td class="col-age mono muted">{ev.lastSeen ? age(ev.lastSeen, app.now) : "—"}</td>
                  </tr>
                {/each}
              </tbody>
            </table>
          {/if}
        </section>
      {/if}
    </div>
  {:else}
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
  {/if}
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
    padding: 0 var(--space-3) var(--space-4);
    display: flex;
    flex-direction: column;
    min-height: 0;
  }
  .section {
    padding: var(--space-3) 0;
    border-bottom: 1px solid var(--border);
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
  }
  .container-row {
    display: flex;
    flex-direction: column;
    gap: var(--space-1);
    padding: var(--space-2) 0;
    border-bottom: 1px solid var(--border-soft);
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
    gap: var(--space-3);
    margin-top: var(--space-1);
  }
  .meter-row {
    display: grid;
    grid-template-columns: auto auto minmax(48px, 1fr);
    align-items: center;
    gap: var(--space-1);
    font-size: 12px;
    min-width: 0;
  }
  .meter-label {
    color: var(--muted);
  }
  .meter-val,
  .meter-plain {
    text-align: left;
    color: var(--text-strong);
    font-variant-numeric: tabular-nums;
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
  .empty,
  .empty-inline {
    margin: 0;
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
