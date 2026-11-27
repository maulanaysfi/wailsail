import type { ResourceType } from "./api";

export interface ResourceGroup {
  name: string;
  items: ResourceType[];
  crd?: boolean;
}

// Well-known native kinds grouped into sections, keyed by "group/kind".
const sections: { name: string; kinds: string[] }[] = [
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
  {
    name: "Helm",
    kinds: ["helm.sh/Release"],
  },
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
    name: "Access",
    kinds: [
      "/ServiceAccount",
      "rbac.authorization.k8s.io/Role",
      "rbac.authorization.k8s.io/RoleBinding",
      "rbac.authorization.k8s.io/ClusterRole",
      "rbac.authorization.k8s.io/ClusterRoleBinding",
    ],
  },
];

export const defaultExpandedGroups = new Set(["Cluster", "Helm", "Workloads", "Network", "Storage", "Config"]);

// Built-in Kubernetes API groups (non-CRD).
const nativeGroups = new Set([
  "",
  "core",
  "helm.sh",
  "apps",
  "batch",
  "autoscaling",
  "policy",
  "networking.k8s.io",
  "discovery.k8s.io",
  "storage.k8s.io",
  "rbac.authorization.k8s.io",
  "scheduling.k8s.io",
  "apiextensions.k8s.io",
  "admissionregistration.k8s.io",
  "apiregistration.k8s.io",
  "coordination.k8s.io",
  "node.k8s.io",
  "certificates.k8s.io",
  "events.k8s.io",
  "flowcontrol.apiserver.k8s.io",
  "authentication.k8s.io",
  "authorization.k8s.io",
  "resource.k8s.io",
  "internal.apiserver.k8s.io",
  "storagemigration.k8s.io",
]);

// Kinds hidden from the sidebar (deprecated duplicates / noise).
const hidden = new Set(["/Endpoints", "/ComponentStatus", "events.k8s.io/Event", "/Binding"]);

export function resourceKey(r: { group: string; resource: string }): string {
  return `${r.group}/${r.resource}`;
}

export function kindKey(r: { group: string; kind: string }): string {
  return `${r.group}/${r.kind}`;
}

/** Arrange discovered resources into curated native sections plus CRD groups. */
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
    if (items.length) groups.push({ name: s.name, items, crd: false });
  }
  const nativeRest = new Map<string, ResourceType[]>();
  const crdRest = new Map<string, ResourceType[]>();
  for (const r of all) {
    const k = kindKey(r);
    if (used.has(k) || hidden.has(k)) continue;
    const g = r.group || "core";
    const target = nativeGroups.has(r.group) ? nativeRest : crdRest;
    if (!target.has(g)) target.set(g, []);
    target.get(g)!.push(r);
  }
  for (const g of [...nativeRest.keys()].sort()) {
    groups.push({ name: g, items: nativeRest.get(g)!.sort((a, b) => a.kind.localeCompare(b.kind)), crd: false });
  }
  for (const g of [...crdRest.keys()].sort()) {
    groups.push({ name: g, items: crdRest.get(g)!.sort((a, b) => a.kind.localeCompare(b.kind)), crd: true });
  }
  return groups;
}

export const scalableKinds = new Set(["apps/Deployment", "apps/StatefulSet", "apps/ReplicaSet"]);
export const restartableKinds = new Set(["apps/Deployment", "apps/StatefulSet", "apps/DaemonSet"]);
export const ownerJumpKinds = new Set(["/Pod", "apps/ReplicaSet", "batch/Job"]);
export const workloadLogKinds = new Set([
  "apps/Deployment",
  "apps/ReplicaSet",
  "apps/StatefulSet",
  "apps/DaemonSet",
  "batch/Job",
  "batch/CronJob",
]);

const kindAbbreviations: Record<string, string> = {
  CustomResourceDefinition: "CRD",
  PersistentVolumeClaim: "PVC",
  PodDisruptionBudget: "PDB",
  HorizontalPodAutoscaler: "HPA",
  VerticalPodAutoscaler: "VPA",
  CertificateSigningRequest: "CSR",
};

/** Short display name for well-known 3+ word resource kinds in the sidebar. */
export function abbreviateKind(kind: string): string {
  return kindAbbreviations[kind] ?? kind;
}
