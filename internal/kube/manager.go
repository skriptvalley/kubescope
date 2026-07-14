// Package kube loads the kubeconfig and hands out Kubernetes clients for the
// current context. Sprint 1 extends this into full context enumeration,
// switching and per-context caches.
package kube

import (
	"fmt"
	"sync"

	"k8s.io/client-go/kubernetes"
	"k8s.io/client-go/rest"
	"k8s.io/client-go/tools/clientcmd"
)

// Manager builds and caches a typed clientset for the kubeconfig's current
// context. A successful clientset is cached; failures are not, so a
// kubeconfig that appears (or is fixed) after startup is picked up on the
// next request without a restart.
type Manager struct {
	kubeconfigPath string

	mu        sync.Mutex
	clientset kubernetes.Interface
}

// NewManager returns a Manager reading the kubeconfig at path. The file is
// not touched until the first client is requested.
func NewManager(path string) *Manager {
	return &Manager{kubeconfigPath: path}
}

// Clientset returns a typed clientset for the current context, building it on
// first use.
func (m *Manager) Clientset() (kubernetes.Interface, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	if m.clientset != nil {
		return m.clientset, nil
	}

	restCfg, err := m.restConfig()
	if err != nil {
		return nil, err
	}
	cs, err := kubernetes.NewForConfig(restCfg)
	if err != nil {
		return nil, fmt.Errorf("building clientset: %w", err)
	}
	m.clientset = cs
	return m.clientset, nil
}

// restConfig loads the kubeconfig and builds a rest.Config for its current
// context. Embedded certs/tokens work as-is; exec-plugin and file-path-cert
// gotchas are documented in ADR-0004.
func (m *Manager) restConfig() (*rest.Config, error) {
	loadingRules := &clientcmd.ClientConfigLoadingRules{ExplicitPath: m.kubeconfigPath}
	clientConfig := clientcmd.NewNonInteractiveDeferredLoadingClientConfig(loadingRules, &clientcmd.ConfigOverrides{})
	restCfg, err := clientConfig.ClientConfig()
	if err != nil {
		return nil, fmt.Errorf("loading kubeconfig %q: %w", m.kubeconfigPath, err)
	}
	return restCfg, nil
}
