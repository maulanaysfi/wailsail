import type { ResourceType } from "./api";

export interface ResourceGroup {
  name: string;
  items: ResourceType[];
}

// Well-known kinds grouped into sections, keyed by "group/kind".
const sections: { name: string; kinds: string[] }[] = [
  {
    name: "Workloads",
    kinds: [
      "/Pod",
      "apps/Deployment",
      "apps/StatefulSet",
      "apps/DaemonSet",
      "apps/ReplicaSet",
      "batch/Job",
      "batch/CronJob",
    ],
  },
  {
    name: "Network",
    kinds: [
      "/Service",
      "networking.k8s.io/Ingress",
      "networking.k8s.io/IngressClass",
      "networking.k8s.io/NetworkPolicy",
      "discovery.k8s.io/EndpointSlice",
      "gateway.networking.k8s.io/Gateway",
      "gateway.networking.k8s.io/HTTPRoute",
    ],
  },
  {
    name: "Config",
    kinds: [
      "/ConfigMap",
      "/Secret",
      "autoscaling/HorizontalPodAutoscaler",
      "policy/PodDisruptionBudget",
      "/ResourceQuota",
      "/LimitRange",
      "scheduling.k8s.io/PriorityClass",
    ],
  },
  {
    name: "Storage",
    kinds: [
      "/PersistentVolumeClaim",
      "/PersistentVolume",
      "storage.k8s.io/StorageClass",
      "storage.k8s.io/VolumeAttachment",
    ],
  },
  {
    name: "Access",
    kinds: [
      "/ServiceAccount",
      "rbac.authorization.k8s.io/Role",
      "rbac.authorization.k8s.io/RoleBinding",
      "rbac.authorization.k8s.io/ClusterRole",
      "rbac.authorization.k8s.io/ClusterRoleBinding",
    ],
  },
  {
    name: "Cluster",
    kinds: [
      "/Node",
      "/Namespace",
      "/Event",
      "apiextensions.k8s.io/CustomResourceDefinition",
      "admissionregistration.k8s.io/ValidatingWebhookConfiguration",
      "admissionregistration.k8s.io/MutatingWebhookConfiguration",
      "apiregistration.k8s.io/APIService",
    ],
  },
];

// Kinds hidden from the sidebar (deprecated duplicates / noise).
const hidden = new Set(["/Endpoints", "/ComponentStatus", "events.k8s.io/Event", "/Binding"]);

export function resourceKey(r: { group: string; resource: string }): string {
  return `${r.group}/${r.resource}`;
}

export function kindKey(r: { group: string; kind: string }): string {
  return `${r.group}/${r.kind}`;
}

/** Arrange discovered resources into curated sections plus one per CRD group. */
export function groupResources(all: ResourceType[]): ResourceGroup[] {
  const byKind = new Map(all.map((r) => [kindKey(r), r]));
  const used = new Set<string>();
  const groups: ResourceGroup[] = [];
  for (const s of sections) {
    const items: ResourceType[] = [];
    for (const k of s.kinds) {
      const r = byKind.get(k);
      if (r) {
        items.push(r);
        used.add(k);
      }
    }
    if (items.length) groups.push({ name: s.name, items });
  }
  const rest = new Map<string, ResourceType[]>();
  for (const r of all) {
    const k = kindKey(r);
    if (used.has(k) || hidden.has(k)) continue;
    const g = r.group || "core";
    if (!rest.has(g)) rest.set(g, []);
    rest.get(g)!.push(r);
  }
  for (const g of [...rest.keys()].sort()) {
    groups.push({ name: g, items: rest.get(g)!.sort((a, b) => a.kind.localeCompare(b.kind)) });
  }
  return groups;
}

export const scalableKinds = new Set(["apps/Deployment", "apps/StatefulSet", "apps/ReplicaSet"]);
export const restartableKinds = new Set(["apps/Deployment", "apps/StatefulSet", "apps/DaemonSet"]);
