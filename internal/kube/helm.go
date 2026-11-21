package kube

import (
	"bytes"
	"compress/gzip"
	"context"
	"crypto/sha1"
	"crypto/sha256"
	"encoding/base32"
	"encoding/base64"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"hash/adler32"
	"io"
	"math"
	"net/url"
	"path"
	"path/filepath"
	"reflect"
	"regexp"
	"sort"
	"strconv"
	"strings"
	"text/template"
	"time"

	corev1 "k8s.io/api/core/v1"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"k8s.io/apimachinery/pkg/watch"
	"sigs.k8s.io/yaml"
)

const (
	HelmGroup    = "helm.sh"
	HelmVersion  = "v1"
	HelmResource = "releases"
	HelmKind     = "Release"

	maxHelmReleaseBytes = 64 << 20 // 64 MiB safety cap for gzip decompression
)

func isHelmReleaseRef(ref ResourceRef) bool {
	return ref.Group == HelmGroup && ref.Resource == HelmResource
}

// HelmRevisionInfo summarizes one revision in a Helm release's history.
type HelmRevisionInfo struct {
	Revision    int    `json:"revision"`
	Updated     string `json:"updated"`
	Status      string `json:"status"`
	Chart       string `json:"chart"`
	AppVersion  string `json:"appVersion"`
	Description string `json:"description"`
}

// HelmReleaseDetail bundles full inspection data for a selected Helm release.
type HelmReleaseDetail struct {
	Name          string             `json:"name"`
	Namespace     string             `json:"namespace"`
	Revision      int                `json:"revision"`
	Status        string             `json:"status"`
	Description   string             `json:"description"`
	FirstDeployed string             `json:"firstDeployed"`
	LastDeployed  string             `json:"lastDeployed"`
	Chart         string             `json:"chart"`
	ChartVersion  string             `json:"chartVersion"`
	AppVersion    string             `json:"appVersion"`
	Notes         string             `json:"notes"`
	UserValues    string             `json:"userValues"`
	MergedValues  string             `json:"mergedValues"`
	Manifest      string             `json:"manifest"`
	History       []HelmRevisionInfo `json:"history"`
}

// Internal wire structures matching Helm v3/v4 in-cluster release JSON.
type helmRelease struct {
	Name      string            `json:"name"`
	Info      *helmInfo         `json:"info,omitempty"`
	Chart     *helmChart        `json:"chart,omitempty"`
	Config    map[string]any    `json:"config,omitempty"`
	Manifest  string            `json:"manifest,omitempty"`
	Hooks     []any             `json:"hooks,omitempty"`
	Version   int               `json:"version,omitempty"`
	Namespace string            `json:"namespace,omitempty"`
	Labels    map[string]string `json:"labels,omitempty"`
}

type helmInfo struct {
	FirstDeployed string `json:"first_deployed,omitempty"`
	LastDeployed  string `json:"last_deployed,omitempty"`
	Deleted       string `json:"deleted,omitempty"`
	Description   string `json:"description,omitempty"`
	Status        string `json:"status,omitempty"`
	Notes         string `json:"notes,omitempty"`
}

type helmChart struct {
	Metadata     *helmChartMeta `json:"metadata,omitempty"`
	Lock         any            `json:"lock,omitempty"`
	Templates    []*helmFile    `json:"templates,omitempty"`
	Values       map[string]any `json:"values,omitempty"`
	Schema       []byte         `json:"schema,omitempty"`
	Files        []*helmFile    `json:"files,omitempty"`
	Dependencies []*helmChart   `json:"dependencies,omitempty"`
}

type helmChartMeta struct {
	Name         string            `json:"name,omitempty"`
	Home         string            `json:"home,omitempty"`
	Sources      []string          `json:"sources,omitempty"`
	Version      string            `json:"version,omitempty"`
	Description  string            `json:"description,omitempty"`
	Keywords     []string          `json:"keywords,omitempty"`
	Maintainers  []any             `json:"maintainers,omitempty"`
	Icon         string            `json:"icon,omitempty"`
	APIVersion   string            `json:"apiVersion,omitempty"`
	Condition    string            `json:"condition,omitempty"`
	Tags         string            `json:"tags,omitempty"`
	AppVersion   string            `json:"appVersion,omitempty"`
	Deprecated   bool              `json:"deprecated,omitempty"`
	Annotations  map[string]string `json:"annotations,omitempty"`
	KubeVersion  string            `json:"kubeVersion,omitempty"`
	Dependencies []any             `json:"dependencies,omitempty"`
	Type         string            `json:"type,omitempty"`
}

type helmFile struct {
	Name string `json:"name"`
	Data []byte `json:"data"`
}

var helmColumns = []Column{
	{Name: "Name", Type: "string"},
	{Name: "Revision", Type: "integer"},
	{Name: "Status", Type: "string"},
	{Name: "Chart", Type: "string"},
	{Name: "App Version", Type: "string"},
}

// decodeHelmReleaseData decodes a Helm release payload from Secret.Data["release"]
// (or ConfigMap.Data["release"]), handling base64 + optional gzip layers.
func decodeHelmReleaseData(raw []byte) (*helmRelease, error) {
	b := bytes.TrimSpace(raw)
	if len(b) == 0 {
		return nil, errors.New("empty helm release payload")
	}
	if len(b) < 2 || b[0] != 0x1f || b[1] != 0x8b {
		decoded, err := base64.StdEncoding.DecodeString(string(b))
		if err != nil {
			return nil, fmt.Errorf("base64 decode helm release: %w", err)
		}
		b = decoded
	}
	if len(b) >= 2 && b[0] == 0x1f && b[1] == 0x8b {
		zr, err := gzip.NewReader(bytes.NewReader(b))
		if err != nil {
			return nil, fmt.Errorf("gunzip helm release: %w", err)
		}
		defer zr.Close()
		b, err = io.ReadAll(io.LimitReader(zr, maxHelmReleaseBytes))
		if err != nil {
			return nil, fmt.Errorf("read gunzipped helm release: %w", err)
		}
	}
	var rel helmRelease
	if err := json.Unmarshal(b, &rel); err != nil {
		return nil, fmt.Errorf("unmarshal helm release: %w", err)
	}
	return &rel, nil
}

// encodeHelmReleaseData serializes a Helm release into the standard
// base64(gzip(json)) byte payload stored in Secret.Data["release"].
func encodeHelmReleaseData(rel *helmRelease) ([]byte, error) {
	rawJSON, err := json.Marshal(rel)
	if err != nil {
		return nil, err
	}
	var buf bytes.Buffer
	zw := gzip.NewWriter(&buf)
	if _, err := zw.Write(rawJSON); err != nil {
		_ = zw.Close()
		return nil, err
	}
	if err := zw.Close(); err != nil {
		return nil, err
	}
	return []byte(base64.StdEncoding.EncodeToString(buf.Bytes())), nil
}

func helmSecretName(releaseName string, revision int) string {
	return fmt.Sprintf("sh.helm.release.v1.%s.v%d", releaseName, revision)
}

