package stream

import (
	"log/slog"
	"sync"
	"sync/atomic"
	"time"

	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"k8s.io/apimachinery/pkg/runtime/schema"
	"k8s.io/client-go/dynamic"
	"k8s.io/client-go/dynamic/dynamicinformer"
	"k8s.io/client-go/tools/cache"
)

// defaultResyncPeriod is the informer's periodic re-sync. It re-delivers the
// store to handlers so a missed watch event self-heals within the period; it is
// not the primary liveness path (watches are).
const defaultResyncPeriod = 10 * time.Minute

// defaultEventBuffer sizes each subscriber's channel. A slow SSE consumer that
// fills its buffer is not blocked-on: further events are dropped and the
// subscriber is flagged for resync so it refetches a clean baseline.
const defaultEventBuffer = 256

// Cluster is the slice of the kube manager the hub needs: resolve the active
// context and build a dynamic client for it. *kube.Manager satisfies it; tests
// fake it. DynamicFor takes an explicit name so the informer is keyed and built
// under the same context, never diverging under a concurrent switch.
type Cluster interface {
	ActiveContextName() (string, error)
	DynamicFor(name string) (dynamic.Interface, error)
}

// Shaper turns a live object into the row a list/feed view renders. Injected so
// the hub does not depend on internal/resources; server wiring passes
// resources.ShapeStreamRow.
type Shaper func(schema.GroupVersionResource, *unstructured.Unstructured) any

// Filter narrows a subscription. Empty Namespace/Name match everything (a list
// watch); a detail subscriber sets both and IncludeObject to receive the full
// object for the one object it renders.
type Filter struct {
	Namespace     string
	Name          string
	IncludeObject bool
}

func (f Filter) matches(u *unstructured.Unstructured) bool {
	if f.Namespace != "" && u.GetNamespace() != f.Namespace {
		return false
	}
	if f.Name != "" && u.GetName() != f.Name {
		return false
	}
	return true
}

// Hub owns one shared informer per (context, GVR), fanned out to all its
// subscribers and torn down when the last one disconnects (ADR-0006).
type Hub struct {
	cluster     Cluster
	shaper      Shaper
	logger      *slog.Logger
	resync      time.Duration
	eventBuffer int

	mu        sync.Mutex
	informers map[hubKey]*sharedGVRInformer
}

type hubKey struct {
	context string
	gvr     schema.GroupVersionResource
}

// HubOption tunes a Hub at construction (informer resync, subscriber buffer).
type HubOption func(*Hub)

// WithResyncPeriod overrides the informer resync period.
func WithResyncPeriod(d time.Duration) HubOption { return func(h *Hub) { h.resync = d } }

// WithEventBuffer overrides the per-subscriber channel buffer size.
func WithEventBuffer(n int) HubOption { return func(h *Hub) { h.eventBuffer = n } }

// NewHub builds a Hub over the given cluster and row shaper.
func NewHub(cluster Cluster, shaper Shaper, logger *slog.Logger, opts ...HubOption) *Hub {
	h := &Hub{
		cluster:     cluster,
		shaper:      shaper,
		logger:      logger,
		resync:      defaultResyncPeriod,
		eventBuffer: defaultEventBuffer,
		informers:   make(map[hubKey]*sharedGVRInformer),
	}
	for _, opt := range opts {
		opt(h)
	}
	return h
}

// Subscribe registers a subscriber to the shared informer for gvr in the active
// context, creating (and starting) that informer on first use. The returned
// Subscription must be Closed to release the ref and, when it is the last one,
// tear the informer down.
func (h *Hub) Subscribe(gvr schema.GroupVersionResource, filter Filter) (*Subscription, error) {
	ctxName, err := h.cluster.ActiveContextName()
	if err != nil {
		return nil, err
	}

	h.mu.Lock()
	defer h.mu.Unlock()

	k := hubKey{context: ctxName, gvr: gvr}
	si := h.informers[k]
	if si == nil {
		dyn, err := h.cluster.DynamicFor(ctxName)
		if err != nil {
			return nil, err
		}
		si = newSharedGVRInformer(dyn, gvr, h.resync, h.logger)
		h.informers[k] = si
	}
	// Register the handler before starting Run so the first subscriber receives
	// the initial LIST as adds; later subscribers get the current store replayed.
	sub := si.addSubscriber(filter, gvr, h.shaper, h.eventBuffer)
	si.refs++
	si.ensureStarted()

	return &Subscription{hub: h, key: k, si: si, sub: sub, context: ctxName}, nil
}

// activeInformers reports how many informers are live — teardown assertions.
func (h *Hub) activeInformers() int {
	h.mu.Lock()
	defer h.mu.Unlock()
	return len(h.informers)
}

// Subscription is one client's handle on a shared informer. Events() streams
// add/update/delete; TakeResync() reports (and clears) a pending resync;
// Close() releases the subscription exactly once.
type Subscription struct {
	hub       *Hub
	key       hubKey
	si        *sharedGVRInformer
	sub       *subscriber
	context   string
	closeOnce sync.Once
}

// Events is the subscriber's event channel (add/update/delete).
func (s *Subscription) Events() <-chan Event { return s.sub.events }

// TakeResync reports whether a resync is pending, clearing the flag. A resync
// is raised by a watch-error re-list or by this subscriber's buffer overflowing.
func (s *Subscription) TakeResync() bool { return s.sub.resync.Swap(false) }

// Context is the context name this subscription is bound to; the SSE handler
// closes the stream when the active context moves off it.
func (s *Subscription) Context() string { return s.context }

