package stream

import (
	"errors"
	"io"
	"log/slog"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/watch"
	"k8s.io/client-go/dynamic"
	clienttesting "k8s.io/client-go/testing"

	"github.com/skriptvalley/kubescope/internal/kube"
)

// errRefused is a representative connection-refused error the classifier sorts
// into FailConnectionRefused.
var errRefused = errors.New("dial tcp 127.0.0.1:6443: connect: connection refused")

// failListReactor / failWatchReactor make the fake dynamic client's list/watch
// fail while `down` is set, and fall through to the tracker otherwise — letting
// a test flip the cluster between unreachable and reachable.
func failListReactor(down *atomic.Bool) clienttesting.ReactionFunc {
	return func(clienttesting.Action) (bool, runtime.Object, error) {
		if down.Load() {
			return true, nil, errRefused
		}
		return false, nil, nil
	}
}

func failWatchReactor(down *atomic.Bool) clienttesting.WatchReactionFunc {
	return func(clienttesting.Action) (bool, watch.Interface, error) {
		if down.Load() {
			return true, nil, errRefused
		}
		return false, nil, nil
	}
}

// TestHubWatchErrorTransitionDampening asserts a run of watch errors produces a
// single unreachable status broadcast — the transition is reported once, repeats
// are dampened (no status/resync storm across clients).
func TestHubWatchErrorTransitionDampening(t *testing.T) {
	client := newFakeClient(t)
	// A large prober backoff keeps the prober from probing/recovering during the
	// test, so the outage stays steady while we assert exactly one broadcast.
	hub := testHub(t, client, WithProberBackoff(time.Hour, time.Hour))
	sub, err := hub.Subscribe(podsGVR, Filter{})
	require.NoError(t, err)
	defer sub.Close()

	sub.si.handleWatchError(errRefused)
	sub.si.handleWatchError(errRefused)
	sub.si.handleWatchError(errRefused)

	st := sub.TakeStatus()
	require.NotNil(t, st, "the reachable→unreachable transition must broadcast one status")
	assert.Equal(t, "unreachable", st.State)
	assert.Equal(t, string(kube.FailConnectionRefused), st.Reason)
	assert.NotEmpty(t, st.Guidance, "an unreachable status carries actionable remediation")
	assert.Nil(t, sub.TakeStatus(), "repeated errors are dampened — no second broadcast")
	assert.True(t, sub.si.isUnreachable())
}

// TestHubRecoveryConnectedAndResync drives the informer unreachable, then brings
// the cluster back and asserts the recovery prober emits a connected status and
// a resync so every client refetches a clean baseline.
func TestHubRecoveryConnectedAndResync(t *testing.T) {
	client := newFakeClient(t)
	var down atomic.Bool
	down.Store(true)
	client.PrependReactor("list", "pods", failListReactor(&down))
	client.PrependWatchReactor("pods", failWatchReactor(&down))

	hub := testHub(t, client, WithProberBackoff(20*time.Millisecond, 80*time.Millisecond))
	sub, err := hub.Subscribe(podsGVR, Filter{})
	require.NoError(t, err)
	defer sub.Close()

	// The failing list/watch drives the informer unreachable and broadcasts it.
	require.Eventually(t, sub.si.isUnreachable, 3*time.Second, 20*time.Millisecond)
	require.Eventually(t, func() bool {
		st := sub.TakeStatus()
		return st != nil && st.State == "unreachable"
	}, 3*time.Second, 20*time.Millisecond, "the outage must surface an unreachable status")

	// Cluster comes back: the prober's cheap LIST succeeds → connected + resync.
	down.Store(false)
	var connected bool
	require.Eventually(t, func() bool {
		if st := sub.TakeStatus(); st != nil && st.State == "connected" {
			connected = true
		}
		return connected
	}, 3*time.Second, 20*time.Millisecond, "recovery must broadcast a connected status")
	require.Eventually(t, sub.TakeResync, 3*time.Second, 20*time.Millisecond,
		"recovery must resync clients to a clean baseline")
	assert.False(t, sub.si.isUnreachable())
}

