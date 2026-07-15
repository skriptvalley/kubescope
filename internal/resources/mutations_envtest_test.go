package resources_test

// Envtest-backed integration test for the Sprint 5 write paths: boots a real
// kube-apiserver and drives generic apply (incl. a CRD instance and a 409
// conflict), the scale subresource, generic delete, node cordon, drain via the
// eviction API (incl. a PDB-blocked pod and a DaemonSet-skip), and Secret
// masking end-to-end through kubeconfig → kube.Manager → router.
//
// Requires KUBEBUILDER_ASSETS (make test sets it); skipped otherwise.

import (
	"bytes"
	"context"
	"encoding/json"
	"io"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"os"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	appsv1 "k8s.io/api/apps/v1"
	corev1 "k8s.io/api/core/v1"
	policyv1 "k8s.io/api/policy/v1"
	apiextensionsv1 "k8s.io/apiextensions-apiserver/pkg/apis/apiextensions/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"k8s.io/apimachinery/pkg/runtime/schema"
	"k8s.io/apimachinery/pkg/util/intstr"
	"k8s.io/client-go/dynamic"
	"k8s.io/client-go/kubernetes"
	"sigs.k8s.io/controller-runtime/pkg/envtest"
	"sigs.k8s.io/yaml"

	"github.com/skriptvalley/kubescope/internal/kube"
	"github.com/skriptvalley/kubescope/internal/server"
)

func int32p(i int32) *int32 { return &i }

