package stream

import (
	"context"
	"errors"
	"io"
	"log/slog"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/runtime/schema"
	"k8s.io/apimachinery/pkg/watch"
	"k8s.io/client-go/dynamic"
	dynamicfake "k8s.io/client-go/dynamic/fake"
	clienttesting "k8s.io/client-go/testing"
)

var podsGVR = schema.GroupVersionResource{Group: "", Version: "v1", Resource: "pods"}

// fakeCluster serves one fake dynamic client for a fixed active context.
type fakeCluster struct {
	dyn     dynamic.Interface
	context string
}

func (f *fakeCluster) ActiveContextName() (string, error)           { return f.context, nil }
func (f *fakeCluster) DynamicFor(string) (dynamic.Interface, error) { return f.dyn, nil }

// nameShaper is a trivial shaper: the row is the object's name, so tests can
// assert delivery/fan-out without depending on internal/resources.
func nameShaper(_ schema.GroupVersionResource, u *unstructured.Unstructured) any { return u.GetName() }

func newFakeClient(t *testing.T) *dynamicfake.FakeDynamicClient {
	t.Helper()
	return dynamicfake.NewSimpleDynamicClientWithCustomListKinds(
		runtime.NewScheme(),
		map[schema.GroupVersionResource]string{podsGVR: "PodList"},
	)
}

func pod(namespace, name string) *unstructured.Unstructured {
	return &unstructured.Unstructured{Object: map[string]any{
		"apiVersion": "v1",
		"kind":       "Pod",
		"metadata":   map[string]any{"name": name, "namespace": namespace, "uid": name + "-uid"},
	}}
}

func createPod(t *testing.T, client dynamic.Interface, namespace, name string) {
	t.Helper()
	_, err := client.Resource(podsGVR).Namespace(namespace).Create(context.Background(), pod(namespace, name), metav1.CreateOptions{})
	require.NoError(t, err)
}

func deletePod(t *testing.T, client dynamic.Interface, namespace, name string) {
	t.Helper()
	require.NoError(t, client.Resource(podsGVR).Namespace(namespace).Delete(context.Background(), name, metav1.DeleteOptions{}))
}

func testHub(t *testing.T, client dynamic.Interface, opts ...HubOption) *Hub {
	t.Helper()
	logger := slog.New(slog.NewTextHandler(io.Discard, nil))
	return NewHub(&fakeCluster{dyn: client, context: "ctx-a"}, nameShaper, logger, opts...)
}

// recv reads one event or fails after the deadline.
func recv(t *testing.T, sub *Subscription, timeout time.Duration) Event {
	t.Helper()
	select {
	case ev := <-sub.Events():
		return ev
	case <-time.After(timeout):
		t.Fatal("timed out waiting for event")
		return Event{}
	}
}

// collectRows reads add events until it has seen every wanted row name or times
// out, asserting exactly that set arrived.
func collectAddRows(t *testing.T, sub *Subscription, want []string, timeout time.Duration) {
	t.Helper()
	remaining := map[string]bool{}
	for _, n := range want {
		remaining[n] = true
	}
	deadline := time.After(timeout)
	for len(remaining) > 0 {
		select {
		case ev := <-sub.Events():
			if ev.Type == EventAdd {
				name, _ := ev.Row.(string)
				delete(remaining, name)
			}
		case <-deadline:
			t.Fatalf("timed out; still waiting for adds: %v", remaining)
		}
	}
}

// expectSilence asserts no event arrives within the window (filter correctness).
func expectSilence(t *testing.T, sub *Subscription, window time.Duration) {
	t.Helper()
	select {
	case ev := <-sub.Events():
		t.Fatalf("expected no event, got %+v", ev)
	case <-time.After(window):
	}
}

func TestHubFanOutAndNamespaceFilter(t *testing.T) {
	client := newFakeClient(t)
	createPod(t, client, "default", "a")
	createPod(t, client, "other", "b")

	hub := testHub(t, client)

	// A default-namespace subscriber sees "a" (initial snapshot) but never "b".
	sub1, err := hub.Subscribe(podsGVR, Filter{Namespace: "default"})
	require.NoError(t, err)
	defer sub1.Close()
	collectAddRows(t, sub1, []string{"a"}, 3*time.Second)

	// A new default-namespace pod reaches sub1; an other-namespace one does not.
	createPod(t, client, "default", "c")
	collectAddRows(t, sub1, []string{"c"}, 3*time.Second)
	createPod(t, client, "other", "d")
	expectSilence(t, sub1, 300*time.Millisecond)

	// A second, unfiltered subscriber shares the SAME informer and gets the full
	// snapshot (all namespaces).
	sub2, err := hub.Subscribe(podsGVR, Filter{})
	require.NoError(t, err)
	defer sub2.Close()
	collectAddRows(t, sub2, []string{"a", "b", "c", "d"}, 3*time.Second)
	assert.Equal(t, 1, hub.activeInformers(), "both subscribers must share one informer")

	// Delete reaches matching subscribers with the object identity.
	deletePod(t, client, "default", "a")
	del := recv(t, sub2, 3*time.Second)
	assert.Equal(t, EventDelete, del.Type)
	require.NotNil(t, del.Ref)
	assert.Equal(t, "a", del.Ref.Name)
	assert.Equal(t, "default", del.Ref.Namespace)
}

