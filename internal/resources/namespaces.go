package resources

import (
	"fmt"
	"log/slog"
	"net/http"
	"sort"

	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
)

type namespaceList struct {
	Items []string `json:"items"`
}

// NamespacesHandler serves GET /api/v1/namespaces: the sorted namespace names
// backing the UI's namespace selector. A missing kubeconfig is a structured
// 503, an unreachable cluster a structured 502.
func NamespacesHandler(cluster Cluster, logger *slog.Logger) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		clientset, err := cluster.Clientset()
		if err != nil {
			logger.Error("kubeconfig unavailable", "error", err)
			writeError(w, logger, http.StatusServiceUnavailable, "kubeconfig_unavailable",
				fmt.Sprintf("cannot load kubeconfig: %v", err))
			return
		}
		nsList, err := clientset.CoreV1().Namespaces().List(r.Context(), metav1.ListOptions{})
		if err != nil {
			writeEngineError(w, logger, "listing namespaces", err, classifierFor(cluster))
			return
		}
		names := make([]string, 0, len(nsList.Items))
		for _, ns := range nsList.Items {
			names = append(names, ns.Name)
		}
		sort.Strings(names)
		writeJSON(w, logger, http.StatusOK, namespaceList{Items: names})
	}
}