// TestHubRecoveryProberBackoffBounded asserts the recovery prober retries on a
// bounded exponential backoff: over a fixed window it makes several attempts but
// nowhere near the tens of thousands a busy loop would — the cap holds.
func TestHubRecoveryProberBackoffBounded(t *testing.T) {
	client := newFakeClient(t)
	// List always fails so the prober never recovers and keeps backing off.
	client.PrependReactor("list", "pods", func(clienttesting.Action) (bool, runtime.Object, error) {
		return true, nil, errRefused
	})
	client.PrependWatchReactor("pods", func(clienttesting.Action) (bool, watch.Interface, error) {
		return true, nil, errRefused
	})

	hub := testHub(t, client, WithProberBackoff(2*time.Millisecond, 10*time.Millisecond))
	sub, err := hub.Subscribe(podsGVR, Filter{})
	require.NoError(t, err)
	defer sub.Close()

	require.Eventually(t, sub.si.isUnreachable, 3*time.Second, 10*time.Millisecond)

	start := sub.si.proberAttempts.Load()
	time.Sleep(300 * time.Millisecond)
	got := sub.si.proberAttempts.Load() - start
	assert.Greater(t, got, int64(2), "the prober must keep retrying during the outage")
	assert.Less(t, got, int64(100), "bounded backoff must cap the retry rate (no busy loop)")
}

// TestHubSubscriberAttachDuringOutage asserts a subscriber that attaches to a
// shared informer already in an outage learns it immediately, without waiting
// for another (dampened) transition.
func TestHubSubscriberAttachDuringOutage(t *testing.T) {
	client := newFakeClient(t)
	// Watch fails (drives the outage); a large prober backoff keeps it steady.
	client.PrependWatchReactor("pods", func(clienttesting.Action) (bool, watch.Interface, error) {
		return true, nil, errRefused
	})
	hub := testHub(t, client, WithProberBackoff(time.Hour, time.Hour))

	first, err := hub.Subscribe(podsGVR, Filter{})
	require.NoError(t, err)
	defer first.Close()
	require.Eventually(t, first.si.isUnreachable, 3*time.Second, 20*time.Millisecond)

	late, err := hub.Subscribe(podsGVR, Filter{})
	require.NoError(t, err)
	defer late.Close()
	require.Equal(t, first.si, late.si, "both subscribers share one informer")

	st := late.TakeStatus()
	require.NotNil(t, st, "a mid-outage subscriber gets the current status at attach")
	assert.Equal(t, "unreachable", st.State)
	assert.Equal(t, string(kube.FailConnectionRefused), st.Reason)
}

// TestHubProberStopsOnClose asserts the recovery prober exits when the informer
// is torn down (last subscriber closes), leaking no goroutine.
func TestHubProberStopsOnClose(t *testing.T) {
	client := newFakeClient(t)
	// Both list and watch fail so the prober keeps running (never recovers).
	client.PrependReactor("list", "pods", func(clienttesting.Action) (bool, runtime.Object, error) {
		return true, nil, errRefused
	})
	client.PrependWatchReactor("pods", func(clienttesting.Action) (bool, watch.Interface, error) {
		return true, nil, errRefused
	})

	hub := testHub(t, client, WithProberBackoff(5*time.Millisecond, 20*time.Millisecond))
	sub, err := hub.Subscribe(podsGVR, Filter{})
	require.NoError(t, err)

	si := sub.si
	require.Eventually(t, si.isUnreachable, 3*time.Second, 10*time.Millisecond)
	require.Eventually(t, si.proberActive, 3*time.Second, 10*time.Millisecond,
		"a recovery prober must be running during the outage")

	sub.Close()
	assert.Equal(t, 0, hub.activeInformers())
	require.Eventually(t, func() bool { return !si.proberActive() }, 3*time.Second, 10*time.Millisecond,
		"the prober must exit when the informer is stopped (no goroutine leak)")
}

// swapCluster is a Cluster fake whose dynamic client can be swapped mid-test —
// the shape of a cluster recreated at a new endpoint, where only a freshly
// resolved client works.
type swapCluster struct {
	mu  sync.Mutex
	dyn dynamic.Interface
}

func (c *swapCluster) ActiveContextName() (string, error) { return "ctx-a", nil }
func (c *swapCluster) DynamicFor(string) (dynamic.Interface, error) {
	c.mu.Lock()
	defer c.mu.Unlock()
	return c.dyn, nil
}
func (c *swapCluster) ClassifyActiveError(err error) kube.Classification {
	return kube.ClassifyError(err, kube.ClassifyHints{})
}
func (c *swapCluster) swapTo(dyn dynamic.Interface) {
	c.mu.Lock()
	c.dyn = dyn
	c.mu.Unlock()
}

