package resources

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"k8s.io/apimachinery/pkg/runtime/schema"
	"k8s.io/client-go/kubernetes/fake"
)

func TestIsSecret(t *testing.T) {
	cases := map[string]struct {
		gvr  schema.GroupVersionResource
		want bool
	}{
		"core secrets":     {schema.GroupVersionResource{Group: "", Version: "v1", Resource: "secrets"}, true},
		"apps deployments": {schema.GroupVersionResource{Group: "apps", Version: "v1", Resource: "deployments"}, false},
		"named-group fake": {schema.GroupVersionResource{Group: "x", Version: "v1", Resource: "secrets"}, false},
		"core configmaps":  {schema.GroupVersionResource{Group: "", Version: "v1", Resource: "configmaps"}, false},
	}
	for name, tc := range cases {
		t.Run(name, func(t *testing.T) { assert.Equal(t, tc.want, isSecret(tc.gvr)) })
	}
}

func TestMaskSecretData(t *testing.T) {
	obj := map[string]any{
		"kind": "Secret",
		"data": map[string]any{
			"username": "YWRtaW4=",
			"password": "aHVudGVyMg==",
		},
		"stringData": map[string]any{"token": "plaintext"},
	}
	maskSecretData(obj)

	data := obj["data"].(map[string]any)
	assert.Equal(t, secretRedaction, data["username"])
	assert.Equal(t, secretRedaction, data["password"])
	assert.Contains(t, data, "username", "keys are preserved, only values masked")
	assert.Equal(t, secretRedaction, obj["stringData"].(map[string]any)["token"])
}

func TestMaskSecretDataNonSecretShape(t *testing.T) {
	obj := map[string]any{"kind": "ConfigMap", "data": map[string]any{"key": "value"}}
	// A ConfigMap is never routed through masking, but the pure function must not
	// panic on the shape and (were it called) would redact a data map — masking is
	// gated by isSecret at the call site, verified separately.
	maskSecretData(obj)
	assert.Equal(t, secretRedaction, obj["data"].(map[string]any)["key"])
}

func TestMaskStreamObject(t *testing.T) {
	t.Run("masks a secret in a copy, leaving the original intact", func(t *testing.T) {
		u := &unstructured.Unstructured{Object: map[string]any{
			"kind": "Secret",
			"data": map[string]any{"password": "aHVudGVyMg=="},
		}}
		out := MaskStreamObject(secretsGVR, u)

		require.NotSame(t, u, out, "a secret must be masked in a fresh copy")
		assert.Equal(t, secretRedaction, out.Object["data"].(map[string]any)["password"])
		// The original (which the informer shares across subscribers) is untouched.
		assert.Equal(t, "aHVudGVyMg==", u.Object["data"].(map[string]any)["password"],
			"the shared informer object must not be mutated")
	})

	t.Run("passes non-secrets through without copying", func(t *testing.T) {
		u := &unstructured.Unstructured{Object: map[string]any{"kind": "ConfigMap"}}
		gvr := schema.GroupVersionResource{Group: "", Version: "v1", Resource: "configmaps"}
		assert.Same(t, u, MaskStreamObject(gvr, u), "a non-secret is returned as-is, no copy")
	})
}

func secretFixture() *corev1.Secret {
	return &corev1.Secret{
		ObjectMeta: metav1.ObjectMeta{Name: "db", Namespace: "default"},
		Data:       map[string][]byte{"password": []byte("hunter2")},
	}
}

func TestRevealSecretHandler(t *testing.T) {
	cluster := &fakeCluster{clientset: fake.NewClientset(secretFixture())}
	h := RevealSecretHandler(cluster, discardLogger())

	t.Run("reveals the requested key", func(t *testing.T) {
		rec := httptest.NewRecorder()
		h(rec, chiRequest(http.MethodGet, "/?key=password", "",
			map[string]string{"namespace": "default", "name": "db"}))
		require.Equal(t, http.StatusOK, rec.Code)
		var body struct{ Key, Value string }
		require.NoError(t, json.Unmarshal(rec.Body.Bytes(), &body))
		assert.Equal(t, "password", body.Key)
		assert.Equal(t, "hunter2", body.Value)
	})

	t.Run("missing key param is 400", func(t *testing.T) {
		rec := httptest.NewRecorder()
		h(rec, chiRequest(http.MethodGet, "/", "", map[string]string{"namespace": "default", "name": "db"}))
		assert.Equal(t, http.StatusBadRequest, rec.Code)
	})

	t.Run("unknown key is 404", func(t *testing.T) {
		rec := httptest.NewRecorder()
		h(rec, chiRequest(http.MethodGet, "/?key=nope", "",
			map[string]string{"namespace": "default", "name": "db"}))
		assert.Equal(t, http.StatusNotFound, rec.Code)
	})

	t.Run("missing secret is 404", func(t *testing.T) {
		rec := httptest.NewRecorder()
		h(rec, chiRequest(http.MethodGet, "/?key=password", "",
			map[string]string{"namespace": "default", "name": "ghost"}))
		assert.Equal(t, http.StatusNotFound, rec.Code)
	})
}
