package graph

import (
	"fmt"
	"sort"

	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
)

// nodeStatus renders one object's compact, kind-appropriate status: the string
// the node badge shows. Tone is *not* decided here — the frontend owns the one
// status→tone classifier (ADR-0009/FB-11), and a second mapping in Go would
// drift from it.
//
// Kinds with nothing meaningful to say (ConfigMap, ServiceAccount, …) return
// "", and the node renders without a badge.
func nodeStatus(obj *unstructured.Unstructured) string {
	if obj == nil {
		return ""
	}
	// A deleted-but-finalizing object reads Terminating whatever its kind — the
	// most load-bearing fact about it.
	if obj.GetDeletionTimestamp() != nil {
		return "Terminating"
	}
	o := obj.Object
	switch obj.GetKind() {
	case kindPod:
		return podStatus(o)
	case kindDeployment, kindStatefulSet, kindReplicaSet:
		return ratio(nestedInt(o, "status", "readyReplicas"), specReplicas(o))
	case kindDaemonSet:
		return ratio(nestedInt(o, "status", "numberReady"), nestedInt(o, "status", "desiredNumberScheduled"))
	case kindJob:
		return jobStatus(o)
	case kindCronJob:
		if nestedBool(o, "spec", "suspend") {
			return "Suspended"
		}
		return nestedString(o, "spec", "schedule")
	case kindService:
		if t := nestedString(o, "spec", "type"); t != "" {
			return t
		}
		return "ClusterIP"
	case kindPVC, "PersistentVolume":
		return nestedString(o, "status", "phase")
	case kindHPA:
		return ratio(nestedInt(o, "status", "currentReplicas"), nestedInt(o, "status", "desiredReplicas"))
	default:
		return ""
	}
}

// podStatus is the kubectl-ish STATUS column: a blocking container reason wins
// over the phase (a Running pod whose container is in CrashLoopBackOff is not
// "Running"), and a succeeded pod reads Completed.
func podStatus(o map[string]any) string {
	for _, field := range []string{"initContainerStatuses", "containerStatuses"} {
		for _, s := range nestedSlice(o, "status", field) {
			cs, ok := s.(map[string]any)
			if !ok {
				continue
			}
			// ContainerCreating/PodInitializing are the normal startup path, not a
			// blocking reason — the phase (Pending) already says that.
			switch reason := nestedString(cs, "state", "waiting", "reason"); reason {
			case "", "ContainerCreating", "PodInitializing":
			default:
				return reason
			}
			switch reason := nestedString(cs, "state", "terminated", "reason"); reason {
			case "", "Completed":
			default:
				return reason
			}
		}
	}
	switch phase := nestedString(o, "status", "phase"); phase {
	case "":
		return "Unknown"
	case "Succeeded":
		return "Completed"
	default:
		return phase
	}
}

// jobStatus reports a Job's terminal condition when it has one, else its
// succeeded/completions progress.
func jobStatus(o map[string]any) string {
	for _, c := range nestedSlice(o, "status", "conditions") {
		cond, ok := c.(map[string]any)
		if !ok || nestedString(cond, "status") != "True" {
			continue
		}
		switch nestedString(cond, "type") {
		case "Complete":
			return "Completed"
		case "Failed":
			return "Failed"
		}
	}
	if active := nestedInt(o, "status", "active"); active > 0 {
		return "Running"
	}
	return ratio(nestedInt(o, "status", "succeeded"), completions(o))
}

// specReplicas reads spec.replicas, which defaults to 1 when unset.
func specReplicas(o map[string]any) int64 {
	if v, found, err := unstructured.NestedInt64(o, "spec", "replicas"); found && err == nil {
		return v
	}
	return 1
}

// completions reads a Job's spec.completions, which defaults to 1 when unset.
func completions(o map[string]any) int64 {
	if v, found, err := unstructured.NestedInt64(o, "spec", "completions"); found && err == nil {
		return v
	}
	return 1
}

func ratio(ready, desired int64) string { return fmt.Sprintf("%d/%d", ready, desired) }

// tallyStatuses summarizes a clubbed set of objects as "2 Completed, 1 Running"
// — the aggregate node's badge, so collapsing a run series never hides that one
// of the runs failed. Ordered by count then name; at most three groups, with
// the rest folded into an "+N other" tail.
func tallyStatuses(items []unstructured.Unstructured) string {
	const maxGroups = 3
	counts := map[string]int{}
	for i := range items {
		s := nodeStatus(&items[i])
		if s == "" {
			s = items[i].GetKind()
		}
		counts[s]++
	}
	type group struct {
		status string
		n      int
	}
	groups := make([]group, 0, len(counts))
	for s, n := range counts {
		groups = append(groups, group{status: s, n: n})
	}
	sort.Slice(groups, func(i, j int) bool {
		if groups[i].n != groups[j].n {
			return groups[i].n > groups[j].n
		}
		return groups[i].status < groups[j].status
	})

	out := ""
	other := 0
	for i, g := range groups {
		if i >= maxGroups {
			other += g.n
			continue
		}
		if out != "" {
			out += ", "
		}
		out += fmt.Sprintf("%d %s", g.n, g.status)
	}
	if other > 0 {
		out += fmt.Sprintf(", +%d other", other)
	}
	return out
}
