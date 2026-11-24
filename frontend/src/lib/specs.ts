import type { Column, ContainerMetrics, Row } from "./api";
import { buildResourceMeter, toneFor, type ResourceMeter } from "./format.ts";

export interface KV {
  label: string;
  value: string;
  fullValue?: string;
  moreCount?: number;
  tone?: string;
  mono?: boolean;
  copy?: boolean;
}

export interface SelectorChip {
  key?: string;
  value: string;
}

export interface SelectorGroup {
  label: string;
  chips: SelectorChip[];
}

const MAX_CRD_ITEMS = 5;
const MAX_CRD_DEPTH = 3;
const MAX_CRD_ROWS = 48;
const MAX_SUMMARY_DEPTH = 5;
const MAX_SUMMARY_KEYS = 8;
const SKIP_GENERIC_KEYS = new Set(["conditions", "openAPIV3Schema"]);
const SKIP_TOP_LEVEL_KEYS = new Set(["apiVersion", "kind", "metadata", "spec", "status", "data", "binaryData", "stringData"]);
const ACRONYM_MAP: Record<string, string> = {
  bootid: "BootID",
  ip: "IP",
  ips: "IPs",
  cidr: "CIDR",
  cidrs: "CIDRs",
  asn: "ASN",
  asns: "ASNs",
  bgp: "BGP",
  id: "ID",
  ids: "IDs",
  fqdn: "FQDN",
  fqdns: "FQDNs",
  url: "URL",
  urls: "URLs",
  dns: "DNS",
  eni: "ENI",
  gcp: "GCP",
  ipam: "IPAM",
  ipv4: "IPv4",
  ipv6: "IPv6",
  l2: "L2",
  l7: "L7",
  crd: "CRD",
  crds: "CRDs",
  ebgp: "EBGP",
  mtu: "MTU",
};

function isPlainObject(v: unknown): v is Record<string, any> {
  return typeof v === "object" && v !== null && !Array.isArray(v);
}

function humanizeSegment(raw: string): string {
  const exact = ACRONYM_MAP[raw.toLowerCase()];
  if (exact) return exact;
  const words = raw
    .replace(/([a-z0-9])([A-Z])/g, "$1 $2")
    .replace(/([A-Z]+)([A-Z](?:[a-rt-z]|[a-z]{2}))/g, "$1 $2")
    .split(/[\s_-]+/)
    .filter(Boolean);
  return words
    .map((w, i) => {
      const lower = w.toLowerCase();
      if (ACRONYM_MAP[lower]) return ACRONYM_MAP[lower];
      return i === 0 ? lower.charAt(0).toUpperCase() + lower.slice(1) : lower;
    })
    .join(" ");
}

function isStringMap(v: unknown): v is Record<string, string> {
  return isPlainObject(v) && Object.values(v).every((x) => typeof x === "string");
}

function isSelectorObject(key: string, v: unknown): v is Record<string, any> {
  if (!isPlainObject(v)) return false;
  if ("matchLabels" in v || "matchExpressions" in v) return true;
  return /selector$/i.test(key) && isStringMap(v);
}

function extractSelectorChips(selObj: Record<string, any>): SelectorChip[] {
  const chips: SelectorChip[] = [];
  const ml = isPlainObject(selObj.matchLabels)
    ? selObj.matchLabels
    : !("matchLabels" in selObj || "matchExpressions" in selObj)
      ? selObj
      : null;
  if (ml) {
    for (const [k, v] of Object.entries(ml)) {
      if (typeof v === "string") chips.push({ key: k, value: v });
    }
  }
  if (Array.isArray(selObj.matchExpressions)) {
    for (const expr of selObj.matchExpressions) {
      if (!isPlainObject(expr) || !expr.key) continue;
      const op = String(expr.operator ?? "In");
      const vals = Array.isArray(expr.values) && expr.values.length > 0 ? ` (${expr.values.join(", ")})` : "";
      chips.push({ value: `${expr.key} ${op}${vals}` });
    }
  }
  return chips;
}

