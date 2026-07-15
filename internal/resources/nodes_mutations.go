package resources

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"net/http"
	"sort"

	"github.com/go-chi/chi/v5"
	corev1 "k8s.io/api/core/v1"
	policyv1 "k8s.io/api/policy/v1"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/fields"
	"k8s.io/apimachinery/pkg/types"
	"k8s.io/client-go/kubernetes"
)

// Node operations (Sprint 5, Story 5.3). Cordon/uncordon patch
// spec.unschedulable; drain evicts pods via the eviction API so
// PodDisruptionBudgets are respected, skipping DaemonSet-owned and mirror
// (static) pods that a drain cannot evict, and reports the outcome per pod so a
// PDB-blocked or failed eviction is surfaced, never swallowed.

// mirrorPodAnnotation marks a static (mirror) pod. Such a pod is owned by the
// kubelet, not the apiserver, so it cannot be evicted and is skipped on drain.
const mirrorPodAnnotation = "kubernetes.io/config.mirror"

// cordonPatch toggles a node's schedulability via a strategic-merge patch on
// spec.unschedulable — the same field `kubectl cordon`/`uncordon` set.
func cordonPatch(unschedulable bool) []byte {
	return []byte(fmt.Sprintf(`{"spec":{"unschedulable":%t}}`, unschedulable))
}

// nodeScheduleResponse is the cordon/uncordon result: the node and its resulting
// schedulability, so the UI reflects the toggle immediately.
type nodeScheduleResponse struct {
	Name          string `json:"name"`
	Unschedulable bool   `json:"unschedulable"`
}

// CordonHandler serves POST /api/v1/nodes/{name}/cordon.
func CordonHandler(cluster Cluster, logger *slog.Logger) http.HandlerFunc {
	return setSchedulable(cluster, logger, true)
}

// UncordonHandler serves POST /api/v1/nodes/{name}/uncordon.
func UncordonHandler(cluster Cluster, logger *slog.Logger) http.HandlerFunc {
	return setSchedulable(cluster, logger, false)
}

func setSchedulable(cluster Cluster, logger *slog.Logger, unschedulable bool) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		name := chi.URLParam(r, "name")
		clientset, err := cluster.Clientset()
		if err != nil {
			writeKubeconfigUnavailable(w, logger, err)
			return
		}
		node, err := clientset.CoreV1().Nodes().Patch(r.Context(), name, types.StrategicMergePatchType,
			cordonPatch(unschedulable), metav1.PatchOptions{})
		if err != nil {
			verb := "cordoning"
			if !unschedulable {
				verb = "uncordoning"
			}
			writeMutationError(w, logger, fmt.Sprintf("%s node %q", verb, name), err, execGuidanceFor(cluster))
			return
		}
		writeJSON(w, logger, http.StatusOK, nodeScheduleResponse{Name: node.Name, Unschedulable: node.Spec.Unschedulable})
	}
}

// podRef identifies a pod in a drain result.
type podRef struct {
	Namespace string `json:"namespace"`
	Name      string `json:"name"`
}

// Drain per-pod result codes.
const (
	drainEvicted = "evicted" // eviction accepted by the apiserver
	drainSkipped = "skipped" // not a drain candidate (DaemonSet/mirror/terminal)
	drainBlocked = "blocked" // eviction rejected by a PodDisruptionBudget (429)
	drainError   = "error"   // eviction failed for another reason
)

// podDrainResult is one pod's drain outcome, with a human-readable reason so a
// blocked or failed eviction is legible per pod.
type podDrainResult struct {
	podRef
	Result string `json:"result"`
	Reason string `json:"reason,omitempty"`
}

// DrainResult is the whole-node drain outcome: the node cordoned, and every pod
// with its result. Counts let the UI summarize without re-tallying.
type DrainResult struct {
	Node    string           `json:"node"`
	Pods    []podDrainResult `json:"pods"`
	Evicted int              `json:"evicted"`
	Skipped int              `json:"skipped"`
	Blocked int              `json:"blocked"`
	Failed  int              `json:"failed"`
}

// drainCandidate is a classified pod on the node: either evictable, or skipped
// with a reason. Pure classification is separated from the eviction I/O so the
// candidate-selection rules are table-tested without a cluster.
type drainCandidate struct {
	pod     corev1.Pod
	skip    bool
	skipMsg string
}

// classifyDrainPods partitions a node's pods into eviction candidates and
// skipped pods (DaemonSet-owned, mirror/static, or already-terminal), mirroring
// `kubectl drain` defaults. Order is preserved so results are deterministic.
func classifyDrainPods(pods []corev1.Pod) []drainCandidate {
	out := make([]drainCandidate, 0, len(pods))
	for i := range pods {
		pod := pods[i]
		switch {
		case isMirrorPod(&pod):
			out = append(out, drainCandidate{pod: pod, skip: true, skipMsg: "static (mirror) pod; managed by the kubelet"})
		case ownedByDaemonSet(&pod):
			out = append(out, drainCandidate{pod: pod, skip: true, skipMsg: "DaemonSet-managed pod"})
		case podTerminal(&pod):
			out = append(out, drainCandidate{pod: pod, skip: true, skipMsg: "already terminated"})
		default:
			out = append(out, drainCandidate{pod: pod})
		}
	}
	return out
}

