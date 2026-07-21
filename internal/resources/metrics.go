package resources

import (
	"fmt"
	"log/slog"
	"net/http"

	apierrors "k8s.io/apimachinery/pkg/api/errors"
	"k8s.io/apimachinery/pkg/api/resource"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"k8s.io/apimachinery/pkg/runtime/schema"
)

// Pod CPU/Memory usage from metrics-server (ADR-0009). Read via the dynamic
// client against metrics.k8s.io/v1beta1 — no new Go dependency, consistent with
// the generic engine (ADR-0003). Metrics are advisory: when metrics-server is
// absent the metrics API is unregistered and the list errors, so we degrade to
// Available:false (the UI renders "—") instead of surfacing an error.

var podMetricsGVR = schema.GroupVersionResource{
	Group:    "metrics.k8s.io",
	Version:  "v1beta1",
	Resource: "pods",
}

// PodMetrics is one pod's summed container usage.
type PodMetrics struct {
	Name      string `json:"name"`
	Namespace string `json:"namespace"`
	CPU       string `json:"cpu"`    // millicores, e.g. "71m"
	Memory    string `json:"memory"` // binary, e.g. "306Mi"
}

// PodMetricsResponse carries the per-pod usage plus whether metrics-server was
// reachable at all (false ⇒ the UI shows "—", not an error).
type PodMetricsResponse struct {
	Available bool         `json:"available"`
	Items     []PodMetrics `json:"items"`
}

// PodMetricsHandler serves GET /api/v1/metrics/pods (optional ?namespace=).
func PodMetricsHandler(cluster Cluster, logger *slog.Logger) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		dyn, err := cluster.Dynamic()
		if err != nil {
			writeKubeconfigUnavailable(w, logger, err)
			return
		}
		namespace := r.URL.Query().Get("namespace")
		ri := dyn.Resource(podMetricsGVR)
		var (
			list  *unstructured.UnstructuredList
			lerr  error
			lopts = metav1.ListOptions{}
		)
		if namespace != "" {
			list, lerr = ri.Namespace(namespace).List(r.Context(), lopts)
		} else {
			list, lerr = ri.List(r.Context(), lopts)
		}
		if lerr != nil {
			// Metrics are advisory: whatever the failure, report unavailable (the UI
			// renders "—") rather than breaking the view. But distinguish the
			// expected "metrics-server not installed" (404 NotFound — quiet) from a
			// real fault (RBAC forbidden, transient apiserver error) worth surfacing
			// in the logs, so a genuine metrics problem isn't silently masked.
			if apierrors.IsNotFound(lerr) {
				logger.Debug("pod metrics unavailable (metrics API not installed)", "error", lerr)
			} else {
				logger.Warn("pod metrics list failed", "error", lerr)
			}
			writeJSON(w, logger, http.StatusOK, PodMetricsResponse{Available: false, Items: []PodMetrics{}})
			return
		}
		items := make([]PodMetrics, 0, len(list.Items))
		for i := range list.Items {
			items = append(items, shapePodMetrics(&list.Items[i]))
		}
		writeJSON(w, logger, http.StatusOK, PodMetricsResponse{Available: true, Items: items})
	}
}

// shapePodMetrics sums a PodMetrics object's per-container CPU and memory usage.
// Unparseable/missing quantities contribute zero rather than failing the row.
func shapePodMetrics(u *unstructured.Unstructured) PodMetrics {
	cpu := resource.NewQuantity(0, resource.DecimalSI)
	mem := resource.NewQuantity(0, resource.BinarySI)
	containers, _, _ := unstructured.NestedSlice(u.Object, "containers")
	for _, c := range containers {
		cm, ok := c.(map[string]interface{})
		if !ok {
			continue
		}
		usage, _, _ := unstructured.NestedMap(cm, "usage")
		if s, ok := usage["cpu"].(string); ok {
			if q, err := resource.ParseQuantity(s); err == nil {
				cpu.Add(q)
			}
		}
		if s, ok := usage["memory"].(string); ok {
			if q, err := resource.ParseQuantity(s); err == nil {
				mem.Add(q)
			}
		}
	}
	return PodMetrics{
		Name:      u.GetName(),
		Namespace: u.GetNamespace(),
		CPU:       fmt.Sprintf("%dm", cpu.MilliValue()),
		Memory:    formatMebibytes(mem),
	}
}

// formatMebibytes renders a memory quantity in whole Mi (kubectl-top style), or
// Gi with one decimal once it reaches 1024 Mi.
func formatMebibytes(q *resource.Quantity) string {
	const mib = 1024 * 1024
	bytes := q.Value()
	mi := bytes / mib
	if mi >= 1024 {
		return fmt.Sprintf("%.1fGi", float64(mi)/1024)
	}
	return fmt.Sprintf("%dMi", mi)
}
