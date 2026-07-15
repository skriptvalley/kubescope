package resources

import (
	"fmt"
	"log/slog"
	"net/http"
	"time"

	"github.com/go-chi/chi/v5"
	appsv1 "k8s.io/api/apps/v1"
	batchv1 "k8s.io/api/batch/v1"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
)

// Typed workload summaries (Sprint 3, ADR-0003). These are the hot-path
// complement to the generic dynamic-client engine: the seven core workload
// kinds get kind-appropriate rows with every field computed server-side, so the
// thin frontend only renders. The generic engine still serves these kinds if
// requested via /resources/... — nothing here replaces it.

// Workload resource path tokens (the plural resource names), used both to route
// /workloads/{resource} and to key owned-pods resolution.
const (
	resourcePods         = "pods"
	resourceDeployments  = "deployments"
	resourceStatefulSets = "statefulsets"
	resourceDaemonSets   = "daemonsets"
	resourceReplicaSets  = "replicasets"
	resourceJobs         = "jobs"
	resourceCronJobs     = "cronjobs"
)

// OwnerRef is a minimal controller owner reference for linking a resource to its
// controller in the UI (e.g. a ReplicaSet to its Deployment, a Pod to its RS).
type OwnerRef struct {
	Kind string `json:"kind"`
	Name string `json:"name"`
	UID  string `json:"uid,omitempty"`
}

// PodSummary is one shaped Pod row: ready-container count, computed status,
// restarts, node placement and creation time (age is formatted client-side from
// the timestamp, matching the generic engine — ADR-0003).
type PodSummary struct {
	Name              string    `json:"name"`
	Namespace         string    `json:"namespace"`
	Ready             string    `json:"ready"` // "readyContainers/totalContainers"
	ReadyContainers   int       `json:"readyContainers"`
	TotalContainers   int       `json:"totalContainers"`
	Status            string    `json:"status"` // kubectl-style STATUS (Running, CrashLoopBackOff, Init:0/1, …)
	Phase             string    `json:"phase"`  // raw pod phase
	Restarts          int32     `json:"restarts"`
	Node              string    `json:"node,omitempty"`
	Owner             *OwnerRef `json:"owner,omitempty"`
	CreationTimestamp string    `json:"creationTimestamp,omitempty"`
}

// DeploymentSummary is one shaped Deployment row with replica health and a
// kubectl-rollout-style status line, all computed in Go.
type DeploymentSummary struct {
	Name              string `json:"name"`
	Namespace         string `json:"namespace"`
	Ready             string `json:"ready"` // "readyReplicas/desiredReplicas"
	DesiredReplicas   int32  `json:"desiredReplicas"`
	ReadyReplicas     int32  `json:"readyReplicas"`
	UpdatedReplicas   int32  `json:"updatedReplicas"`
	AvailableReplicas int32  `json:"availableReplicas"`
	RolloutStatus     string `json:"rolloutStatus"`
	CreationTimestamp string `json:"creationTimestamp,omitempty"`
}

// StatefulSetSummary is one shaped StatefulSet row.
type StatefulSetSummary struct {
	Name              string `json:"name"`
	Namespace         string `json:"namespace"`
	Ready             string `json:"ready"` // "readyReplicas/desiredReplicas"
	DesiredReplicas   int32  `json:"desiredReplicas"`
	ReadyReplicas     int32  `json:"readyReplicas"`
	CurrentReplicas   int32  `json:"currentReplicas"`
	UpdatedReplicas   int32  `json:"updatedReplicas"`
	RolloutStatus     string `json:"rolloutStatus"`
	CreationTimestamp string `json:"creationTimestamp,omitempty"`
}

// DaemonSetSummary is one shaped DaemonSet row.
type DaemonSetSummary struct {
	Name              string `json:"name"`
	Namespace         string `json:"namespace"`
	Desired           int32  `json:"desired"`
	Current           int32  `json:"current"`
	Ready             int32  `json:"ready"`
	UpToDate          int32  `json:"upToDate"`
	Available         int32  `json:"available"`
	RolloutStatus     string `json:"rolloutStatus"`
	CreationTimestamp string `json:"creationTimestamp,omitempty"`
}

