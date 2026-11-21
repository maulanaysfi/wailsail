import type { Column, ContainerMetrics, Row } from "./api";
import { buildResourceMeter, toneFor, type ResourceMeter } from "./format.ts";

export interface KV {
  label: string;
  value: string;
  tone?: string;
  mono?: boolean;
  copy?: boolean;
}

export interface ContainerSummary {
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

/** Build Overview "Specification & status" rows for a Kubernetes object. */
export function buildSpecRows(kind: string, obj: Record<string, any> | null): KV[] {
  if (!obj) return [];
  const spec = (obj.spec ?? {}) as Record<string, any>;
  const status = (obj.status ?? {}) as Record<string, any>;
  const out: KV[] = [];

  const add = (label: string, val: unknown, opts?: { tone?: string; mono?: boolean; copy?: boolean }) => {
    if (val === undefined || val === null || val === "") return;
    out.push({ label, value: String(val), tone: opts?.tone, mono: opts?.mono, copy: opts?.copy });
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
    add("Service name", spec.serviceName, { mono: true, copy: true });
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
    add("Node", spec.nodeName, { mono: true, copy: true });
    add("Pod IP", status.podIP, { mono: true, copy: true });
    add("Host IP", status.hostIP, { mono: true, copy: true });
    add("Service account", spec.serviceAccountName, { mono: true, copy: true });
    add("Restart policy", spec.restartPolicy);
    add("Priority class", spec.priorityClassName);
  } else if (kind === "Service") {
    add("Type", spec.type);
    add("Cluster IP", spec.clusterIP, { mono: true, copy: true });
    if (Array.isArray(spec.externalIPs) && spec.externalIPs.length) {
      add("External IPs", spec.externalIPs.join(", "), { mono: true, copy: true });
    }
    const lbIng = status.loadBalancer?.ingress;
    if (Array.isArray(lbIng) && lbIng.length) {
      add(
        "LoadBalancer ingress",
        lbIng.map((i: any) => i.ip || i.hostname).filter(Boolean).join(", "),
        { mono: true, copy: true },
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
    add("Ingress class", spec.ingressClassName, { mono: true, copy: true });
    if (Array.isArray(spec.rules) && spec.rules.length) {
      const rules = spec.rules.flatMap((r: any) =>
        (r.http?.paths ?? []).map(
          (p: any) =>
            `${r.host || "*"}${p.path || "/"} -> ${p.backend?.service?.name ?? "?"}:${p.backend?.service?.port?.number ?? p.backend?.service?.port?.name ?? "?"}`,
        ),
      );
      add("Rules", rules.join("; "), { mono: true, copy: true });
    }
  } else if (kind === "Job" || kind === "CronJob") {
    add("Schedule", spec.schedule, { mono: true, copy: true });
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
    add("Storage class", spec.storageClassName, { mono: true, copy: true });
    if (Array.isArray(spec.accessModes)) add("Access modes", spec.accessModes.join(", "));
    add("Capacity", status.capacity?.storage ?? spec.capacity?.storage ?? spec.resources?.requests?.storage, { mono: true });
    add("Volume name", spec.volumeName, { mono: true, copy: true });
    add("Volume mode", spec.volumeMode);
    add("Reclaim policy", spec.persistentVolumeReclaimPolicy);
  } else if (kind === "Node") {
    const info = status.nodeInfo ?? {};
    add("Kubelet", info.kubeletVersion, { mono: true, copy: true });
    add("OS / Arch", info.osImage ? `${info.osImage} (${info.architecture ?? ""})` : "");
    add("Kernel", info.kernelVersion, { mono: true });
    add("Runtime", info.containerRuntimeVersion, { mono: true });
    if (spec.unschedulable) add("Schedulable", "No (cordoned)", { tone: "warn" });
    if (Array.isArray(status.addresses)) {
      add(
        "Addresses",
        status.addresses.map((a: any) => `${a.type}=${a.address}`).join(", "),
        { mono: true, copy: true },
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
}

/** Extract label selector entries from spec.selector (matchLabels or flat map). */
export function buildSelectorEntries(obj: Record<string, any> | null): [string, string][] {
  const spec = (obj?.spec ?? {}) as Record<string, any>;
  const selMap = spec.selector?.matchLabels ?? (typeof spec.selector === "object" && !Array.isArray(spec.selector) ? spec.selector : null);
  if (!selMap) return [];
  return Object.entries(selMap as Record<string, string>).filter(([, v]) => typeof v === "string");
}

/** Build container summaries with runtime state and CPU/memory resource meters. */
export function buildContainersList(
  kind: string,
  obj: Record<string, any> | null,
  metrics: Record<string, ContainerMetrics>,
): ContainerSummary[] {
  const spec = (obj?.spec ?? {}) as Record<string, any>;
  const status = (obj?.status ?? {}) as Record<string, any>;
  const podSpec = spec.template?.spec ?? (kind === "Pod" ? spec : null);
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
}

export interface ControlledPodsPlan {
  mode: "owner" | "deployment" | "cronjob" | "service" | "node" | "namespace";
  uid: string;
  namespace: string;
  labelSelector: string;
  fieldSelector: string;
  emptyHint: string;
}

const directPodOwnerKinds = new Set([
  "apps/ReplicaSet",
  "apps/StatefulSet",
  "apps/DaemonSet",
  "batch/Job",
]);

/** Describe how to watch and filter controlled Pods for a selected object, or null if unsupported. */
export function buildControlledPodsPlan(
  kindKeyStr: string,
  obj: Record<string, any> | null,
  fallbackNamespace: string,
): ControlledPodsPlan | null {
  if (!obj) return null;
  const meta = (obj.metadata ?? {}) as Record<string, any>;
  const uid = String(meta.uid ?? "");
  const name = String(meta.name ?? "");
  const ns = String(meta.namespace ?? fallbackNamespace ?? "");

  if (kindKeyStr === "apps/Deployment") {
    return uid ? { mode: "deployment", uid, namespace: ns, labelSelector: "", fieldSelector: "", emptyHint: "" } : null;
  }
  if (kindKeyStr === "batch/CronJob") {
    return uid ? { mode: "cronjob", uid, namespace: ns, labelSelector: "", fieldSelector: "", emptyHint: "" } : null;
  }
  if (directPodOwnerKinds.has(kindKeyStr)) {
    return uid ? { mode: "owner", uid, namespace: ns, labelSelector: "", fieldSelector: "", emptyHint: "" } : null;
  }
  if (kindKeyStr === "/Service") {
    const entries = buildSelectorEntries(obj).sort(([a], [b]) => a.localeCompare(b));
    if (entries.length === 0) {
      return {
        mode: "service",
        uid,
        namespace: ns,
        labelSelector: "",
        fieldSelector: "",
        emptyHint: "No pod selector configured on this Service.",
      };
    }
    const labelSelector = entries.map(([k, v]) => `${k}=${v}`).join(",");
    return { mode: "service", uid, namespace: ns, labelSelector, fieldSelector: "", emptyHint: "" };
  }
  if (kindKeyStr === "/Node") {
    return {
      mode: "node",
      uid,
      namespace: "",
      labelSelector: "",
      fieldSelector: name ? `spec.nodeName=${name}` : "",
      emptyHint: "",
    };
  }
  if (kindKeyStr === "/Namespace") {
    return name
      ? { mode: "namespace", uid, namespace: name, labelSelector: "", fieldSelector: "", emptyHint: "" }
      : null;
  }
  return null;
}

/** Filter and deterministically sort Pod rows matching a ControlledPodsPlan. */
export function filterControlledPods(
  plan: ControlledPodsPlan | null,
  podRows: Iterable<Row>,
  childRows: Iterable<Row> = [],
): Row[] {
  if (!plan || plan.emptyHint) return [];
  let matches: Row[];
  if (plan.mode === "service" || plan.mode === "node" || plan.mode === "namespace") {
    matches = [...podRows];
  } else if (plan.mode === "owner") {
    matches = [];
    for (const r of podRows) {
      if (r.owner?.uid === plan.uid) matches.push(r);
    }
  } else {
    // Deployment -> ReplicaSets -> Pods; CronJob -> Jobs -> Pods.
    // Includes both old and new child controllers so rollout pods stay visible until old pods terminate.
    const childUids = new Set<string>();
    for (const c of childRows) {
      if (c.uid && c.owner?.uid === plan.uid) childUids.add(c.uid);
    }
    matches = [];
    if (childUids.size > 0) {
      for (const r of podRows) {
        if (r.owner?.uid && childUids.has(r.owner.uid)) matches.push(r);
      }
    }
  }
  matches.sort((a, b) => a.name.localeCompare(b.name) || a.namespace.localeCompare(b.namespace));
  return matches;
}

export interface PodSummaryColumns {
  readyIdx: number;
  statusIdx: number;
  restartsIdx: number;
}

/** Locate Ready, Status, and Restarts column indices from a Pod Table header. */
export function podSummaryColumns(cols: Column[]): PodSummaryColumns {
  let readyIdx = -1;
  let statusIdx = -1;
  let restartsIdx = -1;
  cols.forEach((c, i) => {
    const n = c.name.toLowerCase();
    if (n === "ready" && readyIdx === -1) readyIdx = i;
    else if ((n === "status" || n === "phase") && statusIdx === -1) statusIdx = i;
    else if (n === "restarts" && restartsIdx === -1) restartsIdx = i;
  });
  return { readyIdx, statusIdx, restartsIdx };
}

export interface WorkloadColumnIndices {
  readyIdx: number;
  desiredIdx: number;
  completionsIdx: number;
  statusIdx: number;
}

const completedStatuses = new Set(["completed", "complete", "succeeded"]);

function parseFraction(raw: string): [number, number] | null {
  const m = /^(\d+)\s*\/\s*(\d+)$/.exec(raw.trim());
  if (!m) return null;
  return [Number(m[1]), Number(m[2])];
}

/** Locate readiness-related column indices from a table's column definitions. */
export function workloadColumnIndices(cols: Column[]): WorkloadColumnIndices {
  let readyIdx = -1;
  let desiredIdx = -1;
  let completionsIdx = -1;
  let statusIdx = -1;
  cols.forEach((c, i) => {
    const n = c.name.toLowerCase();
    if (n === "ready" && readyIdx === -1) readyIdx = i;
    else if (n === "desired" && desiredIdx === -1) desiredIdx = i;
    else if (n === "completions" && completionsIdx === -1) completionsIdx = i;
    else if ((n === "status" || n === "phase") && statusIdx === -1) statusIdx = i;
  });
  return { readyIdx, desiredIdx, completionsIdx, statusIdx };
}

/**
 * Determine which columns (Name and/or Ready/Completions) should be highlighted
 * yellow for workloads that are not ready yet.
 * Scaled-to-zero (`0/0` or Desired `0`) and completed (`Complete`/`Completed`/`Succeeded`)
 * workloads remain normal; error states with unmet readiness are highlighted yellow.
 */
export function unreadyWorkloadCells(
  kindKeyStr: string,
  cells: string[],
  idx: WorkloadColumnIndices,
): { name: boolean; ready: boolean; completions: boolean } {
  const none = { name: false, ready: false, completions: false };
  const status = idx.statusIdx >= 0 ? (cells[idx.statusIdx] ?? "").trim().toLowerCase() : "";
  if (completedStatuses.has(status)) return none;

  if (kindKeyStr === "/Pod" || kindKeyStr === "apps/Deployment" || kindKeyStr === "apps/StatefulSet") {
    if (idx.readyIdx < 0) return none;
    const frac = parseFraction(cells[idx.readyIdx] ?? "");
    if (!frac) return none;
    const [ready, desired] = frac;
    if (desired === 0 || ready >= desired) return none;
    return { name: true, ready: true, completions: false };
  }

  if (kindKeyStr === "apps/ReplicaSet" || kindKeyStr === "apps/DaemonSet") {
    if (idx.desiredIdx < 0 || idx.readyIdx < 0) return none;
    const dStr = (cells[idx.desiredIdx] ?? "").trim();
    const rStr = (cells[idx.readyIdx] ?? "").trim();
    if (!/^\d+$/.test(dStr) || !/^\d+$/.test(rStr)) return none;
    const desired = Number(dStr);
    const ready = Number(rStr);
    if (desired === 0 || ready >= desired) return none;
    return { name: true, ready: false, completions: false };
  }

  if (kindKeyStr === "batch/Job") {
    if (idx.completionsIdx < 0) return none;
    const frac = parseFraction(cells[idx.completionsIdx] ?? "");
    if (!frac) return none;
    const [done, target] = frac;
    if (target === 0 || done >= target) return none;
    return { name: true, ready: false, completions: true };
  }

  return none;
}
