package kube

import (
	"testing"
	"time"

	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/types"
)

const podTable = `{
 "kind":"Table","apiVersion":"meta.k8s.io/v1","metadata":{"resourceVersion":"42"},
 "columnDefinitions":[
  {"name":"Name","type":"string","priority":0},
  {"name":"Ready","type":"string","priority":0},
  {"name":"Restarts","type":"integer","priority":0},
  {"name":"Age","type":"string","priority":0},
  {"name":"IP","type":"string","priority":1}],
 "rows":[{"cells":["web-1","1/1",3,"5d","10.0.0.4"],
  "object":{"kind":"PartialObjectMetadata","metadata":{"name":"web-1","namespace":"prod","uid":"u-1","resourceVersion":"41","creationTimestamp":"2024-01-02T03:04:05Z"}}}]}`

func TestParseTable(t *testing.T) {
	res, err := parseTable([]byte(podTable), nil)
	if err != nil {
		t.Fatal(err)
	}
	if len(res.Columns) != 4 {
		t.Fatalf("Age column should be dropped, got %+v", res.Columns)
	}
	if res.Columns[3].Name != "IP" || res.Columns[3].Priority != 1 {
		t.Fatalf("unexpected columns %+v", res.Columns)
	}
	r := res.Rows[0]
	if r.Key != "u-1" || r.Namespace != "prod" || r.Created != "2024-01-02T03:04:05Z" {
		t.Fatalf("bad row identity %+v", r)
	}
	want := []string{"web-1", "1/1", "3", "10.0.0.4"}
	for i, w := range want {
		if r.Cells[i] != w {
			t.Fatalf("cell %d = %q, want %q", i, r.Cells[i], w)
		}
	}
	if res.ResourceVersion != "41" {
		t.Fatalf("rv = %s", res.ResourceVersion)
	}

	// A watch event without column definitions reuses the previous layout.
	ev := `{"kind":"Table","columnDefinitions":[],"rows":[{"cells":["web-2","0/1",0,"1s","10.0.0.5"],
	 "object":{"metadata":{"name":"web-2","namespace":"prod","uid":"u-2","resourceVersion":"50"}}}]}`
	res2, err := parseTable([]byte(ev), res)
	if err != nil {
		t.Fatal(err)
	}
	if got := res2.Rows[0].Cells; len(got) != 4 || got[3] != "10.0.0.5" {
		t.Fatalf("cells %v", got)
	}
}

func TestParsePlainList(t *testing.T) {
	raw := `{"kind":"FooList","metadata":{"resourceVersion":"7"},"items":[{"metadata":{"name":"a","namespace":"x"}}]}`
	res, err := parseTable([]byte(raw), nil)
	if err != nil {
		t.Fatal(err)
	}
	if res.ResourceVersion != "7" || len(res.Rows) != 1 || res.Rows[0].Key != "x/a" {
		t.Fatalf("%+v", res)
	}
}

func TestResourcePath(t *testing.T) {
	pods := ResourceRef{Version: "v1", Resource: "pods", Namespaced: true}
	if got := pods.Path("ns", "p"); got != "/api/v1/namespaces/ns/pods/p" {
		t.Fatal(got)
	}
	if got := pods.Path("", ""); got != "/api/v1/pods" {
		t.Fatal(got)
	}
	nodes := ResourceRef{Group: "apps", Version: "v1", Resource: "deployments", Namespaced: true}
	if got := nodes.Path("a", ""); got != "/apis/apps/v1/namespaces/a/deployments" {
		t.Fatal(got)
	}
}

func TestBatcher(t *testing.T) {
	b := newBatcher()
	if _, ok := b.take(); ok {
		t.Fatal("empty batcher should have nothing")
	}
	b.doReset([]Column{{Name: "Name"}}, []Row{{Key: "a"}, {Key: "b"}})
	b.remove("a")
	b.upsert(Row{Key: "c"})
	ev, ok := b.take()
	if !ok || !ev.Reset || len(ev.Upserts) != 2 || len(ev.Deletes) != 0 {
		t.Fatalf("%+v", ev)
	}
	b.upsert(Row{Key: "d"})
	b.remove("d")
	b.remove("b")
	ev, _ = b.take()
	if ev.Reset || len(ev.Upserts) != 0 || len(ev.Deletes) != 2 {
		t.Fatalf("%+v", ev)
	}
}

