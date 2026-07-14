// Package resources implements the resource-facing API handlers. Sprint 0
// ships the typed node list; the generic discovery+dynamic engine lands in
// Sprint 2 (ADR-0003).
package resources

import (
	"fmt"
	"log/slog"
	"net/http"

	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/client-go/kubernetes"
)

// ClientsetProvider hands out a typed clientset for the active context.
// Implemented by *kube.Manager; faked in tests.
type ClientsetProvider interface {
	Clientset() (kubernetes.Interface, error)
}

// NodeSummary is the shaped row the UI renders for one node.
type NodeSummary struct {
	Name    string `json:"name"`
	Status  string `json:"status"`
	Version string `json:"version"`
}

type nodeList struct {
	Items []NodeSummary `json:"items"`
}

// NodesHandler serves GET /api/v1/nodes: name, ready status and kubelet
// version per node. A missing/unreadable kubeconfig is a structured 503, an
// unreachable cluster a structured 502 — the server itself stays up.
func NodesHandler(provider ClientsetProvider, logger *slog.Logger) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		clientset, err := provider.Clientset()
		if err != nil {
			logger.Error("kubeconfig unavailable", "error", err)
			writeError(w, logger, http.StatusServiceUnavailable, "kubeconfig_unavailable",
				fmt.Sprintf("cannot load kubeconfig: %v", err))
			return
		}

		nodes, err := clientset.CoreV1().Nodes().List(r.Context(), metav1.ListOptions{})
		if err != nil {
			logger.Error("listing nodes", "error", err)
			writeError(w, logger, http.StatusBadGateway, "cluster_unreachable",
				fmt.Sprintf("listing nodes: %v", err))
			return
		}

		summaries := make([]NodeSummary, 0, len(nodes.Items))
		for _, node := range nodes.Items {
			summaries = append(summaries, NodeSummary{
				Name:    node.Name,
				Status:  nodeStatus(node),
				Version: node.Status.NodeInfo.KubeletVersion,
			})
		}
		writeJSON(w, logger, http.StatusOK, nodeList{Items: summaries})
	}
}

// nodeStatus derives the display status from the NodeReady condition,
// mirroring kubectl's NotReady/Unknown/Ready wording.
func nodeStatus(node corev1.Node) string {
	for _, cond := range node.Status.Conditions {
		if cond.Type != corev1.NodeReady {
			continue
		}
		switch cond.Status {
		case corev1.ConditionTrue:
			return "Ready"
		case corev1.ConditionFalse:
			return "NotReady"
		default:
			return "Unknown"
		}
	}
	return "Unknown"
}
