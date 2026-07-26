package resources_test

// Envtest-backed integration test for the resource relationship graph (FB-14,
// ADR-0011): boots a real kube-apiserver, seeds a namespace shaped like
// deploy/testenv's `web` and `batch` fixtures, then drives
// GET /api/v1/namespaces/{ns}/graph through the full chain kubeconfig →
// kube.Manager → router → discovery → dynamic client.
//
// envtest runs no controllers, so the ReplicaSet, pods, Endpoints and Jobs are
// materialized here exactly as their controllers would — which also means no
// EndpointSlices exist, so the Service⇄Pod index exercises its v1 Endpoints
// fallback for real.
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

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	appsv1 "k8s.io/api/apps/v1"
	batchv1 "k8s.io/api/batch/v1"
	corev1 "k8s.io/api/core/v1"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/types"
	"k8s.io/client-go/kubernetes"
	"sigs.k8s.io/controller-runtime/pkg/envtest"

	"github.com/skriptvalley/kubescope/internal/graph"
	"github.com/skriptvalley/kubescope/internal/kube"
	"github.com/skriptvalley/kubescope/internal/server"
)

func TestResourceGraphAgainstEnvtest(t *testing.T) {
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
	const ns = "default"

	seedWebFixtures(t, ctx, client, ns)
	seedBatchFixtures(t, ctx, client, ns)

	handler := server.New(server.Options{
		Logger: slog.New(slog.NewTextHandler(io.Discard, nil)),
		Kube:   kube.NewManager([]string{writeKubeconfig(t, cfg)}),
		Dist:   os.DirFS(t.TempDir()),
	})
	fetch := func(t *testing.T, query string) graph.Response {
		t.Helper()
		rec := httptest.NewRecorder()
		handler.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/api/v1/namespaces/"+ns+"/graph?"+query, nil))
		require.Equal(t, http.StatusOK, rec.Code, "body: %s", rec.Body.String())
		var body graph.Response
		require.NoError(t, json.Unmarshal(rec.Body.Bytes(), &body))
		return body
	}

	t.Run("a deployment focus reaches its pods, service and config", func(t *testing.T) {
		resp := fetch(t, "focus=Deployment/api")

		focus := findNode(resp, "Deployment", "api")
		require.NotNil(t, focus)
		assert.True(t, focus.Focus)
		assert.Equal(t, "1/2", focus.Status, "the deployment badge is its real readiness")

		rs := findNode(resp, "ReplicaSet", "api-7d9")
		podA := findNode(resp, "Pod", "api-7d9-aaa")
		podB := findNode(resp, "Pod", "api-7d9-bbb")
		service := findNode(resp, "Service", "api")
		configMap := findNode(resp, "ConfigMap", "frontend-config")
		secret := findNode(resp, "Secret", "api-credentials")
		account := findNode(resp, "ServiceAccount", "default")
		for _, n := range []*graph.Node{rs, podA, podB, service, configMap, secret, account} {
			require.NotNil(t, n)
		}
		assert.Equal(t, 1, rs.Depth)
		assert.Equal(t, 2, podA.Depth)
		assert.Equal(t, "Running", podA.Status)

		assert.Equal(t, graph.RelOwns, findEdge(t, resp, focus, rs).Relation)
		assert.Equal(t, graph.RelOwns, findEdge(t, resp, rs, podA).Relation)
		// The Service⇄Pod index came from the v1 Endpoints object.
		routed := findEdge(t, resp, service, podA)
		assert.Equal(t, graph.RelRoutes, routed.Relation)
		assert.Equal(t, "ready", routed.Label)
		assert.Equal(t, "not ready", findEdge(t, resp, service, podB).Label)
		// Consumed through a volume and envFrom: one edge naming both mechanisms.
		assert.Equal(t, "volume, envFrom", findEdge(t, resp, podA, configMap).Label)
		assert.Equal(t, "volume, envFrom", findEdge(t, resp, podA, secret).Label)
		assert.Equal(t, graph.RelServiceAccount, findEdge(t, resp, podA, account).Relation)

		// The workload's own parts are boxed together; shared config is not.
		require.Len(t, resp.Groups, 1)
		box := resp.Groups[0]
		assert.Equal(t, "api", box.Label)
		for _, n := range []*graph.Node{focus, rs, podA, podB, service} {
			assert.Equal(t, box.ID, n.Parent, "%s %s belongs in the box", n.Kind, n.Name)
		}
		assert.Empty(t, configMap.Parent)
		assert.Empty(t, secret.Parent)
	})

	t.Run("a job focus reaches the secret its pod template consumes", func(t *testing.T) {
		resp := fetch(t, "focus=Job/config-sync&depth=1")
		edge := findEdge(t, resp, findNode(resp, "Job", "config-sync"), findNode(resp, "Secret", "api-credentials"))
		assert.Equal(t, graph.RelEnv, edge.Relation)
		assert.Equal(t, "envFrom", edge.Label)
	})

	t.Run("a cronjob's runs club into one counted node", func(t *testing.T) {
		resp := fetch(t, "focus=CronJob/hourly-report")

		for _, name := range []string{"hourly-report-1", "hourly-report-2", "hourly-report-3"} {
			assert.Nil(t, findNode(resp, "Job", name), "%s should be clubbed, not drawn", name)
		}
		var aggregate *graph.Node
		for i := range resp.Nodes {
			if resp.Nodes[i].Aggregate {
				aggregate = &resp.Nodes[i]
			}
		}
		require.NotNil(t, aggregate, "the run series collapses into one aggregated node")
		assert.Equal(t, "Job", aggregate.Kind)
		assert.Equal(t, 3, aggregate.Count)
		assert.NotEmpty(t, aggregate.Status, "the aggregate carries the run outcomes")
	})

	t.Run("depth bounds the walk", func(t *testing.T) {
		resp := fetch(t, "focus=Deployment/api&depth=1")
		assert.NotNil(t, findNode(resp, "ReplicaSet", "api-7d9"))
		assert.Nil(t, findNode(resp, "Pod", "api-7d9-aaa"), "pods are two hops out")
	})

	t.Run("failures are classified, never a bare 500", func(t *testing.T) {
		tests := []struct {
			name   string
			path   string
			status int
			code   string
		}{
			{"missing focus", "/api/v1/namespaces/default/graph", http.StatusBadRequest, "invalid_focus"},
			{"unknown kind", "/api/v1/namespaces/default/graph?focus=Widget/x", http.StatusNotFound, "unknown_resource"},
			{"missing object", "/api/v1/namespaces/default/graph?focus=Deployment/ghost", http.StatusNotFound, "not_found"},
			{"empty namespace", "/api/v1/namespaces/nope/graph?focus=Deployment/api", http.StatusNotFound, "not_found"},
			{"cluster-scoped focus", "/api/v1/namespaces/default/graph?focus=Node/n1", http.StatusBadRequest, "invalid_scope"},
			{"bad depth", "/api/v1/namespaces/default/graph?focus=Deployment/api&depth=x", http.StatusBadRequest, "invalid_depth"},
		}
		for _, tt := range tests {
			t.Run(tt.name, func(t *testing.T) {
				rec := httptest.NewRecorder()
				handler.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, tt.path, nil))
				require.Equal(t, tt.status, rec.Code, "body: %s", rec.Body.String())
				var envelope struct {
					Error struct{ Code string } `json:"error"`
				}
				require.NoError(t, json.Unmarshal(rec.Body.Bytes(), &envelope))
				assert.Equal(t, tt.code, envelope.Error.Code)
			})
		}
	})
}

