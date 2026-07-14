// Package kube loads the mounted kubeconfig, enumerates its contexts, switches
// the active context, and hands out per-context Kubernetes clients built lazily
// and cached. The kubeconfig is treated strictly read-only — Kubescope never
// writes the mounted file; the active context is in-memory server state only.
// Auth gotchas (embedded vs file-path creds, exec plugins, local clusters) are
// documented in ADR-0004.
package kube

import (
	"context"
	"errors"
	"fmt"
	"sort"
	"sync"
	"time"

	"k8s.io/client-go/discovery"
	"k8s.io/client-go/dynamic"
	"k8s.io/client-go/kubernetes"
	"k8s.io/client-go/rest"
	"k8s.io/client-go/tools/clientcmd"
	clientcmdapi "k8s.io/client-go/tools/clientcmd/api"
)

// defaultProbeTimeout bounds a single per-context health probe so one
// unreachable cluster can never stall the others or the request.
const defaultProbeTimeout = 5 * time.Second

// Manager parses the kubeconfig on demand, tracks the active context in memory,
// and caches a successfully-built rest.Config + clientset per context. Failures
// are not cached, so a kubeconfig that appears (or is fixed) after startup is
// picked up on the next request without a restart.
type Manager struct {
	kubeconfigPath string
	probeTimeout   time.Duration

	mu      sync.RWMutex
	active  string                   // in-memory override; "" = kubeconfig current-context
	clients map[string]*cachedClient // per-context cache of successful builds
}

type cachedClient struct {
	restConfig *rest.Config
	clientset  kubernetes.Interface
	// dynamicClient serves the generic resource engine (ADR-0003): get/list
	// for any GVR, including CRDs, via unstructured objects.
	dynamicClient dynamic.Interface
}

// NewManager returns a Manager reading the kubeconfig at path. The file is not
// touched until the first context or client is requested.
func NewManager(path string) *Manager {
	return &Manager{
		kubeconfigPath: path,
		probeTimeout:   defaultProbeTimeout,
		clients:        make(map[string]*cachedClient),
	}
}

// ContextInfo describes one kubeconfig context for the API.
type ContextInfo struct {
	Name      string `json:"name"`
	Cluster   string `json:"cluster"`
	Namespace string `json:"namespace"`
	Active    bool   `json:"active"`
}

// ContextHealth is the result of probing one context's connectivity.
type ContextHealth struct {
	Name          string `json:"name"`
	Reachable     bool   `json:"reachable"`
	AuthOK        bool   `json:"authOK"`
	ServerVersion string `json:"serverVersion"`
	Error         string `json:"error,omitempty"`
	Guidance      string `json:"guidance,omitempty"`
}

// UnknownContextError is returned when a switch targets a context the
// kubeconfig does not define; handlers map it to a 404.
type UnknownContextError struct{ Name string }

func (e *UnknownContextError) Error() string { return fmt.Sprintf("unknown context %q", e.Name) }

// Contexts enumerates every context in the kubeconfig, marking the active one.
// A missing or malformed kubeconfig is a structured error; the caller stays up.
func (m *Manager) Contexts() ([]ContextInfo, error) {
	raw, err := m.rawConfig()
	if err != nil {
		return nil, err
	}
	active, _ := m.resolveActive(raw) // an unset current-context is not fatal for listing
	infos := make([]ContextInfo, 0, len(raw.Contexts))
	for name, c := range raw.Contexts {
		ns := c.Namespace
		if ns == "" {
			ns = "default"
		}
		infos = append(infos, ContextInfo{
			Name:      name,
			Cluster:   c.Cluster,
			Namespace: ns,
			Active:    name == active,
		})
	}
	sort.Slice(infos, func(i, j int) bool { return infos[i].Name < infos[j].Name })
	return infos, nil
}

// ActiveContextName resolves the active context: the in-memory override if set
// and still present, else the kubeconfig's current-context.
func (m *Manager) ActiveContextName() (string, error) {
	raw, err := m.rawConfig()
	if err != nil {
		return "", err
	}
	return m.resolveActive(raw)
}

