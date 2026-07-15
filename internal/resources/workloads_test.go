package resources

import (
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	appsv1 "k8s.io/api/apps/v1"
	batchv1 "k8s.io/api/batch/v1"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
)

func int32Ptr(i int32) *int32   { return &i }
func boolPtrLocal(b bool) *bool { return &b }

func running() corev1.ContainerState {
	return corev1.ContainerState{Running: &corev1.ContainerStateRunning{}}
}
func waiting(reason string) corev1.ContainerState {
	return corev1.ContainerState{Waiting: &corev1.ContainerStateWaiting{Reason: reason}}
}
func terminated(reason string, code int32) corev1.ContainerState {
	return corev1.ContainerState{Terminated: &corev1.ContainerStateTerminated{Reason: reason, ExitCode: code}}
}

func TestShapePodReadyAndRestarts(t *testing.T) {
	tests := []struct {
		name         string
		pod          corev1.Pod
		wantReady    string
		wantRestarts int32
		wantNode     string
		wantOwner    string
	}{
		{
			name: "all ready no restarts",
			pod: corev1.Pod{
				Spec: corev1.PodSpec{
					NodeName:   "node-a",
					Containers: []corev1.Container{{Name: "a"}, {Name: "b"}},
				},
				Status: corev1.PodStatus{
					ContainerStatuses: []corev1.ContainerStatus{
						{Ready: true, State: running()},
						{Ready: true, State: running()},
					},
				},
			},
			wantReady: "2/2", wantRestarts: 0, wantNode: "node-a",
		},
		{
			name: "partial ready sums restarts across init and app containers",
			pod: corev1.Pod{
				Spec: corev1.PodSpec{Containers: []corev1.Container{{Name: "a"}, {Name: "b"}, {Name: "c"}}},
				Status: corev1.PodStatus{
					InitContainerStatuses: []corev1.ContainerStatus{{RestartCount: 1, State: terminated("Completed", 0)}},
					ContainerStatuses: []corev1.ContainerStatus{
						{Ready: true, RestartCount: 2, State: running()},
						{Ready: false, RestartCount: 3, State: waiting("CrashLoopBackOff")},
						{Ready: true, RestartCount: 0, State: running()},
					},
				},
			},
			wantReady: "2/3", wantRestarts: 6,
		},
		{
			name: "not yet scheduled reads zero over spec count",
			pod: corev1.Pod{
				Spec:   corev1.PodSpec{Containers: []corev1.Container{{Name: "a"}}},
				Status: corev1.PodStatus{Phase: corev1.PodPending},
			},
			wantReady: "0/1", wantRestarts: 0,
		},
		{
			name: "controller owner surfaced",
			pod: corev1.Pod{
				ObjectMeta: metav1.ObjectMeta{
					OwnerReferences: []metav1.OwnerReference{
						{Kind: "Node", Name: "n", Controller: boolPtrLocal(false)},
						{Kind: "ReplicaSet", Name: "web-abc", Controller: boolPtrLocal(true)},
					},
				},
				Spec: corev1.PodSpec{Containers: []corev1.Container{{Name: "a"}}},
			},
			wantReady: "0/1", wantOwner: "web-abc",
		},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			got := shapePod(&tc.pod)
			assert.Equal(t, tc.wantReady, got.Ready)
			assert.Equal(t, tc.wantRestarts, got.Restarts)
			assert.Equal(t, tc.wantNode, got.Node)
			if tc.wantOwner != "" {
				if assert.NotNil(t, got.Owner) {
					assert.Equal(t, tc.wantOwner, got.Owner.Name)
				}
			}
		})
	}
}