// seedWebFixtures materializes deploy/testenv's `web` shape: an api Deployment
// with one ReplicaSet and two pods consuming a ConfigMap and Secret through both
// a volume and envFrom, a Service with one ready and one not-ready endpoint, and
// the config-sync Job that reads the Secret through envFrom.
func seedWebFixtures(t *testing.T, ctx context.Context, client kubernetes.Interface, ns string) {
	t.Helper()
	create := func(err error) { t.Helper(); require.NoError(t, err) }

	_, err := client.CoreV1().ConfigMaps(ns).Create(ctx, &corev1.ConfigMap{
		ObjectMeta: metav1.ObjectMeta{Name: "frontend-config", Namespace: ns},
		Data:       map[string]string{"APP_ENV": "development"},
	}, metav1.CreateOptions{})
	create(err)
	_, err = client.CoreV1().Secrets(ns).Create(ctx, &corev1.Secret{
		ObjectMeta: metav1.ObjectMeta{Name: "api-credentials", Namespace: ns},
		Data:       map[string][]byte{"API_TOKEN": []byte("tok")},
	}, metav1.CreateOptions{})
	create(err)
	// envtest runs no controllers, so the namespace's default ServiceAccount has
	// to exist before the ServiceAccount admission plugin will admit a pod.
	_, err = client.CoreV1().ServiceAccounts(ns).Create(ctx, &corev1.ServiceAccount{
		ObjectMeta: metav1.ObjectMeta{Name: "default", Namespace: ns},
	}, metav1.CreateOptions{})
	if err != nil {
		require.True(t, apierrors.IsAlreadyExists(err), "creating the default service account: %v", err)
	}

	deployment, err := client.AppsV1().Deployments(ns).Create(ctx, &appsv1.Deployment{
		ObjectMeta: metav1.ObjectMeta{Name: "api", Namespace: ns},
		Spec: appsv1.DeploymentSpec{
			Replicas: int32Ptr(2),
			Selector: &metav1.LabelSelector{MatchLabels: map[string]string{"app": "api"}},
			Template: corev1.PodTemplateSpec{
				ObjectMeta: metav1.ObjectMeta{Labels: map[string]string{"app": "api"}},
				Spec:       apiPodSpec(),
			},
		},
	}, metav1.CreateOptions{})
	create(err)
	// One of two replicas ready, so the node badge shows a real "1/2".
	deployment.Status = appsv1.DeploymentStatus{Replicas: 2, ReadyReplicas: 1}
	_, err = client.AppsV1().Deployments(ns).UpdateStatus(ctx, deployment, metav1.UpdateOptions{})
	create(err)

	replicaSet, err := client.AppsV1().ReplicaSets(ns).Create(ctx, &appsv1.ReplicaSet{
		ObjectMeta: metav1.ObjectMeta{
			Name: "api-7d9", Namespace: ns,
			OwnerReferences: []metav1.OwnerReference{controllerRef("apps/v1", "Deployment", deployment.Name, deployment.UID)},
		},
		Spec: appsv1.ReplicaSetSpec{
			Replicas: int32Ptr(2),
			Selector: &metav1.LabelSelector{MatchLabels: map[string]string{"app": "api"}},
			Template: corev1.PodTemplateSpec{
				ObjectMeta: metav1.ObjectMeta{Labels: map[string]string{"app": "api"}},
				Spec:       apiPodSpec(),
			},
		},
	}, metav1.CreateOptions{})
	create(err)

	for _, name := range []string{"api-7d9-aaa", "api-7d9-bbb"} {
		pod, err := client.CoreV1().Pods(ns).Create(ctx, &corev1.Pod{
			ObjectMeta: metav1.ObjectMeta{
				Name: name, Namespace: ns, Labels: map[string]string{"app": "api"},
				OwnerReferences: []metav1.OwnerReference{controllerRef("apps/v1", "ReplicaSet", replicaSet.Name, replicaSet.UID)},
			},
			Spec: apiPodSpec(),
		}, metav1.CreateOptions{})
		create(err)
		pod.Status.Phase = corev1.PodRunning
		_, err = client.CoreV1().Pods(ns).UpdateStatus(ctx, pod, metav1.UpdateOptions{})
		create(err)
	}

	_, err = client.CoreV1().Services(ns).Create(ctx, &corev1.Service{
		ObjectMeta: metav1.ObjectMeta{Name: "api", Namespace: ns},
		Spec: corev1.ServiceSpec{
			Selector: map[string]string{"app": "api"},
			Ports:    []corev1.ServicePort{{Name: "http", Port: 80, Protocol: corev1.ProtocolTCP}},
		},
	}, metav1.CreateOptions{})
	create(err)
	_, err = client.CoreV1().Endpoints(ns).Create(ctx, &corev1.Endpoints{
		ObjectMeta: metav1.ObjectMeta{Name: "api", Namespace: ns},
		Subsets: []corev1.EndpointSubset{{
			Ports:     []corev1.EndpointPort{{Name: "http", Port: 8080, Protocol: corev1.ProtocolTCP}},
			Addresses: []corev1.EndpointAddress{{IP: "10.1.0.1", TargetRef: podRef(ns, "api-7d9-aaa")}},
			NotReadyAddresses: []corev1.EndpointAddress{
				{IP: "10.1.0.2", TargetRef: podRef(ns, "api-7d9-bbb")},
			},
		}},
	}, metav1.CreateOptions{})
	create(err)

	_, err = client.BatchV1().Jobs(ns).Create(ctx, &batchv1.Job{
		ObjectMeta: metav1.ObjectMeta{Name: "config-sync", Namespace: ns},
		Spec: batchv1.JobSpec{Template: corev1.PodTemplateSpec{Spec: corev1.PodSpec{
			RestartPolicy: corev1.RestartPolicyNever,
			Containers: []corev1.Container{{
				Name: "sync", Image: "busybox:1.36",
				EnvFrom: []corev1.EnvFromSource{{SecretRef: &corev1.SecretEnvSource{
					LocalObjectReference: corev1.LocalObjectReference{Name: "api-credentials"},
				}}},
			}},
		}}},
	}, metav1.CreateOptions{})
	create(err)
}