func (r *helmRelease) chartFullName() string {
	if r.Chart == nil || r.Chart.Metadata == nil {
		return ""
	}
	m := r.Chart.Metadata
	if m.Name != "" && m.Version != "" {
		return m.Name + "-" + m.Version
	}
	return m.Name
}

func (r *helmRelease) chartName() string {
	if r.Chart == nil || r.Chart.Metadata == nil {
		return ""
	}
	return r.Chart.Metadata.Name
}

func (r *helmRelease) chartVersion() string {
	if r.Chart == nil || r.Chart.Metadata == nil {
		return ""
	}
	return r.Chart.Metadata.Version
}

func (r *helmRelease) appVersion() string {
	if r.Chart == nil || r.Chart.Metadata == nil {
		return ""
	}
	return r.Chart.Metadata.AppVersion
}

func (r *helmRelease) status() string {
	if r.Info == nil || r.Info.Status == "" {
		return "unknown"
	}
	return r.Info.Status
}

func (r *helmRelease) lastDeployed() string {
	if r.Info == nil {
		return ""
	}
	if r.Info.LastDeployed != "" {
		return r.Info.LastDeployed
	}
	return r.Info.FirstDeployed
}

func helmReleaseRow(rel *helmRelease, fallbackNS string) Row {
	ns := rel.Namespace
	if ns == "" {
		ns = fallbackNS
	}
	key := ns + "/" + rel.Name
	created := ""
	if ts := rel.lastDeployed(); ts != "" {
		if t, err := time.Parse(time.RFC3339Nano, ts); err == nil {
			created = t.UTC().Format("2006-01-02T15:04:05Z")
		} else if t, err := time.Parse(time.RFC3339, ts); err == nil {
			created = t.UTC().Format("2006-01-02T15:04:05Z")
		}
	}
	rev := strconv.Itoa(rel.Version)
	return Row{
		Key:       key,
		UID:       key,
		Name:      rel.Name,
		Namespace: ns,
		Created:   created,
		Cells: []string{
			rel.Name,
			rev,
			rel.status(),
			rel.chartFullName(),
			rel.appVersion(),
		},
	}
}

// helmTracker maintains all decoded revisions per secret and projects the
// latest revision per (namespace, releaseName) into table rows.
type helmTracker struct {
	bySecret map[string]*helmRelease // key: secretNamespace + "/" + secretName
}

func newHelmTracker() *helmTracker {
	return &helmTracker{bySecret: map[string]*helmRelease{}}
}

func (h *helmTracker) putSecret(sec *corev1.Secret) (releaseKey string, ok bool) {
	raw := sec.Data["release"]
	if len(raw) == 0 {
		return "", false
	}
	rel, err := decodeHelmReleaseData(raw)
	if err != nil || rel.Name == "" {
		return "", false
	}
	if rel.Namespace == "" {
		rel.Namespace = sec.Namespace
	}
	sKey := sec.Namespace + "/" + sec.Name
	h.bySecret[sKey] = rel
	return rel.Namespace + "/" + rel.Name, true
}

func (h *helmTracker) deleteSecret(sec *corev1.Secret) (releaseKey string, ok bool) {
	sKey := sec.Namespace + "/" + sec.Name
	prev, exists := h.bySecret[sKey]
	if !exists {
		if name := sec.Labels["name"]; name != "" {
			return sec.Namespace + "/" + name, true
		}
		return "", false
	}
	delete(h.bySecret, sKey)
	return prev.Namespace + "/" + prev.Name, true
}

func (h *helmTracker) latestRow(releaseKey string) (Row, bool) {
	var best *helmRelease
	for _, rel := range h.bySecret {
		if rel.Namespace+"/"+rel.Name != releaseKey {
			continue
		}
		if best == nil || rel.Version > best.Version {
			best = rel
		}
	}
	if best == nil {
		return Row{}, false
	}
	return helmReleaseRow(best, best.Namespace), true
}

func (h *helmTracker) allLatestRows() []Row {
	latest := map[string]*helmRelease{}
	for _, rel := range h.bySecret {
		k := rel.Namespace + "/" + rel.Name
		if cur, ok := latest[k]; !ok || rel.Version > cur.Version {
			latest[k] = rel
		}
	}
	rows := make([]Row, 0, len(latest))
	for _, rel := range latest {
		rows = append(rows, helmReleaseRow(rel, rel.Namespace))
	}
	sort.Slice(rows, func(i, j int) bool {
		if rows[i].Namespace != rows[j].Namespace {
			return rows[i].Namespace < rows[j].Namespace
		}
		return rows[i].Name < rows[j].Name
	})
	return rows
}

// runHelmWatch lists and watches Helm release Secrets, projecting the latest
// revision of each release into WatchEvents.
func (c *Clients) runHelmWatch(ctx context.Context, namespace string, opts watchOptions, emit func(WatchEvent)) {
	b := newBatcher()
	done := make(chan struct{})
	go func() {
		t := time.NewTicker(flushInterval)
		defer t.Stop()
		for {
			select {
			case <-ctx.Done():
				close(done)
				return
			case <-t.C:
				if ev, ok := b.take(); ok {
					emit(ev)
				}
			}
		}
	}()
	defer func() { <-done }()

	tracker := newHelmTracker()
	rv := ""
	backoff := time.Second
	sleep := func() bool {
		select {
		case <-ctx.Done():
			return false
		case <-time.After(backoff):
		}
		if backoff < 30*time.Second {
			backoff *= 2
		}
		return true
	}

	labelSel := "owner=helm"
	if opts.labelSelector != "" {
		labelSel = labelSel + "," + opts.labelSelector
	}

	for ctx.Err() == nil {
		if rv == "" {
			list, err := c.Typed.CoreV1().Secrets(namespace).List(ctx, metav1.ListOptions{
				LabelSelector: labelSel,
				FieldSelector: opts.fieldSelector,
			})
			if err != nil {
				if ctx.Err() != nil {
					return
				}
				b.fail(err.Error())
				if !sleep() {
					return
				}
				continue
			}
			tracker = newHelmTracker()
			for i := range list.Items {
				tracker.putSecret(&list.Items[i])
			}
			rv = list.ResourceVersion
			b.doReset(helmColumns, tracker.allLatestRows())
			backoff = time.Second
		}

		timeout := int64(300)
		w, err := c.Typed.CoreV1().Secrets(namespace).Watch(ctx, metav1.ListOptions{
			LabelSelector:       labelSel,
			FieldSelector:       opts.fieldSelector,
			ResourceVersion:     rv,
			AllowWatchBookmarks: true,
			TimeoutSeconds:      &timeout,
		})
		if err != nil {
			if ctx.Err() != nil {
				return
			}
			if apierrors.IsResourceExpired(err) || IsGone(err) {
				rv = ""
				continue
			}
			b.fail(err.Error())
			rv = ""
			if !sleep() {
				return
			}
			continue
		}

		expired := false
		for ev := range w.ResultChan() {
			if ctx.Err() != nil {
				w.Stop()
				return
			}
			if ev.Type == watch.Error {
				if st, ok := ev.Object.(*metav1.Status); ok {
					if st.Code == 410 || st.Reason == metav1.StatusReasonExpired {
						expired = true
					} else if st.Message != "" {
						b.fail(st.Message)
					}
				}
				break
			}
			sec, ok := ev.Object.(*corev1.Secret)
			if !ok {
				continue
			}
			if sec.ResourceVersion != "" {
				rv = sec.ResourceVersion
			}
			if ev.Type == watch.Bookmark {
				continue
			}
			switch ev.Type {
			case watch.Added, watch.Modified:
				if relKey, ok := tracker.putSecret(sec); ok {
					if row, exists := tracker.latestRow(relKey); exists {
						b.upsert(row)
					}
				}
			case watch.Deleted:
				if relKey, ok := tracker.deleteSecret(sec); ok {
					if row, exists := tracker.latestRow(relKey); exists {
						b.upsert(row)
					} else {
						b.remove(relKey)
					}
				}
			}
		}
		w.Stop()
		if expired {
			rv = ""
		}
	}
}