func TestDecodeManifest(t *testing.T) {
	m := `apiVersion: v1
kind: ConfigMap
metadata:
  name: a
---
apiVersion: v1
kind: List
items:
- apiVersion: v1
  kind: Secret
  metadata: {name: b}
`
	objs, err := DecodeManifest(m)
	if err != nil {
		t.Fatal(err)
	}
	if len(objs) != 2 || objs[1].GetKind() != "Secret" {
		t.Fatalf("%v", objs)
	}
	if _, err := DecodeManifest("kind: ConfigMap\nmetadata: {name: x}\n"); err == nil {
		t.Fatal("expected missing apiVersion error")
	}
}

func TestFilterAndSortEvents(t *testing.T) {
	t1 := metav1.NewTime(time.Date(2026, 11, 16, 10, 0, 0, 0, time.UTC))
	t2 := metav1.NewTime(time.Date(2026, 11, 16, 10, 5, 0, 0, time.UTC))
	items := []corev1.Event{
		{
			InvolvedObject: corev1.ObjectReference{Kind: "Pod", Namespace: "prod", Name: "web-1", UID: types.UID("u-1")},
			Type:           "Normal",
			Reason:         "Scheduled",
			Message:        "Successfully assigned prod/web-1 to node-1",
			Source:         corev1.EventSource{Component: "default-scheduler"},
			LastTimestamp:  t1,
		},
		{
			InvolvedObject: corev1.ObjectReference{Kind: "Pod", Namespace: "prod", Name: "web-1", UID: types.UID("u-1"), FieldPath: "spec.containers{app}"},
			Type:           "Warning",
			Reason:         "BackOff",
			Message:        "Back-off restarting failed container",
			Count:          4,
			Source:         corev1.EventSource{Component: "kubelet", Host: "node-1"},
			LastTimestamp:  t2,
		},
		{
			// Different Kind with same name should be excluded.
			InvolvedObject: corev1.ObjectReference{Kind: "Service", Namespace: "prod", Name: "web-1", UID: types.UID("u-2")},
			Type:           "Normal",
			Reason:         "Updated",
			LastTimestamp:  t2,
		},
	}

	got := filterAndSortEvents(items, "Pod", "prod", "web-1", "u-1")
	if len(got) != 2 {
		t.Fatalf("expected 2 events, got %+v", got)
	}
	if got[0].Reason != "BackOff" || got[0].Count != 4 || got[0].Source != "kubelet (node-1)" {
		t.Fatalf("newest event should be first: %+v", got[0])
	}
	if got[1].Reason != "Scheduled" || got[1].Count != 1 {
		t.Fatalf("second event: %+v", got[1])
	}
}

func TestParsePodMetrics(t *testing.T) {
	raw := []byte(`{"containers":[{"name":"app","usage":{"cpu":"250m","memory":"64Mi"}},{"name":"sidecar","usage":{"cpu":"1","memory":"1Gi"}}]}`)
	m := parsePodMetricsJSON(raw)
	if m["app"].CPUMilli != 250 || m["app"].MemoryBytes != 64*1024*1024 {
		t.Fatalf("unexpected app metrics: %+v", m["app"])
	}
	if m["sidecar"].CPUMilli != 1000 || m["sidecar"].MemoryBytes != 1024*1024*1024 {
		t.Fatalf("unexpected sidecar metrics: %+v", m["sidecar"])
	}

	listRaw := []byte(`{"items":[
		{"containers":[{"name":"app","usage":{"cpu":"100m","memory":"100Mi"}}]},
		{"containers":[{"name":"app","usage":{"cpu":"300m","memory":"300Mi"}}]}
	]}`)
	avg := parsePodMetricsListJSON(listRaw)
	if avg["app"].CPUMilli != 200 || avg["app"].MemoryBytes != 200*1024*1024 {
		t.Fatalf("unexpected averaged metrics: %+v", avg["app"])
	}
}
