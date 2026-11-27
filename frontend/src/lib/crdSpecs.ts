import { isPlainObject, toneFor } from "./format.ts";

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

export function humanizeSegment(raw: string): string {
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
    const parts = entries.slice(0, maxKeys).map(([k, val]) => `${k}=${summarizeValue(val, depth + 1)}`);
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

export interface KvBuilder {
  add: (
    label: string,
    val: unknown,
    opts?: { tone?: string; mono?: boolean; copy?: boolean; fullValue?: string; moreCount?: number },
  ) => void;
  addList: (label: string, items: string[], opts?: { mono?: boolean; copy?: boolean; separator?: string }) => void;
  hasLabel: (label: string) => boolean;
  count: () => number;
  build: () => KV[];
}

export function createKvBuilder(): KvBuilder {
  const out: KV[] = [];
  const usedLabels = new Set<string>();

  const claimLabel = (rawLabel: string, fallbackPrefix = "Status"): string => {
    let label = rawLabel;
    if (usedLabels.has(label)) label = `${fallbackPrefix} / ${rawLabel}`;
    let n = 2;
    while (usedLabels.has(label)) label = `${rawLabel} (${n++})`;
    usedLabels.add(label);
    return label;
  };

  const add: KvBuilder["add"] = (label, val, opts) => {
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

  const addList: KvBuilder["addList"] = (label, items, opts) => {
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

  return {
    add,
    addList,
    hasLabel: (label) => usedLabels.has(label),
    count: () => out.length,
    build: () => out,
  };
}

/** Populate concise summary rows for a CustomResourceDefinition object without openAPIV3Schema bloat. */
export function buildCustomResourceDefinitionRows(
  b: KvBuilder,
  spec: Record<string, any>,
  status: Record<string, any>,
) {
  b.add("Group", spec.group, { mono: true, copy: true });
  b.add("Scope", spec.scope);
  b.add("Kind", spec.names?.kind, { mono: true, copy: true });
  b.add("Plural", spec.names?.plural, { mono: true });
  if (Array.isArray(spec.names?.shortNames) && spec.names.shortNames.length) {
    b.addList("Short names", spec.names.shortNames.map(String), { mono: true });
  }
  if (Array.isArray(spec.versions) && spec.versions.length) {
    const versions = spec.versions.map((v: any) => {
      const flags = [v?.storage ? "storage" : "", v?.served ? "served" : "", v?.deprecated ? "deprecated" : ""]
        .filter(Boolean)
        .join(", ");
      return flags ? `${v?.name ?? "?"} (${flags})` : String(v?.name ?? "?");
    });
    b.addList("Versions", versions, { mono: true });
    const primary = spec.versions.find((v: any) => v?.storage) ?? spec.versions[0];
    if (Array.isArray(primary?.additionalPrinterColumns) && primary.additionalPrinterColumns.length) {
      b.addList(
        "Printer columns",
        primary.additionalPrinterColumns.map((c: any) => `${c?.name ?? "?"} (${c?.type ?? "string"})`),
      );
    }
  }
  if (Array.isArray(status.storedVersions) && status.storedVersions.length) {
    b.addList("Stored versions", status.storedVersions.map(String), { mono: true });
  }
}

/** Populate bounded structured rows for CRDs and custom Kubernetes objects. */
export function buildCrdSpecRows(
  b: KvBuilder,
  obj: Record<string, any>,
  spec: Record<string, any>,
  status: Record<string, any>,
) {
  const selectorObjs = new Set<object>();
  for (const g of collectAllSelectorNodes(obj)) selectorObjs.add(g.node);

  const isCidrBlocks = (key: string, val: unknown[]) =>
    key === "blocks" && val.some((x) => isPlainObject(x) && ("cidr" in x || "start" in x || "stop" in x));

  const visit = (section: string, path: string[], val: unknown, depth: number) => {
    if (b.count() >= MAX_CRD_ROWS || val === null || val === undefined) return;
    const key = path[path.length - 1] ?? section;
    if (SKIP_GENERIC_KEYS.has(key)) return;
    if (isPlainObject(val) && selectorObjs.has(val)) return;

    const labelParts = path.map((seg, i) =>
      i === path.length - 1 && Array.isArray(val) && isCidrBlocks(seg, val) ? "CIDR Blocks" : humanizeSegment(seg),
    );
    const labelText = labelParts.length > 0 ? labelParts.join(" / ") : humanizeSegment(section);
    const safeLabel = (base: string) => (b.hasLabel(base) && section !== "spec" ? `${humanizeSegment(section)} / ${base}` : base);

    if (typeof val === "string" || typeof val === "number" || typeof val === "boolean") {
      b.add(safeLabel(labelText), val, {
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
      b.addList(safeLabel(labelText), items, {
        mono: allPrim ? val.some((x) => isMonoKeyOrValue(key, x)) : true,
        copy: allPrim ? val.some((x) => isCopyableKeyOrValue(key, x)) : /block|address|cidr|ip|endpoint/i.test(key),
        separator: allPrim ? ", " : "; ",
      });
      return;
    }

    if (isPlainObject(val)) {
      if (typeof val["@type"] === "string") {
        b.add(safeLabel(labelText), summarizeValue(val, 0), { mono: true });
        return;
      }
      const entries = Object.entries(val).filter(([, child]) => child !== null && child !== undefined);
      if (entries.length === 0) return;
      if (depth >= MAX_CRD_DEPTH) {
        b.add(safeLabel(labelText), summarizeValue(val, 1), { mono: true });
        return;
      }
      for (const [k, child] of entries) {
        if (b.count() >= MAX_CRD_ROWS) break;
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
      if (b.count() >= MAX_CRD_ROWS) break;
      visit(sectionName, [k], v, 1);
    }
  }

  if (Array.isArray(obj.specs)) {
    const rules = obj.specs.slice(0, MAX_CRD_ITEMS);
    rules.forEach((rule, idx) => {
      if (!isPlainObject(rule) || b.count() >= MAX_CRD_ROWS) return;
      const prefix = obj.specs.length === 1 ? [] : [`Rule ${idx + 1}`];
      for (const [k, v] of Object.entries(rule)) {
        if (b.count() >= MAX_CRD_ROWS) break;
        visit("spec", [...prefix, k], v, 1);
      }
    });
    if (obj.specs.length > MAX_CRD_ITEMS) {
      b.add("Additional rules", `+${obj.specs.length - MAX_CRD_ITEMS} more rule(s) in YAML`);
    }
  }

  for (const [k, v] of Object.entries(obj)) {
    if (b.count() >= MAX_CRD_ROWS || SKIP_TOP_LEVEL_KEYS.has(k)) continue;
    if (k === "specs" && Array.isArray(v)) continue;
    if (isPlainObject(v) && isStringMap(v)) {
      const pairs = Object.entries(v).map(([pk, pv]) => `${pk}=${pv}`);
      b.addList(humanizeSegment(k), pairs, { mono: true, separator: ", " });
    } else {
      visit(k, [], v, 1);
    }
  }
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