// seedBatchFixtures materializes the run series: a CronJob and the three Jobs
// its history keeps.
func seedBatchFixtures(t *testing.T, ctx context.Context, client kubernetes.Interface, ns string) {
	t.Helper()
	cronJob, err := client.BatchV1().CronJobs(ns).Create(ctx, &batchv1.CronJob{
		ObjectMeta: metav1.ObjectMeta{Name: "hourly-report", Namespace: ns},
		Spec: batchv1.CronJobSpec{
			Schedule: "*/1 * * * *",
			JobTemplate: batchv1.JobTemplateSpec{Spec: batchv1.JobSpec{
				Template: corev1.PodTemplateSpec{Spec: corev1.PodSpec{
					RestartPolicy: corev1.RestartPolicyNever,
					Containers:    []corev1.Container{{Name: "report", Image: "busybox:1.36"}},
				}},
			}},
		},
	}, metav1.CreateOptions{})
	require.NoError(t, err)

	for _, name := range []string{"hourly-report-1", "hourly-report-2", "hourly-report-3"} {
		_, err := client.BatchV1().Jobs(ns).Create(ctx, &batchv1.Job{
			ObjectMeta: metav1.ObjectMeta{
				Name: name, Namespace: ns,
				OwnerReferences: []metav1.OwnerReference{controllerRef("batch/v1", "CronJob", cronJob.Name, cronJob.UID)},
			},
			Spec: batchv1.JobSpec{Template: corev1.PodTemplateSpec{Spec: corev1.PodSpec{
				RestartPolicy: corev1.RestartPolicyNever,
				Containers:    []corev1.Container{{Name: "report", Image: "busybox:1.36"}},
			}}},
		}, metav1.CreateOptions{})
		require.NoError(t, err)
	}
}

