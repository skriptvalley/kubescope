package resources

import (
	"fmt"
	"sort"
	"strings"

	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"k8s.io/apimachinery/pkg/runtime/schema"
)

// Per-kind list-column enrichment (Sprint 7). ADR-0003 puts the list-column
// config server-side: the generic engine emits a sane default (name, namespace,
// age), and here well-known config/networking/RBAC/storage kinds get extra
// kind-appropriate columns computed from the unstructured object. The frontend
// stays thin — it renders whatever columns the API describes and reads each
// extra cell by column id (ResourceRow.cells). The same enrichment feeds the
// REST list (shapeList) and the watch→SSE stream (genericStreamRow) so a live
// update carries the identical row shape (ADR-0006).
//
// Secret enrichment reads only key *counts* and the type — never a data value —
// so nothing under ADR-0005 masking leaks into a list row.

// columnSpec is one extra list column: its id/header plus a pure extractor over
// the object. Extractors must tolerate a missing/oddly-shaped field (returning
// "" or a dash) so a malformed object never panics the list.
type columnSpec struct {
	id     string
	header string
	value  func(u *unstructured.Unstructured) string
}

// enrichedColumns maps "group/resource" to its extra columns. The core group is
// the empty string, so its keys read like "/services". Keyed by group+resource
// (version-insensitive) so the preferred version discovery reports still match.
var enrichedColumns = map[string][]columnSpec{
	"/configmaps": {
		{"data", "Data", func(u *unstructured.Unstructured) string { return itoa(dataKeyCount(u)) }},
	},
	"/secrets": {
		{"type", "Type", func(u *unstructured.Unstructured) string { return usString(u, "type") }},
		{"data", "Data", func(u *unstructured.Unstructured) string { return itoa(dataKeyCount(u)) }},
	},
	"/services": {
		{"type", "Type", serviceType},
		{"cluster-ip", "Cluster-IP", func(u *unstructured.Unstructured) string { return dash(usString(u, "spec", "clusterIP")) }},
		{"ports", "Ports", servicePortsSummary},
		{"selector", "Selector", func(u *unstructured.Unstructured) string { return dash(selectorSummary(u, "spec", "selector")) }},
	},
	"networking.k8s.io/ingresses": {
		{"hosts", "Hosts", ingressHosts},
		{"paths", "Paths", ingressPaths},
		{"backends", "Backends", ingressBackends},
	},
	"/persistentvolumeclaims": {
		{"status", "Status", func(u *unstructured.Unstructured) string { return dash(usString(u, "status", "phase")) }},
		{"volume", "Volume", func(u *unstructured.Unstructured) string { return dash(usString(u, "spec", "volumeName")) }},
		{"capacity", "Capacity", func(u *unstructured.Unstructured) string { return dash(usString(u, "status", "capacity", "storage")) }},
		{"access-modes", "Access Modes", func(u *unstructured.Unstructured) string { return accessModesSummary(u, "spec", "accessModes") }},
		{"storageclass", "StorageClass", func(u *unstructured.Unstructured) string { return dash(usString(u, "spec", "storageClassName")) }},
	},
	"/persistentvolumes": {
		{"capacity", "Capacity", func(u *unstructured.Unstructured) string { return dash(usString(u, "spec", "capacity", "storage")) }},
		{"access-modes", "Access Modes", func(u *unstructured.Unstructured) string { return accessModesSummary(u, "spec", "accessModes") }},
		{"reclaim-policy", "Reclaim Policy", func(u *unstructured.Unstructured) string {
			return dash(usString(u, "spec", "persistentVolumeReclaimPolicy"))
		}},
		{"status", "Status", func(u *unstructured.Unstructured) string { return dash(usString(u, "status", "phase")) }},
		{"claim", "Claim", pvClaim},
		{"storageclass", "StorageClass", func(u *unstructured.Unstructured) string { return dash(usString(u, "spec", "storageClassName")) }},
	},
	"storage.k8s.io/storageclasses": {
		{"provisioner", "Provisioner", func(u *unstructured.Unstructured) string { return dash(usString(u, "provisioner")) }},
		{"reclaim-policy", "Reclaim Policy", func(u *unstructured.Unstructured) string { return valueOr(usString(u, "reclaimPolicy"), "Delete") }},
		{"binding-mode", "Binding Mode", func(u *unstructured.Unstructured) string {
			return valueOr(usString(u, "volumeBindingMode"), "Immediate")
		}},
		{"default", "Default", storageClassDefault},
	},
	"/serviceaccounts": {
		{"secrets", "Secrets", func(u *unstructured.Unstructured) string { return itoa(sliceLen(u, "secrets")) }},
	},
	"rbac.authorization.k8s.io/roles": {
		{"rules", "Rules", func(u *unstructured.Unstructured) string { return itoa(sliceLen(u, "rules")) }},
	},
	"rbac.authorization.k8s.io/clusterroles": {
		{"rules", "Rules", func(u *unstructured.Unstructured) string { return itoa(sliceLen(u, "rules")) }},
	},
	"rbac.authorization.k8s.io/rolebindings": {
		{"role", "Role", roleRefSummary},
	},
	"rbac.authorization.k8s.io/clusterrolebindings": {
		{"role", "Role", roleRefSummary},
	},
}

