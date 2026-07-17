package resources_test

// Envtest-backed integration test for the Sprint 3 typed workload engine: boots
// a real kube-apiserver, seeds a Deployment (→ ReplicaSet → Pods), a bare Pod,
// a Job and a CronJob, then drives the typed summary, owned-pods and events
// endpoints through the full chain kubeconfig → kube.Manager → router.
//
// Requires KUBEBUILDER_ASSETS (make test sets it); skipped otherwise.

import (
	"context"
	"encoding/json"
	"io"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"os"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	appsv1 "k8s.io/api/apps/v1"
	batchv1 "k8s.io/api/batch/v1"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/types"
	"k8s.io/client-go/kubernetes"
	"sigs.k8s.io/controller-runtime/pkg/envtest"

	"github.com/skriptvalley/kubescope/internal/kube"
	"github.com/skriptvalley/kubescope/internal/resources"
	"github.com/skriptvalley/kubescope/internal/server"
)

func i32(i int32) *int32 { return &i }

func TestWorkloadEngineAgainstEnvtest(t *testing.T) {
	if os.Getenv("KUBEBUILDER_ASSETS") == "" {
		t.Skip("KUBEBUILDER_ASSETS not set; run via `make test`")
	}

	env := &envtest.Environment{}
	cfg, err := env.Start()
	require.NoError(t, err)
	t.Cleanup(func() { require.NoError(t, env.Stop()) })

	client, err := kubernetes.NewForConfig(cfg)
	require.NoError(t, err)
	ctx := context.Background()
	ns := "default"

	// Deployment → ReplicaSet → 2 Pods, wired with ownerReferences so the
	// owned-pods resolution (selector + ownerRef, RS hop) has something to walk.
	// Envtest has no controllers, so we materialise the RS and pods ourselves.
	labels := map[string]string{"app": "web"}
	dep, err := client.AppsV1().Deployments(ns).Create(ctx, &appsv1.Deployment{
		ObjectMeta: metav1.ObjectMeta{Name: "web", Namespace: ns},
		Spec: appsv1.DeploymentSpec{
			Replicas: i32(2),
			Selector: &metav1.LabelSelector{MatchLabels: labels},
			Template: corev1.PodTemplateSpec{
				ObjectMeta: metav1.ObjectMeta{Labels: labels},
				Spec:       corev1.PodSpec{Containers: []corev1.Container{{Name: "web", Image: "nginx"}}},
			},
		},
	}, metav1.CreateOptions{})
	require.NoError(t, err)

	rs, err := client.AppsV1().ReplicaSets(ns).Create(ctx, &appsv1.ReplicaSet{
		ObjectMeta: metav1.ObjectMeta{
			Name: "web-rs", Namespace: ns, Labels: labels,
			OwnerReferences: []metav1.OwnerReference{ownerRef("apps/v1", "Deployment", dep.Name, dep.UID)},
		},
		Spec: appsv1.ReplicaSetSpec{
			Replicas: i32(2),
			Selector: &metav1.LabelSelector{MatchLabels: labels},
			Template: corev1.PodTemplateSpec{
				ObjectMeta: metav1.ObjectMeta{Labels: labels},
				Spec:       corev1.PodSpec{Containers: []corev1.Container{{Name: "web", Image: "nginx"}}},
			},
		},
	}, metav1.CreateOptions{})
	require.NoError(t, err)

	for _, name := range []string{"web-rs-a", "web-rs-b"} {
		mustCreatePod(t, client, ns, name, labels, ownerRef("apps/v1", "ReplicaSet", rs.Name, rs.UID))
	}
	// A bare pod not owned by the RS — excluded from the deployment's pods.
	mustCreatePod(t, client, ns, "standalone", map[string]string{"app": "other"}, metav1.OwnerReference{})

	// A Job + CronJob to exercise those summaries.
	_, err = client.BatchV1().Jobs(ns).Create(ctx, &batchv1.Job{
		ObjectMeta: metav1.ObjectMeta{Name: "backup", Namespace: ns},
		Spec: batchv1.JobSpec{
			Completions: i32(1),
			Template: corev1.PodTemplateSpec{
				Spec: corev1.PodSpec{RestartPolicy: corev1.RestartPolicyNever, Containers: []corev1.Container{{Name: "b", Image: "busybox"}}},
			},
		},
	}, metav1.CreateOptions{})
	require.NoError(t, err)

	cronJob, err := client.BatchV1().CronJobs(ns).Create(ctx, &batchv1.CronJob{
		ObjectMeta: metav1.ObjectMeta{Name: "nightly", Namespace: ns},
		Spec: batchv1.CronJobSpec{
			Schedule: "0 0 * * *",
			JobTemplate: batchv1.JobTemplateSpec{
				Spec: batchv1.JobSpec{
					Template: corev1.PodTemplateSpec{
						Spec: corev1.PodSpec{RestartPolicy: corev1.RestartPolicyNever, Containers: []corev1.Container{{Name: "c", Image: "busybox"}}},
					},
				},
			},
		},
	}, metav1.CreateOptions{})
	require.NoError(t, err)

	// A Job owned by the CronJob, to exercise the owned-jobs drill-down.
	_, err = client.BatchV1().Jobs(ns).Create(ctx, &batchv1.Job{
		ObjectMeta: metav1.ObjectMeta{
			Name: "nightly-27700000", Namespace: ns,
			OwnerReferences: []metav1.OwnerReference{ownerRef("batch/v1", "CronJob", cronJob.Name, cronJob.UID)},
		},
		Spec: batchv1.JobSpec{
			Completions: i32(1),
			Template: corev1.PodTemplateSpec{
				Spec: corev1.PodSpec{RestartPolicy: corev1.RestartPolicyNever, Containers: []corev1.Container{{Name: "c", Image: "busybox"}}},
			},
		},
	}, metav1.CreateOptions{})
	require.NoError(t, err)

	// An event on a pod, to exercise involvedObject filtering.
	_, err = client.CoreV1().Events(ns).Create(ctx, &corev1.Event{
		ObjectMeta:     metav1.ObjectMeta{Name: "web-rs-a.evt", Namespace: ns},
		InvolvedObject: corev1.ObjectReference{Kind: "Pod", Name: "web-rs-a", Namespace: ns},
		Reason:         "Unhealthy", Message: "liveness probe failed", Type: "Warning",
		Count: 3, LastTimestamp: metav1.NewTime(time.Now()),
	}, metav1.CreateOptions{})
	require.NoError(t, err)

	handler := server.New(server.Options{
		Logger: slog.New(slog.NewTextHandler(io.Discard, nil)),
		Kube:   kube.NewManager([]string{writeKubeconfig(t, cfg)}),
		Dist:   os.DirFS(t.TempDir()),
	})
	do := func(path string) *httptest.ResponseRecorder {
		rec := httptest.NewRecorder()
		handler.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, path, nil))
		return rec
	}

	t.Run("typed pod summary lists pods with computed fields", func(t *testing.T) {
		rec := do("/api/v1/workloads/pods?namespace=default")
		require.Equal(t, http.StatusOK, rec.Code, "body: %s", rec.Body.String())
		var body struct {
			Items []resources.PodSummary `json:"items"`
		}
		require.NoError(t, json.Unmarshal(rec.Body.Bytes(), &body))
		names := map[string]resources.PodSummary{}
		for _, p := range body.Items {
			names[p.Name] = p
		}
		require.Contains(t, names, "web-rs-a")
		assert.Equal(t, "0/1", names["web-rs-a"].Ready, "one spec container, not yet ready in envtest")
	})

	t.Run("typed deployment summary computes rollout status", func(t *testing.T) {
		rec := do("/api/v1/workloads/deployments?namespace=default")
		require.Equal(t, http.StatusOK, rec.Code, "body: %s", rec.Body.String())
		var body struct {
			Items []resources.DeploymentSummary `json:"items"`
		}
		require.NoError(t, json.Unmarshal(rec.Body.Bytes(), &body))
		require.Len(t, body.Items, 1)
		assert.Equal(t, "web", body.Items[0].Name)
		assert.Equal(t, int32(2), body.Items[0].DesiredReplicas)
		assert.NotEmpty(t, body.Items[0].RolloutStatus)
	})

	t.Run("typed cronjob summary carries schedule", func(t *testing.T) {
		rec := do("/api/v1/workloads/cronjobs?namespace=default")
		require.Equal(t, http.StatusOK, rec.Code, "body: %s", rec.Body.String())
		var body struct {
			Items []resources.CronJobSummary `json:"items"`
		}
		require.NoError(t, json.Unmarshal(rec.Body.Bytes(), &body))
		require.Len(t, body.Items, 1)
		assert.Equal(t, "0 0 * * *", body.Items[0].Schedule)
	})

	t.Run("owned-pods resolves a deployment through its replicaset", func(t *testing.T) {
		rec := do("/api/v1/workloads/deployments/default/web/pods")
		require.Equal(t, http.StatusOK, rec.Code, "body: %s", rec.Body.String())
		var body struct {
			Items []resources.PodSummary `json:"items"`
		}
		require.NoError(t, json.Unmarshal(rec.Body.Bytes(), &body))
		got := map[string]bool{}
		for _, p := range body.Items {
			got[p.Name] = true
		}
		assert.True(t, got["web-rs-a"] && got["web-rs-b"], "both RS-owned pods returned")
		assert.False(t, got["standalone"], "unrelated pod excluded")
	})

	t.Run("owned-pods resolves a replicaset directly", func(t *testing.T) {
		rec := do("/api/v1/workloads/replicasets/default/web-rs/pods")
		require.Equal(t, http.StatusOK, rec.Code, "body: %s", rec.Body.String())
		var body struct {
			Items []resources.PodSummary `json:"items"`
		}
		require.NoError(t, json.Unmarshal(rec.Body.Bytes(), &body))
		assert.Len(t, body.Items, 2)
	})

	t.Run("owned-jobs resolves a cronjob's jobs by ownerReference", func(t *testing.T) {
		rec := do("/api/v1/workloads/cronjobs/default/nightly/jobs")
		require.Equal(t, http.StatusOK, rec.Code, "body: %s", rec.Body.String())
		var body struct {
			Items []resources.JobSummary `json:"items"`
		}
		require.NoError(t, json.Unmarshal(rec.Body.Bytes(), &body))
		require.Len(t, body.Items, 1, "only the cronjob-owned job, not the standalone backup job")
		assert.Equal(t, "nightly-27700000", body.Items[0].Name)
	})

	t.Run("owned-jobs is a 404 for a non-cronjob kind", func(t *testing.T) {
		rec := do("/api/v1/workloads/deployments/default/web/jobs")
		assert.Equal(t, http.StatusNotFound, rec.Code)
	})

	t.Run("events filtered by involvedObject", func(t *testing.T) {
		rec := do("/api/v1/events?namespace=default&kind=Pod&name=web-rs-a")
		require.Equal(t, http.StatusOK, rec.Code, "body: %s", rec.Body.String())
		var body struct {
			Items []resources.EventSummary `json:"items"`
		}
		require.NoError(t, json.Unmarshal(rec.Body.Bytes(), &body))
		require.Len(t, body.Items, 1)
		assert.Equal(t, "Unhealthy", body.Items[0].Reason)
		assert.Equal(t, "Warning", body.Items[0].Type)
		assert.Equal(t, int32(3), body.Items[0].Count)
	})

	t.Run("events for an object with none is a clean empty list", func(t *testing.T) {
		rec := do("/api/v1/events?namespace=default&kind=Pod&name=standalone")
		require.Equal(t, http.StatusOK, rec.Code)
		var body struct {
			Items []resources.EventSummary `json:"items"`
		}
		require.NoError(t, json.Unmarshal(rec.Body.Bytes(), &body))
		assert.Empty(t, body.Items)
	})

	t.Run("events requires kind and name", func(t *testing.T) {
		rec := do("/api/v1/events?namespace=default&kind=Pod")
		assert.Equal(t, http.StatusBadRequest, rec.Code)
	})

	t.Run("unknown workload kind is a 404", func(t *testing.T) {
		rec := do("/api/v1/workloads/widgets?namespace=default")
		assert.Equal(t, http.StatusNotFound, rec.Code)
	})
}

func ownerRef(apiVersion, kind, name string, uid types.UID) metav1.OwnerReference {
	controller := true
	return metav1.OwnerReference{APIVersion: apiVersion, Kind: kind, Name: name, UID: uid, Controller: &controller}
}

func mustCreatePod(t *testing.T, client kubernetes.Interface, ns, name string, labels map[string]string, owner metav1.OwnerReference) {
	t.Helper()
	pod := &corev1.Pod{
		ObjectMeta: metav1.ObjectMeta{Name: name, Namespace: ns, Labels: labels},
		Spec:       corev1.PodSpec{Containers: []corev1.Container{{Name: "c", Image: "busybox"}}},
	}
	if owner.Name != "" {
		pod.OwnerReferences = []metav1.OwnerReference{owner}
	}
	_, err := client.CoreV1().Pods(ns).Create(context.Background(), pod, metav1.CreateOptions{})
	require.NoError(t, err)
}