func TestPodDisplayStatus(t *testing.T) {
	tests := []struct {
		name string
		pod  corev1.Pod
		want string
	}{
		{
			name: "running",
			pod: corev1.Pod{Status: corev1.PodStatus{
				Phase:             corev1.PodRunning,
				ContainerStatuses: []corev1.ContainerStatus{{Ready: true, State: running()}},
			}},
			want: "Running",
		},
		{
			name: "pending with no statuses falls back to phase",
			pod:  corev1.Pod{Status: corev1.PodStatus{Phase: corev1.PodPending}},
			want: "Pending",
		},
		{
			name: "crashloop surfaces waiting reason",
			pod: corev1.Pod{Status: corev1.PodStatus{
				Phase:             corev1.PodRunning,
				ContainerStatuses: []corev1.ContainerStatus{{State: waiting("CrashLoopBackOff")}},
			}},
			want: "CrashLoopBackOff",
		},
		{
			name: "completed",
			pod: corev1.Pod{Status: corev1.PodStatus{
				Phase:             corev1.PodSucceeded,
				ContainerStatuses: []corev1.ContainerStatus{{State: terminated("Completed", 0)}},
			}},
			want: "Completed",
		},
		{
			name: "terminated with exit code and no reason",
			pod: corev1.Pod{Status: corev1.PodStatus{
				Phase:             corev1.PodRunning,
				ContainerStatuses: []corev1.ContainerStatus{{State: terminated("", 137)}},
			}},
			want: "ExitCode:137",
		},
		{
			name: "init progress",
			pod: corev1.Pod{
				Spec: corev1.PodSpec{InitContainers: []corev1.Container{{Name: "i1"}, {Name: "i2"}}},
				Status: corev1.PodStatus{
					Phase: corev1.PodPending,
					InitContainerStatuses: []corev1.ContainerStatus{
						{State: waiting("PodInitializing")},
					},
				},
			},
			want: "Init:0/2",
		},
		{
			name: "init container failed",
			pod: corev1.Pod{
				Spec: corev1.PodSpec{InitContainers: []corev1.Container{{Name: "i1"}}},
				Status: corev1.PodStatus{
					Phase: corev1.PodPending,
					InitContainerStatuses: []corev1.ContainerStatus{
						{State: terminated("Error", 1)},
					},
				},
			},
			want: "Init:Error",
		},
		{
			name: "pod-level reason (Evicted) wins over phase",
			pod:  corev1.Pod{Status: corev1.PodStatus{Phase: corev1.PodFailed, Reason: "Evicted"}},
			want: "Evicted",
		},
		{
			name: "deletion timestamp yields Terminating",
			pod: corev1.Pod{
				ObjectMeta: metav1.ObjectMeta{DeletionTimestamp: &metav1.Time{Time: time.Now()}},
				Status: corev1.PodStatus{
					Phase:             corev1.PodRunning,
					ContainerStatuses: []corev1.ContainerStatus{{Ready: true, State: running()}},
				},
			},
			want: "Terminating",
		},
		{
			name: "terminal pod being garbage-collected keeps its terminal status",
			pod: corev1.Pod{
				ObjectMeta: metav1.ObjectMeta{DeletionTimestamp: &metav1.Time{Time: time.Now()}},
				Status: corev1.PodStatus{
					Phase:             corev1.PodSucceeded,
					ContainerStatuses: []corev1.ContainerStatus{{State: terminated("Completed", 0)}},
				},
			},
			want: "Completed",
		},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			assert.Equal(t, tc.want, podDisplayStatus(&tc.pod))
		})
	}
}

