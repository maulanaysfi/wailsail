import test from "node:test";
import assert from "node:assert/strict";
import {
  SECRET_VALUE_MASK,
  buildConfigDataEntries,
  buildResourceMeter,
  clearMeasureTextCache,
  decodeBase64Text,
  formatLogTimestamp,
  maskSecretKey,
  measureTextWidth,
  timezoneOptions,
  toastTimeoutMs,
  toneFor,
} from "../src/lib/format.ts";
import { groupResources } from "../src/lib/resources.ts";
import {
  closePageTabState,
  findTabBySelection,
  formatPageTabTitle,
  navigatePageTab,
  openOrFocusTab,
  type PageTab,
} from "../src/lib/tabs.ts";
import {
  buildContainersList,
  buildControlledPodsPlan,
  buildSelectorEntries,
  buildSpecRows,
  filterControlledPods,
  podSummaryColumns,
  unreadyWorkloadCells,
  workloadColumnIndices,
} from "../src/lib/specs.ts";
import { buildSelectorGroups } from "../src/lib/crdSpecs.ts";
import { tokenizeYaml, tokenizeYamlLine } from "../src/lib/yamlHighlight.ts";
import { computeLogWindow, entryOffset, type LogEntryItem } from "../src/lib/logWindow.ts";
import { pushNavEntry, type NavState } from "../src/lib/nav.ts";

test("maskSecretKey masks keys after first 4 chars and short keys as ****", () => {
  assert.equal(maskSecretKey(""), "****");
  assert.equal(maskSecretKey("pin"), "****");
  assert.equal(maskSecretKey("port"), "****");
  assert.equal(maskSecretKey("tls.crt"), "tls.***");
  assert.equal(maskSecretKey("password"), "pass****");
  assert.equal(SECRET_VALUE_MASK.length, 20);
});

test("decodeBase64Text decodes UTF-8 and falls back to base64 on binary", () => {
  assert.deepEqual(decodeBase64Text(""), { text: "", binary: false });
  assert.deepEqual(decodeBase64Text(btoa("hello\nworld")), { text: "hello\nworld", binary: false });
  assert.deepEqual(decodeBase64Text("AP//fw=="), { text: "AP//fw==", binary: true });
  assert.deepEqual(decodeBase64Text("!!!not-base64!!!"), { text: "!!!not-base64!!!", binary: true });
});

test("buildConfigDataEntries extracts ConfigMap and Secret pairs", () => {
  const sec = buildConfigDataEntries(
    { data: { "db.url": btoa("postgres://db"), pin: btoa("1234") }, stringData: { note: "hi" } },
    true,
  );
  assert.deepEqual(
    sec.map((e) => [e.source, e.key, e.maskedKey, e.value]),
    [
      ["data", "db.url", "db.u**", "postgres://db"],
      ["data", "pin", "****", "1234"],
      ["stringData", "note", "****", "hi"],
    ],
  );

  const cm = buildConfigDataEntries({ data: { a: "1" }, binaryData: { b: "AQID" } }, false);
  assert.equal(cm.length, 2);
  assert.equal(cm[1].source, "binaryData");
  assert.equal(cm[1].binary, true);
});

test("buildResourceMeter computes request marks and color thresholds", () => {
  const plain = buildResourceMeter("cpu", 100, null, null);
  assert.equal(plain.showBar, false);
  assert.equal(plain.usedText, "100m");

  const reqOnly = buildResourceMeter("cpu", 200, "200m", null);
  assert.equal(reqOnly.showBar, true);
  assert.equal(reqOnly.requestMarkPct, 25);
  assert.equal(reqOnly.requestMarkWarn, true);
  assert.equal(reqOnly.tone, "ok");

  const warn = buildResourceMeter("memory", 85 * 1024 * 1024, "50Mi", "100Mi");
  assert.equal(warn.requestMarkPct, 50);
  assert.equal(warn.requestMarkWarn, false);
  assert.equal(warn.tone, "warn");

  const bad = buildResourceMeter("cpu", 960, "500m", "1");
  assert.equal(bad.tone, "bad");
});

