package resources_test

// Envtest-backed test: boots a real kube-apiserver (via controller-runtime
// envtest), writes its connection details to a kubeconfig file, and exercises
// the full chain kubeconfig → kube.Manager → router → /api/v1/nodes.
//
// Requires KUBEBUILDER_ASSETS (make test sets it via setup-envtest); skipped
// otherwise so plain `go test ./...` stays green on a fresh machine.

import (
	"context"
	"encoding/json"
	"io"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/client-go/kubernetes"
	"k8s.io/client-go/rest"
	"k8s.io/client-go/tools/clientcmd"
	clientcmdapi "k8s.io/client-go/tools/clientcmd/api"
	"sigs.k8s.io/controller-runtime/pkg/envtest"

	"github.com/skriptvalley/kubescope/internal/kube"
	"github.com/skriptvalley/kubescope/internal/resources"
	"github.com/skriptvalley/kubescope/internal/server"
)

func writeKubeconfig(t *testing.T, cfg *rest.Config) string {
	t.Helper()
	kc := clientcmdapi.Config{
		Clusters: map[string]*clientcmdapi.Cluster{
			"envtest": {Server: cfg.Host, CertificateAuthorityData: cfg.CAData},
		},
		AuthInfos: map[string]*clientcmdapi.AuthInfo{
			"envtest": {
				ClientCertificateData: cfg.CertData,
				ClientKeyData:         cfg.KeyData,
				Token:                 cfg.BearerToken,
			},
		},
		Contexts: map[string]*clientcmdapi.Context{
			"envtest": {Cluster: "envtest", AuthInfo: "envtest"},
		},
		CurrentContext: "envtest",
	}
	path := filepath.Join(t.TempDir(), "kubeconfig")
	require.NoError(t, clientcmd.WriteToFile(kc, path))
	return path
}

func TestNodesEndpointAgainstEnvtest(t *testing.T) {
	if os.Getenv("KUBEBUILDER_ASSETS") == "" {
		t.Skip("KUBEBUILDER_ASSETS not set; run via `make test`")
	}

	env := &envtest.Environment{}
	cfg, err := env.Start()
	require.NoError(t, err)
	t.Cleanup(func() { require.NoError(t, env.Stop()) })

	// Seed a node with status through a direct clientset.
	direct, err := kubernetes.NewForConfig(cfg)
	require.NoError(t, err)
	ctx := context.Background()

	created, err := direct.CoreV1().Nodes().Create(ctx, &corev1.Node{
		ObjectMeta: metav1.ObjectMeta{Name: "envtest-node-1"},
	}, metav1.CreateOptions{})
	require.NoError(t, err)

	created.Status = corev1.NodeStatus{
		Conditions: []corev1.NodeCondition{{Type: corev1.NodeReady, Status: corev1.ConditionTrue}},
		NodeInfo:   corev1.NodeSystemInfo{KubeletVersion: "v1.33.0"},
	}
	_, err = direct.CoreV1().Nodes().UpdateStatus(ctx, created, metav1.UpdateOptions{})
	require.NoError(t, err)

	// The handler under test reaches the apiserver only through the
	// kubeconfig file, exactly as production does.
	handler := server.New(server.Options{
		Logger: slog.New(slog.NewTextHandler(io.Discard, nil)),
		Kube:   kube.NewManager([]string{writeKubeconfig(t, cfg)}),
		Dist:   os.DirFS(t.TempDir()),
	})

	rec := httptest.NewRecorder()
	handler.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/api/v1/nodes", nil))

	require.Equal(t, http.StatusOK, rec.Code, "body: %s", rec.Body.String())
	var body struct {
		Items []resources.NodeSummary `json:"items"`
	}
	require.NoError(t, json.Unmarshal(rec.Body.Bytes(), &body))
	require.Len(t, body.Items, 1)
	assert.Equal(t, resources.NodeSummary{Name: "envtest-node-1", Status: "Ready", Version: "v1.33.0"}, body.Items[0])
}

func TestNodesEndpointMissingKubeconfig(t *testing.T) {
	handler := server.New(server.Options{
		Logger: slog.New(slog.NewTextHandler(io.Discard, nil)),
		Kube:   kube.NewManager([]string{filepath.Join(t.TempDir(), "does-not-exist")}),
		Dist:   os.DirFS(t.TempDir()),
	})

	rec := httptest.NewRecorder()
	handler.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/api/v1/nodes", nil))

	assert.Equal(t, http.StatusServiceUnavailable, rec.Code)
	var envelope struct {
		Error struct {
			Code string `json:"code"`
		} `json:"error"`
	}
	require.NoError(t, json.Unmarshal(rec.Body.Bytes(), &envelope))
	assert.Equal(t, "kubeconfig_unavailable", envelope.Error.Code)
}
