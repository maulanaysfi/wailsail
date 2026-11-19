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
import { buildContainersList, buildSelectorEntries, buildSpecRows } from "../src/lib/specs.ts";

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