type storedHelmRecord struct {
	secret *corev1.Secret
	rel    *helmRelease
}

func (s *Service) listHelmReleaseRecords(ctx context.Context, c *Clients, namespace, name string) ([]storedHelmRecord, error) {
	sel := fmt.Sprintf("owner=helm,name=%s", name)
	list, err := c.Typed.CoreV1().Secrets(namespace).List(ctx, metav1.ListOptions{LabelSelector: sel})
	if err != nil {
		return nil, err
	}
	var records []storedHelmRecord
	for i := range list.Items {
		sec := &list.Items[i]
		raw := sec.Data["release"]
		if len(raw) == 0 {
			continue
		}
		rel, derr := decodeHelmReleaseData(raw)
		if derr != nil {
			continue
		}
		if rel.Namespace == "" {
			rel.Namespace = sec.Namespace
		}
		records = append(records, storedHelmRecord{secret: sec, rel: rel})
	}
	sort.Slice(records, func(i, j int) bool {
		return records[i].rel.Version > records[j].rel.Version
	})
	return records, nil
}

// GetHelmRelease returns the detail, user/merged values, rendered manifest,
// and revision history for a Helm release.
func (s *Service) GetHelmRelease(contextName, namespace, name string) (HelmReleaseDetail, error) {
	c, err := s.mgr.For(contextName)
	if err != nil {
		return HelmReleaseDetail{}, err
	}
	records, err := s.listHelmReleaseRecords(s.baseCtx, c, namespace, name)
	if err != nil {
		return HelmReleaseDetail{}, err
	}
	if len(records) == 0 {
		return HelmReleaseDetail{}, fmt.Errorf("helm release %q not found in namespace %q", name, namespace)
	}

	latest := records[0].rel
	history := make([]HelmRevisionInfo, 0, len(records))
	for _, rec := range records {
		r := rec.rel
		desc := ""
		if r.Info != nil {
			desc = r.Info.Description
		}
		history = append(history, HelmRevisionInfo{
			Revision:    r.Version,
			Updated:     r.lastDeployed(),
			Status:      r.status(),
			Chart:       r.chartFullName(),
			AppVersion:  r.appVersion(),
			Description: desc,
		})
	}

	userValues := ""
	if len(latest.Config) > 0 {
		if b, yerr := yaml.Marshal(latest.Config); yerr == nil {
			userValues = string(b)
		}
	}
	var defaultVals map[string]any
	if latest.Chart != nil {
		defaultVals = latest.Chart.Values
	}
	mergedMap := coalesceValues(defaultVals, latest.Config)
	mergedValues := ""
	if len(mergedMap) > 0 {
		if b, yerr := yaml.Marshal(mergedMap); yerr == nil {
			mergedValues = string(b)
		}
	}

	firstDep, lastDep, desc, notes := "", "", "", ""
	if latest.Info != nil {
		firstDep = latest.Info.FirstDeployed
		lastDep = latest.Info.LastDeployed
		desc = latest.Info.Description
		notes = latest.Info.Notes
	}

	return HelmReleaseDetail{
		Name:          latest.Name,
		Namespace:     namespace,
		Revision:      latest.Version,
		Status:        latest.status(),
		Description:   desc,
		FirstDeployed: firstDep,
		LastDeployed:  lastDep,
		Chart:         latest.chartName(),
		ChartVersion:  latest.chartVersion(),
		AppVersion:    latest.appVersion(),
		Notes:         notes,
		UserValues:    userValues,
		MergedValues:  mergedValues,
		Manifest:      strings.TrimSpace(latest.Manifest),
		History:       history,
	}, nil
}

// RollbackHelmRelease rolls a Helm release back to a previous revision by
// reconciling its rendered manifest and recording revision v<latest+1>.
func (s *Service) RollbackHelmRelease(contextName, namespace, name string, revision int) error {
	c, err := s.mgr.For(contextName)
	if err != nil {
		return err
	}
	records, err := s.listHelmReleaseRecords(s.baseCtx, c, namespace, name)
	if err != nil {
		return err
	}
	if len(records) == 0 {
		return fmt.Errorf("helm release %q not found", name)
	}
	latest := records[0].rel
	var target *helmRelease
	for _, rec := range records {
		if rec.rel.Version == revision {
			target = rec.rel
			break
		}
	}
	if target == nil {
		return fmt.Errorf("revision %d not found for release %q", revision, name)
	}

	if err := s.reconcileHelmManifest(c, namespace, latest.Manifest, target.Manifest); err != nil {
		return err
	}

	nextRev := latest.Version + 1
	now := time.Now().UTC().Format(time.RFC3339)
	firstDep := now
	if latest.Info != nil && latest.Info.FirstDeployed != "" {
		firstDep = latest.Info.FirstDeployed
	}
	notes := ""
	if target.Info != nil {
		notes = target.Info.Notes
	}

	newRel := &helmRelease{
		Name:      name,
		Namespace: namespace,
		Version:   nextRev,
		Chart:     target.Chart,
		Config:    target.Config,
		Manifest:  target.Manifest,
		Hooks:     target.Hooks,
		Labels:    target.Labels,
		Info: &helmInfo{
			FirstDeployed: firstDep,
			LastDeployed:  now,
			Status:        "deployed",
			Description:   fmt.Sprintf("Rollback to %d", revision),
			Notes:         notes,
		},
	}
	return s.commitHelmRevision(c, namespace, records, newRel)
}

