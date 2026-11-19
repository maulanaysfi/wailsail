// Package kube contains the Kubernetes access layer of wailsail: kubeconfig
// handling, per-context client caching and resource operations.
package kube

import (
	"fmt"
	"net/http"
	"os"
	"path/filepath"
	"regexp"
	"sort"
	"strings"
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
	Context   string
	Config    *rest.Config
	HTTP      *http.Client
	Typed     kubernetes.Interface
	Dynamic   dynamic.Interface
	Discovery discovery.CachedDiscoveryInterface
	Mapper    meta.ResettableRESTMapper
	DefaultNS string
}

// Manager loads kubeconfig files and lazily builds clients per context.
type Manager struct {
	mu          sync.Mutex
	rules       *clientcmd.ClientConfigLoadingRules
	customPaths []string // user-configured files/directories; nil = standard rules
	custom      bool
	gen         uint64
	clients     map[string]*Clients
}

// NewManager uses the standard loading rules ($KUBECONFIG, then ~/.kube/config).
func NewManager() *Manager {
	return NewManagerWithRules(clientcmd.NewDefaultClientConfigLoadingRules())
}

// NewManagerWithRules allows callers (and tests) to supply explicit loading rules.
func NewManagerWithRules(rules *clientcmd.ClientConfigLoadingRules) *Manager {
	return &Manager{rules: rules, clients: map[string]*Clients{}}
}

// SetKubeconfigPaths configures the kubeconfig files and/or directories to
// load. Directories are scanned (non-recursively) for valid kubeconfig
// files. Entries listed in $KUBECONFIG are appended with lower precedence.
// An empty list restores the standard loading rules. Cached clients are
// dropped.
func (m *Manager) SetKubeconfigPaths(paths []string) {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.customPaths = append([]string(nil), paths...)
	m.custom = len(paths) > 0
	m.rebuildRulesLocked()
	m.gen++
	m.clients = map[string]*Clients{}
}

// KubeconfigFiles returns the resolved kubeconfig files currently in use.
func (m *Manager) KubeconfigFiles() []string {
	m.mu.Lock()
	defer m.mu.Unlock()
	if m.rules.ExplicitPath != "" {
		return []string{m.rules.ExplicitPath}
	}
	return append([]string(nil), m.rules.Precedence...)
}

func (m *Manager) rebuildRulesLocked() {
	if !m.custom {
		m.rules = clientcmd.NewDefaultClientConfigLoadingRules()
		return
	}
	files := ResolveKubeconfigPaths(m.customPaths)
	if env := os.Getenv(clientcmd.RecommendedConfigPathEnvVar); env != "" {
		files = appendUnique(files, filepath.SplitList(env)...)
	}
	m.rules = &clientcmd.ClientConfigLoadingRules{Precedence: files}
}

// ResolveKubeconfigPaths expands ~ and environment variables in each entry
// and turns directories into the valid kubeconfig files they contain.
// Missing entries are skipped. The result is de-duplicated and ordered.
func ResolveKubeconfigPaths(paths []string) []string {
	var out []string
	for _, raw := range paths {
		p := ExpandPath(raw)
		if p == "" {
			continue
		}
		fi, err := os.Stat(p)
		if err != nil {
			continue
		}
		if !fi.IsDir() {
			out = appendUnique(out, p)
			continue
		}
		entries, err := os.ReadDir(p)
		if err != nil {
			continue
		}
		names := make([]string, 0, len(entries))
		for _, e := range entries {
			n := e.Name()
			if e.IsDir() || strings.HasPrefix(n, ".") || strings.HasSuffix(n, "~") {
				continue
			}
			names = append(names, n)
		}
		sort.Strings(names)
		for _, n := range names {
			f := filepath.Join(p, n)
			if isKubeconfigFile(f) {
				out = appendUnique(out, f)
			}
		}
	}
	return out
}

const maxKubeconfigSize = 8 << 20

func isKubeconfigFile(path string) bool {
	fi, err := os.Stat(path)
	if err != nil || !fi.Mode().IsRegular() || fi.Size() == 0 || fi.Size() > maxKubeconfigSize {
		return false
	}
	cfg, err := clientcmd.LoadFromFile(path)
	if err != nil {
		return false
	}
	return len(cfg.Contexts) > 0 || len(cfg.Clusters) > 0 || len(cfg.AuthInfos) > 0
}

var windowsEnvVar = regexp.MustCompile(`%([A-Za-z_][A-Za-z0-9_]*)%`)

// ExpandPath expands a leading ~, $VAR / ${VAR} and %VAR% references.
func ExpandPath(p string) string {
	p = strings.TrimSpace(p)
	if p == "" {
		return ""
	}
	if p == "~" || strings.HasPrefix(p, "~/") || strings.HasPrefix(p, `~\`) {
		if home, err := os.UserHomeDir(); err == nil {
			p = filepath.Join(home, p[1:])
		}
	}
	p = windowsEnvVar.ReplaceAllStringFunc(p, func(m string) string {
		if v, ok := os.LookupEnv(m[1 : len(m)-1]); ok {
			return v
		}
		return m
	})
	return filepath.Clean(os.ExpandEnv(p))
}

func appendUnique(list []string, items ...string) []string {
	for _, it := range items {
		if it == "" {
			continue
		}
		dup := false
		for _, have := range list {
			if have == it {
				dup = true
				break
			}
		}
		if !dup {
			list = append(list, it)
		}
	}
	return list
}

// RawConfig returns the merged kubeconfig.
func (m *Manager) RawConfig() (*clientcmdapi.Config, error) {
	m.mu.Lock()
	rules := m.rules
	m.mu.Unlock()
	return rules.Load()
}

// Reset drops all cached clients so the next call re-reads kubeconfig.
// Configured directories are re-scanned.
func (m *Manager) Reset() {
	m.mu.Lock()
	if m.custom {
		m.rebuildRulesLocked()
	}
	m.gen++
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
	gen := m.gen
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
	if m.gen == gen {
		if existing, ok := m.clients[contextName]; ok {
			m.mu.Unlock()
			return existing, nil
		}
		m.clients[contextName] = c
	}
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