// TestHubRecoveryRebuildsInformerOnEndpointChange covers the recreated-cluster
// case (e.g. kind delete + create — the apiserver returns on a NEW port): the
// prober probes with a freshly resolved client, and when that differs from the
// client the informer was built on, the informer is rebuilt on the working
// client so live events actually resume — recovery on the dead client alone
// would reconnect nothing.
func TestHubRecoveryRebuildsInformerOnEndpointChange(t *testing.T) {
	dead := newFakeClient(t)
	dead.PrependReactor("list", "pods", func(clienttesting.Action) (bool, runtime.Object, error) {
		return true, nil, errRefused
	})
	dead.PrependWatchReactor("pods", func(clienttesting.Action) (bool, watch.Interface, error) {
		return true, nil, errRefused
	})
	alive := newFakeClient(t)
	createPod(t, alive, "default", "web-1")

	cluster := &swapCluster{dyn: dead}
	logger := slog.New(slog.NewTextHandler(io.Discard, nil))
	hub := NewHub(cluster, nameShaper, logger, WithProberBackoff(5*time.Millisecond, 20*time.Millisecond))

	sub, err := hub.Subscribe(podsGVR, Filter{})
	require.NoError(t, err)
	defer sub.Close()

	// The dead client drives the informer unreachable.
	require.Eventually(t, sub.si.isUnreachable, 3*time.Second, 10*time.Millisecond)

	// The "cluster" comes back at a new endpoint: only a freshly resolved
	// client reaches it.
	cluster.swapTo(alive)

	var connected bool
	require.Eventually(t, func() bool {
		if st := sub.TakeStatus(); st != nil && st.State == "connected" {
			connected = true
		}
		return connected
	}, 3*time.Second, 10*time.Millisecond, "recovery must broadcast a connected status")
	require.Eventually(t, sub.TakeResync, 3*time.Second, 10*time.Millisecond,
		"recovery must resync clients to a clean baseline")

	// The rebuilt informer delivers from the NEW client: its initial LIST
	// replays the seeded pod, and a later create streams live.
	ev := recv(t, sub, 3*time.Second)
	require.Equal(t, EventAdd, ev.Type)
	require.Equal(t, "web-1", ev.Row)

	createPod(t, alive, "default", "web-2")
	ev = recv(t, sub, 3*time.Second)
	require.Equal(t, EventAdd, ev.Type)
	require.Equal(t, "web-2", ev.Row)
}

// TestHubSyncContextHealthMarksUnreachable covers the probe→hub signal: a
// failed health probe marks the context's informers unreachable (dampened,
// prober started) even though the watch itself never errored — the silent
// TCP-death case — and the prober then drives recovery as usual. Probes for
// other contexts and reachable results are ignored.
func TestHubSyncContextHealthMarksUnreachable(t *testing.T) {
	client := newFakeClient(t)
	logger := slog.New(slog.NewTextHandler(io.Discard, nil))
	// A healthy fake client: the informer never errors on its own, and the
	// prober's first attempt succeeds, so recovery follows the outage signal.
	hub := NewHub(&fakeCluster{dyn: client, context: "ctx-a"}, nameShaper, logger,
		WithProberBackoff(5*time.Millisecond, 20*time.Millisecond))

	sub, err := hub.Subscribe(podsGVR, Filter{})
	require.NoError(t, err)
	defer sub.Close()

	down := kube.ContextHealth{
		Name:     "ctx-a",
		Reason:   string(kube.FailConnectionRefused),
		Error:    "connection refused",
		Guidance: "the cluster may be stopped",
	}

	// A probe failure for a DIFFERENT context must not touch this informer.
	other := down
	other.Name = "ctx-b"
	hub.SyncContextHealth(other)
	assert.False(t, sub.si.isUnreachable())

	// A reachable probe is a no-op (recovery belongs to the prober).
	hub.SyncContextHealth(kube.ContextHealth{Name: "ctx-a", Reachable: true, AuthOK: true})
	assert.False(t, sub.si.isUnreachable())

	hub.SyncContextHealth(down)
	require.Eventually(t, func() bool {
		st := sub.TakeStatus()
		return st != nil && st.State == "unreachable" && st.Reason == string(kube.FailConnectionRefused)
	}, 3*time.Second, 10*time.Millisecond, "a failed probe must surface an unreachable status on the stream")

	// The healthy client lets the prober recover immediately: connected + resync.
	var connected bool
	require.Eventually(t, func() bool {
		if st := sub.TakeStatus(); st != nil && st.State == "connected" {
			connected = true
		}
		return connected
	}, 3*time.Second, 10*time.Millisecond)
	require.Eventually(t, sub.TakeResync, 3*time.Second, 10*time.Millisecond)
	assert.False(t, sub.si.isUnreachable())
}