// UpgradeHelmRelease re-renders the release's chart templates with edited YAML
// values, reconciles the cluster resources via Server-Side Apply, and records
// revision v<latest+1>.
func (s *Service) UpgradeHelmRelease(contextName, namespace, name, valuesYAML string) error {
	c, err := s.mgr.For(contextName)
	if err != nil {
		return err
	}
	newConfig := map[string]any{}
	if strings.TrimSpace(valuesYAML) != "" {
		if err := yaml.Unmarshal([]byte(valuesYAML), &newConfig); err != nil {
			return fmt.Errorf("invalid values YAML: %w", err)
		}
		if newConfig == nil {
			newConfig = map[string]any{}
		}
	}

	records, err := s.listHelmReleaseRecords(s.baseCtx, c, namespace, name)
	if err != nil {
		return err
	}
	if len(records) == 0 {
		return fmt.Errorf("helm release %q not found", name)
	}
	latest := records[0].rel
	if latest.Chart == nil {
		return fmt.Errorf("release %q has no embedded chart metadata", name)
	}

	nextRev := latest.Version + 1
	kubeVer := "v1.30.0"
	if v, verr := c.Discovery.ServerVersion(); verr == nil && v != nil && v.GitVersion != "" {
		kubeVer = v.GitVersion
	}

	newManifest, newNotes, err := renderHelmChart(latest.Chart, name, namespace, nextRev, newConfig, kubeVer, c)
	if err != nil {
		return fmt.Errorf("render chart templates: %w", err)
	}

	if err := s.reconcileHelmManifest(c, namespace, latest.Manifest, newManifest); err != nil {
		return err
	}

	now := time.Now().UTC().Format(time.RFC3339)
	firstDep := now
	if latest.Info != nil && latest.Info.FirstDeployed != "" {
		firstDep = latest.Info.FirstDeployed
	}
	if newNotes == "" && latest.Info != nil {
		newNotes = latest.Info.Notes
	}

	newRel := &helmRelease{
		Name:      name,
		Namespace: namespace,
		Version:   nextRev,
		Chart:     latest.Chart,
		Config:    newConfig,
		Manifest:  newManifest,
		Hooks:     latest.Hooks,
		Labels:    latest.Labels,
		Info: &helmInfo{
			FirstDeployed: firstDep,
			LastDeployed:  now,
			Status:        "deployed",
			Description:   "Upgrade complete",
			Notes:         newNotes,
		},
	}
	return s.commitHelmRevision(c, namespace, records, newRel)
}

// UninstallHelmRelease deletes all Kubernetes objects managed by the release's
// latest manifest and removes its release history Secrets.
func (s *Service) UninstallHelmRelease(contextName, namespace, name string) error {
	c, err := s.mgr.For(contextName)
	if err != nil {
		return err
	}
	records, err := s.listHelmReleaseRecords(s.baseCtx, c, namespace, name)
	if err != nil {
		return err
	}
	if len(records) == 0 {
		return fmt.Errorf("helm release %q not found", name)
	}

	var active *helmRelease
	for _, rec := range records {
		if rec.rel.status() == "deployed" {
			active = rec.rel
			break
		}
	}
	if active == nil {
		active = records[0].rel
	}

	if strings.TrimSpace(active.Manifest) != "" {
		objs, _ := DecodeManifest(active.Manifest)
		for i := len(objs) - 1; i >= 0; i-- {
			_ = s.deleteUnstructuredObject(c, namespace, objs[i])
		}
	}

	for _, rec := range records {
		if err := c.Typed.CoreV1().Secrets(namespace).Delete(s.baseCtx, rec.secret.Name, metav1.DeleteOptions{}); err != nil && !apierrors.IsNotFound(err) {
			return err
		}
	}
	return nil
}

func (s *Service) reconcileHelmManifest(c *Clients, namespace, oldManifest, newManifest string) error {
	var newObjs []*unstructured.Unstructured
	if strings.TrimSpace(newManifest) != "" {
		var err error
		newObjs, err = DecodeManifest(newManifest)
		if err != nil {
			return fmt.Errorf("decode manifest: %w", err)
		}
	}
	newKeys := make(map[string]struct{}, len(newObjs))
	for _, obj := range newObjs {
		res, err := s.applyObject(c, obj, namespace)
		if err != nil {
			return err
		}
		newKeys[objectIdentityKey(obj.GroupVersionKind().Group, res.Kind, res.Namespace, res.Name)] = struct{}{}
	}

	if strings.TrimSpace(oldManifest) != "" {
		oldObjs, _ := DecodeManifest(oldManifest)
		for i := len(oldObjs) - 1; i >= 0; i-- {
			oldObj := oldObjs[i]
			gvk := oldObj.GroupVersionKind()
			ns := oldObj.GetNamespace()
			if ns == "" {
				ns = namespace
			}
			k := objectIdentityKey(gvk.Group, gvk.Kind, ns, oldObj.GetName())
			if _, keep := newKeys[k]; !keep {
				_ = s.deleteUnstructuredObject(c, namespace, oldObj)
			}
		}
	}
	return nil
}

func objectIdentityKey(group, kind, namespace, name string) string {
	return group + "/" + kind + "/" + namespace + "/" + name
}

func (s *Service) deleteUnstructuredObject(c *Clients, defaultNamespace string, obj *unstructured.Unstructured) error {
	gvk := obj.GroupVersionKind()
	mapping, err := c.Mapper.RESTMapping(gvk.GroupKind(), gvk.Version)
	if err != nil {
		c.Mapper.Reset()
		if mapping, err = c.Mapper.RESTMapping(gvk.GroupKind(), gvk.Version); err != nil {
			return err
		}
	}
	namespaced := mapping.Scope.Name() == "namespace"
	ns := obj.GetNamespace()
	if namespaced && ns == "" {
		ns = defaultNamespace
	}
	if !namespaced {
		ns = ""
	}
	ref := ResourceRef{
		Group:      mapping.Resource.Group,
		Version:    mapping.Resource.Version,
		Resource:   mapping.Resource.Resource,
		Kind:       gvk.Kind,
		Namespaced: namespaced,
	}
	err = s.resourceClient(c, ref, ns).Delete(s.baseCtx, obj.GetName(), metav1.DeleteOptions{})
	if apierrors.IsNotFound(err) {
		return nil
	}
	return err
}

func (s *Service) commitHelmRevision(c *Clients, namespace string, existing []storedHelmRecord, newRel *helmRelease) error {
	for _, rec := range existing {
		if rec.rel.status() != "deployed" {
			continue
		}
		if rec.rel.Info == nil {
			rec.rel.Info = &helmInfo{}
		}
		rec.rel.Info.Status = "superseded"
		if enc, err := encodeHelmReleaseData(rec.rel); err == nil {
			secCopy := rec.secret.DeepCopy()
			if secCopy.Labels == nil {
				secCopy.Labels = map[string]string{}
			}
			secCopy.Labels["status"] = "superseded"
			secCopy.Data["release"] = enc
			_, _ = c.Typed.CoreV1().Secrets(namespace).Update(s.baseCtx, secCopy, metav1.UpdateOptions{})
		}
	}

	encoded, err := encodeHelmReleaseData(newRel)
	if err != nil {
		return err
	}
	sec := &corev1.Secret{
		ObjectMeta: metav1.ObjectMeta{
			Name:      helmSecretName(newRel.Name, newRel.Version),
			Namespace: namespace,
			Labels: map[string]string{
				"owner":      "helm",
				"name":       newRel.Name,
				"version":    strconv.Itoa(newRel.Version),
				"status":     newRel.status(),
				"modifiedAt": strconv.FormatInt(time.Now().Unix(), 10),
			},
		},
		Type: corev1.SecretType("helm.sh/release.v1"),
		Data: map[string][]byte{
			"release": encoded,
		},
	}
	_, err = c.Typed.CoreV1().Secrets(namespace).Create(s.baseCtx, sec, metav1.CreateOptions{})
	return err
}