// SwitchContext sets the active context (in memory only — the mounted
// kubeconfig is never written). The name must exist in the kubeconfig.
func (m *Manager) SwitchContext(name string) error {
	if name == "" {
		return errors.New("context name must not be empty")
	}
	raw, err := m.rawConfig()
	if err != nil {
		return err
	}
	if _, ok := raw.Contexts[name]; !ok {
		return &UnknownContextError{Name: name}
	}
	m.mu.Lock()
	m.active = name
	m.mu.Unlock()
	return nil
}

// Clientset returns a typed clientset for the active context.
func (m *Manager) Clientset() (kubernetes.Interface, error) {
	name, err := m.ActiveContextName()
	if err != nil {
		return nil, err
	}
	return m.ClientsetFor(name)
}

// ClientsetFor returns a typed clientset for the named context, building and
// caching it on first use. The cache is concurrency-safe.
func (m *Manager) ClientsetFor(name string) (kubernetes.Interface, error) {
	cached, err := m.clientsFor(name)
	if err != nil {
		return nil, err
	}
	return cached.clientset, nil
}

// Dynamic returns a dynamic client for the active context — the generic
// resource engine's path to any GVR, incl. CRDs (ADR-0003).
func (m *Manager) Dynamic() (dynamic.Interface, error) {
	name, err := m.ActiveContextName()
	if err != nil {
		return nil, err
	}
	return m.DynamicFor(name)
}

// DynamicFor returns a dynamic client for the named context, building and
// caching it alongside the typed clientset on first use.
func (m *Manager) DynamicFor(name string) (dynamic.Interface, error) {
	cached, err := m.clientsFor(name)
	if err != nil {
		return nil, err
	}
	return cached.dynamicClient, nil
}

// Discovery returns a discovery client for the active context, used to
// enumerate every API group/version/resource the cluster serves (Sprint 2).
func (m *Manager) Discovery() (discovery.DiscoveryInterface, error) {
	cs, err := m.Clientset()
	if err != nil {
		return nil, err
	}
	return cs.Discovery(), nil
}

// clientsFor builds (once) and caches the typed and dynamic clients for a
// context. Both share the same rest.Config; only a fully successful build is
// cached, so a kubeconfig fixed after startup is picked up on the next request.
func (m *Manager) clientsFor(name string) (*cachedClient, error) {
	m.mu.RLock()
	cached, ok := m.clients[name]
	m.mu.RUnlock()
	if ok {
		return cached, nil
	}

	m.mu.Lock()
	defer m.mu.Unlock()
	if cached, ok := m.clients[name]; ok { // another goroutine may have built it
		return cached, nil
	}
	restCfg, err := m.buildRestConfig(name)
	if err != nil {
		return nil, err
	}
	cs, err := kubernetes.NewForConfig(restCfg)
	if err != nil {
		return nil, fmt.Errorf("building clientset for context %q: %w", name, err)
	}
	dyn, err := dynamic.NewForConfig(restCfg)
	if err != nil {
		return nil, fmt.Errorf("building dynamic client for context %q: %w", name, err)
	}
	cached = &cachedClient{restConfig: restCfg, clientset: cs, dynamicClient: dyn}
	m.clients[name] = cached
	return cached, nil
}

// ProbeAll probes every context concurrently for reachability, auth and server
// version. Each probe is independently timed out, so one unreachable context
// never blocks the others.
func (m *Manager) ProbeAll(ctx context.Context) ([]ContextHealth, error) {
	raw, err := m.rawConfig()
	if err != nil {
		return nil, err
	}
	names := make([]string, 0, len(raw.Contexts))
	for name := range raw.Contexts {
		names = append(names, name)
	}
	sort.Strings(names)

	results := make([]ContextHealth, len(names))
	var wg sync.WaitGroup
	for i, name := range names {
		wg.Add(1)
		go func(i int, name string) {
			defer wg.Done()
			results[i] = m.probe(ctx, raw, name)
		}(i, name)
	}
	wg.Wait()
	return results, nil
}