function summarizeValue(v: unknown, depth = 0): string {
  if (v === null || v === undefined) return "";
  if (typeof v === "string" || typeof v === "number" || typeof v === "boolean") return String(v);
  if (Array.isArray(v)) {
    if (v.length === 0) return "[]";
    if (depth >= MAX_SUMMARY_DEPTH) return `${v.length} item(s)`;
    const shown = v.slice(0, MAX_CRD_ITEMS).map((item) => summarizeValue(item, depth + 1)).filter(Boolean);
    return v.length > MAX_CRD_ITEMS ? `[${shown.join(", ")}, +${v.length - MAX_CRD_ITEMS} more]` : `[${shown.join(", ")}]`;
  }
  if (isPlainObject(v)) {
    if (typeof v["@type"] === "string") {
      const shortType = v["@type"].split(".").pop() ?? v["@type"];
      return v.name ? `${shortType}: ${v.name}` : shortType;
    }
    if ("matchLabels" in v || "matchExpressions" in v) {
      const chips = extractSelectorChips(v);
      return chips.length ? chips.map((c) => (c.key ? `${c.key}=${c.value}` : c.value)).join(", ") : "*";
    }
    const entries = Object.entries(v).filter(([, val]) => val !== null && val !== undefined);
    if (entries.length === 0) return "{}";
    if (depth >= MAX_SUMMARY_DEPTH) return `{${entries.map(([k]) => k).slice(0, 3).join(", ")}}`;
    const maxKeys = depth === 0 ? MAX_SUMMARY_KEYS : 6;
    const parts = entries
      .slice(0, maxKeys)
      .map(([k, val]) => `${k}=${summarizeValue(val, depth + 1)}`);
    if (entries.length > maxKeys) parts.push(`+${entries.length - maxKeys} more`);
    return parts.join(", ");
  }
  return String(v);
}

function isMonoKeyOrValue(key: string, val: unknown): boolean {
  if (/^(ip|ipv4|ipv6|cidr|port|asn|localasn|peerasn|id|uid|arn|node|address|addresses|egressip|peeraddress|group|kind|plural|shortnames|resources)$/i.test(key)) {
    return true;
  }
  if (typeof val === "string") {
    return /^\d{1,3}(\.\d{1,3}){3}(\/\d+)?$/.test(val) || /^[0-9a-f:]+::[0-9a-f:/]*$/i.test(val);
  }
  return false;
}

function isCopyableKeyOrValue(key: string, val: unknown): boolean {
  return typeof val !== "boolean" && isMonoKeyOrValue(key, val);
}