func TestMutationsAgainstEnvtest(t *testing.T) {
	if os.Getenv("KUBEBUILDER_ASSETS") == "" {
		t.Skip("KUBEBUILDER_ASSETS not set; run via `make test`")
	}

	env := &envtest.Environment{CRDs: []*apiextensionsv1.CustomResourceDefinition{widgetCRD()}}
	cfg, err := env.Start()
	require.NoError(t, err)
	t.Cleanup(func() { require.NoError(t, env.Stop()) })

	ctx := context.Background()
	dyn, err := dynamic.NewForConfig(cfg)
	require.NoError(t, err)
	cs, err := kubernetes.NewForConfig(cfg)
	require.NoError(t, err)
	widgetGVR := schema.GroupVersionResource{Group: "example.com", Version: "v1", Resource: "widgets"}

	handler := server.New(server.Options{
		Logger: slog.New(slog.NewTextHandler(io.Discard, nil)),
		Kube:   kube.NewManager(writeKubeconfig(t, cfg)),
		Dist:   os.DirFS(t.TempDir()),
	})
	do := func(method, path string, body []byte) *httptest.ResponseRecorder {
		var r *http.Request
		if body != nil {
			r = httptest.NewRequest(method, path, bytes.NewReader(body))
			r.Header.Set("Content-Type", "application/json")
		} else {
			r = httptest.NewRequest(method, path, nil)
		}
		rec := httptest.NewRecorder()
		handler.ServeHTTP(rec, r)
		return rec
	}
	applyBody := func(t *testing.T, obj map[string]any) []byte {
		y, err := yaml.Marshal(obj)
		require.NoError(t, err)
		b, err := json.Marshal(map[string]string{"yaml": string(y)})
		require.NoError(t, err)
		return b
	}

	t.Run("apply edits a CRD instance generically", func(t *testing.T) {
		_, err := dyn.Resource(widgetGVR).Namespace("default").Create(ctx, widget("w-edit", "default"), metav1.CreateOptions{})
		require.NoError(t, err)

		// Read it back through the API, flip a spec field, apply.
		rec := do(http.MethodGet, "/api/v1/resources/example.com/v1/widgets/w-edit?namespace=default", nil)
		require.Equal(t, http.StatusOK, rec.Code)
		var got struct {
			Object map[string]any `json:"object"`
		}
		require.NoError(t, json.Unmarshal(rec.Body.Bytes(), &got))
		got.Object["spec"] = map[string]any{"size": "small"}

		rec = do(http.MethodPut, "/api/v1/resources/example.com/v1/widgets/w-edit?namespace=default", applyBody(t, got.Object))
		require.Equal(t, http.StatusOK, rec.Code, "body: %s", rec.Body.String())

		after, err := dyn.Resource(widgetGVR).Namespace("default").Get(ctx, "w-edit", metav1.GetOptions{})
		require.NoError(t, err)
		size, _, _ := unstructured.NestedString(after.Object, "spec", "size")
		assert.Equal(t, "small", size, "apply persisted the edited spec via the dynamic client")
	})

	t.Run("stale resourceVersion is a 409, never a silent overwrite", func(t *testing.T) {
		_, err := dyn.Resource(widgetGVR).Namespace("default").Create(ctx, widget("w-conflict", "default"), metav1.CreateOptions{})
		require.NoError(t, err)

		// Capture the object (with its resourceVersion) as the editor would.
		rec := do(http.MethodGet, "/api/v1/resources/example.com/v1/widgets/w-conflict?namespace=default", nil)
		require.Equal(t, http.StatusOK, rec.Code)
		var got struct {
			Object map[string]any `json:"object"`
		}
		require.NoError(t, json.Unmarshal(rec.Body.Bytes(), &got))

		// Someone else updates it, bumping the resourceVersion out from under us.
		// The new value must differ from the seed ("large") or the write is a
		// no-op and the resourceVersion never moves.
		current, err := dyn.Resource(widgetGVR).Namespace("default").Get(ctx, "w-conflict", metav1.GetOptions{})
		require.NoError(t, err)
		require.NoError(t, unstructured.SetNestedField(current.Object, "changed-externally", "spec", "size"))
		_, err = dyn.Resource(widgetGVR).Namespace("default").Update(ctx, current, metav1.UpdateOptions{})
		require.NoError(t, err)

		// Applying the now-stale copy must conflict.
		rec = do(http.MethodPut, "/api/v1/resources/example.com/v1/widgets/w-conflict?namespace=default", applyBody(t, got.Object))
		assert.Equal(t, http.StatusConflict, rec.Code, "body: %s", rec.Body.String())
		assert.Equal(t, "conflict", errorCodeOf(t, rec.Body.Bytes()))
	})

	t.Run("apply rejects a name mismatch", func(t *testing.T) {
		obj := map[string]any{
			"apiVersion": "example.com/v1", "kind": "Widget",
			"metadata": map[string]any{"name": "renamed", "namespace": "default"},
		}
		rec := do(http.MethodPut, "/api/v1/resources/example.com/v1/widgets/w-edit?namespace=default", applyBody(t, obj))
		assert.Equal(t, http.StatusBadRequest, rec.Code)
		assert.Equal(t, "name_mismatch", errorCodeOf(t, rec.Body.Bytes()))
	})

	t.Run("delete removes any GVR generically", func(t *testing.T) {
		_, err := dyn.Resource(widgetGVR).Namespace("default").Create(ctx, widget("w-del", "default"), metav1.CreateOptions{})
		require.NoError(t, err)

		rec := do(http.MethodDelete, "/api/v1/resources/example.com/v1/widgets/w-del?namespace=default", nil)
		require.Equal(t, http.StatusOK, rec.Code, "body: %s", rec.Body.String())

		_, err = dyn.Resource(widgetGVR).Namespace("default").Get(ctx, "w-del", metav1.GetOptions{})
		assert.Error(t, err, "the widget is gone")
	})

	t.Run("scale updates the replica count via the subresource", func(t *testing.T) {
		_, err := cs.AppsV1().Deployments("default").Create(ctx, sampleDeployment("scale-me"), metav1.CreateOptions{})
		require.NoError(t, err)

		rec := do(http.MethodPost, "/api/v1/workloads/deployments/default/scale-me/scale", []byte(`{"replicas":3}`))
		require.Equal(t, http.StatusOK, rec.Code, "body: %s", rec.Body.String())

		after, err := cs.AppsV1().Deployments("default").Get(ctx, "scale-me", metav1.GetOptions{})
		require.NoError(t, err)
		require.NotNil(t, after.Spec.Replicas)
		assert.Equal(t, int32(3), *after.Spec.Replicas)
	})

	t.Run("cordon and uncordon toggle schedulability", func(t *testing.T) {
		_, err := cs.CoreV1().Nodes().Create(ctx, &corev1.Node{ObjectMeta: metav1.ObjectMeta{Name: "cordon-me"}}, metav1.CreateOptions{})
		require.NoError(t, err)

		rec := do(http.MethodPost, "/api/v1/nodes/cordon-me/cordon", nil)
		require.Equal(t, http.StatusOK, rec.Code, "body: %s", rec.Body.String())
		n, err := cs.CoreV1().Nodes().Get(ctx, "cordon-me", metav1.GetOptions{})
		require.NoError(t, err)
		assert.True(t, n.Spec.Unschedulable)

		rec = do(http.MethodPost, "/api/v1/nodes/cordon-me/uncordon", nil)
		require.Equal(t, http.StatusOK, rec.Code)
		n, err = cs.CoreV1().Nodes().Get(ctx, "cordon-me", metav1.GetOptions{})
		require.NoError(t, err)
		assert.False(t, n.Spec.Unschedulable)
	})

	t.Run("drain evicts, skips DaemonSet pods, and reports PDB-blocked pods", func(t *testing.T) {
		_, err := cs.CoreV1().Nodes().Create(ctx, &corev1.Node{ObjectMeta: metav1.ObjectMeta{Name: "worker"}}, metav1.CreateOptions{})
		require.NoError(t, err)

		// A free pod (evictable), a guarded pod (PDB blocks it), a DaemonSet pod
		// (skipped). All pinned to the node via spec.nodeName.
		_, err = cs.CoreV1().Pods("default").Create(ctx, nodePod("free", "worker", nil, nil), metav1.CreateOptions{})
		require.NoError(t, err)
		guarded, err := cs.CoreV1().Pods("default").Create(ctx, nodePod("guarded", "worker", map[string]string{"app": "guarded"}, nil), metav1.CreateOptions{})
		require.NoError(t, err)
		// The eviction API ignores PDBs for Pending pods (canIgnorePDB); mark the
		// guarded pod Running so its PDB is actually enforced on eviction.
		guarded.Status.Phase = corev1.PodRunning
		_, err = cs.CoreV1().Pods("default").UpdateStatus(ctx, guarded, metav1.UpdateOptions{})
		require.NoError(t, err)
		ctrl := true
		dsRef := []metav1.OwnerReference{{APIVersion: "apps/v1", Kind: "DaemonSet", Name: "ds", UID: "u", Controller: &ctrl}}
		_, err = cs.CoreV1().Pods("default").Create(ctx, nodePod("ds-pod", "worker", nil, dsRef), metav1.CreateOptions{})
		require.NoError(t, err)

		// A PDB that will block eviction of the guarded pod: its status is never
		// processed (envtest runs no disruption controller), so the eviction API
		// rejects with 429 — exactly the blocked path we want to surface.
		_, err = cs.PolicyV1().PodDisruptionBudgets("default").Create(ctx, &policyv1.PodDisruptionBudget{
			ObjectMeta: metav1.ObjectMeta{Name: "guard"},
			Spec: policyv1.PodDisruptionBudgetSpec{
				MinAvailable: ptrIntStr(1),
				Selector:     &metav1.LabelSelector{MatchLabels: map[string]string{"app": "guarded"}},
			},
		}, metav1.CreateOptions{})
		require.NoError(t, err)

		rec := do(http.MethodPost, "/api/v1/nodes/worker/drain", nil)
		require.Equal(t, http.StatusOK, rec.Code, "body: %s", rec.Body.String())

		var res struct {
			Evicted, Skipped, Blocked, Failed int
			Pods                              []struct{ Namespace, Name, Result, Reason string }
		}
		require.NoError(t, json.Unmarshal(rec.Body.Bytes(), &res))
		assert.Equal(t, 1, res.Evicted, "the free pod is evicted")
		assert.Equal(t, 1, res.Skipped, "the DaemonSet pod is skipped")
		assert.Equal(t, 1, res.Blocked, "the guarded pod is PDB-blocked, not swallowed")
		assert.Zero(t, res.Failed)

		// The node is cordoned as part of drain.
		n, err := cs.CoreV1().Nodes().Get(ctx, "worker", metav1.GetOptions{})
		require.NoError(t, err)
		assert.True(t, n.Spec.Unschedulable)
	})

	t.Run("secret data is masked in get and yaml, revealed per key", func(t *testing.T) {
		_, err := cs.CoreV1().Secrets("default").Create(ctx, &corev1.Secret{
			ObjectMeta: metav1.ObjectMeta{Name: "db", Namespace: "default"},
			Data:       map[string][]byte{"password": []byte("hunter2")},
		}, metav1.CreateOptions{})
		require.NoError(t, err)

		// get: the value is redacted, the key preserved.
		rec := do(http.MethodGet, "/api/v1/resources/core/v1/secrets/db?namespace=default", nil)
		require.Equal(t, http.StatusOK, rec.Code)
		assert.Contains(t, rec.Body.String(), "**redacted**")
		assert.NotContains(t, rec.Body.String(), "aHVudGVyMg==", "the base64 value must not ship")

		// yaml: same masking in the raw view.
		rec = do(http.MethodGet, "/api/v1/resources/core/v1/secrets/db/yaml?namespace=default", nil)
		require.Equal(t, http.StatusOK, rec.Code)
		assert.Contains(t, rec.Body.String(), "**redacted**")
		assert.NotContains(t, rec.Body.String(), "aHVudGVyMg==")

		// reveal: the real value, one key, on explicit request.
		rec = do(http.MethodGet, "/api/v1/secrets/default/db/reveal?key=password", nil)
		require.Equal(t, http.StatusOK, rec.Code)
		var reveal struct{ Key, Value string }
		require.NoError(t, json.Unmarshal(rec.Body.Bytes(), &reveal))
		assert.Equal(t, "hunter2", reveal.Value)
	})

	t.Run("read-only mode rejects mutations with a 403", func(t *testing.T) {
		ro := server.New(server.Options{
			Logger:   slog.New(slog.NewTextHandler(io.Discard, nil)),
			Kube:     kube.NewManager(writeKubeconfig(t, cfg)),
			ReadOnly: true,
			Dist:     os.DirFS(t.TempDir()),
		})
		rec := httptest.NewRecorder()
		ro.ServeHTTP(rec, httptest.NewRequest(http.MethodDelete,
			"/api/v1/resources/example.com/v1/widgets/w-edit?namespace=default", nil))
		assert.Equal(t, http.StatusForbidden, rec.Code)

		// And the object is still there — the mutation never reached the cluster.
		_, err := dyn.Resource(widgetGVR).Namespace("default").Get(ctx, "w-edit", metav1.GetOptions{})
		assert.NoError(t, err)
	})
}

