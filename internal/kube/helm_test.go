package kube

import (
	"strings"
	"testing"

	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
)

func sampleHelmRelease(rev int, status string, replicas int) *helmRelease {
	return &helmRelease{
		Name:      "webapp",
		Namespace: "prod",
		Version:   rev,
		Info: &helmInfo{
			FirstDeployed: "2026-01-01T00:00:00Z",
			LastDeployed:  "2026-01-02T00:00:00Z",
			Status:        status,
			Description:   "Install complete",
			Notes:         "Visit http://webapp.local",
		},
		Config: map[string]any{
			"replicaCount": replicas,
		},
		Chart: &helmChart{
			Metadata: &helmChartMeta{
				Name:       "webapp",
				Version:    "1.2.0",
				AppVersion: "2.4.1",
			},
			Values: map[string]any{
				"replicaCount": 1,
				"image": map[string]any{
					"repository": "nginx",
					"tag":        "1.27",
				},
			},
			Templates: []*helmFile{
				{
					Name: "templates/_helpers.tpl",
					Data: []byte(`{{- define "webapp.fullname" -}}{{ .Release.Name }}-{{ .Chart.Name }}{{- end -}}`),
				},
				{
					Name: "templates/deployment.yaml",
					Data: []byte(`apiVersion: apps/v1
kind: Deployment
metadata:
  name: {{ include "webapp.fullname" . }}
  namespace: {{ .Release.Namespace }}
spec:
  replicas: {{ .Values.replicaCount | default 1 }}
  template:
    spec:
      containers:
        - name: {{ .Chart.Name }}
          image: {{ printf "%s:%s" .Values.image.repository (.Values.image.tag | default .Chart.AppVersion) | quote }}`),
				},
				{
					Name: "templates/NOTES.txt",
					Data: []byte(`Release {{ .Release.Name }} rev {{ .Release.Revision }} running {{ .Values.replicaCount }} replicas.`),
				},
			},
		},
	}
}

func TestHelmEncodeDecodeAndTracker(t *testing.T) {
	r1 := sampleHelmRelease(1, "superseded", 1)
	r2 := sampleHelmRelease(2, "deployed", 3)

	b1, err := encodeHelmReleaseData(r1)
	if err != nil {
		t.Fatal(err)
	}
	b2, err := encodeHelmReleaseData(r2)
	if err != nil {
		t.Fatal(err)
	}

	dec2, err := decodeHelmReleaseData(b2)
	if err != nil {
		t.Fatal(err)
	}
	if dec2.Name != "webapp" || dec2.Version != 2 || dec2.chartFullName() != "webapp-1.2.0" {
		t.Fatalf("unexpected decoded release: %+v", dec2)
	}

	tr := newHelmTracker()
	sec1 := &corev1.Secret{
		ObjectMeta: metav1.ObjectMeta{Name: helmSecretName("webapp", 1), Namespace: "prod"},
		Data:       map[string][]byte{"release": b1},
	}
	sec2 := &corev1.Secret{
		ObjectMeta: metav1.ObjectMeta{Name: helmSecretName("webapp", 2), Namespace: "prod"},
		Data:       map[string][]byte{"release": b2},
	}
	tr.putSecret(sec1)
	tr.putSecret(sec2)

	rows := tr.allLatestRows()
	if len(rows) != 1 {
		t.Fatalf("expected 1 deduplicated release row, got %d", len(rows))
	}
	if rows[0].Cells[1] != "2" || rows[0].Cells[2] != "deployed" || rows[0].Cells[3] != "webapp-1.2.0" {
		t.Fatalf("unexpected latest row cells: %+v", rows[0].Cells)
	}

	// Deleting revision 2 falls back to revision 1; deleting revision 1 removes the row.
	tr.deleteSecret(sec2)
	row1, ok := tr.latestRow("prod/webapp")
	if !ok || row1.Cells[1] != "1" {
		t.Fatalf("expected fallback to revision 1, got %v %+v", ok, row1)
	}
	tr.deleteSecret(sec1)
	if _, ok := tr.latestRow("prod/webapp"); ok {
		t.Fatal("expected release row removed after all secrets deleted")
	}
}

func TestRenderHelmChartAndCoalesceValues(t *testing.T) {
	rel := sampleHelmRelease(1, "deployed", 1)
	override := map[string]any{
		"replicaCount": 5,
		"image": map[string]any{
			"tag": "1.28-alpine",
		},
	}
	merged := coalesceValues(rel.Chart.Values, override)
	img, _ := toStringAnyMap(merged["image"])
	if img["repository"] != "nginx" || img["tag"] != "1.28-alpine" || merged["replicaCount"] != 5 {
		t.Fatalf("unexpected merged values: %+v", merged)
	}

	manifest, notes, err := renderHelmChart(rel.Chart, "webapp", "prod", 2, override, "v1.31.0", nil)
	if err != nil {
		t.Fatal(err)
	}
	for _, want := range []string{"name: webapp-webapp", "replicas: 5", `image: "nginx:1.28-alpine"`} {
		if !strings.Contains(manifest, want) {
			t.Fatalf("rendered manifest missing %q:\n%s", want, manifest)
		}
	}
	if !strings.Contains(notes, "rev 2 running 5 replicas") {
		t.Fatalf("unexpected rendered notes: %q", notes)
	}
}
