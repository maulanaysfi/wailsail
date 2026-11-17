package kube

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"sort"
	"strings"
	"time"

	corev1 "k8s.io/api/core/v1"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	"k8s.io/apimachinery/pkg/api/resource"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"k8s.io/apimachinery/pkg/fields"
	"k8s.io/apimachinery/pkg/runtime/schema"
	"k8s.io/apimachinery/pkg/types"
	utilyaml "k8s.io/apimachinery/pkg/util/yaml"
	"k8s.io/client-go/discovery"
	"sigs.k8s.io/yaml"

	"github.com/wailsapp/wails/v3/pkg/application"

	"wailsail/internal/stream"
)

// FieldManager is the server-side-apply manager name used by wailsail.
const FieldManager = "wailsail"

// Service is the Wails-bound API for contexts, discovery and resources.
type Service struct {
	mgr     *Manager
	streams *stream.Registry
	emit    stream.Emitter
	baseCtx context.Context
}

// NewService builds a KubeService.
func NewService(mgr *Manager, emit stream.Emitter) *Service {
	return &Service{mgr: mgr, streams: stream.NewRegistry(), emit: emit, baseCtx: context.Background()}
}

// ServiceStartup is called by Wails when the application starts.
func (s *Service) ServiceStartup(ctx context.Context, _ application.ServiceOptions) error {
	s.baseCtx = ctx
	return nil
}

// ServiceShutdown stops all running watches.
func (s *Service) ServiceShutdown() error {
	s.streams.StopAll()
	return nil
}

// Contexts lists kubeconfig contexts.
func (s *Service) Contexts() ([]ContextInfo, error) {
	return s.mgr.Contexts()
}

// Reload re-reads kubeconfig and drops cached clients and watches.
func (s *Service) Reload() {
	s.streams.StopAll()
	s.mgr.Reset()
}

// SetKubeconfigPaths replaces the kubeconfig files/directories to load,
// stopping watches and dropping cached clients. An empty list restores the
// standard loading rules ($KUBECONFIG, then ~/.kube/config).
func (s *Service) SetKubeconfigPaths(paths []string) {
	s.streams.StopAll()
	s.mgr.SetKubeconfigPaths(paths)
}

// KubeconfigFiles returns the resolved kubeconfig files currently loaded.
func (s *Service) KubeconfigFiles() []string {
	return s.mgr.KubeconfigFiles()
}

// DefaultNamespace returns the namespace configured for a context.
func (s *Service) DefaultNamespace(contextName string) (string, error) {
	c, err := s.mgr.For(contextName)
	if err != nil {
		return "", err
	}
	return c.DefaultNS, nil
}

// ServerVersion returns the cluster's git version (e.g. "v1.31.2").
func (s *Service) ServerVersion(contextName string) (string, error) {
	c, err := s.mgr.For(contextName)
	if err != nil {
		return "", err
	}
	v, err := c.Discovery.ServerVersion()
	if err != nil {
		return "", err
	}
	return v.GitVersion, nil
}

// Namespaces lists namespace names. When listing is forbidden it falls back
// to the context's default namespace.
func (s *Service) Namespaces(contextName string) ([]string, error) {
	c, err := s.mgr.For(contextName)
	if err != nil {
		return nil, err
	}
	list, err := c.Typed.CoreV1().Namespaces().List(s.baseCtx, metav1.ListOptions{})
	if err != nil {
		if apierrors.IsForbidden(err) {
			return []string{c.DefaultNS}, nil
		}
		return nil, err
	}
	out := make([]string, 0, len(list.Items))
	for _, ns := range list.Items {
		out = append(out, ns.Name)
	}
	sort.Strings(out)
	return out, nil
}

// ResourceType is a listable API resource discovered on the cluster.
type ResourceType struct {
	ResourceRef
	ShortNames []string `json:"shortNames"`
	Verbs      []string `json:"verbs"`
}

