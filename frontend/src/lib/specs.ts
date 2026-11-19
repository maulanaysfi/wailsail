import type { ContainerMetrics } from "./api";
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