func TestShapeDeploymentAndRollout(t *testing.T) {
	tests := []struct {
		name          string
		dep           appsv1.Deployment
		wantReady     string
		wantUpdated   int32
		wantAvailable int32
		wantRollout   string
	}{
		{
			name: "fully rolled out",
			dep: appsv1.Deployment{
				ObjectMeta: metav1.ObjectMeta{Generation: 2},
				Spec:       appsv1.DeploymentSpec{Replicas: int32Ptr(3)},
				Status: appsv1.DeploymentStatus{
					ObservedGeneration: 2, Replicas: 3, ReadyReplicas: 3, UpdatedReplicas: 3, AvailableReplicas: 3,
				},
			},
			wantReady: "3/3", wantUpdated: 3, wantAvailable: 3,
			wantRollout: "Deployment successfully rolled out",
		},
		{
			name: "spec not yet observed",
			dep: appsv1.Deployment{
				ObjectMeta: metav1.ObjectMeta{Generation: 5},
				Spec:       appsv1.DeploymentSpec{Replicas: int32Ptr(3)},
				Status:     appsv1.DeploymentStatus{ObservedGeneration: 4},
			},
			wantReady:   "0/3",
			wantRollout: "Waiting for deployment spec update to be observed",
		},
		{
			name: "updating replicas",
			dep: appsv1.Deployment{
				ObjectMeta: metav1.ObjectMeta{Generation: 2},
				Spec:       appsv1.DeploymentSpec{Replicas: int32Ptr(4)},
				Status: appsv1.DeploymentStatus{
					ObservedGeneration: 2, Replicas: 4, ReadyReplicas: 2, UpdatedReplicas: 2, AvailableReplicas: 2,
				},
			},
			wantReady: "2/4", wantUpdated: 2, wantAvailable: 2,
			wantRollout: "Waiting for rollout to finish: 2 out of 4 new replicas have been updated",
		},
		{
			name: "old replicas pending termination",
			dep: appsv1.Deployment{
				ObjectMeta: metav1.ObjectMeta{Generation: 2},
				Spec:       appsv1.DeploymentSpec{Replicas: int32Ptr(3)},
				Status: appsv1.DeploymentStatus{
					ObservedGeneration: 2, Replicas: 5, ReadyReplicas: 3, UpdatedReplicas: 3, AvailableReplicas: 3,
				},
			},
			wantReady: "3/3", wantUpdated: 3, wantAvailable: 3,
			wantRollout: "Waiting for rollout to finish: 2 old replicas are pending termination",
		},
		{
			name: "updated but not yet available",
			dep: appsv1.Deployment{
				ObjectMeta: metav1.ObjectMeta{Generation: 2},
				Spec:       appsv1.DeploymentSpec{Replicas: int32Ptr(3)},
				Status: appsv1.DeploymentStatus{
					ObservedGeneration: 2, Replicas: 3, ReadyReplicas: 1, UpdatedReplicas: 3, AvailableReplicas: 1,
				},
			},
			wantReady: "1/3", wantUpdated: 3, wantAvailable: 1,
			wantRollout: "Waiting for rollout to finish: 1 of 3 updated replicas are available",
		},
		{
			name: "progress deadline exceeded",
			dep: appsv1.Deployment{
				ObjectMeta: metav1.ObjectMeta{Name: "web", Generation: 2},
				Spec:       appsv1.DeploymentSpec{Replicas: int32Ptr(3)},
				Status: appsv1.DeploymentStatus{
					ObservedGeneration: 2, Replicas: 3, UpdatedReplicas: 1,
					Conditions: []appsv1.DeploymentCondition{
						{Type: appsv1.DeploymentProgressing, Reason: "ProgressDeadlineExceeded"},
					},
				},
			},
			wantReady: "0/3", wantUpdated: 1,
			wantRollout: `deployment "web" exceeded its progress deadline`,
		},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			got := shapeDeployment(&tc.dep)
			assert.Equal(t, tc.wantReady, got.Ready)
			assert.Equal(t, tc.wantUpdated, got.UpdatedReplicas)
			assert.Equal(t, tc.wantAvailable, got.AvailableReplicas)
			assert.Equal(t, tc.wantRollout, got.RolloutStatus)
		})
	}
}

