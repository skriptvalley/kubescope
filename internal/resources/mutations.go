package resources

import (
	"encoding/json"
	"fmt"
	"log/slog"
	"net/http"
	"time"

	"github.com/go-chi/chi/v5"
	autoscalingv1 "k8s.io/api/autoscaling/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"k8s.io/apimachinery/pkg/runtime/schema"
	"k8s.io/apimachinery/pkg/types"
	"k8s.io/client-go/dynamic"
	"sigs.k8s.io/yaml"
)

// Write operations (Sprint 5). Every mutating route is registered behind the
// read-only middleware (ADR-0005); these handlers assume they only run when
// mutations are permitted. Generic apply/delete go through the dynamic client so
// any GVK — including CRDs — is editable and deletable with no kind-specific
// code (ADR-0003); scale and rollout-restart use the typed client where the
// subresource / pod-template semantics earn it.

// maxBodyBytes caps a mutation request body. Manifests are small; this guards
// against a client streaming an unbounded body into memory.
const maxBodyBytes = 4 << 20 // 4 MiB

// restartedAtAnnotation is the pod-template annotation a rollout-restart stamps
// to force a new rollout — the same key kubectl uses, so a restart triggered
// here is indistinguishable from `kubectl rollout restart`.
const restartedAtAnnotation = "kubectl.kubernetes.io/restartedAt"

type applyRequest struct {
	YAML string `json:"yaml"`
}

type scaleRequest struct {
	Replicas *int32 `json:"replicas"`
}

type deletedResponse struct {
	Status string `json:"status"`
}

// UpdateHandler serves PUT /api/v1/resources/{group}/{version}/{resource}/{name}:
// applies an edited manifest via the dynamic client for any GVR (ADR-0003). The
// body is {"yaml": "..."} so validation happens server-side. A stale
// resourceVersion surfaces as a 409 (never a silent overwrite); invalid YAML is
// a 400 and server-side validation a 422.
func UpdateHandler(cluster Cluster, disco *DiscoveryService, logger *slog.Logger) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		gvr := gvrFromRequest(r)
		name := chi.URLParam(r, "name")

		info, ok, err := disco.Resolve(gvr)
		if err != nil {
			writeEngineError(w, logger, "resolving resource", err, classifierFor(cluster))
			return
		}
		if !ok {
			writeUnknownResource(w, logger, gvr)
			return
		}

		namespace := r.URL.Query().Get("namespace")
		switch {
		case !info.Namespaced && namespace != "":
			writeInvalidScope(w, logger, "resource %s is cluster-scoped and does not accept a namespace", gvr.Resource)
			return
		case info.Namespaced && namespace == "":
			writeInvalidScope(w, logger, "resource %s is namespaced; a namespace is required", gvr.Resource)
			return
		}

		var req applyRequest
		dec := json.NewDecoder(http.MaxBytesReader(w, r.Body, maxBodyBytes))
		if err := dec.Decode(&req); err != nil {
			writeError(w, logger, http.StatusBadRequest, "invalid_request", `request body must be JSON with a "yaml" field`)
			return
		}

		obj, perr := parseManifest(req.YAML)
		if perr != nil {
			// The parse error may echo YAML content; it never contains a value the
			// object didn't already carry, but we do not log it — apply bodies can
			// hold Secret data (ADR-0005). Surface it to the client only.
			writeError(w, logger, http.StatusBadRequest, "invalid_yaml", fmt.Sprintf("cannot parse manifest: %v", perr))
			return
		}

		// The path identifies the object; the manifest must not rename or move it.
		if got := obj.GetName(); got != name {
			writeError(w, logger, http.StatusBadRequest, "name_mismatch",
				fmt.Sprintf("manifest name %q does not match the object being edited (%q)", got, name))
			return
		}
		if info.Namespaced {
			if ns := obj.GetNamespace(); ns == "" {
				obj.SetNamespace(namespace)
			} else if ns != namespace {
				writeError(w, logger, http.StatusBadRequest, "namespace_mismatch",
					fmt.Sprintf("manifest namespace %q does not match the object being edited (%q)", ns, namespace))
				return
			}
		}

		dyn, err := cluster.Dynamic()
		if err != nil {
			writeError(w, logger, http.StatusServiceUnavailable, "kubeconfig_unavailable",
				fmt.Sprintf("cannot load kubeconfig: %v", err))
			return
		}

		ri := namespacedResource(dyn, gvr, info.Namespaced, namespace)
		updated, err := ri.Update(r.Context(), obj, metav1.UpdateOptions{})
		if err != nil {
			writeMutationError(w, logger, fmt.Sprintf("updating %s %q", gvr.Resource, name), err, classifierFor(cluster))
			return
		}
		// A Secret's data is re-masked on the way back so an apply response never
		// leaks values the read path would have hidden (ADR-0005).
		maskIfSecret(gvr, updated)
		writeJSON(w, logger, http.StatusOK, objectResponse{Object: updated.Object})
	}
}

