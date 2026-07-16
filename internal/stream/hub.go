package stream

import (
	"context"
	"log/slog"
	"sync"
	"sync/atomic"
	"time"

	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"k8s.io/apimachinery/pkg/runtime/schema"
	"k8s.io/client-go/dynamic"
	"k8s.io/client-go/dynamic/dynamicinformer"
	"k8s.io/client-go/tools/cache"

	"github.com/skriptvalley/kubescope/internal/kube"
)

// defaultResyncPeriod is the informer's periodic re-sync. It re-delivers the
// store to handlers so a missed watch event self-heals within the period; it is
// not the primary liveness path (watches are).
const defaultResyncPeriod = 10 * time.Minute

// defaultEventBuffer sizes each subscriber's channel. A slow SSE consumer that
// fills its buffer is not blocked-on: further events are dropped and the
// subscriber is flagged for resync so it refetches a clean baseline.
const defaultEventBuffer = 256

// Recovery-prober backoff bounds. When a watch goes unreachable a single prober
// per shared informer retries a cheap LIST on a bounded exponential backoff
// (base, 2×, … capped), so a sustained outage neither busy-loops nor storms
// resyncs across clients (FB-6). proberListTimeout caps each probe.
const (
	defaultProberBase = 1 * time.Second
	defaultProberCap  = 30 * time.Second
	proberListTimeout = 5 * time.Second
)

// Cluster is the slice of the kube manager the hub needs: resolve the active
// context, build a dynamic client for it, and classify a connectivity failure
// so the watch path can report a typed, actionable status. *kube.Manager
// satisfies it; tests fake it. DynamicFor takes an explicit name so the informer
// is keyed and built under the same context, never diverging under a concurrent
// switch.
type Cluster interface {
	ActiveContextName() (string, error)
	DynamicFor(name string) (dynamic.Interface, error)
	ClassifyActiveError(err error) kube.Classification
	// SourceGeneration increments on every runtime kubeconfig swap (ADR-0007).
	// Informers fold it into their key and streams close when it moves, so a
	// swap never leaves a live view attached to the previous file's cluster.
	SourceGeneration() int64
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
	sanitize    ObjectSanitizer
	logger      *slog.Logger
	resync      time.Duration
	eventBuffer int
	proberBase  time.Duration
	proberCap   time.Duration

	mu        sync.Mutex
	informers map[hubKey]*sharedGVRInformer
}

// ObjectSanitizer scrubs a full object before it is sent to a detail subscriber
// (Sprint 5). It returns the object to ship — the same one when nothing needs
// hiding, or a masked deep copy (never mutating the shared informer cache) when
// it does, e.g. redacting Secret data (ADR-0005). Nil means send objects as-is.
type ObjectSanitizer func(schema.GroupVersionResource, *unstructured.Unstructured) *unstructured.Unstructured

type hubKey struct {
	context string
	// gen is the kubeconfig-source generation the informer was built under.
	// Context names recur across kubeconfig files, so without it a runtime
	// source swap (ADR-0007) would keep serving the old file's cluster.
	gen int64
	gvr schema.GroupVersionResource
}

// HubOption tunes a Hub at construction (informer resync, subscriber buffer).
type HubOption func(*Hub)

// WithResyncPeriod overrides the informer resync period.
func WithResyncPeriod(d time.Duration) HubOption { return func(h *Hub) { h.resync = d } }

// WithEventBuffer overrides the per-subscriber channel buffer size.
func WithEventBuffer(n int) HubOption { return func(h *Hub) { h.eventBuffer = n } }

// WithObjectSanitizer installs a sanitizer run on every full object sent to a
// detail subscriber, so sensitive fields (Secret data) never reach the browser
// over the watch stream — the same masking the REST detail/YAML paths apply.
func WithObjectSanitizer(fn ObjectSanitizer) HubOption { return func(h *Hub) { h.sanitize = fn } }

// WithProberBackoff overrides the recovery prober's base and cap delays. Tests
// inject tiny delays to exercise the backoff without real waits.
func WithProberBackoff(base, capDelay time.Duration) HubOption {
	return func(h *Hub) { h.proberBase = base; h.proberCap = capDelay }
}

