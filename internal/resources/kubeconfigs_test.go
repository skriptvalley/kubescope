package resources

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/go-chi/chi/v5"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/skriptvalley/kubescope/internal/kube"
)

func decodeSourceList(t *testing.T, body []byte) kubeconfigSourceList {
	t.Helper()
	var list kubeconfigSourceList
	require.NoError(t, json.Unmarshal(body, &list))
	return list
}

// withURLParam injects a chi route param so the DELETE handler can read {id}
// without standing up a full router.
func withURLParam(r *http.Request, key, value string) *http.Request {
	rctx := chi.NewRouteContext()
	rctx.URLParams.Add(key, value)
	return r.WithContext(context.WithValue(r.Context(), chi.RouteCtxKey, rctx))
}

func TestListKubeconfigSourcesHandler(t *testing.T) {
	cluster := &fakeCluster{
		sources: []kube.SourceStatus{
			{ID: "abc123", Path: "/kubeconfigs", Kind: "dir", Origin: "env", Status: "ok",
				Files:    []kube.SourceFileStatus{{Path: "/kubeconfigs/a.yaml", Status: "ok", Contexts: []string{"kind-a"}}},
				Contexts: []string{"kind-a"}},
		},
	}
	rec := httptest.NewRecorder()
	ListKubeconfigSourcesHandler(cluster, true, discardLogger())(
		rec, httptest.NewRequest(http.MethodGet, "/api/v1/kubeconfigs", nil))

	require.Equal(t, http.StatusOK, rec.Code)
	list := decodeSourceList(t, rec.Body.Bytes())
	require.Len(t, list.Sources, 1)
	assert.Equal(t, "dir", list.Sources[0].Kind)
	assert.True(t, list.CanSetKubeconfig)
}

func TestListKubeconfigSourcesHandlerFoldsInCanSet(t *testing.T) {
	// canSetKubeconfig is folded in by the caller (read-only mode reports false).
	rec := httptest.NewRecorder()
	ListKubeconfigSourcesHandler(&fakeCluster{}, false, discardLogger())(
		rec, httptest.NewRequest(http.MethodGet, "/api/v1/kubeconfigs", nil))

	require.Equal(t, http.StatusOK, rec.Code)
	assert.False(t, decodeSourceList(t, rec.Body.Bytes()).CanSetKubeconfig)
}