test("measureTextWidth caches and clears cleanly", () => {
  clearMeasureTextCache();
  assert.equal(measureTextWidth("", "12px sans-serif"), 0);
  const w1 = measureTextWidth("250m", "12px sans-serif");
  assert.equal(w1, 32);
  assert.equal(measureTextWidth("250m", "12px sans-serif"), w1);
  assert.equal(measureTextWidth("250m", "12px sans-serif", true), w1);
  clearMeasureTextCache();
  assert.equal(measureTextWidth("250m", "12px sans-serif"), w1);
});

test("specs helpers extract Service, Deployment, and Pod container summaries", () => {
  const svcRows = buildSpecRows("Service", {
    spec: {
      type: "ClusterIP",
      clusterIP: "10.0.0.1",
      ports: [{ name: "http", port: 80, targetPort: 8080, protocol: "TCP" }],
      selector: { app: "web" },
    },
  });
  const portsRow = svcRows.find((r) => r.label === "Ports");
  assert.ok(portsRow);
  assert.equal(portsRow.copy, undefined);
  assert.deepEqual(buildSelectorEntries({ spec: { selector: { app: "web" } } }), [["app", "web"]]);

  const containers = buildContainersList(
    "Pod",
    {
      spec: {
        containers: [
          { name: "app", image: "nginx:1.27", resources: { requests: { cpu: "100m" }, limits: { cpu: "500m" } } },
        ],
      },
      status: {
        containerStatuses: [{ name: "app", ready: true, restartCount: 2, state: { running: {} } }],
      },
    },
    { app: { cpuMilli: 250, memoryBytes: 64 * 1024 * 1024 } },
  );
  assert.equal(containers.length, 1);
  assert.equal(containers[0].state, "Running (Ready)");
  assert.equal(containers[0].cpu.usedPct, 50);
});

test("tokenizeYaml preserves exact source text and highlights Kubernetes YAML tokens", () => {
  const sample = [
    "---",
    "apiVersion: apps/v1",
    "kind: Deployment # workload",
    "metadata:",
    '  "app.kubernetes.io/name": web',
    "  url: https://example.com:8443/path#frag",
    "spec:",
    "  replicas: 3",
    "  paused: false",
    "  nullField: null",
    "  template:",
    "    - image: nginx:1.27",
    "      command: |- ",
    "        echo 'hello' # inline comment",
  ].join("\n");

  const tokenized = tokenizeYaml(sample);
  assert.equal(
    tokenized.map((line) => line.map((t) => t.text).join("")).join("\n"),
    sample,
  );

  const kindLine = tokenizeYamlLine("kind: Deployment # workload");
  assert.deepEqual(
    kindLine.map((t) => [t.type, t.text]),
    [
      ["key", "kind"],
      ["punct", ":"],
      ["plain", " "],
      ["str", "Deployment"],
      ["plain", " "],
      ["comment", "# workload"],
    ],
  );

  const urlLine = tokenizeYamlLine("  url: https://example.com:8443/path#frag");
  assert.deepEqual(
    urlLine.map((t) => [t.type, t.text]),
    [
      ["plain", "  "],
      ["key", "url"],
      ["punct", ":"],
      ["plain", " "],
      ["str", "https://example.com:8443/path#frag"],
    ],
  );

  assert.equal(tokenizeYamlLine("  replicas: 3").find((t) => t.type === "num")?.text, "3");
  assert.equal(tokenizeYamlLine("  paused: false").find((t) => t.type === "bool")?.text, "false");
  assert.equal(tokenizeYamlLine("  nullField: null").find((t) => t.type === "null")?.text, "null");
});

