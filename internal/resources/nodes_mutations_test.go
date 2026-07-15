package resources

import (
	"context"
	"errors"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	corev1 "k8s.io/api/core/v1"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/client-go/kubernetes/fake"
	ktesting "k8s.io/client-go/testing"
)

func TestCordonPatch(t *testing.T) {
	assert.JSONEq(t, `{"spec":{"unschedulable":true}}`, string(cordonPatch(true)))
	assert.JSONEq(t, `{"spec":{"unschedulable":false}}`, string(cordonPatch(false)))
}

func drainPod(name string, opts ...func(*corev1.Pod)) corev1.Pod {
	p := corev1.Pod{
		ObjectMeta: metav1.ObjectMeta{Name: name, Namespace: "default"},
		Status:     corev1.PodStatus{Phase: corev1.PodRunning},
	}
	for _, o := range opts {
		o(&p)
	}
	return p
}

func ownedBy(kind string) func(*corev1.Pod) {
	return func(p *corev1.Pod) {
		ctrl := true
		p.OwnerReferences = []metav1.OwnerReference{{Kind: kind, Name: "owner", Controller: &ctrl}}
	}
}

func TestClassifyDrainPods(t *testing.T) {
	pods := []corev1.Pod{
		drainPod("normal"),
		drainPod("ds-pod", ownedBy("DaemonSet")),
		drainPod("rs-pod", ownedBy("ReplicaSet")),
		drainPod("mirror", func(p *corev1.Pod) {
			p.Annotations = map[string]string{mirrorPodAnnotation: "abc"}
		}),
		drainPod("done", func(p *corev1.Pod) { p.Status.Phase = corev1.PodSucceeded }),
	}
	got := classifyDrainPods(pods)
	require.Len(t, got, 5)

	byName := map[string]drainCandidate{}
	for _, c := range got {
		byName[c.pod.Name] = c
	}
	assert.False(t, byName["normal"].skip, "a normal pod is a candidate")
	assert.False(t, byName["rs-pod"].skip, "a ReplicaSet pod is a candidate")
	assert.True(t, byName["ds-pod"].skip, "DaemonSet pods are skipped")
	assert.True(t, byName["mirror"].skip, "mirror pods are skipped")
	assert.True(t, byName["done"].skip, "terminal pods are skipped")
}

// evictionReactor prepends a reactor firing on pod eviction with the given
// outcome, so drainNode's evicted/blocked/error branches are deterministic
// regardless of the fake clientset's default eviction handling.
func evictionReactor(cs *fake.Clientset, err error) {
	cs.PrependReactor("create", "pods", func(action ktesting.Action) (bool, runtime.Object, error) {
		if action.GetSubresource() == "eviction" {
			return true, nil, err
		}
		return false, nil, nil
	})
}

func TestDrainNodeReportsPerPod(t *testing.T) {
	candidates := classifyDrainPods([]corev1.Pod{
		drainPod("app-1"),
		drainPod("ds-1", ownedBy("DaemonSet")),
	})

	t.Run("evicts candidates, skips DaemonSet pods", func(t *testing.T) {
		cs := fake.NewClientset()
		evictionReactor(cs, nil)
		res := drainNode(context.Background(), cs, "node-1", candidates)
		assert.Equal(t, 1, res.Evicted)
		assert.Equal(t, 1, res.Skipped)
		assert.Zero(t, res.Blocked+res.Failed)
		require.Len(t, res.Pods, 2)
		// Sorted by namespace/name: app-1 before ds-1.
		assert.Equal(t, drainEvicted, res.Pods[0].Result)
		assert.Equal(t, drainSkipped, res.Pods[1].Result)
		assert.NotEmpty(t, res.Pods[1].Reason)
	})

	t.Run("PDB-blocked eviction is reported, not swallowed", func(t *testing.T) {
		cs := fake.NewClientset()
		evictionReactor(cs, apierrors.NewTooManyRequests("blocked by PodDisruptionBudget", 0))
		res := drainNode(context.Background(), cs, "node-1", candidates)
		assert.Equal(t, 1, res.Blocked)
		assert.Equal(t, 0, res.Evicted)
		assert.Equal(t, drainBlocked, res.Pods[0].Result)
		assert.Contains(t, res.Pods[0].Reason, "PodDisruptionBudget")
	})

	t.Run("other eviction failure is reported as error", func(t *testing.T) {
		cs := fake.NewClientset()
		evictionReactor(cs, errors.New("boom"))
		res := drainNode(context.Background(), cs, "node-1", candidates)
		assert.Equal(t, 1, res.Failed)
		assert.Equal(t, drainError, res.Pods[0].Result)
		assert.Contains(t, res.Pods[0].Reason, "boom")
	})
}

func TestCordonHandlerTogglesSchedulability(t *testing.T) {
	cs := fake.NewClientset(&corev1.Node{ObjectMeta: metav1.ObjectMeta{Name: "node-1"}})
	cluster := &fakeCluster{clientset: cs}

	rec := httptest.NewRecorder()
	CordonHandler(cluster, discardLogger())(rec, chiRequest(http.MethodPost, "/", "", map[string]string{"name": "node-1"}))
	require.Equal(t, http.StatusOK, rec.Code, "body: %s", rec.Body.String())

	updated, err := cs.CoreV1().Nodes().Get(context.Background(), "node-1", metav1.GetOptions{})
	require.NoError(t, err)
	assert.True(t, updated.Spec.Unschedulable, "cordon sets spec.unschedulable")
}

func TestUncordonHandlerClearsSchedulability(t *testing.T) {
	cs := fake.NewClientset(&corev1.Node{
		ObjectMeta: metav1.ObjectMeta{Name: "node-1"},
		Spec:       corev1.NodeSpec{Unschedulable: true},
	})
	cluster := &fakeCluster{clientset: cs}

	rec := httptest.NewRecorder()
	UncordonHandler(cluster, discardLogger())(rec, chiRequest(http.MethodPost, "/", "", map[string]string{"name": "node-1"}))
	require.Equal(t, http.StatusOK, rec.Code, "body: %s", rec.Body.String())

	updated, err := cs.CoreV1().Nodes().Get(context.Background(), "node-1", metav1.GetOptions{})
	require.NoError(t, err)
	assert.False(t, updated.Spec.Unschedulable, "uncordon clears spec.unschedulable")
}