// DeleteHandler serves DELETE
// /api/v1/resources/{group}/{version}/{resource}/{name}: generic delete of any
// GVR, namespaced or cluster-scoped, via the dynamic client (ADR-0003).
func DeleteHandler(cluster Cluster, disco *DiscoveryService, logger *slog.Logger) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		gvr := gvrFromRequest(r)
		name := chi.URLParam(r, "name")

		info, ok, err := disco.Resolve(gvr)
		if err != nil {
			writeEngineError(w, logger, "resolving resource", err, classifierFor(cluster))
			return
		}
		if !ok {
			writeUnknownResource(w, logger, gvr)
			return
		}

		namespace := r.URL.Query().Get("namespace")
		switch {
		case !info.Namespaced && namespace != "":
			writeInvalidScope(w, logger, "resource %s is cluster-scoped and does not accept a namespace", gvr.Resource)
			return
		case info.Namespaced && namespace == "":
			writeInvalidScope(w, logger, "resource %s is namespaced; a namespace is required", gvr.Resource)
			return
		}

		dyn, err := cluster.Dynamic()
		if err != nil {
			writeError(w, logger, http.StatusServiceUnavailable, "kubeconfig_unavailable",
				fmt.Sprintf("cannot load kubeconfig: %v", err))
			return
		}

		ri := namespacedResource(dyn, gvr, info.Namespaced, namespace)
		if err := ri.Delete(r.Context(), name, metav1.DeleteOptions{}); err != nil {
			writeMutationError(w, logger, fmt.Sprintf("deleting %s %q", gvr.Resource, name), err, classifierFor(cluster))
			return
		}
		writeJSON(w, logger, http.StatusOK, deletedResponse{Status: "deleted"})
	}
}

// ScaleHandler serves POST /api/v1/workloads/{resource}/{namespace}/{name}/scale
// for Deployments, StatefulSets and ReplicaSets via the scale subresource, so
// the replica count is set atomically without a full-object read-modify-write.
func ScaleHandler(cluster Cluster, logger *slog.Logger) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		resource := chi.URLParam(r, "resource")
		namespace := chi.URLParam(r, "namespace")
		name := chi.URLParam(r, "name")

		var req scaleRequest
		dec := json.NewDecoder(http.MaxBytesReader(w, r.Body, maxBodyBytes))
		if err := dec.Decode(&req); err != nil {
			writeError(w, logger, http.StatusBadRequest, "invalid_request", `request body must be JSON with a "replicas" field`)
			return
		}
		if req.Replicas == nil || *req.Replicas < 0 {
			writeError(w, logger, http.StatusBadRequest, "invalid_request", "replicas must be a non-negative integer")
			return
		}

		clientset, err := cluster.Clientset()
		if err != nil {
			writeKubeconfigUnavailable(w, logger, err)
			return
		}

		ctx := r.Context()
		action := fmt.Sprintf("scaling %s %q", resource, name)
		scale := &autoscalingv1.Scale{
			ObjectMeta: metav1.ObjectMeta{Name: name, Namespace: namespace},
			Spec:       autoscalingv1.ScaleSpec{Replicas: *req.Replicas},
		}
		switch resource {
		case resourceDeployments:
			_, err = clientset.AppsV1().Deployments(namespace).UpdateScale(ctx, name, scale, metav1.UpdateOptions{})
		case resourceStatefulSets:
			_, err = clientset.AppsV1().StatefulSets(namespace).UpdateScale(ctx, name, scale, metav1.UpdateOptions{})
		case resourceReplicaSets:
			_, err = clientset.AppsV1().ReplicaSets(namespace).UpdateScale(ctx, name, scale, metav1.UpdateOptions{})
		default:
			writeError(w, logger, http.StatusBadRequest, "unscalable",
				fmt.Sprintf("%q cannot be scaled; only deployments, statefulsets and replicasets support scaling", resource))
			return
		}
		if err != nil {
			writeMutationError(w, logger, action, err, classifierFor(cluster))
			return
		}
		writeJSON(w, logger, http.StatusOK, map[string]any{"resource": resource, "name": name, "replicas": *req.Replicas})
	}
}

