package graph

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
)

func TestPodSpecRefs(t *testing.T) {
	tests := []struct {
		name string
		spec map[string]any
		want []objectRef
	}{
		{
			name: "empty spec references nothing",
			spec: map[string]any{},
		},
		{
			name: "configMap and secret volumes",
			spec: map[string]any{"volumes": []any{
				map[string]any{"name": "cfg", "configMap": map[string]any{"name": "frontend-config"}},
				map[string]any{"name": "creds", "secret": map[string]any{"secretName": "api-credentials"}},
			}},
			want: []objectRef{
				{Kind: kindConfigMap, Name: "frontend-config", Relation: RelMounts, Label: "volume"},
				{Kind: kindSecret, Name: "api-credentials", Relation: RelMounts, Label: "volume"},
			},
		},
		{
			name: "projected volume sources",
			spec: map[string]any{"volumes": []any{
				map[string]any{"name": "all", "projected": map[string]any{"sources": []any{
					map[string]any{"configMap": map[string]any{"name": "ca-bundle"}},
					map[string]any{"secret": map[string]any{"name": "token"}},
				}}},
			}},
			want: []objectRef{
				{Kind: kindConfigMap, Name: "ca-bundle", Relation: RelMounts, Label: "projected volume"},
				{Kind: kindSecret, Name: "token", Relation: RelMounts, Label: "projected volume"},
			},
		},
		{
			name: "the automounted service-account token projection is skipped whole",
			// Every pod carries this; drawing its kube-root-ca.crt would put a hub
			// node on every graph that the ServiceAccount edge already implies.
			spec: map[string]any{"volumes": []any{
				map[string]any{"name": "kube-api-access-x9k2", "projected": map[string]any{"sources": []any{
					map[string]any{"serviceAccountToken": map[string]any{"path": "token"}},
					map[string]any{"configMap": map[string]any{"name": "kube-root-ca.crt"}},
					map[string]any{"downwardAPI": map[string]any{}},
				}}},
			}},
		},
		{
			name: "a projected volume with real config is still followed",
			spec: map[string]any{"volumes": []any{
				map[string]any{"name": "app", "projected": map[string]any{"sources": []any{
					map[string]any{"configMap": map[string]any{"name": "settings"}},
				}}},
			}},
			want: []objectRef{{Kind: kindConfigMap, Name: "settings", Relation: RelMounts, Label: "projected volume"}},
		},
		{
			name: "persistentVolumeClaim volume",
			spec: map[string]any{"volumes": []any{
				map[string]any{"name": "data", "persistentVolumeClaim": map[string]any{"claimName": "data-0"}},
			}},
			want: []objectRef{{Kind: kindPVC, Name: "data-0", Relation: RelClaims, Label: "volume"}},
		},
		{
			name: "imagePullSecrets",
			spec: map[string]any{"imagePullSecrets": []any{
				map[string]any{"name": "registry-creds"},
			}},
			want: []objectRef{{Kind: kindSecret, Name: "registry-creds", Relation: RelImagePullSecret, Label: "imagePullSecret"}},
		},
		{
			name: "envFrom on a container",
			spec: map[string]any{"containers": []any{map[string]any{
				"name": "app",
				"envFrom": []any{
					map[string]any{"configMapRef": map[string]any{"name": "frontend-config"}},
					map[string]any{"secretRef": map[string]any{"name": "api-credentials"}},
				},
			}}},
			want: []objectRef{
				{Kind: kindConfigMap, Name: "frontend-config", Relation: RelEnv, Label: "envFrom"},
				{Kind: kindSecret, Name: "api-credentials", Relation: RelEnv, Label: "envFrom"},
			},
		},
		{
			name: "env.valueFrom key references, including on an init container",
			spec: map[string]any{
				"initContainers": []any{map[string]any{"name": "wait", "env": []any{
					map[string]any{"name": "HOST", "valueFrom": map[string]any{
						"configMapKeyRef": map[string]any{"name": "endpoints", "key": "host"}}},
				}}},
				"containers": []any{map[string]any{"name": "app", "env": []any{
					map[string]any{"name": "PW", "valueFrom": map[string]any{
						"secretKeyRef": map[string]any{"name": "db", "key": "password"}}},
					map[string]any{"name": "PLAIN", "value": "literal"},
				}}},
			},
			want: []objectRef{
				{Kind: kindConfigMap, Name: "endpoints", Relation: RelEnv, Label: "env"},
				{Kind: kindSecret, Name: "db", Relation: RelEnv, Label: "env"},
			},
		},
		{
			name: "serviceAccountName",
			spec: map[string]any{"serviceAccountName": "builder"},
			want: []objectRef{{Kind: kindServiceAccount, Name: "builder", Relation: RelServiceAccount, Label: "serviceAccountName"}},
		},
		{
			name: "the deprecated serviceAccount alias is only read when the current field is absent",
			spec: map[string]any{"serviceAccount": "legacy"},
			want: []objectRef{{Kind: kindServiceAccount, Name: "legacy", Relation: RelServiceAccount, Label: "serviceAccountName"}},
		},
		{
			name: "the current field wins over the alias",
			spec: map[string]any{"serviceAccountName": "current", "serviceAccount": "legacy"},
			want: []objectRef{{Kind: kindServiceAccount, Name: "current", Relation: RelServiceAccount, Label: "serviceAccountName"}},
		},
		{
			name: "malformed entries are skipped rather than panicking",
			spec: map[string]any{
				"volumes":    []any{"not-a-map", map[string]any{"configMap": map[string]any{"name": ""}}},
				"containers": []any{42, map[string]any{"envFrom": []any{map[string]any{}}}},
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			assert.Equal(t, tt.want, podSpecRefs(tt.spec))
		})
	}
}

func TestPodTemplateSpec(t *testing.T) {
	job := &unstructured.Unstructured{Object: map[string]any{
		"spec": map[string]any{"template": map[string]any{"spec": map[string]any{
			"containers": []any{map[string]any{"name": "sync"}},
		}}},
	}}
	spec := podTemplateSpec(job, "spec", "template", "spec")
	require.NotNil(t, spec)
	assert.Len(t, nestedSlice(spec, "containers"), 1)

	assert.Nil(t, podTemplateSpec(nil, "spec"))
	assert.Nil(t, podTemplateSpec(job, "spec", "missing"))
	assert.Nil(t, podTemplateSpec(&unstructured.Unstructured{Object: map[string]any{"spec": "scalar"}}, "spec"))
}