func ptrIntStr(i int) *intstr.IntOrString {
	v := intstr.FromInt32(int32(i))
	return &v
}

func sampleDeployment(name string) *appsv1.Deployment {
	labels := map[string]string{"app": name}
	return &appsv1.Deployment{
		ObjectMeta: metav1.ObjectMeta{Name: name, Namespace: "default"},
		Spec: appsv1.DeploymentSpec{
			Replicas: int32p(1),
			Selector: &metav1.LabelSelector{MatchLabels: labels},
			Template: corev1.PodTemplateSpec{
				ObjectMeta: metav1.ObjectMeta{Labels: labels},
				Spec:       corev1.PodSpec{Containers: []corev1.Container{{Name: "c", Image: "nginx"}}},
			},
		},
	}
}

func nodePod(name, node string, labels map[string]string, owners []metav1.OwnerReference) *corev1.Pod {
	return &corev1.Pod{
		ObjectMeta: metav1.ObjectMeta{Name: name, Namespace: "default", Labels: labels, OwnerReferences: owners},
		Spec: corev1.PodSpec{
			NodeName:   node,
			Containers: []corev1.Container{{Name: "c", Image: "nginx"}},
		},
	}
}

// errorCodeOf reads the structured error envelope's code (the resources package's
// helper is package-internal; this mirrors it for the external test package).
func errorCodeOf(t *testing.T, body []byte) string {
	t.Helper()
	var env struct {
		Error struct{ Code string } `json:"error"`
	}
	require.NoError(t, json.Unmarshal(body, &env))
	return env.Error.Code
}
