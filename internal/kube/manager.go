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
	"net/url"
	"path/filepath"
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

// discoveryTimeout bounds a discovery enumeration so a black-hole apiserver
// (TCP connects but never responds) fails fast as a 502 instead of parking the
// handler goroutine forever. Discovery walks every group, so it is given more
// headroom than a single-call health probe.
const discoveryTimeout = 15 * time.Second

// Manager parses the kubeconfig on demand, tracks the active context in memory,
// and caches a successfully-built rest.Config + clientset per context. Failures
// are not cached, so a kubeconfig that appears (or is fixed) after startup is
// picked up on the next request without a restart.
type Manager struct {
	kubeconfigPath string
	probeTimeout   time.Duration

	mu       sync.RWMutex
	active   string                   // in-memory override; "" = kubeconfig current-context
	clients  map[string]*cachedClient // per-context cache of successful builds
	onSwitch func(current string)     // notified after a context switch (nil = no observer)
	// sourceGen counts kubeconfig-source swaps (ADR-0007). Context names are
	// not globally unique across kubeconfig files, so every context-keyed cache
	// (discovery, stream informers) folds this into its key — a swap then makes
	// same-named contexts from the old file unreachable via cache keys instead
	// of silently serving the old cluster's data.
	sourceGen int64
	// onSourceChange is notified after a successful SetKubeconfigPath; the
	// wiring tears down live sessions (exec, port-forwards) whose credentials
	// came from the previous source (nil = no observer).
	onSourceChange func()
}

type cachedClient struct {
	restConfig *rest.Config
	clientset  kubernetes.Interface
	// dynamicClient serves the generic resource engine (ADR-0003): get/list
	// for any GVR, including CRDs, via unstructured objects.
	dynamicClient dynamic.Interface
	// discoveryClient enumerates API groups/resources. It is built from a
	// timeout-bearing copy of restConfig so discovery can never hang.
	discoveryClient discovery.DiscoveryInterface
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
	// Reason is the FailureClass string for an unreachable/rejected context.
	Reason string `json:"reason,omitempty"`
	// DocURL links to the doc covering this failure's fix, when one applies.
	DocURL string `json:"docURL,omitempty"`
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
// kubeconfig is never written). The name must exist in the kubeconfig. After a
// successful switch it notifies the switch observer (if registered) so
// per-context live sessions bound to another context — exec terminals,
// port-forwards — are torn down (Sprint 6); those never outlive their context.
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
	obs := m.onSwitch
	m.mu.Unlock()
	if obs != nil {
		obs(name)
	}
	return nil
}

// SetSwitchObserver registers a callback invoked after every successful
// SwitchContext with the new active context name. It lets the streaming layer
// tear down sessions that must not cross a context boundary without the kube
// package depending on it. Set once at startup; a nil fn clears it.
func (m *Manager) SetSwitchObserver(fn func(current string)) {
	m.mu.Lock()
	m.onSwitch = fn
	m.mu.Unlock()
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

// RestConfigFor returns the rest.Config for the named context, built and cached
// alongside the typed and dynamic clients on first use. The exec SPDY executor
// and the port-forward dialer need the raw config (Sprint 6). A copy is returned
// so a caller tuning transport-level fields never mutates the config the shared
// typed/dynamic clients use.
func (m *Manager) RestConfigFor(name string) (*rest.Config, error) {
	cached, err := m.clientsFor(name)
	if err != nil {
		return nil, err
	}
	return rest.CopyConfig(cached.restConfig), nil
}

// DiscoveryFor returns a discovery client for the named context, used to
// enumerate every API group/version/resource the cluster serves (Sprint 2).
// Callers resolve the active context name once and pass it here so the name
// and the client cannot diverge under a concurrent context switch.
func (m *Manager) DiscoveryFor(name string) (discovery.DiscoveryInterface, error) {
	cached, err := m.clientsFor(name)
	if err != nil {
		return nil, err
	}
	return cached.discoveryClient, nil
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
	// Read the source path under the held lock (buildRestConfig takes it as an
	// argument) so a concurrent SetKubeconfigPath can't deadlock on a re-lock.
	restCfg, err := m.buildRestConfig(m.kubeconfigPath, name)
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
	// Discovery gets its own timeout-bearing config copy; the shared restCfg
	// (used by the typed and dynamic clients) is left without a global timeout.
	discCfg := rest.CopyConfig(restCfg)
	discCfg.Timeout = discoveryTimeout
	disc, err := discovery.NewDiscoveryClientForConfig(discCfg)
	if err != nil {
		return nil, fmt.Errorf("building discovery client for context %q: %w", name, err)
	}
	cached = &cachedClient{restConfig: restCfg, clientset: cs, dynamicClient: dyn, discoveryClient: disc}
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
	hints := m.hintsFor(raw, name)

	restCfg, err := m.buildRestConfig(m.KubeconfigPath(), name)
	if err != nil {
		h := classify(err, hints)
		h.Name = name
		return h
	}
	restCfg.Timeout = m.probeTimeout

	cs, err := kubernetes.NewForConfig(restCfg)
	if err != nil {
		h := classify(err, hints)
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
			h := classify(res.err, hints)
			h.Name = name
			return h
		}
		// The probe reached the server on a freshly built config. If a cached
		// client points at a different endpoint, the kubeconfig changed under us
		// (e.g. a local cluster recreated on a new port) — evict it so REST and
		// stream paths rebuild against the current file instead of dialing the
		// dead endpoint forever.
		m.evictStaleClient(name, restCfg.Host)
		return ContextHealth{Name: name, Reachable: true, AuthOK: true, ServerVersion: res.version}
	}
}