// APIResources returns the preferred version of every listable+watchable
// resource. Partial discovery failures (e.g. a broken aggregated API) are
// tolerated.
func (s *Service) APIResources(contextName string) ([]ResourceType, error) {
	c, err := s.mgr.For(contextName)
	if err != nil {
		return nil, err
	}
	lists, err := c.Discovery.ServerPreferredResources()
	if err != nil && !discovery.IsGroupDiscoveryFailedError(err) {
		return nil, err
	}
	var out []ResourceType
	for _, l := range lists {
		gv, perr := schema.ParseGroupVersion(l.GroupVersion)
		if perr != nil {
			continue
		}
		for _, r := range l.APIResources {
			if strings.Contains(r.Name, "/") || !hasVerbs(r.Verbs, "list", "watch") {
				continue
			}
			out = append(out, ResourceType{
				ResourceRef: ResourceRef{
					Group: gv.Group, Version: gv.Version, Resource: r.Name,
					Kind: r.Kind, Namespaced: r.Namespaced,
				},
				ShortNames: r.ShortNames,
				Verbs:      r.Verbs,
			})
		}
	}
	sort.Slice(out, func(i, j int) bool {
		if out[i].Group != out[j].Group {
			return out[i].Group < out[j].Group
		}
		return out[i].Kind < out[j].Kind
	})
	return out, nil
}

func hasVerbs(have metav1.Verbs, want ...string) bool {
	for _, w := range want {
		found := false
		for _, h := range have {
			if h == w {
				found = true
				break
			}
		}
		if !found {
			return false
		}
	}
	return true
}

// WatchTopic is the event name for a watch stream.
func WatchTopic(id string) string { return "wailsail:watch:" + id }

// Watch starts a live table watch for a resource type and returns the
// stream ID. Events are published on WatchTopic(id). namespace == ""
// watches all namespaces. Pass a caller-generated streamID so the frontend
// can subscribe before the first event is emitted.
func (s *Service) Watch(contextName string, ref ResourceRef, namespace string, streamID string) (string, error) {
	c, err := s.mgr.For(contextName)
	if err != nil {
		return "", err
	}
	ctx, id := s.streams.Start(s.baseCtx, streamID)
	topic := WatchTopic(id)
	go func() {
		defer s.streams.Done(id)
		c.runWatch(ctx, ref, namespace, func(ev WatchEvent) { s.emit(topic, ev) })
	}()
	return id, nil
}

// StopWatch cancels a watch stream.
func (s *Service) StopWatch(streamID string) {
	s.streams.Stop(streamID)
}

func (s *Service) resourceClient(c *Clients, ref ResourceRef, namespace string) dynamicResource {
	gvr := schema.GroupVersionResource{Group: ref.Group, Version: ref.Version, Resource: ref.Resource}
	if ref.Namespaced {
		return c.Dynamic.Resource(gvr).Namespace(namespace)
	}
	return c.Dynamic.Resource(gvr)
}

// GetYAML returns the object as YAML without managedFields.
func (s *Service) GetYAML(contextName string, ref ResourceRef, namespace, name string) (string, error) {
	c, err := s.mgr.For(contextName)
	if err != nil {
		return "", err
	}
	obj, err := s.resourceClient(c, ref, namespace).Get(s.baseCtx, name, metav1.GetOptions{})
	if err != nil {
		return "", err
	}
	return ObjectToYAML(obj)
}

// ObjectToYAML renders an object as YAML, dropping noisy managedFields.
func ObjectToYAML(obj *unstructured.Unstructured) (string, error) {
	obj = obj.DeepCopy()
	unstructured.RemoveNestedField(obj.Object, "metadata", "managedFields")
	b, err := yaml.Marshal(obj.Object)
	if err != nil {
		return "", err
	}
	return string(b), nil
}

// EventInfo describes a Kubernetes Event related to a selected object.
type EventInfo struct {
	Type      string `json:"type"`      // "Normal" or "Warning"
	Reason    string `json:"reason"`    // short machine-readable reason
	Message   string `json:"message"`   // human-readable description
	Count     int32  `json:"count"`     // occurrence count
	Source    string `json:"source"`    // reporting component / controller
	SubObject string `json:"subObject"` // involvedObject.fieldPath if set
	FirstSeen string `json:"firstSeen"` // RFC3339 timestamp
	LastSeen  string `json:"lastSeen"`  // RFC3339 timestamp
}

// ContainerMetrics holds parsed live CPU and memory usage for a container.
type ContainerMetrics struct {
	CPUMilli    int64 `json:"cpuMilli"`    // millicores (1000m = 1 core)
	MemoryBytes int64 `json:"memoryBytes"` // bytes
}

