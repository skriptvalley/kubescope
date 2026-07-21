package resources

import (
	"context"
	"errors"
	"testing"

	"github.com/stretchr/testify/assert"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
)

// fakeLister returns canned pages, recording the requested continue tokens. When
// `always` is set it returns that page on every call (to drive the cap path).
type fakeLister struct {
	pages  []*unstructured.UnstructuredList
	always *unstructured.UnstructuredList
	err    error
	calls  int
}

func (f *fakeLister) List(_ context.Context, _ metav1.ListOptions) (*unstructured.UnstructuredList, error) {
	f.calls++
	if f.err != nil {
		return nil, f.err
	}
	if f.always != nil {
		return f.always, nil
	}
	return f.pages[f.calls-1], nil
}

func page(items int, remaining *int64, cont string) *unstructured.UnstructuredList {
	list := &unstructured.UnstructuredList{}
	for i := 0; i < items; i++ {
		list.Items = append(list.Items, unstructured.Unstructured{})
	}
	list.SetContinue(cont)
	list.SetRemainingItemCount(remaining)
	return list
}

func TestCountResourceUsesRemainingItemCount(t *testing.T) {
	rem := int64(11)
	f := &fakeLister{pages: []*unstructured.UnstructuredList{page(1, &rem, "")}}
	n, ok, exact := countResource(context.Background(), f)
	assert.True(t, ok)
	assert.True(t, exact)
	assert.Equal(t, 12, n) // 1 returned + 11 remaining, single call
	assert.Equal(t, 1, f.calls)
}

func TestCountResourceSinglePage(t *testing.T) {
	f := &fakeLister{pages: []*unstructured.UnstructuredList{page(3, nil, "")}}
	n, ok, exact := countResource(context.Background(), f)
	assert.True(t, ok)
	assert.True(t, exact)
	assert.Equal(t, 3, n)
}

func TestCountResourcePaginates(t *testing.T) {
	f := &fakeLister{pages: []*unstructured.UnstructuredList{
		page(200, nil, "tok1"),
		page(50, nil, ""),
	}}
	n, ok, exact := countResource(context.Background(), f)
	assert.True(t, ok)
	assert.True(t, exact)
	assert.Equal(t, 250, n)
	assert.Equal(t, 2, f.calls)
}

// A backend that always returns a continue token and never populates
// RemainingItemCount exhausts the page cap; the count is a floor, not exact.
func TestCountResourceHitsPageCapReturnsInexactFloor(t *testing.T) {
	f := &fakeLister{always: page(200, nil, "more")}
	n, ok, exact := countResource(context.Background(), f)
	assert.True(t, ok)
	assert.False(t, exact) // floor — caller must mark the response partial
	assert.Equal(t, countMaxPages*200, n)
	assert.Equal(t, countMaxPages, f.calls) // stopped at the cap, did not loop forever
}

func TestCountResourceErrorIsNotOk(t *testing.T) {
	f := &fakeLister{err: errors.New("forbidden")}
	n, ok, exact := countResource(context.Background(), f)
	assert.False(t, ok)
	assert.False(t, exact)
	assert.Equal(t, 0, n)
}