func TestAddKubeconfigSourceHandler(t *testing.T) {
	doPost := func(t *testing.T, cluster Cluster, allow bool, body string) *httptest.ResponseRecorder {
		t.Helper()
		rec := httptest.NewRecorder()
		req := httptest.NewRequest(http.MethodPost, "/api/v1/kubeconfigs", strings.NewReader(body))
		AddKubeconfigSourceHandler(cluster, allow, discardLogger())(rec, req)
		return rec
	}

	t.Run("flag off is 403 kubeconfig_set_disabled", func(t *testing.T) {
		cluster := &fakeCluster{}
		rec := doPost(t, cluster, false, `{"path":"/new/kubeconfig"}`)

		assert.Equal(t, http.StatusForbidden, rec.Code)
		assert.Equal(t, "kubeconfig_set_disabled", errorCode(t, rec.Body.Bytes()))
		assert.Empty(t, cluster.addedPath, "the registry must not be touched when the flag is off")
	})

	t.Run("malformed body is 400 invalid_request", func(t *testing.T) {
		cluster := &fakeCluster{}
		rec := doPost(t, cluster, true, `not json`)

		assert.Equal(t, http.StatusBadRequest, rec.Code)
		assert.Equal(t, "invalid_request", errorCode(t, rec.Body.Bytes()))
		assert.Empty(t, cluster.addedPath)
	})

	t.Run("unknown fields are rejected", func(t *testing.T) {
		cluster := &fakeCluster{}
		rec := doPost(t, cluster, true, `{"path":"/new/kubeconfig","extra":true}`)

		assert.Equal(t, http.StatusBadRequest, rec.Code)
		assert.Equal(t, "invalid_request", errorCode(t, rec.Body.Bytes()))
	})

	t.Run("relative path is 400 invalid_request", func(t *testing.T) {
		cluster := &fakeCluster{}
		rec := doPost(t, cluster, true, `{"path":"relative/kubeconfig"}`)

		assert.Equal(t, http.StatusBadRequest, rec.Code)
		assert.Equal(t, "invalid_request", errorCode(t, rec.Body.Bytes()))
		assert.Empty(t, cluster.addedPath)
	})

	t.Run("duplicate path is 409 kubeconfig_source_exists", func(t *testing.T) {
		cluster := &fakeCluster{addErr: &kube.DuplicateSourceError{Path: "/dup"}}
		rec := doPost(t, cluster, true, `{"path":"/dup"}`)

		assert.Equal(t, http.StatusConflict, rec.Code)
		assert.Equal(t, "kubeconfig_source_exists", errorCode(t, rec.Body.Bytes()))
	})

	t.Run("invisible path is 422 with mounted-directory guidance", func(t *testing.T) {
		cluster := &fakeCluster{addErr: &kube.SourceInvisibleError{Path: "/not-mounted"}}
		rec := doPost(t, cluster, true, `{"path":"/not-mounted"}`)

		assert.Equal(t, http.StatusUnprocessableEntity, rec.Code)
		var env struct {
			Error struct {
				Code     string `json:"code"`
				Guidance string `json:"guidance"`
				DocURL   string `json:"docURL"`
			} `json:"error"`
		}
		require.NoError(t, json.Unmarshal(rec.Body.Bytes(), &env))
		assert.Equal(t, "kubeconfig_invalid", env.Error.Code)
		assert.Contains(t, env.Error.Guidance, "mount a directory", "the mounted-directory workflow is named")
		assert.NotEmpty(t, env.Error.DocURL, "the ADR-0004 doc link is attached")
	})

	t.Run("invalid candidate is 422 kubeconfig_invalid", func(t *testing.T) {
		cluster := &fakeCluster{addErr: errors.New(`kubeconfig "/x" defines no contexts`)}
		rec := doPost(t, cluster, true, `{"path":"/x"}`)

		assert.Equal(t, http.StatusUnprocessableEntity, rec.Code)
		assert.Equal(t, "kubeconfig_invalid", errorCode(t, rec.Body.Bytes()))
	})

	t.Run("success returns 200 with the fresh listing", func(t *testing.T) {
		cluster := &fakeCluster{sourcePaths: []string{"/kubeconfig"}}
		rec := doPost(t, cluster, true, `{"path":"/new/kubeconfig"}`)

		require.Equal(t, http.StatusOK, rec.Code, "body: %s", rec.Body.String())
		assert.Equal(t, "/new/kubeconfig", cluster.addedPath, "the add was performed")
		list := decodeSourceList(t, rec.Body.Bytes())
		assert.True(t, list.CanSetKubeconfig)
	})

	t.Run("oversized body is rejected", func(t *testing.T) {
		cluster := &fakeCluster{}
		huge := `{"path":"/` + strings.Repeat("A", (64<<10)+1) + `"}`
		rec := doPost(t, cluster, true, huge)

		assert.Equal(t, http.StatusBadRequest, rec.Code)
		assert.Equal(t, "invalid_request", errorCode(t, rec.Body.Bytes()))
		assert.Empty(t, cluster.addedPath)
	})
}

func TestRemoveKubeconfigSourceHandler(t *testing.T) {
	doDelete := func(t *testing.T, cluster Cluster, allow bool, id string) *httptest.ResponseRecorder {
		t.Helper()
		rec := httptest.NewRecorder()
		req := withURLParam(httptest.NewRequest(http.MethodDelete, "/api/v1/kubeconfigs/"+id, nil), "id", id)
		RemoveKubeconfigSourceHandler(cluster, allow, discardLogger())(rec, req)
		return rec
	}

	t.Run("flag off is 403 kubeconfig_set_disabled", func(t *testing.T) {
		cluster := &fakeCluster{}
		rec := doDelete(t, cluster, false, "abc123")

		assert.Equal(t, http.StatusForbidden, rec.Code)
		assert.Equal(t, "kubeconfig_set_disabled", errorCode(t, rec.Body.Bytes()))
		assert.Empty(t, cluster.removedID, "the registry must not be touched when the flag is off")
	})

	t.Run("unknown id is 404 kubeconfig_source_not_found", func(t *testing.T) {
		cluster := &fakeCluster{removeErr: &kube.UnknownSourceError{ID: "ghost"}}
		rec := doDelete(t, cluster, true, "ghost")

		assert.Equal(t, http.StatusNotFound, rec.Code)
		assert.Equal(t, "kubeconfig_source_not_found", errorCode(t, rec.Body.Bytes()))
		assert.Equal(t, "ghost", cluster.removedID, "the removal was attempted")
	})

	t.Run("success returns 200 with the fresh listing", func(t *testing.T) {
		cluster := &fakeCluster{sourcePaths: []string{"/kubeconfig"}}
		rec := doDelete(t, cluster, true, "abc123")

		require.Equal(t, http.StatusOK, rec.Code, "body: %s", rec.Body.String())
		assert.Equal(t, "abc123", cluster.removedID)
		list := decodeSourceList(t, rec.Body.Bytes())
		assert.True(t, list.CanSetKubeconfig)
	})
}