// ObjectDetail bundles the structured object, its YAML representation,
// related Kubernetes events, and live container metrics for the detail drawer.
type ObjectDetail struct {
	Object  map[string]any              `json:"object"`
	YAML    string                      `json:"yaml"`
	Events  []EventInfo                 `json:"events"`
	Metrics map[string]ContainerMetrics `json:"metrics"`
}

// GetDetail fetches a resource's structured representation, YAML (without
// managedFields), related cluster events, and container resource usage.
func (s *Service) GetDetail(contextName string, ref ResourceRef, namespace, name string) (ObjectDetail, error) {
	c, err := s.mgr.For(contextName)
	if err != nil {
		return ObjectDetail{}, err
	}
	obj, err := s.resourceClient(c, ref, namespace).Get(s.baseCtx, name, metav1.GetOptions{})
	if err != nil {
		return ObjectDetail{}, err
	}
	clean := obj.DeepCopy()
	unstructured.RemoveNestedField(clean.Object, "metadata", "managedFields")
	b, err := yaml.Marshal(clean.Object)
	if err != nil {
		return ObjectDetail{}, err
	}
	evs, _ := s.listObjectEvents(c, ref.Kind, namespace, name, string(obj.GetUID()))
	metrics := s.fetchObjectMetrics(c, ref.Kind, namespace, name, clean.Object)
	return ObjectDetail{
		Object:  clean.Object,
		YAML:    string(b),
		Events:  evs,
		Metrics: metrics,
	}, nil
}

type podMetricsPayload struct {
	Metadata struct {
		Name      string `json:"name"`
		Namespace string `json:"namespace"`
	} `json:"metadata"`
	Containers []struct {
		Name  string `json:"name"`
		Usage struct {
			CPU    string `json:"cpu"`
			Memory string `json:"memory"`
		} `json:"usage"`
	} `json:"containers"`
}

type podMetricsListPayload struct {
	Items []podMetricsPayload `json:"items"`
}

// PodMetrics returns live CPU and memory usage for pods in namespace (or all
// namespaces when namespace == ""). Keys are both "namespace/pod/container"
// and "namespace/pod" (using the first container). Missing metrics-server
// returns an empty map without error.
func (s *Service) PodMetrics(contextName, namespace string) (map[string]ContainerMetrics, error) {
	c, err := s.mgr.For(contextName)
	if err != nil {
		return nil, err
	}
	req := c.Typed.CoreV1().RESTClient().Get()
	if namespace != "" {
		req = req.AbsPath("/apis/metrics.k8s.io/v1beta1/namespaces", namespace, "pods")
	} else {
		req = req.AbsPath("/apis/metrics.k8s.io/v1beta1/pods")
	}
	raw, err := req.DoRaw(s.baseCtx)
	if err != nil {
		return map[string]ContainerMetrics{}, nil
	}
	return parseAllPodsMetricsJSON(raw), nil
}

func parseAllPodsMetricsJSON(raw []byte) map[string]ContainerMetrics {
	var list podMetricsListPayload
	if err := json.Unmarshal(raw, &list); err != nil || len(list.Items) == 0 {
		return map[string]ContainerMetrics{}
	}
	out := make(map[string]ContainerMetrics, len(list.Items)*2)
	for _, item := range list.Items {
		podKey := item.Metadata.Namespace + "/" + item.Metadata.Name
		for i, ct := range item.Containers {
			u := parseContainerUsage(ct.Usage.CPU, ct.Usage.Memory)
			out[podKey+"/"+ct.Name] = u
			if i == 0 {
				out[podKey] = u
			}
		}
	}
	return out
}

func (s *Service) fetchObjectMetrics(c *Clients, kind, namespace, name string, obj map[string]any) map[string]ContainerMetrics {
	if namespace == "" {
		return nil
	}
	if kind == "Pod" {
		raw, err := c.Typed.CoreV1().RESTClient().Get().
			AbsPath("/apis/metrics.k8s.io/v1beta1/namespaces", namespace, "pods", name).
			DoRaw(s.baseCtx)
		if err != nil {
			return nil
		}
		return parsePodMetricsJSON(raw)
	}
	// For workloads (Deployment, StatefulSet, DaemonSet, ReplicaSet), aggregate matching pod metrics.
	matchLabels, _, _ := unstructured.NestedStringMap(obj, "spec", "selector", "matchLabels")
	if len(matchLabels) == 0 {
		return nil
	}
	sel := fields.SelectorFromSet(fields.Set(matchLabels)).String()
	raw, err := c.Typed.CoreV1().RESTClient().Get().
		AbsPath("/apis/metrics.k8s.io/v1beta1/namespaces", namespace, "pods").
		Param("labelSelector", sel).
		DoRaw(s.baseCtx)
	if err != nil {
		return nil
	}
	return parsePodMetricsListJSON(raw)
}