// ReplicaSetSummary is one shaped ReplicaSet row; Owner links to its owning
// Deployment when one exists.
type ReplicaSetSummary struct {
	Name              string    `json:"name"`
	Namespace         string    `json:"namespace"`
	Ready             string    `json:"ready"` // "readyReplicas/desiredReplicas"
	DesiredReplicas   int32     `json:"desiredReplicas"`
	CurrentReplicas   int32     `json:"currentReplicas"`
	ReadyReplicas     int32     `json:"readyReplicas"`
	Owner             *OwnerRef `json:"owner,omitempty"`
	CreationTimestamp string    `json:"creationTimestamp,omitempty"`
}

// JobSummary is one shaped Job row: completions, succeeded/failed counts and
// run duration.
type JobSummary struct {
	Name              string    `json:"name"`
	Namespace         string    `json:"namespace"`
	Completions       string    `json:"completions"` // "succeeded/completions" ("succeeded/1" when unset)
	Succeeded         int32     `json:"succeeded"`
	Failed            int32     `json:"failed"`
	Active            int32     `json:"active"`
	Duration          string    `json:"duration,omitempty"`
	Owner             *OwnerRef `json:"owner,omitempty"`
	CreationTimestamp string    `json:"creationTimestamp,omitempty"`
}

// CronJobSummary is one shaped CronJob row.
type CronJobSummary struct {
	Name              string `json:"name"`
	Namespace         string `json:"namespace"`
	Schedule          string `json:"schedule"`
	Suspend           bool   `json:"suspend"`
	Active            int    `json:"active"`
	LastScheduleTime  string `json:"lastScheduleTime,omitempty"`
	CreationTimestamp string `json:"creationTimestamp,omitempty"`
}

// workloadList is the typed list envelope; T is one of the *Summary types.
type workloadList[T any] struct {
	Items []T `json:"items"`
}