// NewHub builds a Hub over the given cluster and row shaper.
func NewHub(cluster Cluster, shaper Shaper, logger *slog.Logger, opts ...HubOption) *Hub {
	h := &Hub{
		cluster:     cluster,
		shaper:      shaper,
		logger:      logger,
		resync:      defaultResyncPeriod,
		eventBuffer: defaultEventBuffer,
		proberBase:  defaultProberBase,
		proberCap:   defaultProberCap,
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

	k := hubKey{context: ctxName, gen: h.cluster.SourceGeneration(), gvr: gvr}
	si := h.informers[k]
	if si == nil {
		dyn, err := h.cluster.DynamicFor(ctxName)
		if err != nil {
			return nil, err
		}
		// refreshDyn re-resolves the context's dynamic client on every recovery
		// probe: after an outage the endpoint may have moved (a recreated local
		// cluster gets a new port) and only a freshly built client can reach it.
		refreshDyn := func() (dynamic.Interface, error) { return h.cluster.DynamicFor(ctxName) }
		si = newSharedGVRInformer(dyn, refreshDyn, gvr, h.resync, h.logger, h.cluster.ClassifyActiveError, h.proberBase, h.proberCap)
		h.informers[k] = si
	}
	// Register the handler before starting Run so the first subscriber receives
	// the initial LIST as adds; later subscribers get the current store replayed.
	sub := si.addSubscriber(filter, gvr, h.shaper, h.sanitize, h.eventBuffer)
	si.refs++
	si.ensureStarted()

	return &Subscription{hub: h, key: k, si: si, sub: sub, context: ctxName, generation: k.gen}, nil
}

// activeInformers reports how many informers are live — teardown assertions.
func (h *Hub) activeInformers() int {
	h.mu.Lock()
	defer h.mu.Unlock()
	return len(h.informers)
}

// SyncContextHealth feeds a fresh health-probe result into the watch layer: a
// probe that found the context unreachable marks every informer on it
// unreachable (the same dampened transition + recovery prober as a watch
// error). This exists because a cluster can die silently — a half-open TCP
// watch may not error for minutes — while probes detect the outage within
// seconds; without it a recreated cluster leaves informers attached to the
// dead endpoint forever. Recovery stays the prober's job (probe successes are
// not synced), so a transient failed probe costs at most one status flap.
func (h *Hub) SyncContextHealth(health kube.ContextHealth) {
	if health.Reachable {
		return
	}
	info := &StatusInfo{
		State:    "unreachable",
		Reason:   health.Reason,
		Message:  health.Error,
		Guidance: health.Guidance,
	}
	// Collect under hub.mu, mark outside it: Subscription.Close locks si then
	// hub, so holding both here in the opposite order would deadlock.
	h.mu.Lock()
	informers := make([]*sharedGVRInformer, 0, len(h.informers))
	for k, si := range h.informers {
		if k.context == health.Name {
			informers = append(informers, si)
		}
	}
	h.mu.Unlock()
	for _, si := range informers {
		si.markUnreachable(info)
	}
}

// Subscription is one client's handle on a shared informer. Events() streams
// add/update/delete; TakeResync() reports (and clears) a pending resync;
// Close() releases the subscription exactly once.
type Subscription struct {
	hub        *Hub
	key        hubKey
	si         *sharedGVRInformer
	sub        *subscriber
	context    string
	generation int64
	closeOnce  sync.Once
}

// Events is the subscriber's event channel (add/update/delete).
func (s *Subscription) Events() <-chan Event { return s.sub.events }

// TakeResync reports whether a resync is pending, clearing the flag. A resync
// is raised by this subscriber's buffer overflowing or by a recovery from a
// cluster outage (the client refetches a clean baseline).
func (s *Subscription) TakeResync() bool { return s.sub.resync.Swap(false) }

// TakeStatus reports (and clears) a pending connectivity status change for this
// subscriber. Nil when none is pending. Set on a reachable↔unreachable
// transition and, for a subscriber that attaches mid-outage, at attach time.
func (s *Subscription) TakeStatus() *StatusInfo { return s.sub.status.Swap(nil) }

// Context is the context name this subscription is bound to; the SSE handler
// closes the stream when the active context moves off it.
func (s *Subscription) Context() string { return s.context }

// Generation is the kubeconfig-source generation this subscription was bound
// under; the SSE handler closes the stream when a runtime source swap moves it
// (the client resubscribes onto a fresh informer built from the new source).
func (s *Subscription) Generation() int64 { return s.generation }

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
// lifecycle. refs is guarded by Hub.mu (the only writer); everything else —
// including the informer/stopCh pair, which the recovery path can swap for a
// rebuilt one — by mu.
type sharedGVRInformer struct {
	dyn        dynamic.Interface // client the current informer was built on
	refreshDyn func() (dynamic.Interface, error)
	gvr        schema.GroupVersionResource
	classify   func(error) kube.Classification
	resync     time.Duration
	logger     *slog.Logger
	proberBase time.Duration
	proberCap  time.Duration

	mu             sync.Mutex
	informer       cache.SharedIndexInformer
	stopCh         chan struct{}
	stopped        bool
	subs           map[*subscriber]cache.ResourceEventHandlerRegistration
	started        bool
	refs           int // guarded by Hub.mu
	unreachable    bool
	lastStatus     *StatusInfo
	proberOn       bool
	proberAttempts atomic.Int64 // recovery-probe LIST attempts; read in tests
}

func newSharedGVRInformer(dyn dynamic.Interface, refreshDyn func() (dynamic.Interface, error), gvr schema.GroupVersionResource, resync time.Duration, logger *slog.Logger, classify func(error) kube.Classification, proberBase, proberCap time.Duration) *sharedGVRInformer {
	si := &sharedGVRInformer{
		dyn:        dyn,
		refreshDyn: refreshDyn,
		gvr:        gvr,
		classify:   classify,
		resync:     resync,
		stopCh:     make(chan struct{}),
		logger:     logger,
		proberBase: proberBase,
		proberCap:  proberCap,
		subs:       make(map[*subscriber]cache.ResourceEventHandlerRegistration),
	}
	si.informer = si.buildInformer(dyn)
	return si
}

// buildInformer constructs the dynamic informer this sharedGVRInformer fans out
// from — once at creation and again when recovery rebuilds on a fresh client (a
// stopped SharedIndexInformer cannot be re-Run).
func (si *sharedGVRInformer) buildInformer(dyn dynamic.Interface) cache.SharedIndexInformer {
	informer := dynamicinformer.NewFilteredDynamicInformer(
		dyn, si.gvr, metav1NamespaceAll, si.resync,
		cache.Indexers{cache.NamespaceIndex: cache.MetaNamespaceIndexFunc}, nil,
	).Informer()
	if err := informer.SetWatchErrorHandler(func(_ *cache.Reflector, err error) { si.handleWatchError(err) }); err != nil {
		si.logger.Error("setting watch error handler", "error", err)
	}
	return informer
}

// handlerFor is the event-handler set delivering to one subscriber; shared by
// the attach path and the recovery rebuild (which re-registers every
// subscriber on the new informer).
func handlerFor(sub *subscriber) cache.ResourceEventHandlerFuncs {
	return cache.ResourceEventHandlerFuncs{
		AddFunc:    func(obj any) { sub.deliver(EventAdd, obj) },
		UpdateFunc: func(_, obj any) { sub.deliver(EventUpdate, obj) },
		DeleteFunc: func(obj any) { sub.deliver(EventDelete, obj) },
	}
}

// metav1NamespaceAll is the all-namespaces sentinel ("") — the informer watches
// cluster-wide and each subscriber filters to its own namespace.
const metav1NamespaceAll = ""

func (si *sharedGVRInformer) addSubscriber(filter Filter, gvr schema.GroupVersionResource, shaper Shaper, sanitize ObjectSanitizer, buffer int) *subscriber {
	sub := &subscriber{
		events:   make(chan Event, buffer),
		filter:   filter,
		gvr:      gvr,
		shaper:   shaper,
		sanitize: sanitize,
	}
	// Register under the lock so a concurrent recovery rebuild either sees this
	// subscriber in subs (and re-registers it on the new informer) or the
	// handler lands on the new informer directly — never on a stopped one.
	si.mu.Lock()
	reg, err := si.informer.AddEventHandler(handlerFor(sub))
	if err != nil {
		si.logger.Error("adding informer handler", "error", err)
	}
	si.subs[sub] = reg
	// A subscriber attaching during an ongoing outage immediately learns the
	// cluster is unreachable, so its banner shows without waiting for the next
	// transition (which is dampened and may never come while it stays down).
	if si.unreachable && si.lastStatus != nil {
		sub.status.Store(si.lastStatus)
	}
	si.mu.Unlock()
	return sub
}

func (si *sharedGVRInformer) removeSubscriber(sub *subscriber) {
	si.mu.Lock()
	reg := si.subs[sub]
	informer := si.informer // the informer this registration belongs to
	delete(si.subs, sub)
	si.mu.Unlock()
	if reg != nil {
		if err := informer.RemoveEventHandler(reg); err != nil {
			si.logger.Error("removing informer handler", "error", err)
		}
	}
}

// broadcastResync flags every subscriber for resync. Called on recovery so
// every client refetches a clean baseline over the gap the outage left.
func (si *sharedGVRInformer) broadcastResync() {
	si.mu.Lock()
	for sub := range si.subs {
		sub.resync.Store(true)
	}
	si.mu.Unlock()
}

// broadcastStatus hands every subscriber the latest connectivity status, read
// and emitted by each SSE loop. A newer status overwrites an unread one — only
// the current state matters.
func (si *sharedGVRInformer) broadcastStatus(info *StatusInfo) {
	si.mu.Lock()
	for sub := range si.subs {
		sub.status.Store(info)
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
	// The watch-error handler was set at build time (buildInformer): the
	// reflector invokes it on every failed ListAndWatch. The first failure
	// transitions the informer to unreachable and broadcasts a typed status;
	// repeated failures are dampened (no resync storm) and recovery is driven
	// by a dedicated prober.
	go si.informer.Run(si.stopCh)
}

// handleWatchError sorts a watch/list failure into the failure taxonomy and
// marks the informer unreachable.
func (si *sharedGVRInformer) handleWatchError(err error) {
	cls := si.classify(err)
	si.markUnreachable(&StatusInfo{
		State:    "unreachable",
		Reason:   string(cls.Class),
		Message:  err.Error(),
		Guidance: cls.Remediation,
	})
}

// markUnreachable records an outage (from a watch error or a failed health
// probe) and, on the reachable→unreachable transition, broadcasts the status
// once, logs a single warning and starts the recovery prober. While already
// unreachable it dampens: no broadcast, only refreshing the cached reason so a
// subscriber attaching later (or the eventual recovery) reflects the latest.
func (si *sharedGVRInformer) markUnreachable(info *StatusInfo) {
	si.mu.Lock()
	if si.unreachable {
		if si.lastStatus == nil || si.lastStatus.Reason != info.Reason {
			si.lastStatus = info
		}
		si.mu.Unlock()
		return
	}
	si.unreachable = true
	si.lastStatus = info
	startProber := !si.proberOn
	si.proberOn = si.proberOn || startProber
	si.mu.Unlock()

	si.logger.Warn("watch stream unreachable", "gvr", si.gvr.String(), "reason", info.Reason)
	si.broadcastStatus(info)
	if startProber {
		go si.runProber()
	}
}

// runProber retries a cheap LIST on a bounded exponential backoff until it
// succeeds (recovery) or the informer is stopped. Exactly one runs per shared
// informer during an outage. proberOn is cleared in the same critical section
// as the state each exit publishes (recover, or the stop path below) — a
// deferred clear after recover would leave a window where a new watch error
// sees reachable+proberOn and transitions to unreachable without starting a
// prober, sticking the stream in an outage nothing ever ends.
func (si *sharedGVRInformer) runProber() {
	delay := si.proberBase
	for {
		timer := time.NewTimer(delay)
		select {
		case <-si.stopCh:
			timer.Stop()
			si.mu.Lock()
			si.proberOn = false
			si.mu.Unlock()
			return
		case <-timer.C:
		}

		si.proberAttempts.Add(1)
		// Probe with a freshly resolved client, not the informer's: after an
		// outage the endpoint may have moved (a recreated local cluster gets a
		// new port), and only a rebuilt client can reach the new address.
		dyn := si.dyn
		if si.refreshDyn != nil {
			fresh, err := si.refreshDyn()
			if err != nil {
				dyn = nil // kubeconfig currently unreadable — back off and retry
			} else {
				dyn = fresh
			}
		}
		if dyn != nil {
			ctx, cancel := context.WithTimeout(context.Background(), proberListTimeout)
			_, err := dyn.Resource(si.gvr).List(ctx, metav1.ListOptions{Limit: 1})
			cancel()
			if err == nil {
				// The cluster answered. If it answered on a different client than
				// the informer was built on, the old informer can never reconnect
				// — rebuild it on the working client before announcing recovery.
				if dyn != si.dyn {
					si.rebuildInformer(dyn)
				}
				si.recover()
				return
			}
		}

		if delay < si.proberCap {
			delay *= 2
			if delay > si.proberCap {
				delay = si.proberCap
			}
		}
	}
}

// recover clears the unreachable state and tells every subscriber the cluster
// is back: a "connected" status hides the banner and a resync makes each client
// refetch a clean baseline over the gap the outage left. proberOn is cleared
// together with unreachable so a concurrent watch error always observes a
// consistent pair and restarts the prober on the next transition.
func (si *sharedGVRInformer) recover() {
	si.mu.Lock()
	si.proberOn = false
	if !si.unreachable {
		si.mu.Unlock()
		return
	}
	si.unreachable = false
	si.lastStatus = nil
	si.mu.Unlock()

	si.logger.Info("watch stream recovered", "gvr", si.gvr.String())
	si.broadcastStatus(&StatusInfo{State: "connected"})
	si.broadcastResync()
}

// isUnreachable reports the current outage state (teardown/recovery assertions).
func (si *sharedGVRInformer) isUnreachable() bool {
	si.mu.Lock()
	defer si.mu.Unlock()
	return si.unreachable
}

// proberActive reports whether a recovery prober goroutine is running (leak
// assertions after Close).
func (si *sharedGVRInformer) proberActive() bool {
	si.mu.Lock()
	defer si.mu.Unlock()
	return si.proberOn
}

// rebuildInformer swaps in a new informer built on dyn — the recovery path when
// the cluster came back at a different endpoint. The old informer is stopped
// (a stopped SharedIndexInformer cannot be re-Run); every subscriber keeps its
// channel and is re-registered on the new informer, whose initial LIST replays
// the current objects (subscribers also get a resync from recover()).
func (si *sharedGVRInformer) rebuildInformer(dyn dynamic.Interface) {
	si.mu.Lock()
	defer si.mu.Unlock()
	if si.stopped {
		return // torn down while the prober was in flight — nothing to rebuild
	}
	close(si.stopCh)
	si.stopCh = make(chan struct{})
	si.dyn = dyn
	si.informer = si.buildInformer(dyn)
	for sub := range si.subs {
		reg, err := si.informer.AddEventHandler(handlerFor(sub))
		if err != nil {
			si.logger.Error("re-adding informer handler after rebuild", "error", err)
			continue
		}
		si.subs[sub] = reg
	}
	si.logger.Info("watch informer rebuilt on a fresh client", "gvr", si.gvr.String())
	if si.started {
		go si.informer.Run(si.stopCh)
	}
}

func (si *sharedGVRInformer) stop() {
	si.mu.Lock()
	defer si.mu.Unlock()
	if !si.stopped {
		si.stopped = true
		close(si.stopCh)
	}
}

// subscriber is one registration's channel + filter + resync/status flags. The
// status slot holds the latest pending connectivity change (nil when none),
// read and emitted by the SSE loop alongside resync.
type subscriber struct {
	events   chan Event
	resync   atomic.Bool
	status   atomic.Pointer[StatusInfo]
	filter   Filter
	gvr      schema.GroupVersionResource
	shaper   Shaper
	sanitize ObjectSanitizer
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
			// Scrub the full object before it leaves the server (e.g. Secret data
			// masking, ADR-0005). The sanitizer returns a masked deep copy when
			// needed, so the shared informer cache is never mutated.
			out := u
			if sub.sanitize != nil {
				out = sub.sanitize(sub.gvr, u)
			}
			ev.Object = out.Object
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
