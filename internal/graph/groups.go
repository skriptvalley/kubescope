package graph

// Compound grouping. A workload's own parts — its ReplicaSets, its pods (or the
// aggregate standing in for them) and the Services fronting those pods — render
// inside one box, so a Deployment reads as a single unit instead of five
// scattered circles. Nodes point at their box through Node.Parent; the boxes
// themselves are the Groups list.
//
// A group forms around a *root*: a pod controller that nothing in the graph
// owns. So a Deployment is a root while its ReplicaSet is not, and a CronJob is
// a root while the Jobs it schedules are not. Each node joins the first group
// that claims it (walk order is deterministic), which resolves the one genuinely
// ambiguous case — a Service fronting two workloads can only sit in one box.

func (b *builder) buildGroups() {
	ownsChildren := map[string][]string{}
	hasOwner := map[string]bool{}
	routes := map[string][]string{}
	for _, id := range b.edgeOrder {
		e := b.edges[id]
		switch e.Relation {
		case RelOwns:
			ownsChildren[e.Source] = append(ownsChildren[e.Source], e.Target)
			hasOwner[e.Target] = true
		case RelRoutes:
			// Undirected: a Service is pulled into the box whichever way the walk
			// discovered the pair.
			routes[e.Source] = append(routes[e.Source], e.Target)
			routes[e.Target] = append(routes[e.Target], e.Source)
		}
	}

	for _, id := range b.nodeOrder {
		root := b.nodes[id]
		if root.Aggregate || hasOwner[id] || root.Parent != "" {
			continue
		}
		if !podControllers[groupKind(root.Group, root.Kind)] {
			continue
		}

		members := descendants(ownsChildren, id)
		var fronting []string
		for _, m := range members {
			if b.nodes[m].Kind != kindPod { // an aggregate of pods counts too
				continue
			}
			for _, other := range routes[m] {
				if b.nodes[other].Kind == kindService {
					fronting = append(fronting, other)
				}
			}
		}
		members = append(members, fronting...)

		claimed := make([]string, 0, len(members))
		taken := map[string]bool{}
		for _, m := range members {
			if taken[m] || b.nodes[m].Parent != "" {
				continue
			}
			taken[m] = true
			claimed = append(claimed, m)
		}
		// A box around a single node is pure clutter — only group real clusters.
		if len(claimed) < 2 {
			continue
		}

		gid := "group/" + id
		for _, m := range claimed {
			b.nodes[m].Parent = gid
		}
		b.groups = append(b.groups, Group{ID: gid, Label: root.Name, Kind: root.Kind, Root: id})
	}
}

// descendants returns root plus everything reachable from it through ownership,
// in breadth-first order. The visited set guards against a pathological
// ownerReference cycle rather than an expected one.
func descendants(children map[string][]string, root string) []string {
	visited := map[string]bool{root: true}
	out := []string{root}
	for i := 0; i < len(out); i++ {
		for _, child := range children[out[i]] {
			if visited[child] {
				continue
			}
			visited[child] = true
			out = append(out, child)
		}
	}
	return out
}
