package resources

import (
	"log/slog"
	"net/http"
	"sort"
	"strconv"
	"strings"

	"github.com/go-chi/chi/v5"
	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/api/resource"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
)

// Namespace ResourceQuota bars (ADR-0009). One typed read per namespace; each
// quota contributes one entry per constrained resource (used / hard). An empty
// list is a normal 200 (the UI hides the section) — never an error.

// QuotaEntry is one used/hard pair for a single resource within a ResourceQuota.
// Percent is the used/hard ratio (0–100, clamped), computed server-side so the
// bar never has to parse mixed units (cores vs Mi) on the client.
type QuotaEntry struct {
	QuotaName string `json:"quotaName"`
	Resource  string `json:"resource"`
	Used      string `json:"used"`
	Hard      string `json:"hard"`
	Percent   int    `json:"percent"`
}

// QuotasResponse is the namespace's quota entries (flattened across quotas).
type QuotasResponse struct {
	Items []QuotaEntry `json:"items"`
}

// NamespaceQuotasHandler serves GET /api/v1/namespaces/{namespace}/quotas.
func NamespaceQuotasHandler(cluster Cluster, logger *slog.Logger) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		namespace := chi.URLParam(r, "namespace")
		clientset, err := cluster.Clientset()
		if err != nil {
			writeKubeconfigUnavailable(w, logger, err)
			return
		}
		list, err := clientset.CoreV1().ResourceQuotas(namespace).List(r.Context(), metav1.ListOptions{})
		if err != nil {
			writeEngineError(w, logger, "listing resource quotas", err, classifierFor(cluster))
			return
		}

		items := make([]QuotaEntry, 0)
		for i := range list.Items {
			q := &list.Items[i]
			names := make([]string, 0, len(q.Status.Hard))
			for name := range q.Status.Hard {
				names = append(names, string(name))
			}
			sort.Strings(names)
			for _, name := range names {
				rn := corev1.ResourceName(name)
				hard := q.Status.Hard[rn]
				used := q.Status.Used[rn]
				items = append(items, QuotaEntry{
					QuotaName: q.Name,
					Resource:  name,
					Used:      formatQuotaQuantity(name, used),
					Hard:      formatQuotaQuantity(name, hard),
					Percent:   quotaPercent(used, hard),
				})
			}
		}
		writeJSON(w, logger, http.StatusOK, QuotasResponse{Items: items})
	}
}

// quotaPercent is the used/hard ratio as an integer percent (0–100). A zero or
// missing hard limit yields 0 (an unbounded resource shows an empty bar).
func quotaPercent(used, hard resource.Quantity) int {
	h := hard.AsApproximateFloat64()
	if h <= 0 {
		return 0
	}
	pct := int(used.AsApproximateFloat64() / h * 100)
	if pct < 0 {
		return 0
	}
	if pct > 100 {
		return 100
	}
	return pct
}

// formatQuotaQuantity renders a quota quantity kubectl-describe style: CPU as
// cores (1.2 / 4), memory/storage in binary units (2Gi / 512Mi), and object
// counts as plain integers.
func formatQuotaQuantity(name string, q resource.Quantity) string {
	switch {
	case name == "cpu" || strings.HasSuffix(name, "cpu"):
		return strconv.FormatFloat(q.AsApproximateFloat64(), 'f', -1, 64)
	case strings.Contains(name, "memory") || strings.Contains(name, "storage"):
		return q.String() // canonical BinarySI (e.g. 2Gi, 512Mi)
	default:
		return q.String() // object counts, etc.
	}
}
