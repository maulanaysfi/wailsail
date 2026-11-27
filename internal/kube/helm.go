package kube

import (
	"bytes"
	"compress/gzip"
	"context"
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"sort"
	"strconv"
	"strings"
	"time"

	corev1 "k8s.io/api/core/v1"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	"k8s.io/apimachinery/pkg/api/meta"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
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
	return s.uninstallHelmRelease(c, namespace, name)
}

func (s *Service) uninstallHelmRelease(c *Clients, namespace, name string) error {
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
		objs, err := DecodeManifest(active.Manifest)
		if err != nil {
			return fmt.Errorf("decode release manifest: %w", err)
		}
		var delErrs []error
		for i := len(objs) - 1; i >= 0; i-- {
			if err := s.deleteUnstructuredObject(c, namespace, objs[i]); err != nil {
				delErrs = append(delErrs, fmt.Errorf("delete %s %s: %w", objs[i].GetKind(), objs[i].GetName(), err))
			}
		}
		if len(delErrs) > 0 {
			return errors.Join(delErrs...)
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
			ref, err := resolveObjectRef(c, oldObj, namespace)
			if err != nil {
				continue
			}
			k := objectIdentityKey(oldObj.GroupVersionKind().Group, ref.Kind, oldObj.GetNamespace(), oldObj.GetName())
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
	ref, err := resolveObjectRef(c, obj, defaultNamespace)
	if err != nil {
		if meta.IsNoMatchError(err) {
			return nil
		}
		return err
	}
	err = s.resourceClient(c, ref, obj.GetNamespace()).Delete(s.baseCtx, obj.GetName(), metav1.DeleteOptions{})
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