// enrichmentFor returns the extra columns for a GVR, or nil for kinds with no
// enrichment (which then render the default name/namespace/age set).
func enrichmentFor(gvr schema.GroupVersionResource) []columnSpec {
	return enrichedColumns[gvr.Group+"/"+gvr.Resource]
}

// enrichRow computes the extra cell values for one object under the given
// column specs, or nil when there are none (so the row omits the cells map).
func enrichRow(specs []columnSpec, u *unstructured.Unstructured) map[string]string {
	if len(specs) == 0 {
		return nil
	}
	cells := make(map[string]string, len(specs))
	for _, c := range specs {
		cells[c.id] = c.value(u)
	}
	return cells
}

// --- Extractors ---------------------------------------------------------------

func serviceType(u *unstructured.Unstructured) string {
	return valueOr(usString(u, "spec", "type"), "ClusterIP")
}

// servicePortsSummary renders spec.ports as "80/TCP, 443/TCP" (kubectl style).
func servicePortsSummary(u *unstructured.Unstructured) string {
	ports, ok, _ := unstructured.NestedSlice(u.Object, "spec", "ports")
	if !ok || len(ports) == 0 {
		return "—"
	}
	parts := make([]string, 0, len(ports))
	for _, p := range ports {
		m, ok := p.(map[string]any)
		if !ok {
			continue
		}
		port := intFromAny(m["port"])
		proto, _ := m["protocol"].(string)
		if proto == "" {
			proto = "TCP"
		}
		parts = append(parts, fmt.Sprintf("%d/%s", port, proto))
	}
	if len(parts) == 0 {
		return "—"
	}
	return strings.Join(parts, ", ")
}

// selectorSummary renders a label map at path as sorted "k=v,k=v", or "" when
// empty/absent.
func selectorSummary(u *unstructured.Unstructured, fields ...string) string {
	m, ok, _ := unstructured.NestedStringMap(u.Object, fields...)
	if !ok || len(m) == 0 {
		return ""
	}
	keys := make([]string, 0, len(m))
	for k := range m {
		keys = append(keys, k)
	}
	sort.Strings(keys)
	parts := make([]string, 0, len(keys))
	for _, k := range keys {
		parts = append(parts, k+"="+m[k])
	}
	return strings.Join(parts, ",")
}

// accessModesSummary renders spec.accessModes as short codes ("RWO, RWX").
func accessModesSummary(u *unstructured.Unstructured, fields ...string) string {
	modes, ok, _ := unstructured.NestedStringSlice(u.Object, fields...)
	if !ok || len(modes) == 0 {
		return "—"
	}
	parts := make([]string, 0, len(modes))
	for _, m := range modes {
		parts = append(parts, shortAccessMode(m))
	}
	return strings.Join(parts, ", ")
}

func shortAccessMode(m string) string {
	switch m {
	case "ReadWriteOnce":
		return "RWO"
	case "ReadOnlyMany":
		return "ROX"
	case "ReadWriteMany":
		return "RWX"
	case "ReadWriteOncePod":
		return "RWOP"
	default:
		return m
	}
}

// pvClaim renders a PV's bound claim as "namespace/name", or "—" when unbound.
func pvClaim(u *unstructured.Unstructured) string {
	ns := usString(u, "spec", "claimRef", "namespace")
	name := usString(u, "spec", "claimRef", "name")
	if name == "" {
		return "—"
	}
	if ns == "" {
		return name
	}
	return ns + "/" + name
}

// storageClassDefault reports "Yes" when the class carries the default-class
// annotation (stable or beta form), else "".
func storageClassDefault(u *unstructured.Unstructured) string {
	anns := u.GetAnnotations()
	if anns["storageclass.kubernetes.io/is-default-class"] == "true" ||
		anns["storageclass.beta.kubernetes.io/is-default-class"] == "true" {
		return "Yes"
	}
	return ""
}

// roleRefSummary renders a binding's roleRef as "Kind/name".
func roleRefSummary(u *unstructured.Unstructured) string {
	kind := usString(u, "roleRef", "kind")
	name := usString(u, "roleRef", "name")
	if name == "" {
		return "—"
	}
	if kind == "" {
		return name
	}
	return kind + "/" + name
}

