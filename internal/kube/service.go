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

	corev1 "k8s.io/api/core/v1"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
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
