package resources

import (
	"fmt"
	"log/slog"
	"net/http"
	"sort"

	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
)

// OverviewResponse is the per-cluster summary for the active context.
type OverviewResponse struct {
	Context       string   `json:"context"`
	ServerVersion string   `json:"serverVersion"`
	NodeCount     int      `json:"nodeCount"`
	Namespaces    []string `json:"namespaces"`
}

// OverviewHandler serves GET /api/v1/overview: server version, node count and
// namespace list for the active context. A missing kubeconfig is a structured
// 503, an unreachable cluster a structured 502 — never a blank page.
func OverviewHandler(cluster Cluster, logger *slog.Logger) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		active, err := cluster.ActiveContextName()
		if err != nil {
			logger.Error("resolving active context", "error", err)
			writeError(w, logger, http.StatusServiceUnavailable, "kubeconfig_unavailable",
				fmt.Sprintf("no active context: %v", err))
			return
		}
		clientset, err := cluster.Clientset()
		if err != nil {
			logger.Error("kubeconfig unavailable", "error", err)
			writeError(w, logger, http.StatusServiceUnavailable, "kubeconfig_unavailable",
				fmt.Sprintf("cannot load kubeconfig: %v", err))
			return
		}

		ctx := r.Context()
		version, err := clientset.Discovery().ServerVersion()
		if err != nil {
			writeUnreachable(w, logger, "fetching server version", err, cluster.ExecGuidance(active))
			return
		}
		nodes, err := clientset.CoreV1().Nodes().List(ctx, metav1.ListOptions{})
		if err != nil {
			writeUnreachable(w, logger, "listing nodes", err, cluster.ExecGuidance(active))
			return
		}
		nsList, err := clientset.CoreV1().Namespaces().List(ctx, metav1.ListOptions{})
		if err != nil {
			writeUnreachable(w, logger, "listing namespaces", err, cluster.ExecGuidance(active))
			return
		}

		namespaces := make([]string, 0, len(nsList.Items))
		for _, ns := range nsList.Items {
			namespaces = append(namespaces, ns.Name)
		}
		sort.Strings(namespaces)

		writeJSON(w, logger, http.StatusOK, OverviewResponse{
			Context:       active,
			ServerVersion: version.GitVersion,
			NodeCount:     len(nodes.Items),
			Namespaces:    namespaces,
		})
	}
}

func writeUnreachable(w http.ResponseWriter, logger *slog.Logger, action string, err error, guidance string) {
	logger.Error(action, "error", err)
	writeErrorGuidance(w, logger, http.StatusBadGateway, "cluster_unreachable",
		fmt.Sprintf("%s: %v", action, err), guidance)
}