// ingressHosts renders the distinct rule hosts, "*" for a catch-all rule.
func ingressHosts(u *unstructured.Unstructured) string {
	rules, ok, _ := unstructured.NestedSlice(u.Object, "spec", "rules")
	if !ok || len(rules) == 0 {
		return "*"
	}
	var hosts []string
	seen := map[string]bool{}
	for _, r := range rules {
		m, ok := r.(map[string]any)
		if !ok {
			continue
		}
		host, _ := m["host"].(string)
		if host == "" {
			host = "*"
		}
		if !seen[host] {
			seen[host] = true
			hosts = append(hosts, host)
		}
	}
	if len(hosts) == 0 {
		return "*"
	}
	return strings.Join(hosts, ", ")
}

// ingressPaths renders the distinct HTTP paths across all rules.
func ingressPaths(u *unstructured.Unstructured) string {
	var paths []string
	seen := map[string]bool{}
	for _, p := range ingressHTTPPaths(u) {
		path, _ := p["path"].(string)
		if path == "" {
			path = "/"
		}
		if !seen[path] {
			seen[path] = true
			paths = append(paths, path)
		}
	}
	if len(paths) == 0 {
		return "—"
	}
	return strings.Join(paths, ", ")
}

// ingressBackends renders the distinct backend services as "svc:port".
func ingressBackends(u *unstructured.Unstructured) string {
	var backends []string
	seen := map[string]bool{}
	add := func(b string) {
		if b != "" && !seen[b] {
			seen[b] = true
			backends = append(backends, b)
		}
	}
	// Rule-level backends.
	for _, p := range ingressHTTPPaths(u) {
		add(ingressBackendString(p["backend"]))
	}
	// Default backend (no host/path match).
	if def, ok, _ := unstructured.NestedMap(u.Object, "spec", "defaultBackend"); ok {
		add(ingressBackendString(def))
	}
	if len(backends) == 0 {
		return "—"
	}
	return strings.Join(backends, ", ")
}

// ingressHTTPPaths flattens every rule's http.paths entries into a slice of
// path maps, tolerating rules without an http block.
func ingressHTTPPaths(u *unstructured.Unstructured) []map[string]any {
	rules, ok, _ := unstructured.NestedSlice(u.Object, "spec", "rules")
	if !ok {
		return nil
	}
	var out []map[string]any
	for _, r := range rules {
		rm, ok := r.(map[string]any)
		if !ok {
			continue
		}
		http, ok := rm["http"].(map[string]any)
		if !ok {
			continue
		}
		paths, ok := http["paths"].([]any)
		if !ok {
			continue
		}
		for _, p := range paths {
			if pm, ok := p.(map[string]any); ok {
				out = append(out, pm)
			}
		}
	}
	return out
}

// ingressBackendString renders a networking.k8s.io/v1 backend as "svc:port".
func ingressBackendString(backend any) string {
	m, ok := backend.(map[string]any)
	if !ok {
		return ""
	}
	svc, ok := m["service"].(map[string]any)
	if !ok {
		return ""
	}
	name, _ := svc["name"].(string)
	if name == "" {
		return ""
	}
	port, ok := svc["port"].(map[string]any)
	if !ok {
		return name
	}
	if n := intFromAny(port["number"]); n != 0 {
		return fmt.Sprintf("%s:%d", name, n)
	}
	if pn, _ := port["name"].(string); pn != "" {
		return name + ":" + pn
	}
	return name
}

// --- Small unstructured helpers -----------------------------------------------

// usString reads a nested string, returning "" on any miss (absent or wrong
// type) — extractors treat that as "no value".
func usString(u *unstructured.Unstructured, fields ...string) string {
	s, _, _ := unstructured.NestedString(u.Object, fields...)
	return s
}

// dataKeyCount counts a ConfigMap/Secret's data keys (data + binaryData), never
// reading a value — safe for Secrets under ADR-0005.
func dataKeyCount(u *unstructured.Unstructured) int {
	n := 0
	if m, ok, _ := unstructured.NestedMap(u.Object, "data"); ok {
		n += len(m)
	}
	if m, ok, _ := unstructured.NestedMap(u.Object, "binaryData"); ok {
		n += len(m)
	}
	return n
}

// sliceLen counts entries in a top-level slice field (e.g. a ServiceAccount's
// secrets, a Role's rules).
func sliceLen(u *unstructured.Unstructured, fields ...string) int {
	s, ok, _ := unstructured.NestedSlice(u.Object, fields...)
	if !ok {
		return 0
	}
	return len(s)
}

// intFromAny coerces an unstructured numeric (int64 or float64 after JSON
// round-trip) to int, tolerating anything else as 0.
func intFromAny(v any) int64 {
	switch n := v.(type) {
	case int64:
		return n
	case float64:
		return int64(n)
	case int:
		return int64(n)
	default:
		return 0
	}
}

func itoa(n int) string { return fmt.Sprintf("%d", n) }

// dash returns "—" for an empty string, else the value.
func dash(s string) string { return valueOr(s, "—") }

// valueOr returns fallback when s is empty, else s.
func valueOr(s, fallback string) string {
	if s == "" {
		return fallback
	}
	return s
}
