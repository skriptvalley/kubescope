package server

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestCrossOriginGuard(t *testing.T) {
	srv := testServer(t, spaFixture())
	send := func(method, path string, hdr map[string]string) *httptest.ResponseRecorder {
		req := httptest.NewRequest(method, path, nil)
		req.Host = "projects.example.dev"
		for k, v := range hdr {
			req.Header.Set(k, v)
		}
		rec := httptest.NewRecorder()
		srv.ServeHTTP(rec, req)
		return rec
	}
	rejected := func(t *testing.T, rec *httptest.ResponseRecorder) {
		t.Helper()
		require.Equal(t, http.StatusForbidden, rec.Code)
		var env struct {
			Error struct{ Code string } `json:"error"`
		}
		require.NoError(t, json.Unmarshal(rec.Body.Bytes(), &env))
		assert.Equal(t, "cross_origin_rejected", env.Error.Code)
	}

	// A body-less POST mutation from another site or a sibling subdomain is refused.
	rejected(t, send(http.MethodPost, "/api/v1/nodes/n1/cordon", map[string]string{"Sec-Fetch-Site": "cross-site"}))
	rejected(t, send(http.MethodPost, "/api/v1/nodes/n1/cordon", map[string]string{"Sec-Fetch-Site": "same-site"}))
	// Without Fetch Metadata, a mismatched Origin is refused too.
	rejected(t, send(http.MethodPost, "/api/v1/contexts/switch", map[string]string{"Origin": "https://evil.example.dev"}))

	// Same-origin browser requests and non-browser clients reach the handler.
	for name, hdr := range map[string]map[string]string{
		"same-origin":     {"Sec-Fetch-Site": "same-origin"},
		"matching origin": {"Origin": "https://projects.example.dev"},
		"no headers":      {},
	} {
		rec := send(http.MethodPost, "/api/v1/contexts/switch", hdr)
		assert.NotEqual(t, http.StatusForbidden, rec.Code, name)
	}

	// Safe methods are never blocked, even cross-site.
	rec := send(http.MethodGet, "/api/v1/nodes", map[string]string{"Sec-Fetch-Site": "cross-site"})
	assert.Equal(t, http.StatusOK, rec.Code)
}
