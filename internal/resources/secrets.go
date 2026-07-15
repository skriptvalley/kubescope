package resources

import (
	"fmt"
	"log/slog"
	"net/http"

	"github.com/go-chi/chi/v5"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"k8s.io/apimachinery/pkg/runtime/schema"
)

// Secret masking (Sprint 5, ADR-0005). Secret data values are masked by default
// everywhere the generic engine renders an object — detail and YAML — so a value
// never travels to the browser (or into a screen-share) unless the user asks for
// it. Per-key reveal is a separate, deliberate read. Secret values are never
// logged, here or on any read/write path.

// secretRedaction replaces every Secret data value on the default read path. It
// is a fixed marker, not the real length or content, so nothing about the value
// leaks — the key names are structural (and still shown) but the bytes are not.
const secretRedaction = "**redacted**"

// secretsGVR is the core Secret resource. Masking keys off the GVR, not the
// object's self-reported kind, so a mislabelled object cannot dodge masking.
var secretsGVR = schema.GroupVersionResource{Group: "", Version: "v1", Resource: "secrets"}

// isSecret reports whether a GVR is the core Secret resource.
func isSecret(gvr schema.GroupVersionResource) bool {
	return gvr.Group == secretsGVR.Group && gvr.Resource == secretsGVR.Resource
}

// maskSecretData redacts every value under `data` and `stringData` in place,
// preserving the keys. It is pure over the object map so it can be table-tested,
// and is a no-op for a shape without those fields.
func maskSecretData(obj map[string]any) {
	for _, field := range []string{"data", "stringData"} {
		if m, ok := obj[field].(map[string]any); ok {
			for k := range m {
				m[k] = secretRedaction
			}
		}
	}
}

// maskIfSecret masks a fetched object's Secret values when the GVR is a Secret.
// Callers run it on every generic get/yaml/apply response before writing.
func maskIfSecret(gvr schema.GroupVersionResource, obj *unstructured.Unstructured) {
	if obj == nil || !isSecret(gvr) {
		return
	}
	maskSecretData(obj.Object)
}

type revealResponse struct {
	Key   string `json:"key"`
	Value string `json:"value"`
}

// RevealSecretHandler serves GET
// /api/v1/secrets/{namespace}/{name}/reveal?key=K: the plaintext of a single
// Secret key, fetched fresh and returned only for the requested key. This is the
// one path that returns a real Secret value, and only one key at a time, on an
// explicit user action. The value is never logged.
func RevealSecretHandler(cluster Cluster, logger *slog.Logger) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		namespace := chi.URLParam(r, "namespace")
		name := chi.URLParam(r, "name")
		key := r.URL.Query().Get("key")
		if key == "" {
			writeError(w, logger, http.StatusBadRequest, "invalid_request", "a key query parameter is required")
			return
		}

		clientset, err := cluster.Clientset()
		if err != nil {
			writeKubeconfigUnavailable(w, logger, err)
			return
		}

		secret, err := clientset.CoreV1().Secrets(namespace).Get(r.Context(), name, metav1.GetOptions{})
		if err != nil {
			// The action string names the key, never a value.
			writeEngineError(w, logger, fmt.Sprintf("revealing secret %q key %q", name, key), err, execGuidanceFor(cluster))
			return
		}

		// The typed client decodes Secret.Data to bytes already; return it as a
		// string (binary values round-trip verbatim). stringData is write-only on
		// the wire but honored here for completeness.
		if raw, ok := secret.Data[key]; ok {
			writeJSON(w, logger, http.StatusOK, revealResponse{Key: key, Value: string(raw)})
			return
		}
		if raw, ok := secret.StringData[key]; ok {
			writeJSON(w, logger, http.StatusOK, revealResponse{Key: key, Value: raw})
			return
		}
		writeError(w, logger, http.StatusNotFound, "not_found", fmt.Sprintf("secret %q has no key %q", name, key))
	}
}