func isMirrorPod(pod *corev1.Pod) bool {
	_, ok := pod.Annotations[mirrorPodAnnotation]
	return ok
}

func ownedByDaemonSet(pod *corev1.Pod) bool {
	for i := range pod.OwnerReferences {
		ref := pod.OwnerReferences[i]
		if ref.Kind == "DaemonSet" && ref.Controller != nil && *ref.Controller {
			return true
		}
	}
	return false
}

func podTerminal(pod *corev1.Pod) bool {
	return pod.Status.Phase == corev1.PodSucceeded || pod.Status.Phase == corev1.PodFailed
}

// DrainHandler serves POST /api/v1/nodes/{name}/drain: cordons the node, then
// evicts every candidate pod via the eviction API, reporting each pod's outcome.
// It is a single eviction pass — a PDB-blocked pod is reported as blocked rather
// than retried indefinitely, so the caller sees exactly what happened.
func DrainHandler(cluster Cluster, logger *slog.Logger) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		name := chi.URLParam(r, "name")
		clientset, err := cluster.Clientset()
		if err != nil {
			writeKubeconfigUnavailable(w, logger, err)
			return
		}
		ctx := r.Context()

		// Cordon first so nothing new schedules onto the node mid-drain.
		if _, err := clientset.CoreV1().Nodes().Patch(ctx, name, types.StrategicMergePatchType,
			cordonPatch(true), metav1.PatchOptions{}); err != nil {
			writeMutationError(w, logger, fmt.Sprintf("cordoning node %q for drain", name), err, execGuidanceFor(cluster))
			return
		}

		pods, err := clientset.CoreV1().Pods(metav1.NamespaceAll).List(ctx, metav1.ListOptions{
			FieldSelector: fields.OneTermEqualSelector("spec.nodeName", name).String(),
		})
		if err != nil {
			writeMutationError(w, logger, fmt.Sprintf("listing pods on node %q", name), err, execGuidanceFor(cluster))
			return
		}

		result := drainNode(ctx, clientset, name, classifyDrainPods(pods.Items))
		writeJSON(w, logger, http.StatusOK, result)
	}
}

// drainNode evicts each candidate and tallies the outcome. It performs the
// eviction I/O; classification is done by classifyDrainPods.
func drainNode(ctx context.Context, clientset kubernetes.Interface, node string, candidates []drainCandidate) DrainResult {
	result := DrainResult{Node: node, Pods: make([]podDrainResult, 0, len(candidates))}
	for i := range candidates {
		c := candidates[i]
		ref := podRef{Namespace: c.pod.Namespace, Name: c.pod.Name}
		if c.skip {
			result.Pods = append(result.Pods, podDrainResult{podRef: ref, Result: drainSkipped, Reason: c.skipMsg})
			result.Skipped++
			continue
		}
		err := evictPod(ctx, clientset, c.pod.Namespace, c.pod.Name)
		switch {
		case err == nil:
			result.Pods = append(result.Pods, podDrainResult{podRef: ref, Result: drainEvicted})
			result.Evicted++
		case apierrors.IsTooManyRequests(err):
			result.Pods = append(result.Pods, podDrainResult{podRef: ref, Result: drainBlocked, Reason: pdbMessage(err)})
			result.Blocked++
		default:
			result.Pods = append(result.Pods, podDrainResult{podRef: ref, Result: drainError, Reason: err.Error()})
			result.Failed++
		}
	}
	// Deterministic ordering independent of the apiserver's list order.
	sort.Slice(result.Pods, func(i, j int) bool {
		if result.Pods[i].Namespace != result.Pods[j].Namespace {
			return result.Pods[i].Namespace < result.Pods[j].Namespace
		}
		return result.Pods[i].Name < result.Pods[j].Name
	})
	return result
}

// evictPod requests eviction of one pod via the policy/v1 eviction subresource,
// so a PodDisruptionBudget can reject it (surfaced as a 429 → "blocked").
func evictPod(ctx context.Context, clientset kubernetes.Interface, namespace, name string) error {
	return clientset.CoreV1().Pods(namespace).EvictV1(ctx, &policyv1.Eviction{
		ObjectMeta: metav1.ObjectMeta{Name: name, Namespace: namespace},
	})
}

// pdbMessage extracts the apiserver's reason for a PDB-blocked eviction, falling
// back to the raw error text.
func pdbMessage(err error) string {
	var status apierrors.APIStatus
	if errors.As(err, &status) {
		if msg := status.Status().Message; msg != "" {
			return msg
		}
	}
	return err.Error()
}
