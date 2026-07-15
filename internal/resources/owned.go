package resources

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"net/http"

	"github.com/go-chi/chi/v5"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/types"
	"k8s.io/client-go/kubernetes"
)

// Owned-resource resolution (Sprint 3, Story 3.3): given a controller, resolve
// the pods (or, for a CronJob, the Jobs) it owns — server-side via label
// selector + ownerReferences so the frontend just renders the resulting rows.

// OwnedPodsHandler serves GET
// /api/v1/workloads/{resource}/{namespace}/{name}/pods: the pods a controller
// owns, each shaped as a PodSummary that links to Pod detail. Resolution walks
// the controller's label selector, then filters by ownerReference so pods that
// merely match the labels (but belong to a different controller) are excluded.
func OwnedPodsHandler(cluster Cluster, logger *slog.Logger) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		resource := chi.URLParam(r, "resource")
		namespace := chi.URLParam(r, "namespace")
		name := chi.URLParam(r, "name")

		clientset, err := cluster.Clientset()
		if err != nil {
			writeKubeconfigUnavailable(w, logger, err)
			return
		}

		ctx := r.Context()
		pods, err := ownedPods(ctx, clientset, resource, namespace, name)
		if err != nil {
			var unknown *unknownWorkloadError
			if errors.As(err, &unknown) {
				writeError(w, logger, http.StatusNotFound, "unknown_workload", unknown.Error())
				return
			}
			writeEngineError(w, logger, fmt.Sprintf("resolving pods for %s %q", resource, name), err, execGuidanceFor(cluster))
			return
		}
		writeJSON(w, logger, http.StatusOK, workloadList[PodSummary]{Items: pods})
	}
}

// OwnedJobsHandler serves GET
// /api/v1/workloads/{resource}/{namespace}/{name}/jobs: the Jobs a CronJob owns
// (its active + recent runs), each shaped as a JobSummary. Only CronJobs expose
// this; other kinds are a 404.
func OwnedJobsHandler(cluster Cluster, logger *slog.Logger) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		resource := chi.URLParam(r, "resource")
		namespace := chi.URLParam(r, "namespace")
		name := chi.URLParam(r, "name")

		if resource != resourceCronJobs {
			writeError(w, logger, http.StatusNotFound, "unknown_workload",
				fmt.Sprintf("%q does not own Jobs", resource))
			return
		}

		clientset, err := cluster.Clientset()
		if err != nil {
			writeKubeconfigUnavailable(w, logger, err)
			return
		}

		ctx := r.Context()
		cronJob, err := clientset.BatchV1().CronJobs(namespace).Get(ctx, name, metav1.GetOptions{})
		if err != nil {
			writeEngineError(w, logger, fmt.Sprintf("getting cronjob %q", name), err, execGuidanceFor(cluster))
			return
		}
		jobList, err := clientset.BatchV1().Jobs(namespace).List(ctx, metav1.ListOptions{})
		if err != nil {
			writeEngineError(w, logger, fmt.Sprintf("listing jobs for cronjob %q", name), err, execGuidanceFor(cluster))
			return
		}
		items := make([]JobSummary, 0)
		for i := range jobList.Items {
			if isControlledBy(jobList.Items[i].OwnerReferences, cronJob.UID) {
				items = append(items, shapeJob(&jobList.Items[i]))
			}
		}
		writeJSON(w, logger, http.StatusOK, workloadList[JobSummary]{Items: items})
	}
}

// unknownWorkloadError marks a request for owned pods of a kind that does not
// own pods (e.g. a CronJob, which owns Jobs).
type unknownWorkloadError struct{ resource string }

func (e *unknownWorkloadError) Error() string {
	return fmt.Sprintf("%q does not own pods directly", e.resource)
}