// WorkloadListHandler serves GET /api/v1/workloads/{resource}: a typed summary
// list for one of the seven workload kinds, scoped by `?namespace=` (omit for
// all namespaces). Unknown kinds are a structured 404 — the generic engine
// (ADR-0003) is the fallback for everything else.
func WorkloadListHandler(cluster Cluster, logger *slog.Logger) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		resource := chi.URLParam(r, "resource")
		clientset, err := cluster.Clientset()
		if err != nil {
			writeKubeconfigUnavailable(w, logger, err)
			return
		}
		namespace := r.URL.Query().Get("namespace")
		ctx := r.Context()
		opts := metav1.ListOptions{}

		switch resource {
		case resourcePods:
			list, err := clientset.CoreV1().Pods(namespace).List(ctx, opts)
			if err != nil {
				writeUnreachable(w, logger, "listing pods", err, execGuidanceFor(cluster))
				return
			}
			items := make([]PodSummary, 0, len(list.Items))
			for i := range list.Items {
				items = append(items, shapePod(&list.Items[i]))
			}
			writeJSON(w, logger, http.StatusOK, workloadList[PodSummary]{Items: items})
		case resourceDeployments:
			list, err := clientset.AppsV1().Deployments(namespace).List(ctx, opts)
			if err != nil {
				writeUnreachable(w, logger, "listing deployments", err, execGuidanceFor(cluster))
				return
			}
			items := make([]DeploymentSummary, 0, len(list.Items))
			for i := range list.Items {
				items = append(items, shapeDeployment(&list.Items[i]))
			}
			writeJSON(w, logger, http.StatusOK, workloadList[DeploymentSummary]{Items: items})
		case resourceStatefulSets:
			list, err := clientset.AppsV1().StatefulSets(namespace).List(ctx, opts)
			if err != nil {
				writeUnreachable(w, logger, "listing statefulsets", err, execGuidanceFor(cluster))
				return
			}
			items := make([]StatefulSetSummary, 0, len(list.Items))
			for i := range list.Items {
				items = append(items, shapeStatefulSet(&list.Items[i]))
			}
			writeJSON(w, logger, http.StatusOK, workloadList[StatefulSetSummary]{Items: items})
		case resourceDaemonSets:
			list, err := clientset.AppsV1().DaemonSets(namespace).List(ctx, opts)
			if err != nil {
				writeUnreachable(w, logger, "listing daemonsets", err, execGuidanceFor(cluster))
				return
			}
			items := make([]DaemonSetSummary, 0, len(list.Items))
			for i := range list.Items {
				items = append(items, shapeDaemonSet(&list.Items[i]))
			}
			writeJSON(w, logger, http.StatusOK, workloadList[DaemonSetSummary]{Items: items})
		case resourceReplicaSets:
			list, err := clientset.AppsV1().ReplicaSets(namespace).List(ctx, opts)
			if err != nil {
				writeUnreachable(w, logger, "listing replicasets", err, execGuidanceFor(cluster))
				return
			}
			items := make([]ReplicaSetSummary, 0, len(list.Items))
			for i := range list.Items {
				items = append(items, shapeReplicaSet(&list.Items[i]))
			}
			writeJSON(w, logger, http.StatusOK, workloadList[ReplicaSetSummary]{Items: items})
		case resourceJobs:
			list, err := clientset.BatchV1().Jobs(namespace).List(ctx, opts)
			if err != nil {
				writeUnreachable(w, logger, "listing jobs", err, execGuidanceFor(cluster))
				return
			}
			items := make([]JobSummary, 0, len(list.Items))
			for i := range list.Items {
				items = append(items, shapeJob(&list.Items[i]))
			}
			writeJSON(w, logger, http.StatusOK, workloadList[JobSummary]{Items: items})
		case resourceCronJobs:
			list, err := clientset.BatchV1().CronJobs(namespace).List(ctx, opts)
			if err != nil {
				writeUnreachable(w, logger, "listing cronjobs", err, execGuidanceFor(cluster))
				return
			}
			items := make([]CronJobSummary, 0, len(list.Items))
			for i := range list.Items {
				items = append(items, shapeCronJob(&list.Items[i]))
			}
			writeJSON(w, logger, http.StatusOK, workloadList[CronJobSummary]{Items: items})
		default:
			writeError(w, logger, http.StatusNotFound, "unknown_workload",
				fmt.Sprintf("no typed workload summary for %q", resource))
		}
	}
}

// --- Shaping (pure; table-tested) ---------------------------------------------

func shapePod(pod *corev1.Pod) PodSummary {
	ready, total := podReadyCounts(pod)
	return PodSummary{
		Name:              pod.Name,
		Namespace:         pod.Namespace,
		Ready:             fmt.Sprintf("%d/%d", ready, total),
		ReadyContainers:   ready,
		TotalContainers:   total,
		Status:            podDisplayStatus(pod),
		Phase:             string(pod.Status.Phase),
		Restarts:          podRestarts(pod),
		Node:              pod.Spec.NodeName,
		Owner:             controllerOwner(pod.OwnerReferences),
		CreationTimestamp: formatTimestamp(pod.CreationTimestamp),
	}
}

// podReadyCounts returns (ready containers, total containers). Total is the
// number of spec containers (kubectl's denominator); ready is counted from
// status, so a not-yet-scheduled pod reads 0/N rather than 0/0.
func podReadyCounts(pod *corev1.Pod) (ready, total int) {
	total = len(pod.Spec.Containers)
	for i := range pod.Status.ContainerStatuses {
		if pod.Status.ContainerStatuses[i].Ready {
			ready++
		}
	}
	return ready, total
}