// apiPodSpec consumes the ConfigMap and Secret through both a volume and
// envFrom — the shape deploy/testenv's web/api workload has after FB-12.
func apiPodSpec() corev1.PodSpec {
	return corev1.PodSpec{
		ServiceAccountName: "default",
		Containers: []corev1.Container{{
			Name: "api", Image: "busybox:1.36",
			EnvFrom: []corev1.EnvFromSource{
				{ConfigMapRef: &corev1.ConfigMapEnvSource{
					LocalObjectReference: corev1.LocalObjectReference{Name: "frontend-config"}}},
				{SecretRef: &corev1.SecretEnvSource{
					LocalObjectReference: corev1.LocalObjectReference{Name: "api-credentials"}}},
			},
			VolumeMounts: []corev1.VolumeMount{
				{Name: "config", MountPath: "/etc/frontend-config"},
				{Name: "credentials", MountPath: "/etc/api-credentials"},
			},
		}},
		Volumes: []corev1.Volume{
			{Name: "config", VolumeSource: corev1.VolumeSource{ConfigMap: &corev1.ConfigMapVolumeSource{
				LocalObjectReference: corev1.LocalObjectReference{Name: "frontend-config"}}}},
			{Name: "credentials", VolumeSource: corev1.VolumeSource{Secret: &corev1.SecretVolumeSource{
				SecretName: "api-credentials"}}},
		},
	}
}

func controllerRef(apiVersion, kind, name string, uid types.UID) metav1.OwnerReference {
	controller := true
	return metav1.OwnerReference{
		APIVersion: apiVersion, Kind: kind, Name: name, UID: uid, Controller: &controller,
	}
}

func podRef(namespace, name string) *corev1.ObjectReference {
	return &corev1.ObjectReference{Kind: "Pod", Namespace: namespace, Name: name}
}

func int32Ptr(i int32) *int32 { return &i }

func findNode(resp graph.Response, kind, name string) *graph.Node {
	for i := range resp.Nodes {
		if !resp.Nodes[i].Aggregate && resp.Nodes[i].Kind == kind && resp.Nodes[i].Name == name {
			return &resp.Nodes[i]
		}
	}
	return nil
}

func findEdge(t *testing.T, resp graph.Response, source, target *graph.Node) *graph.Edge {
	t.Helper()
	require.NotNil(t, source)
	require.NotNil(t, target)
	for i := range resp.Edges {
		if resp.Edges[i].Source == source.ID && resp.Edges[i].Target == target.ID {
			return &resp.Edges[i]
		}
	}
	t.Fatalf("no edge from %s to %s", source.ID, target.ID)
	return nil
}
