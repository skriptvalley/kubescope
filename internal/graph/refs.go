package graph

import (
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
)

// objectRef is one same-namespace object a pod spec references, with the
// mechanism that produced it. Extraction is pure: it reads a pod spec map and
// makes no API calls, so every branch is table-testable.
type objectRef struct {
	Kind     string
	Name     string
	Relation Relation
	// Label names the mechanism ("volume", "envFrom", "env", "imagePullSecret").
	Label string
}

// podSpecRefs extracts every object a pod spec points at: ConfigMaps and
// Secrets reached through volumes (including projected sources), envFrom and
// env.valueFrom, Secrets listed in imagePullSecrets, the PersistentVolumeClaims
// its volumes bind, and its ServiceAccount.
//
// The same spec shape appears verbatim inside a Job's spec.template.spec, so
// the Job→Secret/ConfigMap edges come from this one extractor too.
func podSpecRefs(spec map[string]any) []objectRef {
	var out []objectRef
	add := func(kind, name string, rel Relation, label string) {
		if name == "" {
			return
		}
		out = append(out, objectRef{Kind: kind, Name: name, Relation: rel, Label: label})
	}

	for _, v := range nestedSlice(spec, "volumes") {
		vol, ok := v.(map[string]any)
		if !ok {
			continue
		}
		add(kindConfigMap, nestedString(vol, "configMap", "name"), RelMounts, "volume")
		add(kindSecret, nestedString(vol, "secret", "secretName"), RelMounts, "volume")
		add(kindPVC, nestedString(vol, "persistentVolumeClaim", "claimName"), RelClaims, "volume")
		// A projected volume nests the same references one level down. The one
		// exception is the service-account token the apiserver automounts into
		// *every* pod: its kube-root-ca.crt ConfigMap would become a hub node on
		// every graph, saying nothing the Pod→ServiceAccount edge does not
		// already say. A projected volume carrying a serviceAccountToken is that
		// automount, so it is skipped whole.
		sources := nestedSlice(vol, "projected", "sources")
		if projectsServiceAccountToken(sources) {
			continue
		}
		for _, s := range sources {
			src, ok := s.(map[string]any)
			if !ok {
				continue
			}
			add(kindConfigMap, nestedString(src, "configMap", "name"), RelMounts, "projected volume")
			add(kindSecret, nestedString(src, "secret", "name"), RelMounts, "projected volume")
		}
	}

	for _, p := range nestedSlice(spec, "imagePullSecrets") {
		ref, ok := p.(map[string]any)
		if !ok {
			continue
		}
		add(kindSecret, nestedString(ref, "name"), RelImagePullSecret, "imagePullSecret")
	}

	for _, field := range []string{"initContainers", "containers", "ephemeralContainers"} {
		for _, c := range nestedSlice(spec, field) {
			ctr, ok := c.(map[string]any)
			if !ok {
				continue
			}
			for _, e := range nestedSlice(ctr, "envFrom") {
				src, ok := e.(map[string]any)
				if !ok {
					continue
				}
				add(kindConfigMap, nestedString(src, "configMapRef", "name"), RelEnv, "envFrom")
				add(kindSecret, nestedString(src, "secretRef", "name"), RelEnv, "envFrom")
			}
			for _, e := range nestedSlice(ctr, "env") {
				entry, ok := e.(map[string]any)
				if !ok {
					continue
				}
				add(kindConfigMap, nestedString(entry, "valueFrom", "configMapKeyRef", "name"), RelEnv, "env")
				add(kindSecret, nestedString(entry, "valueFrom", "secretKeyRef", "name"), RelEnv, "env")
			}
		}
	}

	// serviceAccount is the deprecated alias the apiserver still mirrors; read it
	// only when the current field is absent.
	sa := nestedString(spec, "serviceAccountName")
	if sa == "" {
		sa = nestedString(spec, "serviceAccount")
	}
	add(kindServiceAccount, sa, RelServiceAccount, "serviceAccountName")

	return out
}

// projectsServiceAccountToken reports whether a projected volume's sources
// include a service-account token — the signature of the apiserver's automount.
func projectsServiceAccountToken(sources []any) bool {
	for _, s := range sources {
		src, ok := s.(map[string]any)
		if !ok {
			continue
		}
		if _, found, err := unstructured.NestedFieldNoCopy(src, "serviceAccountToken"); found && err == nil {
			return true
		}
	}
	return false
}

// podTemplateSpec returns the pod spec embedded in a workload object, or nil
// when the object carries none. Jobs (and every other templated controller)
// nest it at spec.template.spec; a Pod is its own spec.
func podTemplateSpec(obj *unstructured.Unstructured, fields ...string) map[string]any {
	if obj == nil {
		return nil
	}
	v, found, err := unstructured.NestedFieldNoCopy(obj.Object, fields...)
	if !found || err != nil {
		return nil
	}
	spec, ok := v.(map[string]any)
	if !ok {
		return nil
	}
	return spec
}

// nestedSlice reads a nested []any without the deep copy NestedSlice performs;
// the builder only ever reads these values.
func nestedSlice(obj map[string]any, fields ...string) []any {
	v, found, err := unstructured.NestedFieldNoCopy(obj, fields...)
	if !found || err != nil {
		return nil
	}
	items, ok := v.([]any)
	if !ok {
		return nil
	}
	return items
}

// nestedString reads a nested string, treating a missing or wrongly-typed field
// as "" — every caller means "absent" by that.
func nestedString(obj map[string]any, fields ...string) string {
	s, found, err := unstructured.NestedString(obj, fields...)
	if !found || err != nil {
		return ""
	}
	return s
}

// nestedInt reads a nested integer, treating absence as 0.
func nestedInt(obj map[string]any, fields ...string) int64 {
	n, found, err := unstructured.NestedInt64(obj, fields...)
	if !found || err != nil {
		return 0
	}
	return n
}

// nestedBool reads a nested boolean, treating absence as false.
func nestedBool(obj map[string]any, fields ...string) bool {
	b, found, err := unstructured.NestedBool(obj, fields...)
	if !found || err != nil {
		return false
	}
	return b
}