// podRestarts sums restart counts across app and init containers, mirroring the
// value kubectl surfaces in the RESTARTS column.
func podRestarts(pod *corev1.Pod) int32 {
	var restarts int32
	for i := range pod.Status.InitContainerStatuses {
		restarts += pod.Status.InitContainerStatuses[i].RestartCount
	}
	for i := range pod.Status.ContainerStatuses {
		restarts += pod.Status.ContainerStatuses[i].RestartCount
	}
	return restarts
}

// podDisplayStatus computes the kubectl-style STATUS string, folding in
// init-container progress, per-container waiting/terminated reasons, and
// deletion (Terminating). It is a faithful subset of kubectl's printer logic.
func podDisplayStatus(pod *corev1.Pod) string {
	reason := string(pod.Status.Phase)
	if pod.Status.Reason != "" {
		reason = pod.Status.Reason // e.g. Evicted, NodeLost
	}

	initializing := false
	for i := range pod.Status.InitContainerStatuses {
		cs := pod.Status.InitContainerStatuses[i]
		switch {
		case cs.State.Terminated != nil && cs.State.Terminated.ExitCode == 0:
			continue
		case cs.State.Terminated != nil:
			if cs.State.Terminated.Reason != "" {
				reason = "Init:" + cs.State.Terminated.Reason
			} else if cs.State.Terminated.Signal != 0 {
				reason = fmt.Sprintf("Init:Signal:%d", cs.State.Terminated.Signal)
			} else {
				reason = fmt.Sprintf("Init:ExitCode:%d", cs.State.Terminated.ExitCode)
			}
			initializing = true
		case cs.State.Waiting != nil && cs.State.Waiting.Reason != "" && cs.State.Waiting.Reason != "PodInitializing":
			reason = "Init:" + cs.State.Waiting.Reason
			initializing = true
		default:
			reason = fmt.Sprintf("Init:%d/%d", i, len(pod.Spec.InitContainers))
			initializing = true
		}
		break
	}

	if !initializing {
		hasRunning := false
		for i := len(pod.Status.ContainerStatuses) - 1; i >= 0; i-- {
			cs := pod.Status.ContainerStatuses[i]
			switch {
			case cs.State.Waiting != nil && cs.State.Waiting.Reason != "":
				reason = cs.State.Waiting.Reason
			case cs.State.Terminated != nil && cs.State.Terminated.Reason != "":
				reason = cs.State.Terminated.Reason
			case cs.State.Terminated != nil:
				if cs.State.Terminated.Signal != 0 {
					reason = fmt.Sprintf("Signal:%d", cs.State.Terminated.Signal)
				} else {
					reason = fmt.Sprintf("ExitCode:%d", cs.State.Terminated.ExitCode)
				}
			case cs.Ready && cs.State.Running != nil:
				hasRunning = true
			}
		}
		// "Completed" with a container still running means a partial completion.
		if reason == "Completed" && hasRunning && hasPodReadyCondition(pod.Status.Conditions) {
			reason = "Running"
		}
	}

	// A pod being deleted reads "Terminating" — but a pod that already reached a
	// terminal phase keeps its terminal status (Completed/Failed) while it is
	// garbage-collected, matching kubectl.
	if pod.DeletionTimestamp != nil && pod.Status.Reason == "NodeLost" {
		reason = "Unknown"
	} else if pod.DeletionTimestamp != nil && !podPhaseTerminal(pod.Status.Phase) {
		reason = "Terminating"
	}
	return reason
}

// podPhaseTerminal reports whether a pod phase is terminal (Succeeded/Failed) —
// such a pod is done and should not be relabelled "Terminating" during deletion.
func podPhaseTerminal(phase corev1.PodPhase) bool {
	return phase == corev1.PodSucceeded || phase == corev1.PodFailed
}

func hasPodReadyCondition(conditions []corev1.PodCondition) bool {
	for i := range conditions {
		if conditions[i].Type == corev1.PodReady && conditions[i].Status == corev1.ConditionTrue {
			return true
		}
	}
	return false
}