func parsePodMetricsJSON(raw []byte) map[string]ContainerMetrics {
	var pm podMetricsPayload
	if err := json.Unmarshal(raw, &pm); err != nil || len(pm.Containers) == 0 {
		return nil
	}
	out := make(map[string]ContainerMetrics, len(pm.Containers))
	for _, ct := range pm.Containers {
		out[ct.Name] = parseContainerUsage(ct.Usage.CPU, ct.Usage.Memory)
	}
	return out
}

func parsePodMetricsListJSON(raw []byte) map[string]ContainerMetrics {
	var list podMetricsListPayload
	if err := json.Unmarshal(raw, &list); err != nil || len(list.Items) == 0 {
		return nil
	}
	totals := map[string]ContainerMetrics{}
	counts := map[string]int64{}
	for _, item := range list.Items {
		for _, ct := range item.Containers {
			u := parseContainerUsage(ct.Usage.CPU, ct.Usage.Memory)
			cur := totals[ct.Name]
			cur.CPUMilli += u.CPUMilli
			cur.MemoryBytes += u.MemoryBytes
			totals[ct.Name] = cur
			counts[ct.Name]++
		}
	}
	for name, tot := range totals {
		if n := counts[name]; n > 1 {
			totals[name] = ContainerMetrics{
				CPUMilli:    tot.CPUMilli / n,
				MemoryBytes: tot.MemoryBytes / n,
			}
		}
	}
	return totals
}

func parseContainerUsage(cpuStr, memStr string) ContainerMetrics {
	var m ContainerMetrics
	if q, err := resource.ParseQuantity(cpuStr); err == nil {
		m.CPUMilli = q.MilliValue()
	}
	if q, err := resource.ParseQuantity(memStr); err == nil {
		m.MemoryBytes = q.Value()
	}
	return m
}

// ObjectEvents returns Kubernetes events involving the specified object.
func (s *Service) ObjectEvents(contextName, kind, namespace, name string) ([]EventInfo, error) {
	c, err := s.mgr.For(contextName)
	if err != nil {
		return nil, err
	}
	return s.listObjectEvents(c, kind, namespace, name, "")
}

func (s *Service) listObjectEvents(c *Clients, kind, namespace, name, uid string) ([]EventInfo, error) {
	sel := fields.OneTermEqualSelector("involvedObject.name", name).String()
	list, err := c.Typed.CoreV1().Events(namespace).List(s.baseCtx, metav1.ListOptions{FieldSelector: sel})
	if err != nil {
		return []EventInfo{}, err
	}
	return filterAndSortEvents(list.Items, kind, namespace, name, uid), nil
}

func filterAndSortEvents(items []corev1.Event, kind, namespace, name, uid string) []EventInfo {
	out := make([]EventInfo, 0, len(items))
	for _, ev := range items {
		inv := ev.InvolvedObject
		if inv.Name != name {
			continue
		}
		if namespace != "" && inv.Namespace != "" && inv.Namespace != namespace {
			continue
		}
		if kind != "" && inv.Kind != "" && !strings.EqualFold(inv.Kind, kind) {
			continue
		}
		if uid != "" && string(inv.UID) != "" && string(inv.UID) != uid {
			continue
		}
		src := ev.Source.Component
		if src == "" {
			src = ev.ReportingController
		}
		if ev.Source.Host != "" {
			if src != "" {
				src += " (" + ev.Source.Host + ")"
			} else {
				src = ev.Source.Host
			}
		}
		count := ev.Count
		if ev.Series != nil && ev.Series.Count > count {
			count = ev.Series.Count
		}
		if count == 0 {
			count = 1
		}
		first := eventTimestamp(ev.FirstTimestamp, ev.EventTime, ev.CreationTimestamp)
		last := eventLastTimestamp(ev)
		out = append(out, EventInfo{
			Type:      ev.Type,
			Reason:    ev.Reason,
			Message:   ev.Message,
			Count:     count,
			Source:    src,
			SubObject: inv.FieldPath,
			FirstSeen: first,
			LastSeen:  last,
		})
	}
	sort.Slice(out, func(i, j int) bool {
		if out[i].LastSeen != out[j].LastSeen {
			return out[i].LastSeen > out[j].LastSeen
		}
		return out[i].Reason < out[j].Reason
	})
	return out
}

