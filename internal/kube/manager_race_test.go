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
	m := newManager(writeConfig(t, cfg))

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

// TestManagerConcurrentRegistryMutation races AddSource/RemoveSource against
// readers (Contexts / ClientsetFor / Sources / SourcePaths) to prove the
// mutex-guarded registry swap and client-cache reset are concurrency-safe. Run
// with `go test -race`. The env-baseline source is kept for the whole run so the
// registry always resolves, while a second source is added and removed under
// contention; assertions stay loose because concurrent add/remove of the same
// path is inherently racy on the final membership — the invariant under test is
// the absence of data races and a always-resolvable registry.
func TestManagerConcurrentRegistryMutation(t *testing.T) {
	ca := testCACert(t)
	newFile := func(ctxName string) string {
		return writeConfig(t, clientcmdapi.Config{
			Clusters:       map[string]*clientcmdapi.Cluster{"c": {Server: "https://127.0.0.1:6443", CertificateAuthorityData: ca}},
			AuthInfos:      map[string]*clientcmdapi.AuthInfo{"u": {Token: "t"}},
			Contexts:       map[string]*clientcmdapi.Context{ctxName: {Cluster: "c", AuthInfo: "u"}},
			CurrentContext: ctxName,
		})
	}
	pathA := newFile("alpha") // stable env baseline, never removed
	pathB := newFile("beta")  // toggled in and out under contention
	idB := sourceID(pathB)
	m := newManager(pathA)

	const workers = 16
	var wg sync.WaitGroup
	wg.Add(workers)
	for w := 0; w < workers; w++ {
		go func(w int) {
			defer wg.Done()
			for i := 0; i < 40; i++ {
				switch (w + i) % 5 {
				case 0:
					_ = m.AddSource(pathB)
				case 1:
					_ = m.RemoveSource(idB)
				case 2:
					_, _ = m.Contexts()
				case 3:
					_ = m.SourcePaths()
					_, _ = m.ClientsetFor("alpha")
				case 4:
					_ = m.Sources()
				}
			}
		}(w)
	}
	wg.Wait()

	// The baseline survives regardless of how the race resolved, so the registry
	// still serves contexts.
	require.Contains(t, m.SourcePaths(), pathA)
	_, err := m.Contexts()
	require.NoError(t, err)
}