function crdToneFor(key: string, val: unknown): string | undefined {
  if (/disabled$/i.test(key) && typeof val === "boolean") return val ? "warn" : "ok";
  if (/(state|phase|status|health)$/i.test(key) && typeof val === "string") return toneFor(val);
  return undefined;
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
  const usedLabels = new Set<string>();

  const claimLabel = (rawLabel: string, fallbackPrefix = "Status"): string => {
    let label = rawLabel;
    if (usedLabels.has(label)) label = `${fallbackPrefix} / ${rawLabel}`;
    let n = 2;
    while (usedLabels.has(label)) {
      label = `${rawLabel} (${n++})`;
    }
    usedLabels.add(label);
    return label;
  };

  const add = (
    label: string,
    val: unknown,
    opts?: { tone?: string; mono?: boolean; copy?: boolean; fullValue?: string; moreCount?: number },
  ) => {
    if (val === undefined || val === null || val === "") return;
    out.push({
      label: claimLabel(label),
      value: String(val),
      fullValue: opts?.fullValue,
      moreCount: opts?.moreCount,
      tone: opts?.tone,
      mono: opts?.mono,
      copy: opts?.copy,
    });
  };

  const addList = (
    label: string,
    items: string[],
    opts?: { mono?: boolean; copy?: boolean; separator?: string },
  ) => {
    const clean = items.filter((s) => s !== "");
    if (clean.length === 0) return;
    const sep = opts?.separator ?? ", ";
    if (clean.length <= MAX_CRD_ITEMS) {
      add(label, clean.join(sep), { mono: opts?.mono, copy: opts?.copy });
      return;
    }
    add(label, clean.slice(0, MAX_CRD_ITEMS).join(sep), {
      fullValue: clean.join(sep),
      moreCount: clean.length - MAX_CRD_ITEMS,
      mono: opts?.mono,
      copy: opts?.copy,
    });
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
  } else if (kind === "CustomResourceDefinition") {
    add("Group", spec.group, { mono: true, copy: true });
    add("Scope", spec.scope);
    add("Kind", spec.names?.kind, { mono: true, copy: true });
    add("Plural", spec.names?.plural, { mono: true });
    if (Array.isArray(spec.names?.shortNames) && spec.names.shortNames.length) {
      addList("Short names", spec.names.shortNames.map(String), { mono: true });
    }
    if (Array.isArray(spec.versions) && spec.versions.length) {
      const versions = spec.versions.map((v: any) => {
        const flags = [v?.storage ? "storage" : "", v?.served ? "served" : "", v?.deprecated ? "deprecated" : ""]
          .filter(Boolean)
          .join(", ");
        return flags ? `${v?.name ?? "?"} (${flags})` : String(v?.name ?? "?");
      });
      addList("Versions", versions, { mono: true });
      const primary = spec.versions.find((v: any) => v?.storage) ?? spec.versions[0];
      if (Array.isArray(primary?.additionalPrinterColumns) && primary.additionalPrinterColumns.length) {
        addList(
          "Printer columns",
          primary.additionalPrinterColumns.map((c: any) => `${c?.name ?? "?"} (${c?.type ?? "string"})`),
        );
      }
    }
    if (Array.isArray(status.storedVersions) && status.storedVersions.length) {
      addList("Stored versions", status.storedVersions.map(String), { mono: true });
    }
  } else {
    const selectorObjs = new Set<object>();
    for (const g of collectAllSelectorNodes(obj)) selectorObjs.add(g.node);

    const isCidrBlocks = (key: string, val: unknown[]) =>
      key === "blocks" && val.some((x) => isPlainObject(x) && ("cidr" in x || "start" in x || "stop" in x));

    const visit = (section: string, path: string[], val: unknown, depth: number) => {
      if (out.length >= MAX_CRD_ROWS || val === null || val === undefined) return;
      const key = path[path.length - 1] ?? section;
      if (SKIP_GENERIC_KEYS.has(key)) return;
      if (isPlainObject(val) && selectorObjs.has(val)) return;

      const labelParts = path.map((seg, i) =>
        i === path.length - 1 && Array.isArray(val) && isCidrBlocks(seg, val) ? "CIDR Blocks" : humanizeSegment(seg),
      );
      const labelText = labelParts.length > 0 ? labelParts.join(" / ") : humanizeSegment(section);
      const safeLabel = (base: string) => (usedLabels.has(base) && section !== "spec" ? `${humanizeSegment(section)} / ${base}` : base);

      if (typeof val === "string" || typeof val === "number" || typeof val === "boolean") {
        add(safeLabel(labelText), val, {
          mono: isMonoKeyOrValue(key, val),
          copy: isCopyableKeyOrValue(key, val),
          tone: crdToneFor(key, val),
        });
        return;
      }

      if (Array.isArray(val)) {
        if (val.length === 0) return;
        const allPrim = val.every((x) => typeof x === "string" || typeof x === "number" || typeof x === "boolean");
        const isPolicyRuleList = /^(ingress|egress|ingressDeny|egressDeny)$/i.test(key);
        const items = val
          .map((x) => {
            const s = summarizeValue(x, 0);
            return isPolicyRuleList && s === "{}" ? "* (allow all)" : s;
          })
          .filter(Boolean);
        addList(safeLabel(labelText), items, {
          mono: allPrim ? val.some((x) => isMonoKeyOrValue(key, x)) : true,
          copy: allPrim ? val.some((x) => isCopyableKeyOrValue(key, x)) : /block|address|cidr|ip|endpoint/i.test(key),
          separator: allPrim ? ", " : "; ",
        });
        return;
      }

      if (isPlainObject(val)) {
        if (typeof val["@type"] === "string") {
          add(safeLabel(labelText), summarizeValue(val, 0), { mono: true });
          return;
        }
        const entries = Object.entries(val).filter(([, child]) => child !== null && child !== undefined);
        if (entries.length === 0) return;
        if (depth >= MAX_CRD_DEPTH) {
          add(safeLabel(labelText), summarizeValue(val, 1), { mono: true });
          return;
        }
        for (const [k, child] of entries) {
          if (out.length >= MAX_CRD_ROWS) break;
          visit(section, [...path, k], child, depth + 1);
        }
      }
    };

    for (const [sectionName, sectionObj] of [
      ["spec", spec],
      ["status", status],
    ] as const) {
      if (!isPlainObject(sectionObj)) continue;
      for (const [k, v] of Object.entries(sectionObj)) {
        if (out.length >= MAX_CRD_ROWS) break;
        visit(sectionName, [k], v, 1);
      }
    }

    if (Array.isArray(obj.specs)) {
      const rules = obj.specs.slice(0, MAX_CRD_ITEMS);
      rules.forEach((rule, idx) => {
        if (!isPlainObject(rule) || out.length >= MAX_CRD_ROWS) return;
        const prefix = obj.specs.length === 1 ? [] : [`Rule ${idx + 1}`];
        for (const [k, v] of Object.entries(rule)) {
          if (out.length >= MAX_CRD_ROWS) break;
          visit("spec", [...prefix, k], v, 1);
        }
      });
      if (obj.specs.length > MAX_CRD_ITEMS) {
        add("Additional rules", `+${obj.specs.length - MAX_CRD_ITEMS} more rule(s) in YAML`);
      }
    }

    for (const [k, v] of Object.entries(obj)) {
      if (out.length >= MAX_CRD_ROWS || SKIP_TOP_LEVEL_KEYS.has(k)) continue;
      if (k === "specs" && Array.isArray(v)) continue;
      if (isPlainObject(v) && isStringMap(v)) {
        const pairs = Object.entries(v).map(([pk, pv]) => `${pk}=${pv}`);
        addList(humanizeSegment(k), pairs, { mono: true, separator: ", " });
      } else {
        visit(k, [], v, 1);
      }
    }
  }

  return out;
}