// evictStaleClient drops the cached client for name when its endpoint no longer
// matches the freshly resolved server host. Only called after a successful
// probe of that host, so a working cache is never dropped on a transient error.
func (m *Manager) evictStaleClient(name, freshHost string) {
	m.mu.Lock()
	defer m.mu.Unlock()
	if cached, ok := m.clients[name]; ok && cached.restConfig.Host != freshHost {
		delete(m.clients, name)
	}
}

// rawConfig loads the kubeconfig from the current source path. A missing or
// malformed file returns a wrapped error rather than falling back to other
// kubeconfigs.
func (m *Manager) rawConfig() (clientcmdapi.Config, error) {
	return m.rawConfigAt(m.KubeconfigPath())
}

// rawConfigAt loads and validates a kubeconfig at an explicit path. It never
// touches Manager state, so it doubles as the validate-before-swap step of
// SetKubeconfigPath. Error messages carry the path and the clientcmd parse
// error only — never file contents.
func (m *Manager) rawConfigAt(path string) (clientcmdapi.Config, error) {
	loadingRules := &clientcmd.ClientConfigLoadingRules{ExplicitPath: path}
	cfg, err := loadingRules.Load()
	if err != nil {
		return clientcmdapi.Config{}, fmt.Errorf("loading kubeconfig %q: %w", path, err)
	}
	if cfg == nil {
		return clientcmdapi.Config{}, fmt.Errorf("loading kubeconfig %q: empty config", path)
	}
	return *cfg, nil
}

// buildRestConfig builds a rest.Config for the named context from the given
// kubeconfig path. Embedded certs/tokens work as-is; exec-plugin and
// file-path-cert gotchas surface at call time and are handled by the health
// probe (ADR-0004). The path is passed in (not read from m) so a caller holding
// m.mu can reuse the value it already read without re-locking.
func (m *Manager) buildRestConfig(path, name string) (*rest.Config, error) {
	loadingRules := &clientcmd.ClientConfigLoadingRules{ExplicitPath: path}
	overrides := &clientcmd.ConfigOverrides{CurrentContext: name}
	cc := clientcmd.NewNonInteractiveDeferredLoadingClientConfig(loadingRules, overrides)
	restCfg, err := cc.ClientConfig()
	if err != nil {
		return nil, fmt.Errorf("building rest config for context %q: %w", name, err)
	}
	return restCfg, nil
}

// KubeconfigPath returns the current kubeconfig source path.
func (m *Manager) KubeconfigPath() string {
	m.mu.RLock()
	defer m.mu.RUnlock()
	return m.kubeconfigPath
}

