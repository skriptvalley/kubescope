package kube

import (
	"bytes"
	"os"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"k8s.io/client-go/tools/clientcmd"
	clientcmdapi "k8s.io/client-go/tools/clientcmd/api"
)

// twoContextConfig builds a valid kubeconfig defining a shared context (pointing
// at the given cluster label) and one context unique to this file. current-context
// is the unique one.
func twoContextConfig(t *testing.T, cluster, unique string) clientcmdapi.Config {
	t.Helper()
	ca := testCACert(t)
	return clientcmdapi.Config{
		Clusters:  map[string]*clientcmdapi.Cluster{cluster: {Server: "https://" + cluster + ":6443", CertificateAuthorityData: ca}},
		AuthInfos: map[string]*clientcmdapi.AuthInfo{"u": tokenAuth()},
		Contexts: map[string]*clientcmdapi.Context{
			"shared": {Cluster: cluster, AuthInfo: "u"},
			unique:   {Cluster: cluster, AuthInfo: "u"},
		},
		CurrentContext: unique,
	}
}

// writeConfigAt writes a kubeconfig into dir/name and returns the full path.
func writeConfigAt(t *testing.T, dir, name string, cfg clientcmdapi.Config) string {
	t.Helper()
	path := filepath.Join(dir, name)
	require.NoError(t, clientcmd.WriteToFile(cfg, path))
	return path
}

// TestMergePrecedenceFirstWins pins the kubectl merge semantics: with two file
// sources defining the same context name, the first source in precedence wins
// the value, and the later source's definition is reported shadowed.
func TestMergePrecedenceFirstWins(t *testing.T) {
	pathA := writeConfig(t, twoContextConfig(t, "cA", "onlyA"))
	pathB := writeConfig(t, twoContextConfig(t, "cB", "onlyB"))
	m := newManager(pathA, pathB)

	infos, err := m.Contexts()
	require.NoError(t, err)
	byName := map[string]ContextInfo{}
	for _, in := range infos {
		byName[in.Name] = in
	}
	require.Contains(t, byName, "shared")
	assert.Equal(t, "cA", byName["shared"].Cluster, "the first source in precedence wins the shared context")

	sources := m.Sources()
	require.Len(t, sources, 2)
	assert.Equal(t, []string{"onlyA", "shared"}, sources[0].Contexts, "source A contributes both of its contexts")
	assert.Empty(t, sources[0].Shadowed)
	assert.Equal(t, []string{"onlyB"}, sources[1].Contexts, "source B contributes only its unique context")
	assert.Equal(t, []string{"shared"}, sources[1].Shadowed, "source B's shared definition is shadowed by A")
}

// TestDirectoryExpansionSkipRules pins the non-recursive directory expansion:
// files are listed lexicographically with per-file classified statuses (hidden,
// unparseable, too_large, ok), subdirectories are skipped silently, and only
// parseable files feed the merge — a broken file never fails the others.
func TestDirectoryExpansionSkipRules(t *testing.T) {
	dir := t.TempDir()
	writeConfigAt(t, dir, "a.yaml", singleContextConfig(t, "kind-a"))
	require.NoError(t, os.WriteFile(filepath.Join(dir, "b-broken.yaml"), []byte("not: [valid"), 0o600))
	writeConfigAt(t, dir, ".hidden.yaml", singleContextConfig(t, "hidden-ctx"))
	require.NoError(t, os.WriteFile(filepath.Join(dir, "big.yaml"), bytes.Repeat([]byte("x"), maxKubeconfigFileBytes+1), 0o600))
	require.NoError(t, os.MkdirAll(filepath.Join(dir, "sub"), 0o755))
	writeConfigAt(t, dir, filepath.Join("sub", "nested.yaml"), singleContextConfig(t, "nested-ctx"))

	m := newManager(dir)
	sources := m.Sources()
	require.Len(t, sources, 1)
	src := sources[0]
	assert.Equal(t, kindDir, src.Kind)
	assert.Equal(t, sourceStatusOK, src.Status, "a directory with ≥1 parseable file is ok")

	// Files are listed lexicographically; the subdirectory is not listed.
	got := map[string]string{}
	for _, f := range src.Files {
		got[filepath.Base(f.Path)] = f.Status
	}
	assert.Equal(t, map[string]string{
		".hidden.yaml":  fileStatusHidden,
		"a.yaml":        fileStatusOK,
		"b-broken.yaml": fileStatusUnparseable,
		"big.yaml":      fileStatusTooLarge,
	}, got, "every non-subdir entry is classified; the subdirectory is omitted")

	// Only the one parseable, visible, right-sized file feeds the merge.
	infos, err := m.Contexts()
	require.NoError(t, err)
	require.Len(t, infos, 1)
	assert.Equal(t, "kind-a", infos[0].Name, "a broken file in the directory never fails the usable one")
}