test("controlled pods plan and filter support Deployment rollouts, Services, and Nodes", () => {
  const depPlan = buildControlledPodsPlan("apps/Deployment", { metadata: { uid: "dep-1", namespace: "prod" } }, "prod");
  assert.ok(depPlan);
  assert.equal(depPlan.mode, "deployment");

  const rsOld = { key: "rs-old", uid: "rs-old", name: "web-111", namespace: "prod", created: "", cells: [], owner: { uid: "dep-1", apiVersion: "apps/v1", kind: "ReplicaSet", name: "web" } };
  const rsNew = { key: "rs-new", uid: "rs-new", name: "web-222", namespace: "prod", created: "", cells: [], owner: { uid: "dep-1", apiVersion: "apps/v1", kind: "ReplicaSet", name: "web" } };
  const otherRs = { key: "rs-x", uid: "rs-x", name: "other", namespace: "prod", created: "", cells: [], owner: { uid: "dep-2", apiVersion: "apps/v1", kind: "ReplicaSet", name: "other" } };
  const p1 = { key: "p1", uid: "p1", name: "web-111-a", namespace: "prod", created: "", cells: [], owner: { uid: "rs-old", apiVersion: "apps/v1", kind: "ReplicaSet", name: "web-111" } };
  const p2 = { key: "p2", uid: "p2", name: "web-222-b", namespace: "prod", created: "", cells: [], owner: { uid: "rs-new", apiVersion: "apps/v1", kind: "ReplicaSet", name: "web-222" } };
  const pOther = { key: "p3", uid: "p3", name: "other-1", namespace: "prod", created: "", cells: [], owner: { uid: "rs-x", apiVersion: "apps/v1", kind: "ReplicaSet", name: "other" } };

  assert.deepEqual(
    filterControlledPods(depPlan, [p2, pOther, p1], [rsOld, rsNew, otherRs]).map((r) => r.name),
    ["web-111-a", "web-222-b"],
  );

  const svcEmpty = buildControlledPodsPlan("/Service", { metadata: { uid: "s1", namespace: "default" }, spec: {} }, "default");
  assert.ok(svcEmpty?.emptyHint);
  const svcSel = buildControlledPodsPlan("/Service", { metadata: { uid: "s2", namespace: "prod" }, spec: { selector: { tier: "api", app: "web" } } }, "prod");
  assert.equal(svcSel?.labelSelector, "app=web,tier=api");

  const nodePlan = buildControlledPodsPlan("/Node", { metadata: { uid: "n1", name: "worker-1" } }, "");
  assert.equal(nodePlan?.fieldSelector, "spec.nodeName=worker-1");
  assert.equal(nodePlan?.namespace, "");

  const nsPlan = buildControlledPodsPlan("/Namespace", { metadata: { uid: "ns1", name: "prod" } }, "");
  assert.equal(nsPlan?.mode, "namespace");
  assert.equal(nsPlan?.namespace, "prod");
  assert.deepEqual(filterControlledPods(nsPlan, [p1]).map((r) => r.name), ["web-111-a"]);

  assert.deepEqual(
    podSummaryColumns([
      { name: "Name", type: "string", description: "", priority: 0 },
      { name: "Ready", type: "string", description: "", priority: 0 },
      { name: "Status", type: "string", description: "", priority: 0 },
      { name: "Restarts", type: "integer", description: "", priority: 0 },
    ]),
    { readyIdx: 1, statusIdx: 2, restartsIdx: 3 },
  );
});