func eventTimestamp(t metav1.Time, micro metav1.MicroTime, fallback metav1.Time) string {
	if !t.IsZero() {
		return t.UTC().Format(time.RFC3339)
	}
	if !micro.IsZero() {
		return micro.UTC().Format(time.RFC3339)
	}
	if !fallback.IsZero() {
		return fallback.UTC().Format(time.RFC3339)
	}
	return ""
}

func eventLastTimestamp(ev corev1.Event) string {
	if ev.Series != nil && !ev.Series.LastObservedTime.IsZero() {
		return ev.Series.LastObservedTime.UTC().Format(time.RFC3339)
	}
	return eventTimestamp(ev.LastTimestamp, ev.EventTime, ev.CreationTimestamp)
}

// ApplyResult describes one object touched by Apply.
type ApplyResult struct {
	Kind      string `json:"kind"`
	Namespace string `json:"namespace"`
	Name      string `json:"name"`
}

// Apply server-side-applies one or more YAML/JSON documents. Objects
// without a namespace go to defaultNamespace when their kind is namespaced.
func (s *Service) Apply(contextName string, manifest string, defaultNamespace string) ([]ApplyResult, error) {
	c, err := s.mgr.For(contextName)
	if err != nil {
		return nil, err
	}
	objs, err := DecodeManifest(manifest)
	if err != nil {
		return nil, err
	}
	if len(objs) == 0 {
		return nil, errors.New("manifest contains no objects")
	}
	var results []ApplyResult
	for _, obj := range objs {
		gvk := obj.GroupVersionKind()
		mapping, err := c.Mapper.RESTMapping(gvk.GroupKind(), gvk.Version)
		if err != nil {
			c.Mapper.Reset()
			if mapping, err = c.Mapper.RESTMapping(gvk.GroupKind(), gvk.Version); err != nil {
				return results, fmt.Errorf("%s: %w", gvk.Kind, err)
			}
		}
		namespaced := mapping.Scope.Name() == "namespace"
		if namespaced && obj.GetNamespace() == "" {
			ns := defaultNamespace
			if ns == "" {
				ns = c.DefaultNS
			}
			obj.SetNamespace(ns)
		}
		if !namespaced {
			obj.SetNamespace("")
		}
		PrepareForApply(obj)
		data, err := json.Marshal(obj.Object)
		if err != nil {
			return results, err
		}
		ref := ResourceRef{Group: mapping.Resource.Group, Version: mapping.Resource.Version,
			Resource: mapping.Resource.Resource, Kind: gvk.Kind, Namespaced: namespaced}
		force := true
		_, err = s.resourceClient(c, ref, obj.GetNamespace()).Patch(s.baseCtx, obj.GetName(),
			types.ApplyPatchType, data, metav1.PatchOptions{FieldManager: FieldManager, Force: &force})
		if err != nil {
			return results, fmt.Errorf("%s %s: %w", gvk.Kind, obj.GetName(), err)
		}
		results = append(results, ApplyResult{Kind: gvk.Kind, Namespace: obj.GetNamespace(), Name: obj.GetName()})
	}
	return results, nil
}

// PrepareForApply removes fields that server-side apply rejects or that
// would make an edited copy conflict needlessly.
func PrepareForApply(obj *unstructured.Unstructured) {
	unstructured.RemoveNestedField(obj.Object, "metadata", "managedFields")
	unstructured.RemoveNestedField(obj.Object, "metadata", "creationTimestamp")
	unstructured.RemoveNestedField(obj.Object, "metadata", "generation")
	unstructured.RemoveNestedField(obj.Object, "metadata", "uid")
	unstructured.RemoveNestedField(obj.Object, "status")
}