// TestDirectoryIntraDirShadowing pins that shadowing is computed across the full
// expanded file list, including two files within the same directory defining the
// same context name — the lexicographically-earlier file wins.
func TestDirectoryIntraDirShadowing(t *testing.T) {
	dir := t.TempDir()
	writeConfigAt(t, dir, "1-first.yaml", singleContextConfig(t, "dup"))
	writeConfigAt(t, dir, "2-second.yaml", singleContextConfig(t, "dup"))

	m := newManager(dir)
	sources := m.Sources()
	require.Len(t, sources, 1)
	require.Len(t, sources[0].Files, 2)

	byName := map[string]SourceFileStatus{}
	for _, f := range sources[0].Files {
		byName[filepath.Base(f.Path)] = f
	}
	assert.Equal(t, []string{"dup"}, byName["1-first.yaml"].Contexts, "the earlier file wins the name")
	assert.Empty(t, byName["1-first.yaml"].Shadowed)
	assert.Equal(t, []string{"dup"}, byName["2-second.yaml"].Shadowed, "the later file's definition is shadowed")
	assert.Empty(t, byName["2-second.yaml"].Contexts)
}

// TestEmptyDirectorySourceIsValid pins that an empty directory is an accepted
// source (status empty) — "mount a dir, drop files in later" is the point.
func TestEmptyDirectorySourceIsValid(t *testing.T) {
	fileA := writeConfig(t, singleContextConfig(t, "alpha"))
	emptyDir := t.TempDir()
	m := newManager(fileA)

	require.NoError(t, m.AddSource(emptyDir), "an empty directory is a valid runtime source")
	sources := m.Sources()
	require.Len(t, sources, 2)
	assert.Equal(t, kindDir, sources[1].Kind)
	assert.Equal(t, sourceStatusEmpty, sources[1].Status)
	assert.Empty(t, sources[1].Files, "an empty directory lists no files")

	// The empty directory contributes nothing; the file source still serves.
	infos, err := m.Contexts()
	require.NoError(t, err)
	require.Len(t, infos, 1)
	assert.Equal(t, "alpha", infos[0].Name)
}

// TestNoUsableSourceErrorWhenAllMissing pins that a registry whose every source
// is missing resolves to a typed NoUsableSourceError carrying the per-source
// statuses, with paths and statuses in the message but never file contents.
func TestNoUsableSourceErrorWhenAllMissing(t *testing.T) {
	absentA := filepath.Join(t.TempDir(), "absent-a")
	absentB := filepath.Join(t.TempDir(), "absent-b")
	m := newManager(absentA, absentB)

	_, err := m.Contexts()
	var noUsable *NoUsableSourceError
	require.ErrorAs(t, err, &noUsable)
	require.Len(t, noUsable.Statuses, 2)
	assert.Equal(t, sourceStatusMissing, noUsable.Statuses[0].Status)
	assert.Contains(t, err.Error(), absentA)
	assert.Contains(t, err.Error(), "missing")
}

// TestSourcesReportsIDKindOrigin pins the listing metadata: a stable 12-hex id
// derived from the path, the on-disk kind, and the env origin for baseline
// sources, all in precedence order.
func TestSourcesReportsIDKindOrigin(t *testing.T) {
	fileA := writeConfig(t, singleContextConfig(t, "alpha"))
	m := newManager(fileA)

	sources := m.Sources()
	require.Len(t, sources, 1)
	assert.Equal(t, sourceID(fileA), sources[0].ID)
	assert.Len(t, sources[0].ID, 12, "the id is the first 12 hex chars of sha256(path)")
	assert.Equal(t, fileA, sources[0].Path)
	assert.Equal(t, kindFile, sources[0].Kind)
	assert.Equal(t, originEnv, sources[0].Origin)

	// A runtime add is reported with origin runtime.
	fileB := writeConfig(t, singleContextConfig(t, "beta"))
	require.NoError(t, m.AddSource(fileB))
	sources = m.Sources()
	require.Len(t, sources, 2)
	assert.Equal(t, originRuntime, sources[1].Origin)
}