// coalesceValues deeply merges base chart defaults with user override values
// without mutating either input map.
func coalesceValues(base, override map[string]any) map[string]any {
	out := deepCopyMap(base)
	for k, v := range override {
		if v == nil {
			delete(out, k)
			continue
		}
		if vm, ok := toStringAnyMap(v); ok {
			if bm, ok := toStringAnyMap(out[k]); ok {
				out[k] = coalesceValues(bm, vm)
				continue
			}
		}
		out[k] = deepCopyValue(v)
	}
	return out
}

func toStringAnyMap(v any) (map[string]any, bool) {
	if m, ok := v.(map[string]any); ok {
		return m, true
	}
	return nil, false
}

func deepCopyMap(m map[string]any) map[string]any {
	if m == nil {
		return map[string]any{}
	}
	out := make(map[string]any, len(m))
	for k, v := range m {
		out[k] = deepCopyValue(v)
	}
	return out
}

func deepCopyValue(v any) any {
	switch val := v.(type) {
	case map[string]any:
		return deepCopyMap(val)
	case []any:
		cp := make([]any, len(val))
		for i := range val {
			cp[i] = deepCopyValue(val[i])
		}
		return cp
	default:
		return val
	}
}

// helmFiles implements Helm's .Files template object.
type helmFiles struct {
	byName map[string][]byte
}

func newHelmFiles(files []*helmFile) helmFiles {
	m := make(map[string][]byte, len(files))
	for _, f := range files {
		if f != nil {
			m[f.Name] = f.Data
		}
	}
	return helmFiles{byName: m}
}

func (f helmFiles) Get(name string) string {
	return string(f.byName[name])
}

func (f helmFiles) GetBytes(name string) []byte {
	return f.byName[name]
}

func (f helmFiles) Glob(pattern string) map[string][]byte {
	out := map[string][]byte{}
	for k, v := range f.byName {
		if matched, _ := path.Match(pattern, k); matched {
			out[k] = v
		}
	}
	return out
}

func (f helmFiles) Lines(name string) []string {
	s := string(f.byName[name])
	if s == "" {
		return nil
	}
	return strings.Split(s, "\n")
}

func (f helmFiles) AsConfig() string {
	m := map[string]string{}
	for k, v := range f.byName {
		m[path.Base(k)] = string(v)
	}
	b, _ := yaml.Marshal(m)
	return strings.TrimSpace(string(b))
}

func (f helmFiles) AsSecrets() string {
	m := map[string]string{}
	for k, v := range f.byName {
		m[path.Base(k)] = base64.StdEncoding.EncodeToString(v)
	}
	b, _ := yaml.Marshal(m)
	return strings.TrimSpace(string(b))
}

// renderHelmChart executes the chart's embedded Go templates against merged values.
func renderHelmChart(ch *helmChart, releaseName, namespace string, revision int, userConfig map[string]any, kubeVer string, c *Clients) (manifest string, notes string, err error) {
	if ch == nil {
		return "", "", errors.New("nil chart")
	}

	t := template.New("gotpl")
	includedNames := map[string]int{}

	var funcMap template.FuncMap
	funcMap = helmTemplateFuncs(t, includedNames, &funcMap, c)
	t.Funcs(funcMap)

	type chartRenderJob struct {
		chartPath string
		chart     *helmChart
		values    map[string]any
		file      *helmFile
	}

	mergedRoot := coalesceValues(ch.Values, userConfig)
	var jobs []chartRenderJob

	var collect func(prefix string, cur *helmChart, vals map[string]any) error
	collect = func(prefix string, cur *helmChart, vals map[string]any) error {
		if cur == nil {
			return nil
		}
		cName := prefix
		if cName == "" && cur.Metadata != nil {
			cName = cur.Metadata.Name
		}
		if cName == "" {
			cName = "chart"
		}
		for _, f := range cur.Templates {
			if f == nil {
				continue
			}
			fullName := path.Join(cName, f.Name)
			if _, perr := t.New(fullName).Parse(string(f.Data)); perr != nil {
				return fmt.Errorf("parse %s: %w", fullName, perr)
			}
			base := path.Base(f.Name)
			if strings.HasPrefix(base, "_") {
				continue
			}
			jobs = append(jobs, chartRenderJob{
				chartPath: cName,
				chart:     cur,
				values:    vals,
				file:      f,
			})
		}
		globalVals, _ := toStringAnyMap(vals["global"])
		for _, dep := range cur.Dependencies {
			if dep == nil || dep.Metadata == nil {
				continue
			}
			depName := dep.Metadata.Name
			subOverride, _ := toStringAnyMap(vals[depName])
			depBase := coalesceValues(dep.Values, subOverride)
			if len(globalVals) > 0 {
				existingGlobal, _ := toStringAnyMap(depBase["global"])
				depBase["global"] = coalesceValues(existingGlobal, globalVals)
			}
			if err := collect(path.Join(cName, "charts", depName), dep, depBase); err != nil {
				return err
			}
		}
		return nil
	}

	if err := collect("", ch, mergedRoot); err != nil {
		return "", "", err
	}

	sort.SliceStable(jobs, func(i, j int) bool {
		return path.Join(jobs[i].chartPath, jobs[i].file.Name) < path.Join(jobs[j].chartPath, jobs[j].file.Name)
	})

	major, minor := "1", "30"
	cleanVer := strings.TrimPrefix(kubeVer, "v")
	if parts := strings.Split(cleanVer, "."); len(parts) >= 2 {
		major = parts[0]
		minor = parts[1]
	}

	var manifestBuf strings.Builder
	var notesBuf strings.Builder

	for _, job := range jobs {
		fullName := path.Join(job.chartPath, job.file.Name)
		meta := job.chart.Metadata
		if meta == nil {
			meta = &helmChartMeta{Name: job.chartPath}
		}
		data := map[string]any{
			"Values": job.values,
			"Release": map[string]any{
				"Name":      releaseName,
				"Namespace": namespace,
				"Revision":  revision,
				"IsUpgrade": true,
				"IsInstall": false,
				"Service":   "Helm",
			},
			"Chart": struct {
				*helmChartMeta
				IsRoot bool
			}{
				helmChartMeta: meta,
				IsRoot:        job.chart == ch,
			},
			"Files": newHelmFiles(job.chart.Files),
			"Capabilities": map[string]any{
				"KubeVersion": map[string]any{
					"Version":    kubeVer,
					"GitVersion": kubeVer,
					"Major":      major,
					"Minor":      minor,
				},
				"HelmVersion": map[string]any{
					"Version": "v3.17.0",
				},
			},
			"Template": map[string]any{
				"Name":     fullName,
				"BasePath": path.Join(job.chartPath, "templates"),
			},
		}

		var out bytes.Buffer
		if err := t.ExecuteTemplate(&out, fullName, data); err != nil {
			return "", "", fmt.Errorf("render %s: %w", fullName, err)
		}
		rendered := strings.TrimSpace(out.String())
		if rendered == "" {
			continue
		}
		base := path.Base(job.file.Name)
		if strings.EqualFold(base, "NOTES.txt") {
			if job.chart == ch {
				notesBuf.WriteString(rendered)
			}
			continue
		}
		if !strings.HasSuffix(base, ".yaml") && !strings.HasSuffix(base, ".yml") {
			continue
		}
		fmt.Fprintf(&manifestBuf, "---\n# Source: %s\n%s\n", fullName, rendered)
	}

	return strings.TrimSpace(manifestBuf.String()), strings.TrimSpace(notesBuf.String()), nil
}