test("unreadyWorkloadCells highlights expected columns and respects 0/0, Completed, and errors", () => {
  const podCols = workloadColumnIndices([
    { name: "Name", type: "string", description: "", priority: 0 },
    { name: "Ready", type: "string", description: "", priority: 0 },
    { name: "Status", type: "string", description: "", priority: 0 },
  ]);
  assert.deepEqual(unreadyWorkloadCells("/Pod", ["p", "1/1", "Running"], podCols), { name: false, ready: false, completions: false });
  assert.deepEqual(unreadyWorkloadCells("/Pod", ["p", "0/1", "Pending"], podCols), { name: true, ready: true, completions: false });
  assert.deepEqual(unreadyWorkloadCells("/Pod", ["p", "0/1", "CrashLoopBackOff"], podCols), { name: true, ready: true, completions: false });
  assert.deepEqual(unreadyWorkloadCells("/Pod", ["p", "0/1", "Completed"], podCols), { name: false, ready: false, completions: false });

  const depCols = workloadColumnIndices([
    { name: "Name", type: "string", description: "", priority: 0 },
    { name: "Ready", type: "string", description: "", priority: 0 },
  ]);
  assert.deepEqual(unreadyWorkloadCells("apps/Deployment", ["d", "0/0"], depCols), { name: false, ready: false, completions: false });
  assert.deepEqual(unreadyWorkloadCells("apps/Deployment", ["d", "1/3"], depCols), { name: true, ready: true, completions: false });
  assert.deepEqual(unreadyWorkloadCells("apps/StatefulSet", ["s", "0/2"], depCols), { name: true, ready: true, completions: false });

  const rsCols = workloadColumnIndices([
    { name: "Name", type: "string", description: "", priority: 0 },
    { name: "Desired", type: "integer", description: "", priority: 0 },
    { name: "Current", type: "integer", description: "", priority: 0 },
    { name: "Ready", type: "integer", description: "", priority: 0 },
  ]);
  assert.deepEqual(unreadyWorkloadCells("apps/ReplicaSet", ["rs", "0", "0", "0"], rsCols), { name: false, ready: false, completions: false });
  assert.deepEqual(unreadyWorkloadCells("apps/ReplicaSet", ["rs", "3", "3", "2"], rsCols), { name: true, ready: false, completions: false });
  assert.deepEqual(unreadyWorkloadCells("apps/DaemonSet", ["ds", "4", "4", "3"], rsCols), { name: true, ready: false, completions: false });

  const jobCols = workloadColumnIndices([
    { name: "Name", type: "string", description: "", priority: 0 },
    { name: "Status", type: "string", description: "", priority: 0 },
    { name: "Completions", type: "string", description: "", priority: 0 },
  ]);
  assert.deepEqual(unreadyWorkloadCells("batch/Job", ["j", "Running", "0/1"], jobCols), { name: true, ready: false, completions: true });
  assert.deepEqual(unreadyWorkloadCells("batch/Job", ["j", "Failed", "0/1"], jobCols), { name: true, ready: false, completions: true });
  assert.deepEqual(unreadyWorkloadCells("batch/Job", ["j", "Complete", "1/1"], jobCols), { name: false, ready: false, completions: false });
});

test("groupResources places Helm section right after Cluster and toneFor maps Helm statuses", () => {
  const groups = groupResources([
    { group: "", version: "v1", resource: "nodes", kind: "Node", namespaced: false },
    { group: "helm.sh", version: "v1", resource: "releases", kind: "Release", namespaced: true },
    { group: "", version: "v1", resource: "pods", kind: "Pod", namespaced: true },
  ]);
  assert.deepEqual(
    groups.map((g) => g.name),
    ["Cluster", "Helm", "Workloads"],
  );
  assert.equal(groups[1].items[0].kind, "Release");
  assert.equal(toneFor("deployed"), "ok");
  assert.equal(toneFor("superseded"), "muted");
  assert.equal(toneFor("pending-upgrade"), "warn");
  assert.equal(toneFor("failed"), "bad");
});

test("computeLogWindow handles unwrapped and wrapped rows without blank overshoot", () => {
  const mk = (id: number, body: string, pod = "web-0"): LogEntryItem => ({
    id,
    lower: `${pod} ${body}`.toLowerCase(),
    pod,
    podColor: 0,
    ts: "",
    body,
  });

  // 400 wrapped lines (~48px each via measured cache, totalHeight = 19200px).
  const entries = Array.from({ length: 400 }, (_, i) => mk(i + 1, "x".repeat(180)));
  const measured = new Map<number, number>(entries.map((e) => [e.id, 48]));

  // Previously fixed 16px math with scrollTop=16000 computed first=1000 > total=400 -> 0 items.
  const wrapped = computeLogWindow(entries, {
    wrap: true,
    stickBottom: false,
    scrollTop: 16000,
    viewportHeight: 320,
    viewportWidth: 640,
    measured,
    overscan: 10,
  });
  assert.ok(wrapped.items.length > 0);
  assert.equal(wrapped.first, 323);
  assert.equal(wrapped.topPad, 323 * 48);
  assert.equal(wrapped.totalHeight, 400 * 48);
  assert.equal(wrapped.topPad + wrapped.items.length * 48 + wrapped.bottomPad, wrapped.totalHeight);

  // Toggling wrap calculates target offset for anchor preservation.
  assert.equal(entryOffset(entries, 11, false, measured, 640), 10 * 16);
  assert.equal(entryOffset(entries, 11, true, measured, 640), 10 * 48);

  // Search-filtered shrink clamps unwrapped scrollTop so items stay visible.
  const unwrappedClamped = computeLogWindow(entries.slice(0, 10), {
    wrap: false,
    stickBottom: false,
    scrollTop: 50000,
    viewportHeight: 320,
    viewportWidth: 640,
    measured,
    overscan: 5,
  });
  assert.equal(unwrappedClamped.items.length, 10);
  assert.equal(unwrappedClamped.topPad, 0);
  assert.equal(unwrappedClamped.bottomPad, 0);
});