// RestartHandler serves POST
// /api/v1/workloads/{resource}/{namespace}/{name}/restart for Deployments,
// StatefulSets and DaemonSets: stamps the pod-template restart annotation so the
// controller rolls out fresh pods (visible in the Sprint 3 rollout status).
func RestartHandler(cluster Cluster, logger *slog.Logger) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		resource := chi.URLParam(r, "resource")
		namespace := chi.URLParam(r, "namespace")
		name := chi.URLParam(r, "name")

		if !restartable(resource) {
			writeError(w, logger, http.StatusBadRequest, "unrestartable",
				fmt.Sprintf("%q cannot be rollout-restarted; only deployments, statefulsets and daemonsets have a pod template", resource))
			return
		}

		clientset, err := cluster.Clientset()
		if err != nil {
			writeKubeconfigUnavailable(w, logger, err)
			return
		}

		ctx := r.Context()
		patch := restartPatch(time.Now())
		action := fmt.Sprintf("restarting %s %q", resource, name)
		switch resource {
		case resourceDeployments:
			_, err = clientset.AppsV1().Deployments(namespace).Patch(ctx, name, types.StrategicMergePatchType, patch, metav1.PatchOptions{})
		case resourceStatefulSets:
			_, err = clientset.AppsV1().StatefulSets(namespace).Patch(ctx, name, types.StrategicMergePatchType, patch, metav1.PatchOptions{})
		case resourceDaemonSets:
			_, err = clientset.AppsV1().DaemonSets(namespace).Patch(ctx, name, types.StrategicMergePatchType, patch, metav1.PatchOptions{})
		}
		if err != nil {
			writeMutationError(w, logger, action, err, classifierFor(cluster))
			return
		}
		writeJSON(w, logger, http.StatusOK, map[string]any{"resource": resource, "name": name, "restarted": true})
	}
}

// --- Pure helpers (table-tested) ---------------------------------------------

// parseManifest converts an edited YAML manifest to an unstructured object. An
// empty document or a non-mapping top level (e.g. a bare scalar or list) is an
// error, so a malformed edit is rejected before it reaches the apiserver.
func parseManifest(text string) (*unstructured.Unstructured, error) {
	var raw map[string]any
	if err := yaml.Unmarshal([]byte(text), &raw); err != nil {
		return nil, err
	}
	if len(raw) == 0 {
		return nil, fmt.Errorf("manifest is empty")
	}
	return &unstructured.Unstructured{Object: raw}, nil
}

// restartPatch builds the strategic-merge patch that stamps the restart
// annotation onto the pod template. Factored out so its shape is table-tested
// without a cluster.
func restartPatch(now time.Time) []byte {
	return []byte(fmt.Sprintf(
		`{"spec":{"template":{"metadata":{"annotations":{%q:%q}}}}}`,
		restartedAtAnnotation, now.UTC().Format(time.RFC3339)))
}

// restartable reports whether a workload kind has a pod template that a
// rollout-restart can stamp.
func restartable(resource string) bool {
	switch resource {
	case resourceDeployments, resourceStatefulSets, resourceDaemonSets:
		return true
	default:
		return false
	}
}

// namespacedResource returns the dynamic resource interface scoped to a
// namespace for namespaced kinds, or the cluster-scoped interface otherwise —
// the one place that branch is made for every generic write.
func namespacedResource(dyn dynamic.Interface, gvr schema.GroupVersionResource, namespaced bool, namespace string) dynamic.ResourceInterface {
	if namespaced {
		return dyn.Resource(gvr).Namespace(namespace)
	}
	return dyn.Resource(gvr)
}