func helmTemplateFuncs(t *template.Template, includedNames map[string]int, funcMapPtr *template.FuncMap, c *Clients) template.FuncMap {
	toStr := func(v any) string {
		switch s := v.(type) {
		case nil:
			return ""
		case string:
			return s
		case []byte:
			return string(s)
		case fmt.Stringer:
			return s.String()
		default:
			return fmt.Sprintf("%v", v)
		}
	}
	toInt64 := func(v any) int64 {
		switch n := v.(type) {
		case int:
			return int64(n)
		case int64:
			return n
		case int32:
			return int64(n)
		case float64:
			return int64(n)
		case float32:
			return int64(n)
		case json.Number:
			i, _ := n.Int64()
			return i
		case string:
			i, _ := strconv.ParseInt(strings.TrimSpace(n), 10, 64)
			return i
		default:
			return 0
		}
	}
	toFloat64 := func(v any) float64 {
		switch n := v.(type) {
		case float64:
			return n
		case float32:
			return float64(n)
		case int:
			return float64(n)
		case int64:
			return float64(n)
		case string:
			f, _ := strconv.ParseFloat(strings.TrimSpace(n), 64)
			return f
		default:
			return 0
		}
	}
	isEmpty := func(v any) bool {
		if v == nil {
			return true
		}
		rv := reflect.ValueOf(v)
		switch rv.Kind() {
		case reflect.Bool:
			return !rv.Bool()
		case reflect.Int, reflect.Int8, reflect.Int16, reflect.Int32, reflect.Int64:
			return rv.Int() == 0
		case reflect.Uint, reflect.Uint8, reflect.Uint16, reflect.Uint32, reflect.Uint64:
			return rv.Uint() == 0
		case reflect.Float32, reflect.Float64:
			return rv.Float() == 0
		case reflect.String, reflect.Array, reflect.Slice, reflect.Map:
			return rv.Len() == 0
		case reflect.Ptr, reflect.Interface:
			return rv.IsNil()
		}
		return false
	}
	indentStr := func(spaces int, v string) string {
		pad := strings.Repeat(" ", spaces)
		return pad + strings.ReplaceAll(v, "\n", "\n"+pad)
	}
	toYAML := func(v any) (string, error) {
		b, err := yaml.Marshal(v)
		if err != nil {
			return "", err
		}
		return strings.TrimSuffix(string(b), "\n"), nil
	}
	toJSON := func(v any) (string, error) {
		b, err := json.Marshal(v)
		if err != nil {
			return "", err
		}
		return string(b), nil
	}

	return template.FuncMap{
		"include": func(name string, data any) (string, error) {
			if includedNames[name] > 100 {
				return "", fmt.Errorf("rendering template has a recursive reference name: %s", name)
			}
			includedNames[name]++
			var buf bytes.Buffer
			err := t.ExecuteTemplate(&buf, name, data)
			includedNames[name]--
			return buf.String(), err
		},
		"tpl": func(tplStr string, data any) (string, error) {
			if strings.TrimSpace(tplStr) == "" {
				return "", nil
			}
			sub, err := t.Clone()
			if err != nil {
				sub = template.New("gotpl").Funcs(*funcMapPtr)
			}
			sub, err = sub.New("inline-tpl").Parse(tplStr)
			if err != nil {
				return "", err
			}
			var buf bytes.Buffer
			if err := sub.ExecuteTemplate(&buf, "inline-tpl", data); err != nil {
				return "", err
			}
			return buf.String(), nil
		},
		"required": func(warn string, val any) (any, error) {
			if val == nil {
				return nil, errors.New(warn)
			}
			if s, ok := val.(string); ok && s == "" {
				return nil, errors.New(warn)
			}
			return val, nil
		},
		"fail": func(msg string) (string, error) {
			return "", errors.New(msg)
		},
		"toYaml": func(v any) string {
			s, _ := toYAML(v)
			return s
		},
		"mustToYaml": toYAML,
		"fromYaml": func(str string) map[string]any {
			m := map[string]any{}
			if err := yaml.Unmarshal([]byte(str), &m); err != nil {
				m["Error"] = err.Error()
			}
			return m
		},
		"toJson": func(v any) string {
			s, _ := toJSON(v)
			return s
		},
		"mustToJson": toJSON,
		"toPrettyJson": func(v any) string {
			b, _ := json.MarshalIndent(v, "", "  ")
			return string(b)
		},
		"fromJson": func(str string) any {
			var out any
			_ = json.Unmarshal([]byte(str), &out)
			return out
		},
		"indent":  indentStr,
		"nindent": func(spaces int, v string) string { return "\n" + indentStr(spaces, v) },
		"default": func(def any, given ...any) any {
			if len(given) == 0 || isEmpty(given[0]) {
				return def
			}
			return given[0]
		},
		"empty": isEmpty,
		"coalesce": func(vals ...any) any {
			for _, v := range vals {
				if !isEmpty(v) {
					return v
				}
			}
			return nil
		},
		"ternary": func(vt, vf any, cond bool) any {
			if cond {
				return vt
			}
			return vf
		},
		"quote": func(vals ...any) string {
			out := make([]string, 0, len(vals))
			for _, v := range vals {
				if v == nil {
					continue
				}
				out = append(out, fmt.Sprintf("%q", toStr(v)))
			}
			return strings.Join(out, " ")
		},
		"squote": func(vals ...any) string {
			out := make([]string, 0, len(vals))
			for _, v := range vals {
				if v == nil {
					continue
				}
				out = append(out, "'"+strings.ReplaceAll(toStr(v), "'", "''")+"'")
			}
			return strings.Join(out, " ")
		},
		"cat": func(vals ...any) string {
			out := make([]string, 0, len(vals))
			for _, v := range vals {
				if v == nil {
					continue
				}
				out = append(out, toStr(v))
			}
			return strings.Join(out, " ")
		},
		"trim":       strings.TrimSpace,
		"trimAll":    func(cut, s string) string { return strings.Trim(s, cut) },
		"trimSuffix": func(suf, s string) string { return strings.TrimSuffix(s, suf) },
		"trimPrefix": func(pre, s string) string { return strings.TrimPrefix(s, pre) },
		"trunc": func(n int, s string) string {
			if n < 0 && len(s)+n > 0 {
				return s[len(s)+n:]
			}
			if n >= 0 && len(s) > n {
				return s[:n]
			}
			return s
		},
		"abbrev": func(max int, s string) string {
			if max <= 3 || len(s) <= max {
				return s
			}
			return s[:max-3] + "..."
		},
		"lower":      strings.ToLower,
		"upper":      strings.ToUpper,
		"title":      strings.Title,
		"replace":    func(old, new, s string) string { return strings.ReplaceAll(s, old, new) },
		"contains":   func(sub, s string) bool { return strings.Contains(s, sub) },
		"hasPrefix":  func(pre, s string) bool { return strings.HasPrefix(s, pre) },
		"hasSuffix":  func(suf, s string) bool { return strings.HasSuffix(s, suf) },
		"repeat":     func(n int, s string) string { return strings.Repeat(s, n) },
		"nospace":    func(s string) string { return strings.Join(strings.Fields(s), "") },
		"splitList":  func(sep, s string) []string { return strings.Split(s, sep) },
		"split": func(sep, s string) map[string]string {
			parts := strings.Split(s, sep)
			res := make(map[string]string, len(parts))
			for i, p := range parts {
				res[fmt.Sprintf("_%d", i)] = p
			}
			return res
		},
		"join": func(sep string, v any) string {
			rv := reflect.ValueOf(v)
			if rv.Kind() != reflect.Slice && rv.Kind() != reflect.Array {
				return toStr(v)
			}
			out := make([]string, rv.Len())
			for i := 0; i < rv.Len(); i++ {
				out[i] = toStr(rv.Index(i).Interface())
			}
			return strings.Join(out, sep)
		},
		"sortAlpha": func(v any) []string {
			rv := reflect.ValueOf(v)
			if rv.Kind() != reflect.Slice && rv.Kind() != reflect.Array {
				return nil
			}
			out := make([]string, rv.Len())
			for i := 0; i < rv.Len(); i++ {
				out[i] = toStr(rv.Index(i).Interface())
			}
			sort.Strings(out)
			return out
		},
		"substr": func(start, end int, s string) string {
			if start < 0 {
				start = 0
			}
			if end < 0 || end > len(s) {
				end = len(s)
			}
			if start >= end || start >= len(s) {
				return ""
			}
			return s[start:end]
		},
		"b64enc": func(s string) string { return base64.StdEncoding.EncodeToString([]byte(s)) },
		"b64dec": func(s string) string {
			b, _ := base64.StdEncoding.DecodeString(s)
			return string(b)
		},
		"b32enc": func(s string) string { return base32.StdEncoding.EncodeToString([]byte(s)) },
		"b32dec": func(s string) string {
			b, _ := base32.StdEncoding.DecodeString(s)
			return string(b)
		},
		"sha256sum": func(s string) string {
			sum := sha256.Sum256([]byte(s))
			return hex.EncodeToString(sum[:])
		},
		"sha1sum": func(s string) string {
			sum := sha1.Sum([]byte(s))
			return hex.EncodeToString(sum[:])
		},
		"adler32sum": func(s string) string {
			return strconv.FormatUint(uint64(adler32.Checksum([]byte(s))), 10)
		},
		"add": func(vals ...any) int64 {
			var sum int64
			for _, v := range vals {
				sum += toInt64(v)
			}
			return sum
		},
		"add1": func(v any) int64 { return toInt64(v) + 1 },
		"sub":  func(a, b any) int64 { return toInt64(a) - toInt64(b) },
		"mul": func(a any, rest ...any) int64 {
			res := toInt64(a)
			for _, v := range rest {
				res *= toInt64(v)
			}
			return res
		},
		"div": func(a, b any) int64 {
			d := toInt64(b)
			if d == 0 {
				return 0
			}
			return toInt64(a) / d
		},
		"mod": func(a, b any) int64 {
			d := toInt64(b)
			if d == 0 {
				return 0
			}
			return toInt64(a) % d
		},
		"max": func(a any, rest ...any) int64 {
			m := toInt64(a)
			for _, v := range rest {
				if n := toInt64(v); n > m {
					m = n
				}
			}
			return m
		},
		"min": func(a any, rest ...any) int64 {
			m := toInt64(a)
			for _, v := range rest {
				if n := toInt64(v); n < m {
					m = n
				}
			}
			return m
		},
		"floor":    func(v any) float64 { return math.Floor(toFloat64(v)) },
		"ceil":     func(v any) float64 { return math.Ceil(toFloat64(v)) },
		"int":      func(v any) int { return int(toInt64(v)) },
		"int64":    toInt64,
		"float64":  toFloat64,
		"atoi":     func(s string) int { i, _ := strconv.Atoi(strings.TrimSpace(s)); return i },
		"toString": toStr,
		"toStrings": func(v any) []string {
			rv := reflect.ValueOf(v)
			if rv.Kind() != reflect.Slice && rv.Kind() != reflect.Array {
				return []string{toStr(v)}
			}
			out := make([]string, rv.Len())
			for i := 0; i < rv.Len(); i++ {
				out[i] = toStr(rv.Index(i).Interface())
			}
			return out
		},
		"list": func(v ...any) []any { return v },
		"append": func(l any, v any) []any {
			s := toAnySlice(l)
			return append(s, v)
		},
		"mustAppend": func(l any, v any) ([]any, error) {
			return append(toAnySlice(l), v), nil
		},
		"prepend": func(l any, v any) []any {
			return append([]any{v}, toAnySlice(l)...)
		},
		"mustPrepend": func(l any, v any) ([]any, error) {
			return append([]any{v}, toAnySlice(l)...), nil
		},
		"first": func(l any) any {
			s := toAnySlice(l)
			if len(s) == 0 {
				return nil
			}
			return s[0]
		},
		"last": func(l any) any {
			s := toAnySlice(l)
			if len(s) == 0 {
				return nil
			}
			return s[len(s)-1]
		},
		"rest": func(l any) []any {
			s := toAnySlice(l)
			if len(s) <= 1 {
				return nil
			}
			return s[1:]
		},
		"initial": func(l any) []any {
			s := toAnySlice(l)
			if len(s) <= 1 {
				return nil
			}
			return s[:len(s)-1]
		},
		"uniq": func(l any) []any {
			s := toAnySlice(l)
			var out []any
			for _, item := range s {
				found := false
				for _, existing := range out {
					if reflect.DeepEqual(item, existing) {
						found = true
						break
					}
				}
				if !found {
					out = append(out, item)
				}
			}
			return out
		},
		"has": func(needle any, l any) bool {
			for _, item := range toAnySlice(l) {
				if reflect.DeepEqual(needle, item) {
					return true
				}
			}
			return false
		},
		"compact": func(l any) []any {
			var out []any
			for _, item := range toAnySlice(l) {
				if !isEmpty(item) {
					out = append(out, item)
				}
			}
			return out
		},
		"concat": func(lists ...any) []any {
			var out []any
			for _, l := range lists {
				out = append(out, toAnySlice(l)...)
			}
			return out
		},
		"dict": func(v ...any) map[string]any {
			d := map[string]any{}
			for i := 0; i < len(v); i += 2 {
				k := toStr(v[i])
				if i+1 < len(v) {
					d[k] = v[i+1]
				} else {
					d[k] = ""
				}
			}
			return d
		},
		"get": func(d map[string]any, k string) any {
			if d == nil {
				return ""
			}
			if v, ok := d[k]; ok {
				return v
			}
			return ""
		},
		"set": func(d map[string]any, k string, v any) map[string]any {
			if d == nil {
				d = map[string]any{}
			}
			d[k] = v
			return d
		},
		"unset": func(d map[string]any, k string) map[string]any {
			delete(d, k)
			return d
		},
		"hasKey": func(d map[string]any, k string) bool {
			_, ok := d[k]
			return ok
		},
		"keys": func(dicts ...map[string]any) []string {
			var out []string
			for _, d := range dicts {
				for k := range d {
					out = append(out, k)
				}
			}
			return out
		},
		"values": func(d map[string]any) []any {
			var out []any
			for _, v := range d {
				out = append(out, v)
			}
			return out
		},
		"pick": func(d map[string]any, keys ...string) map[string]any {
			res := map[string]any{}
			for _, k := range keys {
				if v, ok := d[k]; ok {
					res[k] = v
				}
			}
			return res
		},
		"omit": func(d map[string]any, keys ...string) map[string]any {
			drop := map[string]bool{}
			for _, k := range keys {
				drop[k] = true
			}
			res := map[string]any{}
			for k, v := range d {
				if !drop[k] {
					res[k] = v
				}
			}
			return res
		},
		"merge": func(dst map[string]any, srcs ...map[string]any) map[string]any {
			for _, src := range srcs {
				dst = mergeNoOverwrite(dst, src)
			}
			return dst
		},
		"mustMerge": func(dst map[string]any, srcs ...map[string]any) (map[string]any, error) {
			for _, src := range srcs {
				dst = mergeNoOverwrite(dst, src)
			}
			return dst, nil
		},
		"mergeOverwrite": func(dst map[string]any, srcs ...map[string]any) map[string]any {
			for _, src := range srcs {
				dst = coalesceValues(dst, src)
			}
			return dst
		},
		"mustMergeOverwrite": func(dst map[string]any, srcs ...map[string]any) (map[string]any, error) {
			for _, src := range srcs {
				dst = coalesceValues(dst, src)
			}
			return dst, nil
		},
		"deepCopy":     deepCopyValue,
		"mustDeepCopy": func(v any) (any, error) { return deepCopyValue(v), nil },
		"dig": func(args ...any) (any, error) {
			if len(args) < 2 {
				return nil, nil
			}
			def := args[len(args)-2]
			cur := args[len(args)-1]
			for _, k := range args[:len(args)-2] {
				ks, ok := k.(string)
				if !ok {
					return def, nil
				}
				m, ok := toStringAnyMap(cur)
				if !ok {
					return def, nil
				}
				next, exists := m[ks]
				if !exists {
					return def, nil
				}
				cur = next
			}
			if cur == nil {
				return def, nil
			}
			return cur, nil
		},
		"regexMatch": func(pattern, s string) bool {
			matched, _ := regexp.MatchString(pattern, s)
			return matched
		},
		"mustRegexMatch": regexp.MatchString,
		"regexReplaceAll": func(pattern, s, repl string) string {
			re, err := regexp.Compile(pattern)
			if err != nil {
				return s
			}
			return re.ReplaceAllString(s, repl)
		},
		"regexReplaceAllLiteral": func(pattern, s, repl string) string {
			re, err := regexp.Compile(pattern)
			if err != nil {
				return s
			}
			return re.ReplaceAllLiteralString(s, repl)
		},
		"now": time.Now,
		"date": func(fmtStr string, date any) string {
			if t, ok := date.(time.Time); ok {
				return t.Format(convertStrftime(fmtStr))
			}
			return time.Now().Format(convertStrftime(fmtStr))
		},
		"base":    path.Base,
		"dir":     path.Dir,
		"ext":     path.Ext,
		"clean":   path.Clean,
		"isAbs":   path.IsAbs,
		"osBase":  filepath.Base,
		"osDir":   filepath.Dir,
		"osExt":   filepath.Ext,
		"osClean": filepath.Clean,
		"urlParse": func(raw string) map[string]any {
			u, _ := url.Parse(raw)
			if u == nil {
				return map[string]any{}
			}
			return map[string]any{
				"scheme":   u.Scheme,
				"host":     u.Host,
				"hostname": u.Hostname(),
				"path":     u.Path,
				"query":    u.RawQuery,
				"fragment": u.Fragment,
			}
		},
		"semverCompare": semverCompareSimple,
		"typeOf":        func(v any) string { return fmt.Sprintf("%T", v) },
		"kindOf": func(v any) string {
			if v == nil {
				return "invalid"
			}
			return reflect.ValueOf(v).Kind().String()
		},
		"kindIs": func(want string, v any) bool {
			if v == nil {
				return want == "invalid"
			}
			return reflect.ValueOf(v).Kind().String() == want
		},
		"deepEqual": reflect.DeepEqual,
		"lookup": func(apiVersion, kind, namespace, name string) (map[string]any, error) {
			return map[string]any{}, nil
		},
	}
}