test("buildSpecRows and buildSelectorGroups render CRD specs cleanly with 5-item cap and CRD summary", () => {
  const poolRows = buildSpecRows("CiliumLoadBalancerIPPool", {
    spec: {
      disabled: false,
      allowFirstLastIPs: "Yes",
      serviceSelector: { matchLabels: { env: "prod" }, matchExpressions: [{ key: "tier", operator: "In", values: ["web"] }] },
      blocks: [
        { cidr: "10.0.1.0/24" },
        { cidr: "10.0.2.0/24" },
        { cidr: "10.0.3.0/24" },
        { cidr: "10.0.4.0/24" },
        { cidr: "10.0.5.0/24" },
        { start: "10.0.6.10", stop: "10.0.6.20" },
      ],
    },
  });
  const disabledRow = poolRows.find((r) => r.label === "Disabled");
  assert.equal(disabledRow?.tone, "ok");
  assert.equal(poolRows.find((r) => r.label === "Allow first last IPs")?.value, "Yes");
  assert.equal(poolRows.some((r) => r.label.includes("Service selector")), false);
  const blocksRow = poolRows.find((r) => r.label === "CIDR Blocks");
  assert.ok(blocksRow);
  assert.equal(blocksRow.moreCount, 1);
  assert.ok(blocksRow.fullValue?.includes("start=10.0.6.10"));
  assert.ok(!blocksRow.value.includes("item(s)"));

  const l2Rows = buildSpecRows("CiliumL2AnnouncementPolicy", {
    spec: { externalIPs: true, loadBalancerIPs: true, interfaces: ["eth0"] },
  });
  assert.equal(l2Rows.find((r) => r.label === "External IPs")?.value, "true");
  assert.equal(l2Rows.find((r) => r.label === "Load balancer IPs")?.value, "true");

  const egwRows = buildSpecRows("CiliumEgressGatewayPolicy", {
    spec: { destinationCIDRs: ["0.0.0.0/0"], egressGateway: { egressIP: "10.0.0.10" } },
  });
  assert.equal(egwRows.find((r) => r.label === "Destination CIDRs")?.value, "0.0.0.0/0");

  const cnodeRows = buildSpecRows("CiliumNode", {
    spec: { bootid: "abc-123" },
  });
  assert.equal(cnodeRows.find((r) => r.label === "BootID")?.value, "abc-123");

  const ccnpDoc = {
    specs: [
      {
        description: "rule-one",
        nodeSelector: { matchLabels: { role: "worker" } },
        ingress: [{ fromEntities: ["host"], toPorts: [{ ports: [{ port: "9100", protocol: "TCP" }] }] }],
        egress: [{}],
      },
      {
        endpointSelector: { matchLabels: { app: "dns" } },
        egress: [{ toEntities: ["world"] }],
      },
    ],
  };
  const ccnpRows = buildSpecRows("CiliumClusterwideNetworkPolicy", ccnpDoc);
  const ccnpSelectors = buildSelectorGroups(ccnpDoc);
  assert.deepEqual(
    ccnpSelectors.map((s) => s.label),
    ["Rule 1 / Node selector", "Rule 2 / Endpoint selector"],
  );
  const r1Ingress = ccnpRows.find((r) => r.label === "Rule 1 / Ingress")?.value ?? "";
  assert.ok(r1Ingress.includes("port=9100"));
  assert.ok(!r1Ingress.includes("item(s)"));
  assert.equal(ccnpRows.find((r) => r.label === "Rule 1 / Egress")?.value, "* (allow all)");
  assert.ok(ccnpRows.find((r) => r.label === "Rule 2 / Egress")?.value.includes("toEntities=[world]"));

  const selectors = buildSelectorGroups({
    spec: {
      endpointSelector: {},
      serviceSelector: { matchLabels: { env: "prod" } },
      egressGateway: { nodeSelector: { matchLabels: { role: "gw" } } },
    },
  });
  assert.deepEqual(
    selectors.map((s) => s.label),
    ["Endpoint selector", "Service selector", "Egress gateway / Node selector"],
  );
  assert.equal(selectors[0].chips[0].value, "* (all)");
  assert.deepEqual(selectors[1].chips[0], { key: "env", value: "prod" });

  const envoyRows = buildSpecRows("CiliumEnvoyConfig", {
    spec: {
      nodeSelector: { matchLabels: { role: "edge" } },
      resources: [{ "@type": "type.googleapis.com/envoy.config.listener.v3.Listener", name: "http-listener", hugeBlob: {} }],
    },
  });
  assert.equal(envoyRows.find((r) => r.label === "Resources")?.value, "Listener: http-listener");

  const identRows = buildSpecRows("CiliumIdentity", {
    "security-labels": { "k8s:app": "web", "k8s:io.kubernetes.pod.namespace": "default" },
  });
  assert.equal(identRows[0]?.label, "Security labels");
  assert.ok(identRows[0]?.value.includes("k8s:app=web"));

  const crdRows = buildSpecRows("CustomResourceDefinition", {
    spec: {
      group: "cilium.io",
      scope: "Cluster",
      names: { kind: "CiliumNode", plural: "ciliumnodes", shortNames: ["cn"] },
      versions: [
        {
          name: "v2",
          served: true,
          storage: true,
          additionalPrinterColumns: [{ name: "CiliumInternalIP", type: "string" }],
          schema: { openAPIV3Schema: { properties: { spec: { type: "object" } } } },
        },
      ],
    },
    status: { storedVersions: ["v2"] },
  });
  assert.equal(crdRows.find((r) => r.label === "Group")?.value, "cilium.io");
  assert.equal(crdRows.find((r) => r.label === "Versions")?.value, "v2 (storage, served)");
  assert.equal(crdRows.find((r) => r.label === "Printer columns")?.value, "CiliumInternalIP (string)");
  assert.equal(crdRows.some((r) => r.label.toLowerCase().includes("schema")), false);
});