// SetKubeconfigPath repoints the Manager at a different kubeconfig file at
// runtime (ADR-0007). The candidate is validated BEFORE any swap — it must be a
// non-empty absolute path that parses and defines at least one context — so a
// working source is never lost to a typo. On success the source path, the
// in-memory active-context override, and the per-context client cache are reset
// in a single critical section, so setup state and /contexts reflect the new
// file immediately. File contents are never logged or echoed in errors.
func (m *Manager) SetKubeconfigPath(path string) error {
	if path == "" {
		return errors.New("kubeconfig path must not be empty")
	}
	if !filepath.IsAbs(path) {
		return fmt.Errorf("kubeconfig path %q must be absolute", path)
	}
	raw, err := m.rawConfigAt(path)
	if err != nil {
		return err
	}
	if len(raw.Contexts) == 0 {
		return fmt.Errorf("kubeconfig %q defines no contexts", path)
	}

	m.mu.Lock()
	m.kubeconfigPath = path
	m.active = ""
	m.clients = make(map[string]*cachedClient)
	m.sourceGen++
	obs := m.onSourceChange
	m.mu.Unlock()
	if obs != nil {
		obs()
	}
	return nil
}

// SourceGeneration identifies the current kubeconfig source: it increments on
// every successful SetKubeconfigPath. Context-keyed caches include it in their
// keys so a source swap never serves data cached from the previous file's
// same-named context.
func (m *Manager) SourceGeneration() int64 {
	m.mu.RLock()
	defer m.mu.RUnlock()
	return m.sourceGen
}

// SetSourceObserver registers a callback invoked after every successful
// SetKubeconfigPath. It lets the streaming layer tear down live sessions
// (exec terminals, port-forwards) built on the previous source's credentials —
// the source-swap analog of the context-switch observer. Set once at startup;
// a nil fn clears it.
func (m *Manager) SetSourceObserver(fn func()) {
	m.mu.Lock()
	m.onSourceChange = fn
	m.mu.Unlock()
}

// hintsFor derives the classification hints for a context: the exec-plugin
// command (if any) and whether the apiserver host is a loopback address.
func (m *Manager) hintsFor(raw clientcmdapi.Config, name string) ClassifyHints {
	return ClassifyHints{
		ExecCommand:    execCommand(raw, name),
		LoopbackServer: serverIsLoopback(raw, name),
	}
}

// serverIsLoopback reports whether the named context's cluster server URL points
// at a loopback host (127.0.0.1 / localhost / ::1) — the case where a
// containerized Kubescope needs host.docker.internal or --network host.
func serverIsLoopback(raw clientcmdapi.Config, name string) bool {
	c, ok := raw.Contexts[name]
	if !ok {
		return false
	}
	cl, ok := raw.Clusters[c.Cluster]
	if !ok {
		return false
	}
	u, err := url.Parse(cl.Server)
	if err != nil {
		return false
	}
	switch u.Hostname() { // Hostname strips the port and IPv6 brackets
	case "127.0.0.1", "localhost", "::1":
		return true
	}
	return false
}

// ClassifyActiveError sorts an error from the active context into the failure
// taxonomy, resolving that context's hints on a best-effort basis (zero hints if
// the kubeconfig can't be read or no context is active).
func (m *Manager) ClassifyActiveError(err error) Classification {
	var hints ClassifyHints
	if raw, rerr := m.rawConfig(); rerr == nil {
		if name, aerr := m.resolveActive(raw); aerr == nil {
			hints = m.hintsFor(raw, name)
		}
	}
	return ClassifyError(err, hints)
}

// ProbeContext probes a single named context's connectivity. A kubeconfig that
// can't be read, or a name the kubeconfig does not define, yields an unreachable
// ContextHealth with reason "unknown" rather than an error.
func (m *Manager) ProbeContext(ctx context.Context, name string) ContextHealth {
	raw, err := m.rawConfig()
	if err != nil {
		return ContextHealth{Name: name, Error: err.Error(), Reason: string(FailUnknown)}
	}
	if _, ok := raw.Contexts[name]; !ok {
		return ContextHealth{Name: name, Error: "unknown context", Reason: string(FailUnknown)}
	}
	return m.probe(ctx, raw, name)
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
