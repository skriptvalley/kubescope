package graph

import (
	"context"
	"fmt"
	"sort"

	apierrors "k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"k8s.io/apimachinery/pkg/runtime/schema"
	"k8s.io/client-go/dynamic"
)

// listLimit bounds a single neighbour list. A namespace with more objects of a
// kind than this is truncated — surfaced as Partial with a note, never as a
// silently short graph (the same posture as the FB-11 counts endpoint).
const listLimit = 500

// store is the per-request read cache in front of the dynamic client: every
// object is fetched at most once and every kind listed at most once, so a
// traversal that revisits the same Secret from twenty pods costs one GET.
type store struct {
	dyn dynamic.Interface
	ns  string

	// objects caches gets; a nil value is a confirmed absence (NotFound), which
	// the builder renders as a Missing node rather than re-fetching.
	objects map[string]*unstructured.Unstructured
	lists   map[schema.GroupVersionResource][]unstructured.Unstructured
}

func newStore(dyn dynamic.Interface, namespace string) *store {
	return &store{
		dyn:     dyn,
		ns:      namespace,
		objects: map[string]*unstructured.Unstructured{},
		lists:   map[schema.GroupVersionResource][]unstructured.Unstructured{},
	}
}

func gvrOf(info ResourceInfo) schema.GroupVersionResource {
	return schema.GroupVersionResource{Group: info.Group, Version: info.Version, Resource: info.Resource}
}

// ri returns the namespaced (or cluster-scoped) client for a resource type.
func (s *store) ri(info ResourceInfo) dynamic.ResourceInterface {
	nri := s.dyn.Resource(gvrOf(info))
	if info.Namespaced {
		return nri.Namespace(s.ns)
	}
	return nri
}

// get fetches one object, returning (nil, nil) when it does not exist so a
// dangling reference is a fact about the graph rather than a failure.
func (s *store) get(ctx context.Context, info ResourceInfo, name string) (*unstructured.Unstructured, error) {
	key := gvrOf(info).String() + "/" + name
	if obj, ok := s.objects[key]; ok {
		return obj, nil
	}
	obj, err := s.ri(info).Get(ctx, name, metav1.GetOptions{})
	if apierrors.IsNotFound(err) {
		s.objects[key] = nil
		return nil, nil
	}
	if err != nil {
		return nil, fmt.Errorf("getting %s %q: %w", info.Resource, name, err)
	}
	s.objects[key] = obj
	return obj, nil
}

// getStrict fetches the focus object, surfacing NotFound as the apierror it is
// so the handler's classifier turns it into a 404 rather than an empty graph.
func (s *store) getStrict(ctx context.Context, info ResourceInfo, name string) (*unstructured.Unstructured, error) {
	obj, err := s.ri(info).Get(ctx, name, metav1.GetOptions{})
	if err != nil {
		return nil, fmt.Errorf("getting %s %q: %w", info.Resource, name, err)
	}
	s.objects[gvrOf(info).String()+"/"+name] = obj
	return obj, nil
}

// list returns every object of a kind in the namespace, name-sorted for a
// deterministic graph. truncated reports that the namespace holds more than
// listLimit of them.
func (s *store) list(ctx context.Context, info ResourceInfo) (items []unstructured.Unstructured, truncated bool, err error) {
	gvr := gvrOf(info)
	if cached, ok := s.lists[gvr]; ok {
		return cached, false, nil
	}
	list, err := s.ri(info).List(ctx, metav1.ListOptions{Limit: listLimit})
	if err != nil {
		return nil, false, fmt.Errorf("listing %s: %w", info.Resource, err)
	}
	items = list.Items
	sort.Slice(items, func(i, j int) bool { return items[i].GetName() < items[j].GetName() })
	s.lists[gvr] = items
	// Cache the object identities too: a listed child is almost always visited
	// next, and this keeps that free.
	for i := range items {
		s.objects[gvr.String()+"/"+items[i].GetName()] = &items[i]
	}
	return items, list.GetContinue() != "", nil
}
