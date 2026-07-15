package resources

import (
	"fmt"
	"log/slog"
	"net/http"
	"time"

	"github.com/go-chi/chi/v5"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"k8s.io/apimachinery/pkg/runtime/schema"
	"sigs.k8s.io/yaml"
)

// listColumn describes one server-shaped list column. The generic engine emits
// a sane default set (name, namespace, age); typed handlers may add more
// (Sprint 3) — the thin frontend renders whatever columns the API describes
// (ADR-0003).
type listColumn struct {
	ID     string `json:"id"`
	Header string `json:"header"`
}

// listRow is one shaped row. creationTimestamp backs the age column so the
// frontend can both format a relative age and sort by it client-side.
type listRow struct {
	Name              string `json:"name"`
	Namespace         string `json:"namespace,omitempty"`
	CreationTimestamp string `json:"creationTimestamp,omitempty"`
	UID               string `json:"uid,omitempty"`
}

type listResponse struct {
	Group      string       `json:"group"`
	Version    string       `json:"version"`
	Resource   string       `json:"resource"`
	Kind       string       `json:"kind"`
	Namespaced bool         `json:"namespaced"`
	Columns    []listColumn `json:"columns"`
	Rows       []listRow    `json:"rows"`
}

type objectResponse struct {
	Object map[string]any `json:"object"`
}

type yamlResponse struct {
	YAML string `json:"yaml"`
}

// ListHandler serves GET /api/v1/resources/{group}/{version}/{resource} for any
// GVR via the dynamic client (ADR-0003). `?namespace=` selects a single
// namespace; omitting it lists across all namespaces for namespaced resources.
// A namespace on a cluster-scoped resource is a 400; an unknown GVR is a 404.
func ListHandler(cluster Cluster, disco *DiscoveryService, logger *slog.Logger) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		gvr := gvrFromRequest(r)
		info, ok, err := disco.Resolve(gvr)
		if err != nil {
			writeEngineError(w, logger, "resolving resource", err, execGuidanceFor(cluster))
			return
		}
		if !ok {
			writeUnknownResource(w, logger, gvr)
			return
		}

		namespace := r.URL.Query().Get("namespace")
		if !info.Namespaced && namespace != "" {
			writeInvalidScope(w, logger, "resource %s is cluster-scoped and does not accept a namespace", gvr.Resource)
			return
		}

		dyn, err := cluster.Dynamic()
		if err != nil {
			writeError(w, logger, http.StatusServiceUnavailable, "kubeconfig_unavailable",
				fmt.Sprintf("cannot load kubeconfig: %v", err))
			return
		}

		ri := dyn.Resource(gvr)
		var list *unstructured.UnstructuredList
		if info.Namespaced && namespace != "" {
			list, err = ri.Namespace(namespace).List(r.Context(), metav1.ListOptions{})
		} else {
			list, err = ri.List(r.Context(), metav1.ListOptions{})
		}
		if err != nil {
			writeEngineError(w, logger, fmt.Sprintf("listing %s", gvr.Resource), err, execGuidanceFor(cluster))
			return
		}

		writeJSON(w, logger, http.StatusOK, shapeList(info, list))
	}
}

// GetHandler serves GET /api/v1/resources/{group}/{version}/{resource}/{name}:
// the full object, suitable for metadata rendering and YAML. Namespaced
// resources require `?namespace=`; cluster-scoped ones reject it.
func GetHandler(cluster Cluster, disco *DiscoveryService, logger *slog.Logger) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		obj, done := fetchObject(w, r, cluster, disco, logger)
		if done {
			return
		}
		// Secret data is masked by default before it leaves the server (ADR-0005);
		// per-key plaintext is a separate, explicit reveal.
		maskIfSecret(gvrFromRequest(r), obj)
		writeJSON(w, logger, http.StatusOK, objectResponse{Object: obj.Object})
	}
}

// YAMLHandler serves GET
// /api/v1/resources/{group}/{version}/{resource}/{name}/yaml: the object
// rendered as canonical YAML for the read-only YAML tab.
func YAMLHandler(cluster Cluster, disco *DiscoveryService, logger *slog.Logger) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		obj, done := fetchObject(w, r, cluster, disco, logger)
		if done {
			return
		}
		// Mask Secret data before marshaling: the raw-YAML view of a Secret masks
		// its values too (ADR-0005).
		maskIfSecret(gvrFromRequest(r), obj)
		out, err := yaml.Marshal(obj.Object)
		if err != nil {
			logger.Error("marshaling yaml", "error", err)
			writeError(w, logger, http.StatusInternalServerError, "internal_error", "cannot render object as YAML")
			return
		}
		writeJSON(w, logger, http.StatusOK, yamlResponse{YAML: string(out)})
	}
}

