package resources

import (
	"context"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	appsv1 "k8s.io/api/apps/v1"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/types"
	"k8s.io/client-go/kubernetes/fake"
)

func ctrlRef(kind, name string, uid types.UID) metav1.OwnerReference {
	return metav1.OwnerReference{Kind: kind, Name: name, UID: uid, Controller: boolPtrLocal(true)}
}

func labeledPod(name, ns string, labels map[string]string, owner metav1.OwnerReference) *corev1.Pod {
	return &corev1.Pod{
		ObjectMeta: metav1.ObjectMeta{
			Name: name, Namespace: ns, Labels: labels,
			OwnerReferences: []metav1.OwnerReference{owner},
		},
		Spec: corev1.PodSpec{Containers: []corev1.Container{{Name: "c"}}},
	}
}

func TestControllerOwner(t *testing.T) {
	// No refs → nil.
	assert.Nil(t, controllerOwner(nil))

	// Prefers the controller ref.
	owner := controllerOwner([]metav1.OwnerReference{
		{Kind: "Node", Name: "n", Controller: boolPtrLocal(false)},
		{Kind: "ReplicaSet", Name: "rs", Controller: boolPtrLocal(true)},
	})
	require.NotNil(t, owner)
	assert.Equal(t, "ReplicaSet", owner.Kind)

	// Falls back to the first ref when none is a controller.
	owner = controllerOwner([]metav1.OwnerReference{{Kind: "Foo", Name: "f"}})
	require.NotNil(t, owner)
	assert.Equal(t, "Foo", owner.Kind)
}

func TestOwnedPodsForReplicaSetFiltersBySelectorAndOwner(t *testing.T) {
	ns := "default"
	rsUID := types.UID("rs-uid")
	rs := &appsv1.ReplicaSet{
		ObjectMeta: metav1.ObjectMeta{Name: "web-rs", Namespace: ns, UID: rsUID},
		Spec:       appsv1.ReplicaSetSpec{Selector: &metav1.LabelSelector{MatchLabels: map[string]string{"app": "web"}}},
	}
	owned := labeledPod("web-1", ns, map[string]string{"app": "web"}, ctrlRef("ReplicaSet", "web-rs", rsUID))
	// Matches the labels but is owned by a different controller — must be excluded.
	foreign := labeledPod("web-2", ns, map[string]string{"app": "web"}, ctrlRef("ReplicaSet", "other-rs", "other-uid"))
	// Different labels — excluded by the selector.
	other := labeledPod("api-1", ns, map[string]string{"app": "api"}, ctrlRef("ReplicaSet", "web-rs", rsUID))

	client := fake.NewSimpleClientset(rs, owned, foreign, other)
	pods, err := ownedPods(context.Background(), client, resourceReplicaSets, ns, "web-rs")
	require.NoError(t, err)
	require.Len(t, pods, 1)
	assert.Equal(t, "web-1", pods[0].Name)
}

func TestOwnedPodsForDeploymentResolvesThroughReplicaSets(t *testing.T) {
	ns := "default"
	depUID := types.UID("dep-uid")
	rsUID := types.UID("rs-uid")
	dep := &appsv1.Deployment{
		ObjectMeta: metav1.ObjectMeta{Name: "web", Namespace: ns, UID: depUID},
		Spec:       appsv1.DeploymentSpec{Selector: &metav1.LabelSelector{MatchLabels: map[string]string{"app": "web"}}},
	}
	rs := &appsv1.ReplicaSet{
		ObjectMeta: metav1.ObjectMeta{
			Name: "web-rs", Namespace: ns, UID: rsUID,
			Labels:          map[string]string{"app": "web"},
			OwnerReferences: []metav1.OwnerReference{ctrlRef("Deployment", "web", depUID)},
		},
		Spec: appsv1.ReplicaSetSpec{Selector: &metav1.LabelSelector{MatchLabels: map[string]string{"app": "web"}}},
	}
	// A stray RS matching the labels but NOT owned by this deployment; its pod
	// must be excluded from the deployment's owned-pods.
	strayRS := &appsv1.ReplicaSet{
		ObjectMeta: metav1.ObjectMeta{Name: "stray-rs", Namespace: ns, UID: "stray-uid", Labels: map[string]string{"app": "web"}},
		Spec:       appsv1.ReplicaSetSpec{Selector: &metav1.LabelSelector{MatchLabels: map[string]string{"app": "web"}}},
	}
	owned := labeledPod("web-1", ns, map[string]string{"app": "web"}, ctrlRef("ReplicaSet", "web-rs", rsUID))
	strayPod := labeledPod("stray-1", ns, map[string]string{"app": "web"}, ctrlRef("ReplicaSet", "stray-rs", "stray-uid"))

	client := fake.NewSimpleClientset(dep, rs, strayRS, owned, strayPod)
	pods, err := ownedPods(context.Background(), client, resourceDeployments, ns, "web")
	require.NoError(t, err)
	require.Len(t, pods, 1)
	assert.Equal(t, "web-1", pods[0].Name)
}

func TestOwnedPodsUnknownKind(t *testing.T) {
	client := fake.NewSimpleClientset()
	_, err := ownedPods(context.Background(), client, resourceCronJobs, "default", "cj")
	var unknown *unknownWorkloadError
	assert.ErrorAs(t, err, &unknown)
}