func TestHubRefCountingTearsDownInformer(t *testing.T) {
	client := newFakeClient(t)
	hub := testHub(t, client)

	sub1, err := hub.Subscribe(podsGVR, Filter{})
	require.NoError(t, err)
	sub2, err := hub.Subscribe(podsGVR, Filter{})
	require.NoError(t, err)
	assert.Equal(t, 1, hub.activeInformers())

	// First close leaves the shared informer running for the survivor.
	sub1.Close()
	assert.Equal(t, 1, hub.activeInformers())

	// Last close tears it down.
	sub2.Close()
	assert.Equal(t, 0, hub.activeInformers())

	// Close is idempotent.
	sub2.Close()
	assert.Equal(t, 0, hub.activeInformers())

	// A fresh subscription rebuilds the informer from scratch.
	sub3, err := hub.Subscribe(podsGVR, Filter{})
	require.NoError(t, err)
	defer sub3.Close()
	assert.Equal(t, 1, hub.activeInformers())
}

func TestHubIncludeObjectForDetailSubscribers(t *testing.T) {
	client := newFakeClient(t)
	createPod(t, client, "default", "web-1")
	hub := testHub(t, client)

	// A list subscriber gets rows only; a detail subscriber gets the full object.
	list, err := hub.Subscribe(podsGVR, Filter{Namespace: "default"})
	require.NoError(t, err)
	defer list.Close()
	detail, err := hub.Subscribe(podsGVR, Filter{Namespace: "default", Name: "web-1", IncludeObject: true})
	require.NoError(t, err)
	defer detail.Close()

	listEv := recv(t, list, 3*time.Second)
	assert.Nil(t, listEv.Object, "list subscriber must not carry the full object")

	detailEv := recv(t, detail, 3*time.Second)
	require.NotNil(t, detailEv.Object, "detail subscriber must carry the full object")
	assert.Equal(t, "web-1", detailEv.Object["metadata"].(map[string]any)["name"])
}

// TestHubSanitizesDetailObject verifies the ADR-0005 fix: a full object sent to
// a detail subscriber is passed through the sanitizer first, so sensitive fields
// (Secret data) are masked server-side before they ever reach a client. Plumbing
// only — the masking logic itself is tested in internal/resources.
func TestHubSanitizesDetailObject(t *testing.T) {
	secretsGVR := schema.GroupVersionResource{Group: "", Version: "v1", Resource: "secrets"}
	client := dynamicfake.NewSimpleDynamicClientWithCustomListKinds(
		runtime.NewScheme(),
		map[schema.GroupVersionResource]string{secretsGVR: "SecretList"},
	)
	secret := &unstructured.Unstructured{Object: map[string]any{
		"apiVersion": "v1",
		"kind":       "Secret",
		"metadata":   map[string]any{"name": "db", "namespace": "default", "uid": "db-uid"},
		"data":       map[string]any{"password": "aHVudGVyMg=="},
	}}
	_, err := client.Resource(secretsGVR).Namespace("default").Create(context.Background(), secret, metav1.CreateOptions{})
	require.NoError(t, err)

	// A sanitizer that redacts secret data in a deep copy (never mutating input).
	sanitizer := func(gvr schema.GroupVersionResource, u *unstructured.Unstructured) *unstructured.Unstructured {
		if gvr != secretsGVR {
			return u
		}
		cp := u.DeepCopy()
		if d, ok := cp.Object["data"].(map[string]any); ok {
			for k := range d {
				d[k] = "REDACTED"
			}
		}
		return cp
	}

	logger := slog.New(slog.NewTextHandler(io.Discard, nil))
	hub := NewHub(&fakeCluster{dyn: client, context: "ctx-a"}, nameShaper, logger, WithObjectSanitizer(sanitizer))

	detail, err := hub.Subscribe(secretsGVR, Filter{Namespace: "default", Name: "db", IncludeObject: true})
	require.NoError(t, err)
	defer detail.Close()

	ev := recv(t, detail, 3*time.Second)
	require.NotNil(t, ev.Object, "detail subscriber receives the full object")
	data := ev.Object["data"].(map[string]any)
	assert.Equal(t, "REDACTED", data["password"], "the streamed Secret data must be masked by the sanitizer")
}

func TestHubResyncBroadcastAndOverflow(t *testing.T) {
	t.Run("watch-error broadcast flags every subscriber", func(t *testing.T) {
		client := newFakeClient(t)
		hub := testHub(t, client)
		sub, err := hub.Subscribe(podsGVR, Filter{})
		require.NoError(t, err)
		defer sub.Close()

		assert.False(t, sub.TakeResync())
		sub.si.broadcastResync()
		assert.True(t, sub.TakeResync(), "resync must be pending after a broadcast")
		assert.False(t, sub.TakeResync(), "TakeResync must clear the flag")
	})

	t.Run("apiserver watch error triggers a resync (SetWatchErrorHandler wiring)", func(t *testing.T) {
		client := newFakeClient(t)
		// The informer lists successfully (empty) but every watch fails; the
		// reflector invokes the watch-error handler, which must broadcast resync.
		client.PrependWatchReactor("pods", func(clienttesting.Action) (bool, watch.Interface, error) {
			return true, nil, errors.New("simulated apiserver watch failure")
		})
		hub := testHub(t, client)
		sub, err := hub.Subscribe(podsGVR, Filter{})
		require.NoError(t, err)
		defer sub.Close()

		require.Eventually(t, sub.TakeResync, 5*time.Second, 20*time.Millisecond,
			"a watch error must raise a resync for the subscriber")
	})

	t.Run("buffer overflow flags a resync instead of blocking", func(t *testing.T) {
		client := newFakeClient(t)
		hub := testHub(t, client, WithEventBuffer(1))
		sub, err := hub.Subscribe(podsGVR, Filter{})
		require.NoError(t, err)
		defer sub.Close()

		// Never drain the channel; a burst overflows the size-1 buffer.
		for i := 0; i < 10; i++ {
			createPod(t, client, "default", string(rune('a'+i)))
		}
		require.Eventually(t, sub.TakeResync, 3*time.Second, 20*time.Millisecond,
			"overflow must raise a resync")
	})
}
