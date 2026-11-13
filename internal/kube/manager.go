// Package kube contains the Kubernetes access layer of wailsail: kubeconfig
// handling, per-context client caching and resource operations.
package kube

import (
	"fmt"
	"net/http"
	"sort"
	"sync"

	"k8s.io/apimachinery/pkg/api/meta"
	"k8s.io/client-go/discovery"
	"k8s.io/client-go/discovery/cached/memory"
	"k8s.io/client-go/dynamic"
	"k8s.io/client-go/kubernetes"
	"k8s.io/client-go/rest"
	"k8s.io/client-go/restmapper"
	"k8s.io/client-go/tools/clientcmd"
	clientcmdapi "k8s.io/client-go/tools/clientcmd/api"
)

// Clients bundles every client wailsail needs for a single context.
type Clients struct {
	Context    string
	Config     *rest.Config
	HTTP       *http.Client
	Typed      kubernetes.Interface
	Dynamic    dynamic.Interface
	Discovery  discovery.CachedDiscoveryInterface
	Mapper     meta.ResettableRESTMapper
	DefaultNS  string
}

// Manager loads kubeconfig files and lazily builds clients per context.
type Manager struct {
	mu      sync.Mutex
	rules   *clientcmd.ClientConfigLoadingRules
	clients map[string]*Clients
}

// NewManager uses the standard loading rules ($KUBECONFIG, then ~/.kube/config).
func NewManager() *Manager {
	return NewManagerWithRules(clientcmd.NewDefaultClientConfigLoadingRules())
}

// NewManagerWithRules allows callers (and tests) to supply explicit loading rules.
func NewManagerWithRules(rules *clientcmd.ClientConfigLoadingRules) *Manager {
	return &Manager{rules: rules, clients: map[string]*Clients{}}
}

// RawConfig returns the merged kubeconfig.
func (m *Manager) RawConfig() (*clientcmdapi.Config, error) {
	m.mu.Lock()
	rules := m.rules
	m.mu.Unlock()
	return rules.Load()
}

// Reset drops all cached clients so the next call re-reads kubeconfig.
func (m *Manager) Reset() {
	m.mu.Lock()
	m.clients = map[string]*Clients{}
	m.mu.Unlock()
}

// For returns (and caches) the clients for the named context.
func (m *Manager) For(contextName string) (*Clients, error) {
	m.mu.Lock()
	if c, ok := m.clients[contextName]; ok {
		m.mu.Unlock()
		return c, nil
	}
	rules := m.rules
	m.mu.Unlock()

	if contextName == "" {
		return nil, fmt.Errorf("no context selected")
	}
	cc := clientcmd.NewNonInteractiveDeferredLoadingClientConfig(rules,
		&clientcmd.ConfigOverrides{CurrentContext: contextName})
	cfg, err := cc.ClientConfig()
	if err != nil {
		return nil, fmt.Errorf("context %q: %w", contextName, err)
	}
	cfg.QPS = 50
	cfg.Burst = 100
	cfg.UserAgent = "wailsail"

	ns, _, err := cc.Namespace()
	if err != nil || ns == "" {
		ns = "default"
	}

	httpClient, err := rest.HTTPClientFor(cfg)
	if err != nil {
		return nil, err
	}
	typed, err := kubernetes.NewForConfigAndClient(cfg, httpClient)
	if err != nil {
		return nil, err
	}
	dyn, err := dynamic.NewForConfigAndClient(cfg, httpClient)
	if err != nil {
		return nil, err
	}
	disc, err := discovery.NewDiscoveryClientForConfigAndClient(cfg, httpClient)
	if err != nil {
		return nil, err
	}
	cached := memory.NewMemCacheClient(disc)

	c := &Clients{
		Context:   contextName,
		Config:    cfg,
		HTTP:      httpClient,
		Typed:     typed,
		Dynamic:   dyn,
		Discovery: cached,
		Mapper:    restmapper.NewDeferredDiscoveryRESTMapper(cached),
		DefaultNS: ns,
	}
	m.mu.Lock()
	m.clients[contextName] = c
	m.mu.Unlock()
	return c, nil
}

// ContextInfo describes one kubeconfig context.
type ContextInfo struct {
	Name      string `json:"name"`
	Cluster   string `json:"cluster"`
	User      string `json:"user"`
	Namespace string `json:"namespace"`
	Server    string `json:"server"`
	Current   bool   `json:"current"`
}

// Contexts lists all contexts in the merged kubeconfig, sorted by name.
func (m *Manager) Contexts() ([]ContextInfo, error) {
	raw, err := m.RawConfig()
	if err != nil {
		return nil, err
	}
	out := make([]ContextInfo, 0, len(raw.Contexts))
	for name, ctx := range raw.Contexts {
		info := ContextInfo{
			Name:      name,
			Cluster:   ctx.Cluster,
			User:      ctx.AuthInfo,
			Namespace: ctx.Namespace,
			Current:   name == raw.CurrentContext,
		}
		if cl, ok := raw.Clusters[ctx.Cluster]; ok && cl != nil {
			info.Server = cl.Server
		}
		out = append(out, info)
	}
	sort.Slice(out, func(i, j int) bool { return out[i].Name < out[j].Name })
	return out, nil
}