func shapeDeployment(d *appsv1.Deployment) DeploymentSummary {
	desired := int32(1)
	if d.Spec.Replicas != nil {
		desired = *d.Spec.Replicas
	}
	return DeploymentSummary{
		Name:              d.Name,
		Namespace:         d.Namespace,
		Ready:             fmt.Sprintf("%d/%d", d.Status.ReadyReplicas, desired),
		DesiredReplicas:   desired,
		ReadyReplicas:     d.Status.ReadyReplicas,
		UpdatedReplicas:   d.Status.UpdatedReplicas,
		AvailableReplicas: d.Status.AvailableReplicas,
		RolloutStatus:     deploymentRolloutStatus(d),
		CreationTimestamp: formatTimestamp(d.CreationTimestamp),
	}
}

// deploymentRolloutStatus mirrors `kubectl rollout status deployment`.
func deploymentRolloutStatus(d *appsv1.Deployment) string {
	if d.Generation > d.Status.ObservedGeneration {
		return "Waiting for deployment spec update to be observed"
	}
	for i := range d.Status.Conditions {
		c := d.Status.Conditions[i]
		if c.Type == appsv1.DeploymentProgressing && c.Reason == "ProgressDeadlineExceeded" {
			return fmt.Sprintf("deployment %q exceeded its progress deadline", d.Name)
		}
	}
	desired := int32(0)
	if d.Spec.Replicas != nil {
		desired = *d.Spec.Replicas
	}
	if d.Spec.Replicas != nil && d.Status.UpdatedReplicas < desired {
		return fmt.Sprintf("Waiting for rollout to finish: %d out of %d new replicas have been updated",
			d.Status.UpdatedReplicas, desired)
	}
	if d.Status.Replicas > d.Status.UpdatedReplicas {
		return fmt.Sprintf("Waiting for rollout to finish: %d old replicas are pending termination",
			d.Status.Replicas-d.Status.UpdatedReplicas)
	}
	if d.Status.AvailableReplicas < d.Status.UpdatedReplicas {
		return fmt.Sprintf("Waiting for rollout to finish: %d of %d updated replicas are available",
			d.Status.AvailableReplicas, d.Status.UpdatedReplicas)
	}
	return "Deployment successfully rolled out"
}

func shapeStatefulSet(s *appsv1.StatefulSet) StatefulSetSummary {
	desired := int32(1)
	if s.Spec.Replicas != nil {
		desired = *s.Spec.Replicas
	}
	return StatefulSetSummary{
		Name:              s.Name,
		Namespace:         s.Namespace,
		Ready:             fmt.Sprintf("%d/%d", s.Status.ReadyReplicas, desired),
		DesiredReplicas:   desired,
		ReadyReplicas:     s.Status.ReadyReplicas,
		CurrentReplicas:   s.Status.CurrentReplicas,
		UpdatedReplicas:   s.Status.UpdatedReplicas,
		RolloutStatus:     statefulSetRolloutStatus(s),
		CreationTimestamp: formatTimestamp(s.CreationTimestamp),
	}
}