func TestShapeStatefulSet(t *testing.T) {
	ss := appsv1.StatefulSet{
		ObjectMeta: metav1.ObjectMeta{Generation: 1},
		Spec: appsv1.StatefulSetSpec{
			Replicas:       int32Ptr(3),
			UpdateStrategy: appsv1.StatefulSetUpdateStrategy{Type: appsv1.RollingUpdateStatefulSetStrategyType},
		},
		Status: appsv1.StatefulSetStatus{
			ObservedGeneration: 1, ReadyReplicas: 3, CurrentReplicas: 3, UpdatedReplicas: 3,
			CurrentRevision: "rev-1", UpdateRevision: "rev-1",
		},
	}
	got := shapeStatefulSet(&ss)
	assert.Equal(t, "3/3", got.Ready)
	assert.Equal(t, int32(3), got.DesiredReplicas)
	assert.Equal(t, "StatefulSet successfully rolled out", got.RolloutStatus)

	// Not enough ready replicas.
	ss.Status.ReadyReplicas = 1
	assert.Equal(t, "Waiting for 2 pods to be ready", shapeStatefulSet(&ss).RolloutStatus)

	// A partitioned rollout is "complete" once the at-or-above-partition pods are
	// updated, even though lower ordinals stay on the old revision.
	ss.Status.ReadyReplicas = 3
	ss.Spec.UpdateStrategy.RollingUpdate = &appsv1.RollingUpdateStatefulSetStrategy{Partition: int32Ptr(2)}
	ss.Status.CurrentRevision = "rev-1"
	ss.Status.UpdateRevision = "rev-2" // differs, so the non-partition path would wait
	ss.Status.UpdatedReplicas = 1      // want = replicas(3) - partition(2) = 1
	assert.Equal(t, "Partitioned rollout complete: 1 new pods have been updated",
		shapeStatefulSet(&ss).RolloutStatus)

	ss.Status.UpdatedReplicas = 0
	assert.Equal(t, "Waiting for partitioned rollout to finish: 0 out of 1 new pods have been updated",
		shapeStatefulSet(&ss).RolloutStatus)
	ss.Spec.UpdateStrategy.RollingUpdate = nil

	// OnDelete is reported as unmanaged.
	ss.Spec.UpdateStrategy.Type = appsv1.OnDeleteStatefulSetStrategyType
	assert.Contains(t, shapeStatefulSet(&ss).RolloutStatus, "OnDelete")
}

func TestShapeDaemonSet(t *testing.T) {
	ds := appsv1.DaemonSet{
		ObjectMeta: metav1.ObjectMeta{Generation: 1},
		Spec:       appsv1.DaemonSetSpec{UpdateStrategy: appsv1.DaemonSetUpdateStrategy{Type: appsv1.RollingUpdateDaemonSetStrategyType}},
		Status: appsv1.DaemonSetStatus{
			ObservedGeneration: 1, DesiredNumberScheduled: 3, CurrentNumberScheduled: 3,
			NumberReady: 3, UpdatedNumberScheduled: 3, NumberAvailable: 3,
		},
	}
	got := shapeDaemonSet(&ds)
	assert.Equal(t, int32(3), got.Desired)
	assert.Equal(t, int32(3), got.Ready)
	assert.Equal(t, "DaemonSet successfully rolled out", got.RolloutStatus)

	ds.Status.UpdatedNumberScheduled = 1
	assert.Equal(t, "Waiting for daemon set rollout to finish: 1 out of 3 new pods have been updated",
		shapeDaemonSet(&ds).RolloutStatus)

	ds.Status.UpdatedNumberScheduled = 3
	ds.Status.NumberAvailable = 2
	assert.Equal(t, "Waiting for daemon set rollout to finish: 2 of 3 updated pods are available",
		shapeDaemonSet(&ds).RolloutStatus)

	// A spec change not yet observed by the controller.
	ds.Status.NumberAvailable = 3
	ds.ObjectMeta.Generation = 2
	ds.Status.ObservedGeneration = 1
	assert.Equal(t, "Waiting for daemon set spec update to be observed", shapeDaemonSet(&ds).RolloutStatus)
	ds.Status.ObservedGeneration = 2

	// OnDelete is reported as unmanaged.
	ds.Spec.UpdateStrategy.Type = appsv1.OnDeleteDaemonSetStrategyType
	assert.Contains(t, shapeDaemonSet(&ds).RolloutStatus, "OnDelete")
}