func toAnySlice(v any) []any {
	if v == nil {
		return nil
	}
	if s, ok := v.([]any); ok {
		return s
	}
	rv := reflect.ValueOf(v)
	if rv.Kind() != reflect.Slice && rv.Kind() != reflect.Array {
		return []any{v}
	}
	out := make([]any, rv.Len())
	for i := 0; i < rv.Len(); i++ {
		out[i] = rv.Index(i).Interface()
	}
	return out
}

func mergeNoOverwrite(dst, src map[string]any) map[string]any {
	if dst == nil {
		dst = map[string]any{}
	}
	for k, sv := range src {
		if dv, exists := dst[k]; exists {
			dm, dok := toStringAnyMap(dv)
			sm, sok := toStringAnyMap(sv)
			if dok && sok {
				dst[k] = mergeNoOverwrite(dm, sm)
			}
			continue
		}
		dst[k] = deepCopyValue(sv)
	}
	return dst
}

func convertStrftime(f string) string {
	r := strings.NewReplacer(
		"2006", "2006", "%Y", "2006", "%m", "01", "%d", "02",
		"%H", "15", "%M", "04", "%S", "05",
	)
	return r.Replace(f)
}

func semverCompareSimple(constraint, version string) bool {
	c := strings.TrimSpace(constraint)
	op := "="
	for _, prefix := range []string{">=", "<=", "!=", ">", "<", "="} {
		if strings.HasPrefix(c, prefix) {
			op = prefix
			c = strings.TrimSpace(strings.TrimPrefix(c, prefix))
			break
		}
	}
	cmp := compareSemverTuple(parseSemverTuple(version), parseSemverTuple(c))
	switch op {
	case ">=":
		return cmp >= 0
	case "<=":
		return cmp <= 0
	case ">":
		return cmp > 0
	case "<":
		return cmp < 0
	case "!=":
		return cmp != 0
	default:
		return cmp == 0
	}
}

func parseSemverTuple(v string) [3]int {
	v = strings.TrimPrefix(strings.TrimSpace(v), "v")
	if idx := strings.IndexAny(v, "-+"); idx != -1 {
		v = v[:idx]
	}
	parts := strings.Split(v, ".")
	var out [3]int
	for i := 0; i < len(parts) && i < 3; i++ {
		out[i], _ = strconv.Atoi(parts[i])
	}
	return out
}

func compareSemverTuple(a, b [3]int) int {
	for i := 0; i < 3; i++ {
		if a[i] != b[i] {
			if a[i] < b[i] {
				return -1
			}
			return 1
		}
	}
	return 0
}