// statefulSetRolloutStatus mirrors `kubectl rollout status statefulset` for the
// RollingUpdate strategy (the default); OnDelete is reported as unmanaged.
func statefulSetRolloutStatus(s *appsv1.StatefulSet) string {
	if s.Spec.UpdateStrategy.Type == appsv1.OnDeleteStatefulSetStrategyType {
		return "StatefulSet update strategy is OnDelete; pods are updated manually"
	}
	if s.Status.ObservedGeneration < s.Generation {
		return "Waiting for statefulset spec update to be observed"
	}
	desired := int32(1)
	if s.Spec.Replicas != nil {
		desired = *s.Spec.Replicas
	}
	if s.Status.ReadyReplicas < desired {
		return fmt.Sprintf("Waiting for %d pods to be ready", desired-s.Status.ReadyReplicas)
	}
	// A partitioned rollout only updates ordinals at or above the partition, so
	// it is "complete" once those pods are updated — kubectl reports this
	// specially rather than waiting for every pod to reach the new revision.
	if ru := s.Spec.UpdateStrategy.RollingUpdate; ru != nil && ru.Partition != nil && *ru.Partition > 0 {
		want := desired - *ru.Partition
		if s.Status.UpdatedReplicas < want {
			return fmt.Sprintf("Waiting for partitioned rollout to finish: %d out of %d new pods have been updated",
				s.Status.UpdatedReplicas, want)
		}
		return fmt.Sprintf("Partitioned rollout complete: %d new pods have been updated", s.Status.UpdatedReplicas)
	}
	if s.Status.UpdateRevision != s.Status.CurrentRevision {
		return fmt.Sprintf("Waiting for rollout to finish: %d out of %d new pods have been updated",
			s.Status.UpdatedReplicas, desired)
	}
	return "StatefulSet successfully rolled out"
}

func shapeDaemonSet(d *appsv1.DaemonSet) DaemonSetSummary {
	return DaemonSetSummary{
		Name:              d.Name,
		Namespace:         d.Namespace,
		Desired:           d.Status.DesiredNumberScheduled,
		Current:           d.Status.CurrentNumberScheduled,
		Ready:             d.Status.NumberReady,
		UpToDate:          d.Status.UpdatedNumberScheduled,
		Available:         d.Status.NumberAvailable,
		RolloutStatus:     daemonSetRolloutStatus(d),
		CreationTimestamp: formatTimestamp(d.CreationTimestamp),
	}
}

// daemonSetRolloutStatus mirrors `kubectl rollout status daemonset` for the
// RollingUpdate strategy; OnDelete is reported as unmanaged.
func daemonSetRolloutStatus(d *appsv1.DaemonSet) string {
	if d.Spec.UpdateStrategy.Type == appsv1.OnDeleteDaemonSetStrategyType {
		return "DaemonSet update strategy is OnDelete; pods are updated manually"
	}
	if d.Status.ObservedGeneration < d.Generation {
		return "Waiting for daemon set spec update to be observed"
	}
	if d.Status.UpdatedNumberScheduled < d.Status.DesiredNumberScheduled {
		return fmt.Sprintf("Waiting for daemon set rollout to finish: %d out of %d new pods have been updated",
			d.Status.UpdatedNumberScheduled, d.Status.DesiredNumberScheduled)
	}
	if d.Status.NumberAvailable < d.Status.DesiredNumberScheduled {
		return fmt.Sprintf("Waiting for daemon set rollout to finish: %d of %d updated pods are available",
			d.Status.NumberAvailable, d.Status.DesiredNumberScheduled)
	}
	return "DaemonSet successfully rolled out"
}

func shapeReplicaSet(rs *appsv1.ReplicaSet) ReplicaSetSummary {
	desired := int32(1)
	if rs.Spec.Replicas != nil {
		desired = *rs.Spec.Replicas
	}
	return ReplicaSetSummary{
		Name:              rs.Name,
		Namespace:         rs.Namespace,
		Ready:             fmt.Sprintf("%d/%d", rs.Status.ReadyReplicas, desired),
		DesiredReplicas:   desired,
		CurrentReplicas:   rs.Status.Replicas,
		ReadyReplicas:     rs.Status.ReadyReplicas,
		Owner:             controllerOwner(rs.OwnerReferences),
		CreationTimestamp: formatTimestamp(rs.CreationTimestamp),
	}
}

func shapeJob(j *batchv1.Job) JobSummary {
	completions := fmt.Sprintf("%d/1", j.Status.Succeeded)
	if j.Spec.Completions != nil {
		completions = fmt.Sprintf("%d/%d", j.Status.Succeeded, *j.Spec.Completions)
	}
	return JobSummary{
		Name:              j.Name,
		Namespace:         j.Namespace,
		Completions:       completions,
		Succeeded:         j.Status.Succeeded,
		Failed:            j.Status.Failed,
		Active:            j.Status.Active,
		Duration:          jobDuration(j),
		Owner:             controllerOwner(j.OwnerReferences),
		CreationTimestamp: formatTimestamp(j.CreationTimestamp),
	}
}