test("toastTimeoutMs scales timeouts conditionally above 10 words and caps at 18s", () => {
  assert.equal(toastTimeoutMs("Pod deleted", "ok"), 3500);
  assert.equal(toastTimeoutMs("Path already added", "info"), 3500);
  assert.equal(toastTimeoutMs("Large action pending", "warn"), 5500);
  assert.equal(toastTimeoutMs("Connection refused", "error"), 8000);

  const twentyWords = Array.from({ length: 20 }, (_, i) => `word${i}`).join(" ");
  assert.equal(toastTimeoutMs(twentyWords, "info"), 3500 + 10 * 250);
  assert.equal(toastTimeoutMs(twentyWords, "warn"), 5500 + 10 * 250);
  assert.equal(toastTimeoutMs(twentyWords, "error"), 8000 + 10 * 250);

  const huge = Array.from({ length: 80 }, () => "token").join(" ");
  assert.equal(toastTimeoutMs(huge, "error"), 18_000);
});

test("formatLogTimestamp converts RFC3339 timestamps to selected timezone as YYYY-MM-DD HH:mm:ss", () => {
  assert.equal(formatLogTimestamp("2026-11-26T04:30:12.123456Z ", "UTC"), "2026-11-26 04:30:12 ");
  assert.equal(formatLogTimestamp("2026-11-26T04:30:12.123456Z ", "Asia/Jakarta"), "2026-11-26 11:30:12 ");
  assert.equal(formatLogTimestamp("2026-11-26T17:05:09Z", "UTC"), "2026-11-26 17:05:09");
  assert.equal(formatLogTimestamp("2026-11-26T00:05:09Z ", "UTC"), "2026-11-26 00:05:09 ");
  assert.equal(formatLogTimestamp("2026-11-26T04:30:12Z ", "Invalid/Zone"), "2026-11-26T04:30:12Z ");

  const zones = timezoneOptions();
  assert.equal(zones[0].value, "local");
  assert.equal(zones[1].value, "UTC");
  assert.ok(zones.some((z) => z.value === "Asia/Jakarta"));
});