function collectSelectorNodes(spec: Record<string, any>, prefix = ""): Array<{ label: string; node: Record<string, any> }> {
  const found: Array<{ label: string; node: Record<string, any> }> = [];
  if (!isPlainObject(spec)) return found;
  for (const [k, v] of Object.entries(spec)) {
    if (k === "template") continue;
    if (isSelectorObject(k, v)) {
      const base = k === "selector" ? "Selector" : humanizeSegment(k);
      found.push({ label: prefix ? `${prefix} / ${base}` : base, node: v });
    } else if (isPlainObject(v)) {
      for (const [ck, cv] of Object.entries(v)) {
        if (isSelectorObject(ck, cv)) {
          const base = `${humanizeSegment(k)} / ${humanizeSegment(ck)}`;
          found.push({ label: prefix ? `${prefix} / ${base}` : base, node: cv });
        }
      }
    }
  }
  return found;
}

function collectAllSelectorNodes(obj: Record<string, any> | null): Array<{ label: string; node: Record<string, any> }> {
  if (!obj) return [];
  const found = isPlainObject(obj.spec) ? collectSelectorNodes(obj.spec) : [];
  if (Array.isArray(obj.specs)) {
    obj.specs.slice(0, MAX_CRD_ITEMS).forEach((rule, idx) => {
      if (!isPlainObject(rule)) return;
      const prefix = obj.specs.length === 1 ? "" : `Rule ${idx + 1}`;
      found.push(...collectSelectorNodes(rule, prefix));
    });
  }
  return found;
}

/** Extract labeled Kubernetes selector chip groups from spec and specs rules. */
export function buildSelectorGroups(obj: Record<string, any> | null): SelectorGroup[] {
  const nodes = collectAllSelectorNodes(obj);
  if (nodes.length === 0) return [];
  const groups: SelectorGroup[] = [];
  const seenLabels = new Set<string>();
  for (const { label, node } of nodes) {
    let chips = extractSelectorChips(node);
    if (chips.length === 0 && /selector$/i.test(label) && Object.keys(node).length === 0) {
      chips = [{ value: "* (all)" }];
    }
    if (chips.length === 0) continue;
    let unique = label;
    let n = 2;
    while (seenLabels.has(unique)) unique = `${label} (${n++})`;
    seenLabels.add(unique);
    groups.push({ label: unique, chips });
  }
  return groups;
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