// probe checks one context. It builds a short-timeout rest.Config (never the
// cached long-lived one) and calls ServerVersion, classifying the outcome.
func (m *Manager) probe(ctx context.Context, raw clientcmdapi.Config, name string) ContextHealth {
	usesExec := contextUsesExec(raw, name)
	cmd := execCommand(raw, name)

	restCfg, err := m.buildRestConfig(name)
	if err != nil {
		h := classify(err, usesExec, cmd)
		h.Name = name
		return h
	}
	restCfg.Timeout = m.probeTimeout

	cs, err := kubernetes.NewForConfig(restCfg)
	if err != nil {
		h := classify(err, usesExec, cmd)
		h.Name = name
		return h
	}

	// ServerVersion has no context parameter; run it off-goroutine so caller
	// cancellation returns promptly even if the HTTP layer is slower.
	type outcome struct {
		version string
		err     error
	}
	done := make(chan outcome, 1)
	go func() {
		v, err := cs.Discovery().ServerVersion()
		if err != nil {
			done <- outcome{err: err}
			return
		}
		done <- outcome{version: v.GitVersion}
	}()

	select {
	case <-ctx.Done():
		return ContextHealth{Name: name, Error: ctx.Err().Error()}
	case res := <-done:
		if res.err != nil {
			h := classify(res.err, usesExec, cmd)
			h.Name = name
			return h
		}
		return ContextHealth{Name: name, Reachable: true, AuthOK: true, ServerVersion: res.version}
	}
}

// rawConfig loads the kubeconfig from its explicit path. A missing or malformed
// file returns a wrapped error rather than falling back to other kubeconfigs.
func (m *Manager) rawConfig() (clientcmdapi.Config, error) {
	loadingRules := &clientcmd.ClientConfigLoadingRules{ExplicitPath: m.kubeconfigPath}
	cfg, err := loadingRules.Load()
	if err != nil {
		return clientcmdapi.Config{}, fmt.Errorf("loading kubeconfig %q: %w", m.kubeconfigPath, err)
	}
	if cfg == nil {
		return clientcmdapi.Config{}, fmt.Errorf("loading kubeconfig %q: empty config", m.kubeconfigPath)
	}
	return *cfg, nil
}

// buildRestConfig builds a rest.Config for the named context. Embedded
// certs/tokens work as-is; exec-plugin and file-path-cert gotchas surface at
// call time and are handled by the health probe (ADR-0004).
func (m *Manager) buildRestConfig(name string) (*rest.Config, error) {
	loadingRules := &clientcmd.ClientConfigLoadingRules{ExplicitPath: m.kubeconfigPath}
	overrides := &clientcmd.ConfigOverrides{CurrentContext: name}
	cc := clientcmd.NewNonInteractiveDeferredLoadingClientConfig(loadingRules, overrides)
	restCfg, err := cc.ClientConfig()
	if err != nil {
		return nil, fmt.Errorf("building rest config for context %q: %w", name, err)
	}
	return restCfg, nil
}

// resolveActive returns the active context name: the in-memory override if set
// and still present, otherwise the kubeconfig's current-context. An unset
// current-context with no override is a structured error, not a panic.
func (m *Manager) resolveActive(raw clientcmdapi.Config) (string, error) {
	m.mu.RLock()
	override := m.active
	m.mu.RUnlock()
	if override != "" {
		if _, ok := raw.Contexts[override]; ok {
			return override, nil
		}
		// Stale override (kubeconfig changed under us) — fall back below.
	}
	if raw.CurrentContext != "" {
		if _, ok := raw.Contexts[raw.CurrentContext]; ok {
			return raw.CurrentContext, nil
		}
	}
	if len(raw.Contexts) == 0 {
		return "", errors.New("kubeconfig defines no contexts")
	}
	return "", errors.New("kubeconfig has no current-context set; select a context to continue")
}