// Close removes the subscriber and, if it was the last, stops and forgets the
// informer.
func (s *Subscription) Close() {
	s.closeOnce.Do(func() {
		s.si.removeSubscriber(s.sub)
		s.hub.mu.Lock()
		s.si.refs--
		if s.si.refs == 0 {
			s.si.stop()
			if s.hub.informers[s.key] == s.si {
				delete(s.hub.informers, s.key)
			}
		}
		s.hub.mu.Unlock()
	})
}

// sharedGVRInformer wraps one dynamic informer, its subscribers and its
// lifecycle. refs is guarded by Hub.mu (the only writer); everything else by mu.
type sharedGVRInformer struct {
	informer cache.SharedIndexInformer
	stopCh   chan struct{}
	stopOnce sync.Once
	logger   *slog.Logger

	mu      sync.Mutex
	subs    map[*subscriber]cache.ResourceEventHandlerRegistration
	started bool
	refs    int // guarded by Hub.mu
}

func newSharedGVRInformer(dyn dynamic.Interface, gvr schema.GroupVersionResource, resync time.Duration, logger *slog.Logger) *sharedGVRInformer {
	informer := dynamicinformer.NewFilteredDynamicInformer(
		dyn, gvr, metav1NamespaceAll, resync,
		cache.Indexers{cache.NamespaceIndex: cache.MetaNamespaceIndexFunc}, nil,
	).Informer()
	return &sharedGVRInformer{
		informer: informer,
		stopCh:   make(chan struct{}),
		logger:   logger,
		subs:     make(map[*subscriber]cache.ResourceEventHandlerRegistration),
	}
}

// metav1NamespaceAll is the all-namespaces sentinel ("") — the informer watches
// cluster-wide and each subscriber filters to its own namespace.
const metav1NamespaceAll = ""

func (si *sharedGVRInformer) addSubscriber(filter Filter, gvr schema.GroupVersionResource, shaper Shaper, buffer int) *subscriber {
	sub := &subscriber{
		events: make(chan Event, buffer),
		filter: filter,
		gvr:    gvr,
		shaper: shaper,
	}
	reg, err := si.informer.AddEventHandler(cache.ResourceEventHandlerFuncs{
		AddFunc:    func(obj any) { sub.deliver(EventAdd, obj) },
		UpdateFunc: func(_, obj any) { sub.deliver(EventUpdate, obj) },
		DeleteFunc: func(obj any) { sub.deliver(EventDelete, obj) },
	})
	if err != nil {
		si.logger.Error("adding informer handler", "error", err)
	}
	si.mu.Lock()
	si.subs[sub] = reg
	si.mu.Unlock()
	return sub
}

func (si *sharedGVRInformer) removeSubscriber(sub *subscriber) {
	si.mu.Lock()
	reg := si.subs[sub]
	delete(si.subs, sub)
	si.mu.Unlock()
	if reg != nil {
		if err := si.informer.RemoveEventHandler(reg); err != nil {
			si.logger.Error("removing informer handler", "error", err)
		}
	}
}

// broadcastResync flags every subscriber for resync. Called from the watch
// error handler (a re-list means the client may have gaps).
func (si *sharedGVRInformer) broadcastResync() {
	si.mu.Lock()
	for sub := range si.subs {
		sub.resync.Store(true)
	}
	si.mu.Unlock()
}

func (si *sharedGVRInformer) ensureStarted() {
	si.mu.Lock()
	defer si.mu.Unlock()
	if si.started {
		return
	}
	si.started = true
	// Set before Run: a watch error triggers a re-list, and subscribers must be
	// told so they refetch a clean baseline.
	if err := si.informer.SetWatchErrorHandler(func(_ *cache.Reflector, _ error) { si.broadcastResync() }); err != nil {
		si.logger.Error("setting watch error handler", "error", err)
	}
	go si.informer.Run(si.stopCh)
}

func (si *sharedGVRInformer) stop() {
	si.stopOnce.Do(func() { close(si.stopCh) })
}

// subscriber is one registration's channel + filter + resync flag.
type subscriber struct {
	events chan Event
	resync atomic.Bool
	filter Filter
	gvr    schema.GroupVersionResource
	shaper Shaper
}

// deliver shapes and fans one informer notification to this subscriber, subject
// to its filter. A full buffer is never blocked on — the event is dropped and
// the subscriber flagged for resync.
func (sub *subscriber) deliver(t EventType, obj any) {
	u, ok := toUnstructured(obj)
	if !ok {
		return
	}
	if !sub.filter.matches(u) {
		return
	}
	ev := Event{Type: t}
	if t == EventDelete {
		ev.Ref = &ObjectRef{Name: u.GetName(), Namespace: u.GetNamespace(), UID: string(u.GetUID())}
	} else {
		ev.Row = sub.shaper(sub.gvr, u)
		if sub.filter.IncludeObject {
			ev.Object = u.Object
		}
	}
	select {
	case sub.events <- ev:
	default:
		sub.resync.Store(true)
	}
}

// toUnstructured unwraps an informer object, tolerating the tombstone the
// DeleteFunc receives when the final state was missed.
func toUnstructured(obj any) (*unstructured.Unstructured, bool) {
	switch v := obj.(type) {
	case *unstructured.Unstructured:
		return v, true
	case cache.DeletedFinalStateUnknown:
		if u, ok := v.Obj.(*unstructured.Unstructured); ok {
			return u, true
		}
	}
	return nil, false
}