// ownedPods resolves and shapes the pods a controller owns. For Deployments it
// first resolves the owned ReplicaSets (pods are owned by the RS, not the
// Deployment); for the other controllers it filters pods directly by
// ownerReference.
func ownedPods(ctx context.Context, clientset kubernetes.Interface, resource, namespace, name string) ([]PodSummary, error) {
	switch resource {
	case resourceDeployments:
		return deploymentPods(ctx, clientset, namespace, name)
	case resourceStatefulSets:
		s, err := clientset.AppsV1().StatefulSets(namespace).Get(ctx, name, metav1.GetOptions{})
		if err != nil {
			return nil, err
		}
		return podsBySelectorOwnedBy(ctx, clientset, namespace, s.Spec.Selector, s.UID)
	case resourceDaemonSets:
		d, err := clientset.AppsV1().DaemonSets(namespace).Get(ctx, name, metav1.GetOptions{})
		if err != nil {
			return nil, err
		}
		return podsBySelectorOwnedBy(ctx, clientset, namespace, d.Spec.Selector, d.UID)
	case resourceReplicaSets:
		rs, err := clientset.AppsV1().ReplicaSets(namespace).Get(ctx, name, metav1.GetOptions{})
		if err != nil {
			return nil, err
		}
		return podsBySelectorOwnedBy(ctx, clientset, namespace, rs.Spec.Selector, rs.UID)
	case resourceJobs:
		j, err := clientset.BatchV1().Jobs(namespace).Get(ctx, name, metav1.GetOptions{})
		if err != nil {
			return nil, err
		}
		return podsBySelectorOwnedBy(ctx, clientset, namespace, j.Spec.Selector, j.UID)
	default:
		return nil, &unknownWorkloadError{resource: resource}
	}
}

// deploymentPods resolves a Deployment's pods through its ReplicaSets: find the
// RSes the Deployment controls, then the pods those RSes control. Pods are
// listed by the Deployment's selector (which the RSes inherit) and filtered to
// the resolved RS UIDs, so pods from an unrelated RS are excluded.
func deploymentPods(ctx context.Context, clientset kubernetes.Interface, namespace, name string) ([]PodSummary, error) {
	dep, err := clientset.AppsV1().Deployments(namespace).Get(ctx, name, metav1.GetOptions{})
	if err != nil {
		return nil, err
	}
	selector, err := metav1.LabelSelectorAsSelector(dep.Spec.Selector)
	if err != nil {
		return nil, fmt.Errorf("parsing deployment selector: %w", err)
	}
	rsList, err := clientset.AppsV1().ReplicaSets(namespace).List(ctx, metav1.ListOptions{LabelSelector: selector.String()})
	if err != nil {
		return nil, err
	}
	rsUIDs := map[types.UID]bool{}
	for i := range rsList.Items {
		if isControlledBy(rsList.Items[i].OwnerReferences, dep.UID) {
			rsUIDs[rsList.Items[i].UID] = true
		}
	}
	podList, err := clientset.CoreV1().Pods(namespace).List(ctx, metav1.ListOptions{LabelSelector: selector.String()})
	if err != nil {
		return nil, err
	}
	items := make([]PodSummary, 0)
	for i := range podList.Items {
		if podControlledByAny(podList.Items[i].OwnerReferences, rsUIDs) {
			items = append(items, shapePod(&podList.Items[i]))
		}
	}
	return items, nil
}

// podsBySelectorOwnedBy lists pods matching selector in namespace and keeps only
// those whose controller ownerReference is ownerUID.
func podsBySelectorOwnedBy(ctx context.Context, clientset kubernetes.Interface, namespace string, sel *metav1.LabelSelector, ownerUID types.UID) ([]PodSummary, error) {
	selector, err := metav1.LabelSelectorAsSelector(sel)
	if err != nil {
		return nil, fmt.Errorf("parsing selector: %w", err)
	}
	podList, err := clientset.CoreV1().Pods(namespace).List(ctx, metav1.ListOptions{LabelSelector: selector.String()})
	if err != nil {
		return nil, err
	}
	items := make([]PodSummary, 0)
	for i := range podList.Items {
		if isControlledBy(podList.Items[i].OwnerReferences, ownerUID) {
			items = append(items, shapePod(&podList.Items[i]))
		}
	}
	return items, nil
}

// isControlledBy reports whether refs contains a controller ownerReference with
// the given UID.
func isControlledBy(refs []metav1.OwnerReference, uid types.UID) bool {
	for i := range refs {
		if refs[i].UID == uid && refs[i].Controller != nil && *refs[i].Controller {
			return true
		}
	}
	return false
}

// podControlledByAny reports whether refs contains a controller ownerReference
// whose UID is in uids.
func podControlledByAny(refs []metav1.OwnerReference, uids map[types.UID]bool) bool {
	for i := range refs {
		if refs[i].Controller != nil && *refs[i].Controller && uids[refs[i].UID] {
			return true
		}
	}
	return false
}
