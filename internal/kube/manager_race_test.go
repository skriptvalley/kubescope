package kube

import (
	"fmt"
	"sync"
	"testing"

	"github.com/stretchr/testify/require"
	clientcmdapi "k8s.io/client-go/tools/clientcmd/api"
)

// TestManagerConcurrentAccess hammers the per-context client cache and the
// active-context state from many goroutines. Run with `go test -race` (make
// test sets -race) to prove the cache is concurrency-safe.
func TestManagerConcurrentAccess(t *testing.T) {
	ca := testCACert(t)
	const nContexts = 6
	clusters := map[string]*clientcmdapi.Cluster{}
	auths := map[string]*clientcmdapi.AuthInfo{}
	contexts := map[string]*clientcmdapi.Context{}
	names := make([]string, nContexts)
	for i := 0; i < nContexts; i++ {
		name := fmt.Sprintf("ctx-%d", i)
		names[i] = name
		clusters[name] = &clientcmdapi.Cluster{Server: "https://127.0.0.1:6443", CertificateAuthorityData: ca}
		auths[name] = &clientcmdapi.AuthInfo{Token: "t"}
		contexts[name] = &clientcmdapi.Context{Cluster: name, AuthInfo: name}
	}
	cfg := clientcmdapi.Config{
		Clusters:       clusters,
		AuthInfos:      auths,
		Contexts:       contexts,
		CurrentContext: names[0],
	}
	m := NewManager(writeConfig(t, cfg))

	const workers = 24
	var wg sync.WaitGroup
	wg.Add(workers)
	for w := 0; w < workers; w++ {
		go func(w int) {
			defer wg.Done()
			for i := 0; i < 50; i++ {
				name := names[(w+i)%nContexts]
				switch (w + i) % 4 {
				case 0:
					_, _ = m.ClientsetFor(name)
				case 1:
					_, _ = m.Clientset()
				case 2:
					_ = m.SwitchContext(name)
				case 3:
					_, _ = m.Contexts()
				}
			}
		}(w)
	}
	wg.Wait()

	// Cache is populated and usable after the storm.
	cs, err := m.ClientsetFor(names[0])
	require.NoError(t, err)
	require.NotNil(t, cs)
}
