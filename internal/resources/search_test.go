package resources

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestMatchName(t *testing.T) {
	cases := map[string]struct {
		name, query string
		want        bool
	}{
		"exact":            {"nginx", "nginx", true},
		"substring":        {"nginx-deploy", "deploy", true},
		"case-insensitive": {"NGINX", "nginx", true},
		"query upper":      {"nginx", "NGINX", true},
		"no match":         {"nginx", "redis", false},
		"empty query":      {"nginx", "", true}, // handler rejects empty; match is substring-of-anything
	}
	for name, tc := range cases {
		t.Run(name, func(t *testing.T) {
			assert.Equal(t, tc.want, matchName(tc.name, tc.query))
		})
	}
}

func TestBoundResults(t *testing.T) {
	t.Run("sorts shorter names first, then resource/namespace/name", func(t *testing.T) {
		in := []SearchResult{
			{Resource: "pods", Namespace: "b", Name: "web-longer"},
			{Resource: "pods", Namespace: "a", Name: "web-2"},
			{Resource: "configmaps", Namespace: "a", Name: "web-2"},
		}
		out, truncated := boundResults(in, 50)
		assert.False(t, truncated)
		require.Len(t, out, 3)
		// Shortest names first (equal length "web-2" ties broken by resource).
		assert.Equal(t, "web-longer", out[2].Name)
		assert.Equal(t, "configmaps", out[0].Resource, "equal-length names order by resource")
		assert.Equal(t, "pods", out[1].Resource)
	})

	t.Run("truncates to the limit and flags it", func(t *testing.T) {
		in := []SearchResult{
			{Resource: "pods", Name: "a"},
			{Resource: "pods", Name: "b"},
			{Resource: "pods", Name: "c"},
		}
		out, truncated := boundResults(in, 2)
		assert.True(t, truncated)
		require.Len(t, out, 2)
	})

	t.Run("at-limit is not truncated", func(t *testing.T) {
		in := []SearchResult{{Name: "a"}, {Name: "b"}}
		out, truncated := boundResults(in, 2)
		assert.False(t, truncated)
		assert.Len(t, out, 2)
	})
}

func TestParseLimit(t *testing.T) {
	assert.Equal(t, defaultSearchLimit, parseLimit(""))
	assert.Equal(t, defaultSearchLimit, parseLimit("nonsense"))
	assert.Equal(t, defaultSearchLimit, parseLimit("0"))
	assert.Equal(t, defaultSearchLimit, parseLimit("-5"))
	assert.Equal(t, 10, parseLimit("10"))
	assert.Equal(t, maxSearchLimit, parseLimit("9999"), "clamped to the hard cap")
}

func TestSearchableGVRsDedupesAndSkips(t *testing.T) {
	result := DiscoveryResult{Groups: []APIGroupInfo{
		{Name: "", Resources: []APIResourceInfo{
			{Group: "", Version: "v1", Resource: "pods", Kind: "Pod"},
			{Group: "", Version: "v1", Resource: "events", Kind: "Event"}, // skipped
			{Group: "", Version: "v1", Resource: "pods", Kind: "Pod"},     // dupe
		}},
		{Name: "events.k8s.io", Resources: []APIResourceInfo{
			{Group: "events.k8s.io", Version: "v1", Resource: "events", Kind: "Event"}, // skipped
		}},
	}}
	got := searchableGVRs(result)
	require.Len(t, got, 1)
	assert.Equal(t, "pods", got[0].Resource)
}