// DecodeManifest splits a multi-document YAML/JSON manifest into objects,
// expanding `kind: List` wrappers.
func DecodeManifest(manifest string) ([]*unstructured.Unstructured, error) {
	dec := utilyaml.NewYAMLOrJSONDecoder(bytes.NewBufferString(manifest), 4096)
	var out []*unstructured.Unstructured
	for {
		var m map[string]any
		if err := dec.Decode(&m); err != nil {
			if errors.Is(err, io.EOF) {
				return out, nil
			}
			return nil, err
		}
		if len(m) == 0 {
			continue
		}
		u := &unstructured.Unstructured{Object: m}
		if u.IsList() {
			if err := u.EachListItem(func(o runtimeObject) error {
				item, ok := o.(*unstructured.Unstructured)
				if ok {
					out = append(out, item)
				}
				return nil
			}); err != nil {
				return nil, err
			}
			continue
		}
		if u.GetKind() == "" || u.GetAPIVersion() == "" {
			return nil, errors.New("every document needs apiVersion and kind")
		}
		if u.GetName() == "" {
			return nil, fmt.Errorf("%s: metadata.name is required", u.GetKind())
		}
		out = append(out, u)
	}
}

// Delete deletes an object with background propagation.
func (s *Service) Delete(contextName string, ref ResourceRef, namespace, name string) error {
	c, err := s.mgr.For(contextName)
	if err != nil {
		return err
	}
	policy := metav1.DeletePropagationBackground
	return s.resourceClient(c, ref, namespace).Delete(s.baseCtx, name, metav1.DeleteOptions{PropagationPolicy: &policy})
}

// Scale sets spec.replicas through the scale subresource.
func (s *Service) Scale(contextName string, ref ResourceRef, namespace, name string, replicas int32) error {
	c, err := s.mgr.For(contextName)
	if err != nil {
		return err
	}
	patch := fmt.Sprintf(`{"spec":{"replicas":%d}}`, replicas)
	_, err = s.resourceClient(c, ref, namespace).Patch(s.baseCtx, name, types.MergePatchType,
		[]byte(patch), metav1.PatchOptions{FieldManager: FieldManager}, "scale")
	return err
}

// Restart triggers a rollout restart of a Deployment/StatefulSet/DaemonSet.
func (s *Service) Restart(contextName string, ref ResourceRef, namespace, name string) error {
	c, err := s.mgr.For(contextName)
	if err != nil {
		return err
	}
	patch := fmt.Sprintf(`{"spec":{"template":{"metadata":{"annotations":{"wailsail.dev/restartedAt":%q}}}}}`,
		metav1.Now().UTC().Format("2006-01-02T15:04:05Z"))
	_, err = s.resourceClient(c, ref, namespace).Patch(s.baseCtx, name, types.StrategicMergePatchType,
		[]byte(patch), metav1.PatchOptions{FieldManager: FieldManager})
	return err
}

// ContainerPort is a port declared by a container or service.
type ContainerPort struct {
	Name     string `json:"name"`
	Port     int32  `json:"port"`
	Protocol string `json:"protocol"`
}

// ContainerInfo describes a container in a pod.
type ContainerInfo struct {
	Name  string          `json:"name"`
	Type  string          `json:"type"` // "init", "main" or "ephemeral"
	Image string          `json:"image"`
	Ports []ContainerPort `json:"ports"`
}

// PodContainers returns the containers of a pod (init, main, ephemeral).
func (s *Service) PodContainers(contextName, namespace, pod string) ([]ContainerInfo, error) {
	c, err := s.mgr.For(contextName)
	if err != nil {
		return nil, err
	}
	p, err := c.Typed.CoreV1().Pods(namespace).Get(s.baseCtx, pod, metav1.GetOptions{})
	if err != nil {
		return nil, err
	}
	return podContainers(p), nil
}

func podContainers(p *corev1.Pod) []ContainerInfo {
	var out []ContainerInfo
	add := func(typ string, cs []corev1.Container) {
		for _, ct := range cs {
			info := ContainerInfo{Name: ct.Name, Type: typ, Image: ct.Image}
			for _, port := range ct.Ports {
				info.Ports = append(info.Ports, ContainerPort{Name: port.Name, Port: port.ContainerPort, Protocol: string(port.Protocol)})
			}
			out = append(out, info)
		}
	}
	add("main", p.Spec.Containers)
	add("init", p.Spec.InitContainers)
	for _, e := range p.Spec.EphemeralContainers {
		out = append(out, ContainerInfo{Name: e.Name, Type: "ephemeral", Image: e.Image})
	}
	return out
}
