package graph

import (
	"testing"

	"github.com/stretchr/testify/assert"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
)

func object(kind string, fields map[string]any) *unstructured.Unstructured {
	o := &unstructured.Unstructured{Object: fields}
	o.SetKind(kind)
	o.SetName("x")
	return o
}

func TestNodeStatus(t *testing.T) {
	tests := []struct {
		name string
		obj  *unstructured.Unstructured
		want string
	}{
		{name: "nil object", obj: nil, want: ""},
		{
			name: "running pod",
			obj:  object(kindPod, map[string]any{"status": map[string]any{"phase": "Running"}}),
			want: "Running",
		},
		{
			name: "a blocking container reason beats the phase",
			obj: object(kindPod, map[string]any{"status": map[string]any{
				"phase": "Running",
				"containerStatuses": []any{map[string]any{
					"state": map[string]any{"waiting": map[string]any{"reason": "CrashLoopBackOff"}},
				}},
			}}),
			want: "CrashLoopBackOff",
		},
		{
			name: "normal startup is not a blocking reason",
			obj: object(kindPod, map[string]any{"status": map[string]any{
				"phase": "Pending",
				"containerStatuses": []any{map[string]any{
					"state": map[string]any{"waiting": map[string]any{"reason": "ContainerCreating"}},
				}},
			}}),
			want: "Pending",
		},
		{
			name: "an init container failure surfaces",
			obj: object(kindPod, map[string]any{"status": map[string]any{
				"phase": "Pending",
				"initContainerStatuses": []any{map[string]any{
					"state": map[string]any{"terminated": map[string]any{"reason": "Error"}},
				}},
			}}),
			want: "Error",
		},
		{
			name: "a cleanly terminated container does not mask the phase",
			obj: object(kindPod, map[string]any{"status": map[string]any{
				"phase": "Succeeded",
				"containerStatuses": []any{map[string]any{
					"state": map[string]any{"terminated": map[string]any{"reason": "Completed"}},
				}},
			}}),
			want: "Completed",
		},
		{
			name: "a pod with no status at all",
			obj:  object(kindPod, map[string]any{}),
			want: "Unknown",
		},
		{
			name: "deployment readiness",
			obj: object(kindDeployment, map[string]any{
				"spec":   map[string]any{"replicas": int64(3)},
				"status": map[string]any{"readyReplicas": int64(2)},
			}),
			want: "2/3",
		},
		{
			name: "replicas defaults to one when unset",
			obj:  object(kindDeployment, map[string]any{"status": map[string]any{"readyReplicas": int64(1)}}),
			want: "1/1",
		},
		{
			name: "daemonset readiness reads its own fields",
			obj: object(kindDaemonSet, map[string]any{"status": map[string]any{
				"numberReady": int64(2), "desiredNumberScheduled": int64(2),
			}}),
			want: "2/2",
		},
		{
			name: "completed job",
			obj: object(kindJob, map[string]any{"status": map[string]any{
				"conditions": []any{map[string]any{"type": "Complete", "status": "True"}},
			}}),
			want: "Completed",
		},
		{
			name: "failed job",
			obj: object(kindJob, map[string]any{"status": map[string]any{
				"conditions": []any{map[string]any{"type": "Failed", "status": "True"}},
			}}),
			want: "Failed",
		},
		{
			name: "a condition that is not True is ignored",
			obj: object(kindJob, map[string]any{"status": map[string]any{
				"conditions": []any{map[string]any{"type": "Complete", "status": "False"}},
				"active":     int64(1),
			}}),
			want: "Running",
		},
		{
			name: "a job still working through its completions",
			obj: object(kindJob, map[string]any{
				"spec":   map[string]any{"completions": int64(5)},
				"status": map[string]any{"succeeded": int64(2)},
			}),
			want: "2/5",
		},
		{
			name: "cronjob shows its schedule",
			obj:  object(kindCronJob, map[string]any{"spec": map[string]any{"schedule": "*/1 * * * *"}}),
			want: "*/1 * * * *",
		},
		{
			name: "a suspended cronjob says so instead",
			obj: object(kindCronJob, map[string]any{"spec": map[string]any{
				"schedule": "*/1 * * * *", "suspend": true,
			}}),
			want: "Suspended",
		},
		{
			name: "service type",
			obj:  object(kindService, map[string]any{"spec": map[string]any{"type": "NodePort"}}),
			want: "NodePort",
		},
		{
			name: "an unset service type is ClusterIP, as the API defaults it",
			obj:  object(kindService, map[string]any{}),
			want: "ClusterIP",
		},
		{
			name: "claim phase",
			obj:  object(kindPVC, map[string]any{"status": map[string]any{"phase": "Bound"}}),
			want: "Bound",
		},
		{
			name: "autoscaler replicas",
			obj: object(kindHPA, map[string]any{"status": map[string]any{
				"currentReplicas": int64(2), "desiredReplicas": int64(5),
			}}),
			want: "2/5",
		},
		{
			name: "kinds with nothing to say stay silent",
			obj:  object(kindConfigMap, map[string]any{"data": map[string]any{"a": "b"}}),
			want: "",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			assert.Equal(t, tt.want, nodeStatus(tt.obj))
		})
	}
}

func TestNodeStatusTerminatingBeatsEverything(t *testing.T) {
	pod := object(kindPod, map[string]any{"status": map[string]any{"phase": "Running"}})
	now := metav1.Now()
	pod.SetDeletionTimestamp(&now)
	assert.Equal(t, "Terminating", nodeStatus(pod))
}

func TestTallyStatuses(t *testing.T) {
	pod := func(phase string) unstructured.Unstructured {
		return *object(kindPod, map[string]any{"status": map[string]any{"phase": phase}})
	}
	tests := []struct {
		name  string
		items []unstructured.Unstructured
		want  string
	}{
		{name: "nothing", want: ""},
		{name: "one", items: []unstructured.Unstructured{pod("Running")}, want: "1 Running"},
		{
			name:  "grouped, biggest first",
			items: []unstructured.Unstructured{pod("Succeeded"), pod("Running"), pod("Succeeded")},
			want:  "2 Completed, 1 Running",
		},
		{
			name: "a failure inside a clubbed run series is never hidden",
			items: []unstructured.Unstructured{
				pod("Succeeded"), pod("Succeeded"), pod("Succeeded"), pod("Failed"),
			},
			want: "3 Completed, 1 Failed",
		},
		{
			name: "more than three distinct statuses fold into a tail",
			items: []unstructured.Unstructured{
				pod("Running"), pod("Running"), pod("Succeeded"), pod("Failed"), pod("Pending"), pod("Unknown"),
			},
			want: "2 Running, 1 Completed, 1 Failed, +2 other",
		},
		{
			name:  "kinds with no status fall back to the kind name",
			items: []unstructured.Unstructured{*object(kindConfigMap, map[string]any{})},
			want:  "1 ConfigMap",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			assert.Equal(t, tt.want, tallyStatuses(tt.items))
		})
	}
}