// jobDuration reports how long a Job ran: completion − start for a finished
// job, now − start for a running one, or "" before it starts.
func jobDuration(j *batchv1.Job) string {
	if j.Status.StartTime == nil {
		return ""
	}
	start := j.Status.StartTime.Time
	end := time.Now()
	if j.Status.CompletionTime != nil {
		end = j.Status.CompletionTime.Time
	}
	d := end.Sub(start)
	if d < 0 {
		d = 0
	}
	return shortDuration(d)
}

func shapeCronJob(c *batchv1.CronJob) CronJobSummary {
	suspend := false
	if c.Spec.Suspend != nil {
		suspend = *c.Spec.Suspend
	}
	last := ""
	if c.Status.LastScheduleTime != nil {
		last = formatTimestamp(*c.Status.LastScheduleTime)
	}
	return CronJobSummary{
		Name:              c.Name,
		Namespace:         c.Namespace,
		Schedule:          c.Spec.Schedule,
		Suspend:           suspend,
		Active:            len(c.Status.Active),
		LastScheduleTime:  last,
		CreationTimestamp: formatTimestamp(c.CreationTimestamp),
	}
}

// --- Shared helpers -----------------------------------------------------------

// controllerOwner returns the controlling owner reference (controller=true), or
// the first owner reference if none is marked controller, or nil when there are
// none.
func controllerOwner(refs []metav1.OwnerReference) *OwnerRef {
	if len(refs) == 0 {
		return nil
	}
	for i := range refs {
		if refs[i].Controller != nil && *refs[i].Controller {
			return &OwnerRef{Kind: refs[i].Kind, Name: refs[i].Name, UID: string(refs[i].UID)}
		}
	}
	return &OwnerRef{Kind: refs[0].Kind, Name: refs[0].Name, UID: string(refs[0].UID)}
}

// formatTimestamp renders a metav1.Time as RFC3339 UTC, or "" when zero — the
// same wire shape the generic engine uses so the frontend age formatter is
// shared across typed and generic rows (ADR-0003).
func formatTimestamp(t metav1.Time) string {
	if t.IsZero() {
		return ""
	}
	return t.UTC().Format(time.RFC3339)
}

// shortDuration renders a duration in kubectl's compact style (e.g. 45s, 12m,
// 5h, 3d), showing the two most significant units.
func shortDuration(d time.Duration) string {
	if d < time.Second {
		return "0s"
	}
	seconds := int64(d.Seconds())
	days := seconds / 86400
	hours := (seconds % 86400) / 3600
	minutes := (seconds % 3600) / 60
	secs := seconds % 60
	switch {
	case days > 0:
		if hours > 0 {
			return fmt.Sprintf("%dd%dh", days, hours)
		}
		return fmt.Sprintf("%dd", days)
	case hours > 0:
		if minutes > 0 {
			return fmt.Sprintf("%dh%dm", hours, minutes)
		}
		return fmt.Sprintf("%dh", hours)
	case minutes > 0:
		if secs > 0 {
			return fmt.Sprintf("%dm%ds", minutes, secs)
		}
		return fmt.Sprintf("%dm", minutes)
	default:
		return fmt.Sprintf("%ds", secs)
	}
}

// writeKubeconfigUnavailable is the shared 503 for a missing/unreadable
// kubeconfig, logged once at the handler boundary.
func writeKubeconfigUnavailable(w http.ResponseWriter, logger *slog.Logger, err error) {
	logger.Error("kubeconfig unavailable", "error", err)
	writeError(w, logger, http.StatusServiceUnavailable, "kubeconfig_unavailable",
		fmt.Sprintf("cannot load kubeconfig: %v", err))
}
