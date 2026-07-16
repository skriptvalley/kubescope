package stream

// Envtest-backed integration test for the watch→SSE bridge: boots a real
// kube-apiserver and asserts add/update/delete watch events reach a subscriber
// through a shared informer, and that namespace filtering excludes other
// namespaces. Watch-error resync is covered by the unit tests (forcing an
// apiserver watch error deterministically is impractical here).
//
// Requires KUBEBUILDER_ASSETS (make test sets it); skipped otherwise.

import (
	"context"
	"io"
	"log/slog"
	"os"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime/schema"
	"k8s.io/client-go/dynamic"
	"k8s.io/client-go/kubernetes"
	"sigs.k8s.io/controller-runtime/pkg/envtest"

	"github.com/skriptvalley/kubescope/internal/kube"
)

var configMapsGVR = schema.GroupVersionResource{Group: "", Version: "v1", Resource: "configmaps"}

type envCluster struct{ dyn dynamic.Interface }

func (e *envCluster) ActiveContextName() (string, error)           { return "env", nil }
func (e *envCluster) DynamicFor(string) (dynamic.Interface, error) { return e.dyn, nil }
func (e *envCluster) ClassifyActiveError(err error) kube.Classification {
	return kube.ClassifyError(err, kube.ClassifyHints{})
}

// waitForNamed drains events until one of the wanted type+name arrives, failing
// on timeout. Unrelated events (other objects seeded by the apiserver) are
// skipped.
func waitForNamed(t *testing.T, sub *Subscription, want EventType, name string, timeout time.Duration) Event {
	t.Helper()
	deadline := time.After(timeout)
	for {
		select {
		case ev := <-sub.Events():
			if ev.Type != want {
				continue
			}
			got := ""
			if ev.Ref != nil {
				got = ev.Ref.Name
			} else if s, ok := ev.Row.(string); ok {
				got = s
			}
			if got == name {
				return ev
			}
		case <-deadline:
			t.Fatalf("timed out waiting for %s event for %q", want, name)
			return Event{}
		}
	}
}

func TestStreamWatchDeliveryAgainstEnvtest(t *testing.T) {
	if os.Getenv("KUBEBUILDER_ASSETS") == "" {
		t.Skip("KUBEBUILDER_ASSETS not set; run via `make test`")
	}

	env := &envtest.Environment{}
	cfg, err := env.Start()
	require.NoError(t, err)
	t.Cleanup(func() { require.NoError(t, env.Stop()) })

	dyn, err := dynamic.NewForConfig(cfg)
	require.NoError(t, err)
	cs, err := kubernetes.NewForConfig(cfg)
	require.NoError(t, err)
	ctx := context.Background()
	logger := slog.New(slog.NewTextHandler(io.Discard, nil))

	hub := NewHub(&envCluster{dyn: dyn}, nameShaper, logger, WithResyncPeriod(time.Hour))

	sub, err := hub.Subscribe(configMapsGVR, Filter{Namespace: "default"})
	require.NoError(t, err)
	defer sub.Close()

	cm := &corev1.ConfigMap{
		ObjectMeta: metav1.ObjectMeta{Name: "live", Namespace: "default"},
		Data:       map[string]string{"k": "v1"},
	}
	_, err = cs.CoreV1().ConfigMaps("default").Create(ctx, cm, metav1.CreateOptions{})
	require.NoError(t, err)
	waitForNamed(t, sub, EventAdd, "live", 5*time.Second)

	cm.Data["k"] = "v2"
	_, err = cs.CoreV1().ConfigMaps("default").Update(ctx, cm, metav1.UpdateOptions{})
	require.NoError(t, err)
	waitForNamed(t, sub, EventUpdate, "live", 5*time.Second)

	require.NoError(t, cs.CoreV1().ConfigMaps("default").Delete(ctx, "live", metav1.DeleteOptions{}))
	del := waitForNamed(t, sub, EventDelete, "live", 5*time.Second)
	require.NotNil(t, del.Ref)
	require.Equal(t, "default", del.Ref.Namespace)
}

func TestStreamNamespaceFilterAgainstEnvtest(t *testing.T) {
	if os.Getenv("KUBEBUILDER_ASSETS") == "" {
		t.Skip("KUBEBUILDER_ASSETS not set; run via `make test`")
	}

	env := &envtest.Environment{}
	cfg, err := env.Start()
	require.NoError(t, err)
	t.Cleanup(func() { require.NoError(t, env.Stop()) })

	dyn, err := dynamic.NewForConfig(cfg)
	require.NoError(t, err)
	cs, err := kubernetes.NewForConfig(cfg)
	require.NoError(t, err)
	ctx := context.Background()
	logger := slog.New(slog.NewTextHandler(io.Discard, nil))

	_, err = cs.CoreV1().Namespaces().Create(ctx, &corev1.Namespace{ObjectMeta: metav1.ObjectMeta{Name: "other"}}, metav1.CreateOptions{})
	require.NoError(t, err)

	hub := NewHub(&envCluster{dyn: dyn}, nameShaper, logger, WithResyncPeriod(time.Hour))
	sub, err := hub.Subscribe(configMapsGVR, Filter{Namespace: "default"})
	require.NoError(t, err)
	defer sub.Close()

	// A configmap in "other" must never reach a default-scoped subscriber, while
	// one in "default" does — proving the filter, not a race.
	_, err = cs.CoreV1().ConfigMaps("other").Create(ctx,
		&corev1.ConfigMap{ObjectMeta: metav1.ObjectMeta{Name: "elsewhere", Namespace: "other"}}, metav1.CreateOptions{})
	require.NoError(t, err)
	_, err = cs.CoreV1().ConfigMaps("default").Create(ctx,
		&corev1.ConfigMap{ObjectMeta: metav1.ObjectMeta{Name: "here", Namespace: "default"}}, metav1.CreateOptions{})
	require.NoError(t, err)

	// Once "here" (default) is observed, "elsewhere" (other) must not have been.
	seenElsewhere := false
	deadline := time.After(5 * time.Second)
	for {
		done := false
		select {
		case ev := <-sub.Events():
			name := ""
			if s, ok := ev.Row.(string); ok {
				name = s
			}
			if name == "elsewhere" {
				seenElsewhere = true
			}
			if name == "here" {
				done = true
			}
		case <-deadline:
			t.Fatal("timed out waiting for the default-namespace configmap")
		}
		if done {
			break
		}
	}
	require.False(t, seenElsewhere, "default-scoped subscriber must not see other-namespace events")
}
