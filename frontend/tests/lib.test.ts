import test from "node:test";
import assert from "node:assert/strict";
import {
  SECRET_VALUE_MASK,
  buildConfigDataEntries,
  buildResourceMeter,
  clearMeasureTextCache,
  decodeBase64Text,
  maskSecretKey,
  measureTextWidth,
} from "../src/lib/format.ts";
import {
  buildContainersList,
  buildControlledPodsPlan,
  buildSelectorEntries,
  buildSpecRows,
  filterControlledPods,
  podSummaryColumns,
} from "../src/lib/specs.ts";
import { tokenizeYaml, tokenizeYamlLine } from "../src/lib/yamlHighlight.ts";

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