// fetchObject resolves the GVR, validates scope, and fetches a single object.
// It returns done=true (having already written the response) on any error.
func fetchObject(w http.ResponseWriter, r *http.Request, cluster Cluster, disco *DiscoveryService, logger *slog.Logger) (*unstructured.Unstructured, bool) {
	gvr := gvrFromRequest(r)
	name := chi.URLParam(r, "name")

	info, ok, err := disco.Resolve(gvr)
	if err != nil {
		writeEngineError(w, logger, "resolving resource", err, execGuidanceFor(cluster))
		return nil, true
	}
	if !ok {
		writeUnknownResource(w, logger, gvr)
		return nil, true
	}

	namespace := r.URL.Query().Get("namespace")
	switch {
	case !info.Namespaced && namespace != "":
		writeInvalidScope(w, logger, "resource %s is cluster-scoped and does not accept a namespace", gvr.Resource)
		return nil, true
	case info.Namespaced && namespace == "":
		writeInvalidScope(w, logger, "resource %s is namespaced; a namespace is required", gvr.Resource)
		return nil, true
	}

	dyn, err := cluster.Dynamic()
	if err != nil {
		writeError(w, logger, http.StatusServiceUnavailable, "kubeconfig_unavailable",
			fmt.Sprintf("cannot load kubeconfig: %v", err))
		return nil, true
	}

	ri := dyn.Resource(gvr)
	var obj *unstructured.Unstructured
	if info.Namespaced {
		obj, err = ri.Namespace(namespace).Get(r.Context(), name, metav1.GetOptions{})
	} else {
		obj, err = ri.Get(r.Context(), name, metav1.GetOptions{})
	}
	if err != nil {
		writeEngineError(w, logger, fmt.Sprintf("getting %s %q", gvr.Resource, name), err, execGuidanceFor(cluster))
		return nil, true
	}
	return obj, false
}

// gvrFromRequest builds the GVR from the route params, mapping the core-group
// token back to the empty group.
func gvrFromRequest(r *http.Request) schema.GroupVersionResource {
	return parseGVR(chi.URLParam(r, "group"), chi.URLParam(r, "version"), chi.URLParam(r, "resource"))
}

func parseGVR(group, version, resource string) schema.GroupVersionResource {
	if group == coreGroupToken {
		group = ""
	}
	return schema.GroupVersionResource{Group: group, Version: version, Resource: resource}
}

// shapeList builds the server-side column config and rows. Namespaced kinds get
// a namespace column; cluster-scoped kinds omit it.
func shapeList(info APIResourceInfo, list *unstructured.UnstructuredList) listResponse {
	columns := []listColumn{{ID: "name", Header: "Name"}}
	if info.Namespaced {
		columns = append(columns, listColumn{ID: "namespace", Header: "Namespace"})
	}
	columns = append(columns, listColumn{ID: "age", Header: "Age"})

	rows := make([]listRow, 0, len(list.Items))
	for i := range list.Items {
		item := &list.Items[i]
		row := listRow{Name: item.GetName(), UID: string(item.GetUID())}
		if info.Namespaced {
			row.Namespace = item.GetNamespace()
		}
		if ts := item.GetCreationTimestamp(); !ts.IsZero() {
			row.CreationTimestamp = ts.UTC().Format(time.RFC3339)
		}
		rows = append(rows, row)
	}
	return listResponse{
		Group:      info.Group,
		Version:    info.Version,
		Resource:   info.Resource,
		Kind:       info.Kind,
		Namespaced: info.Namespaced,
		Columns:    columns,
		Rows:       rows,
	}
}

func writeUnknownResource(w http.ResponseWriter, logger *slog.Logger, gvr schema.GroupVersionResource) {
	writeError(w, logger, http.StatusNotFound, "unknown_resource",
		fmt.Sprintf("the active cluster serves no resource %s", gvr.String()))
}

func writeInvalidScope(w http.ResponseWriter, logger *slog.Logger, format string, args ...any) {
	writeError(w, logger, http.StatusBadRequest, "invalid_scope", fmt.Sprintf(format, args...))
}