test("pushNavEntry tracks resource and object visits while deduplicating and truncating forward branch", () => {
  let nav: NavState = { entries: [], index: -1 };
  nav = pushNavEntry(nav, { resourceKey: "/pods", selection: null });
  nav = pushNavEntry(nav, { resourceKey: "/pods", selection: null }); // duplicate skipped
  assert.equal(nav.entries.length, 1);
  assert.equal(nav.index, 0);

  nav = pushNavEntry(nav, {
    resourceKey: "apps/deployments",
    selection: { resourceKey: "apps/deployments", namespace: "default", name: "web" },
  });
  nav = pushNavEntry(nav, {
    resourceKey: "/pods",
    selection: { resourceKey: "/pods", namespace: "default", name: "web-1" },
  });
  assert.equal(nav.index, 2);

  // Simulate stepping Back to index 1 then visiting a new resource.
  nav = pushNavEntry({ ...nav, index: 1 }, { resourceKey: "/services", selection: null });
  assert.equal(nav.entries.length, 3);
  assert.equal(nav.index, 2);
  assert.equal(nav.entries[2].resourceKey, "/services");
});

test("page tab helpers open/deduplicate, format prefix, find tabs, navigate history, and close to adjacent tab", () => {
  const podType = { group: "", version: "v1", resource: "pods", kind: "Pod", namespaced: true };
  const depType = { group: "apps", version: "v1", resource: "deployments", kind: "Deployment", namespaced: true };
  const pvcType = { group: "", version: "v1", resource: "persistentvolumeclaims", kind: "PersistentVolumeClaim", namespaced: true };
  let seq = 0;
  const makeId = () => `page-${++seq}`;

  assert.equal(formatPageTabTitle({ type: podType, namespace: "default", name: "web-0" }), "Pod/web-0");
  assert.equal(formatPageTabTitle({ type: pvcType, namespace: "default", name: "data-vol" }), "PVC/data-vol");

  let tabs: PageTab[] = [];
  let activeId: string | null = null;

  const first = openOrFocusTab(tabs, { type: depType, namespace: "default", name: "web" }, makeId);
  tabs = first.tabs;
  activeId = first.activeId;
  assert.equal(tabs.length, 1);
  assert.equal(activeId, "page-1");

  const dup = openOrFocusTab(tabs, { type: depType, namespace: "default", name: "web" }, makeId);
  assert.equal(dup.tabs.length, 1);
  assert.equal(dup.activeId, "page-1");
  assert.equal(findTabBySelection(tabs, { type: depType, namespace: "default", name: "web" })?.id, "page-1");
  assert.equal(findTabBySelection(tabs, { type: depType, namespace: "default", name: "other" }), undefined);

  const second = openOrFocusTab(tabs, { type: podType, namespace: "default", name: "web-0" }, makeId);
  tabs = second.tabs;
  activeId = second.activeId;
  assert.equal(tabs.length, 2);
  assert.equal(activeId, "page-2");

  tabs = navigatePageTab(tabs, "page-2", { type: depType, namespace: "default", name: "api" });
  assert.equal(tabs[1].selection.name, "api");
  assert.equal(tabs[1].nav.entries.length, 2);
  assert.equal(tabs[1].nav.index, 1);

  const closedSecond = closePageTabState(tabs, activeId, "page-2");
  tabs = closedSecond.tabs;
  activeId = closedSecond.activeId;
  assert.equal(tabs.length, 1);
  assert.equal(activeId, "page-1");

  const closedFirst = closePageTabState(tabs, activeId, "page-1");
  assert.equal(closedFirst.tabs.length, 0);
  assert.equal(closedFirst.activeId, null);
});