func TestShapeReplicaSet(t *testing.T) {
	rs := appsv1.ReplicaSet{
		ObjectMeta: metav1.ObjectMeta{
			OwnerReferences: []metav1.OwnerReference{{Kind: "Deployment", Name: "web", Controller: boolPtrLocal(true)}},
		},
		Spec:   appsv1.ReplicaSetSpec{Replicas: int32Ptr(3)},
		Status: appsv1.ReplicaSetStatus{Replicas: 3, ReadyReplicas: 2},
	}
	got := shapeReplicaSet(&rs)
	assert.Equal(t, "2/3", got.Ready)
	assert.Equal(t, int32(3), got.CurrentReplicas)
	if assert.NotNil(t, got.Owner) {
		assert.Equal(t, "Deployment", got.Owner.Kind)
		assert.Equal(t, "web", got.Owner.Name)
	}
}

func TestShapeJob(t *testing.T) {
	start := metav1.NewTime(time.Now().Add(-90 * time.Second))
	done := metav1.NewTime(start.Add(30 * time.Second))
	tests := []struct {
		name            string
		job             batchv1.Job
		wantCompletions string
		wantDuration    string
	}{
		{
			name: "finished parallel job",
			job: batchv1.Job{
				Spec:   batchv1.JobSpec{Completions: int32Ptr(3)},
				Status: batchv1.JobStatus{Succeeded: 3, StartTime: &start, CompletionTime: &done},
			},
			wantCompletions: "3/3", wantDuration: "30s",
		},
		{
			name: "unset completions renders over one",
			job: batchv1.Job{
				Status: batchv1.JobStatus{Succeeded: 1, StartTime: &start, CompletionTime: &done},
			},
			wantCompletions: "1/1", wantDuration: "30s",
		},
		{
			name:            "not started has no duration",
			job:             batchv1.Job{Spec: batchv1.JobSpec{Completions: int32Ptr(1)}},
			wantCompletions: "0/1", wantDuration: "",
		},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			got := shapeJob(&tc.job)
			assert.Equal(t, tc.wantCompletions, got.Completions)
			assert.Equal(t, tc.wantDuration, got.Duration)
		})
	}
}

func TestShapeCronJob(t *testing.T) {
	last := metav1.NewTime(time.Date(2026, 7, 15, 10, 0, 0, 0, time.UTC))
	cj := batchv1.CronJob{
		Spec: batchv1.CronJobSpec{Schedule: "*/5 * * * *", Suspend: boolPtrLocal(true)},
		Status: batchv1.CronJobStatus{
			Active:           []corev1.ObjectReference{{Name: "job-1"}, {Name: "job-2"}},
			LastScheduleTime: &last,
		},
	}
	got := shapeCronJob(&cj)
	assert.Equal(t, "*/5 * * * *", got.Schedule)
	assert.True(t, got.Suspend)
	assert.Equal(t, 2, got.Active)
	assert.Equal(t, "2026-07-15T10:00:00Z", got.LastScheduleTime)

	// Nil suspend defaults to false; nil last-schedule renders empty.
	cj.Spec.Suspend = nil
	cj.Status.LastScheduleTime = nil
	got = shapeCronJob(&cj)
	assert.False(t, got.Suspend)
	assert.Equal(t, "", got.LastScheduleTime)
}

func TestShortDuration(t *testing.T) {
	tests := []struct {
		d    time.Duration
		want string
	}{
		{500 * time.Millisecond, "0s"},
		{45 * time.Second, "45s"},
		{90 * time.Second, "1m30s"},
		{5 * time.Minute, "5m"},
		{2*time.Hour + 15*time.Minute, "2h15m"},
		{3 * time.Hour, "3h"},
		{50 * time.Hour, "2d2h"},
		{72 * time.Hour, "3d"},
	}
	for _, tc := range tests {
		assert.Equal(t, tc.want, shortDuration(tc.d), "duration %s", tc.d)
	}
}

func TestFormatTimestamp(t *testing.T) {
	assert.Equal(t, "", formatTimestamp(metav1.Time{}))
	ts := metav1.NewTime(time.Date(2026, 7, 15, 9, 30, 0, 0, time.UTC))
	assert.Equal(t, "2026-07-15T09:30:00Z", formatTimestamp(ts))
}
