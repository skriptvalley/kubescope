package kube

import (
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"

	"k8s.io/client-go/tools/clientcmd"
	clientcmdapi "k8s.io/client-go/tools/clientcmd/api"
)

// maxKubeconfigFileBytes bounds a directory-source file the expander will parse:
// a kubeconfig is small, so anything larger is almost certainly not one and is
// skipped (status too_large) rather than read into memory (ADR-0008).
const maxKubeconfigFileBytes = 1 << 20 // 1 MiB

// Source-level status values (SourceStatus.Status). A file source is
// missing|unparseable|empty|ok; a directory source is missing|empty|ok.
const (
	sourceStatusOK          = "ok"
	sourceStatusMissing     = "missing"
	sourceStatusUnparseable = "unparseable"
	sourceStatusEmpty       = "empty"
)

// Per-file status values (SourceFileStatus.Status) for the files inside a
// directory source. An empty-but-parseable file is "ok" here — it parses and
// feeds the merge, it simply contributes no contexts.
const (
	fileStatusOK          = "ok"
	fileStatusUnparseable = "unparseable"
	fileStatusTooLarge    = "too_large"
	fileStatusHidden      = "hidden"
)

const (
	kindFile = "file"
	kindDir  = "dir"

	originEnv     = "env"
	originRuntime = "runtime"
)

// registrySource is one entry of the in-memory kubeconfig source registry: a
// path (file or directory) plus where it came from (env baseline or a runtime
// add). Origin is reported in the API and is otherwise cosmetic — env and
// runtime sources are treated identically by the loader (ADR-0008).
type registrySource struct {
	path   string
	origin string
}

// SourceFileStatus describes one file inside a directory source: its status and,
// for a parseable file, the context names whose winning definition it provides
// and those it defines but that an earlier file already won (shadowed).
type SourceFileStatus struct {
	Path     string   `json:"path"`
	Status   string   `json:"status"`
	Message  string   `json:"message,omitempty"`
	Contexts []string `json:"contexts,omitempty"`
	Shadowed []string `json:"shadowed,omitempty"`
}

// SourceStatus describes one registered kubeconfig source for the API: its
// stable id, path, kind (file|dir), origin (env|runtime), current on-disk
// status, per-file expansion (directories only), and the context names it
// contributes to / has shadowed in the merged config.
type SourceStatus struct {
	ID       string             `json:"id"`
	Path     string             `json:"path"`
	Kind     string             `json:"kind"`
	Origin   string             `json:"origin"`
	Status   string             `json:"status"`
	Message  string             `json:"message,omitempty"`
	Files    []SourceFileStatus `json:"files,omitempty"`
	Contexts []string           `json:"contexts,omitempty"`
	Shadowed []string           `json:"shadowed,omitempty"`
}

// sourceID is the stable, URL-safe identifier for a source path: the first 12
// hex chars of its sha256. It is derived from the path alone so the same source
// always maps to the same id across requests and restarts.
func sourceID(path string) string {
	sum := sha256.Sum256([]byte(path))
	return hex.EncodeToString(sum[:])[:12]
}

// candidate is one usable file in the merge precedence order, carrying the
// context names it defines and a back-reference to the SourceStatus (and, for a
// directory file, the index of its SourceFileStatus) so the first-wins walk can
// attribute contributed/shadowed names back to the listing.
type candidate struct {
	path     string
	contexts []string // sorted context names defined in this file
	srcIdx   int
	fileIdx  int // -1 when the candidate IS a file source (attribute to the source)
}

// expandSources stats each source, expands directories (non-recursive,
// lexicographic, per-file classified), parses each candidate file once, and
// computes contributed/shadowed context names by a first-wins walk over the full
// expanded file list in precedence order. It takes a plain snapshot and never
// touches Manager state or locks, so callers holding m.mu can invoke it without
// a re-lock deadlock (mirroring the pre-registry path-passing pattern). It
// returns the per-source statuses and the usable file paths (dir files with
// status ok, plus file sources that parse) in precedence order — the exact list
// fed to clientcmd's Precedence merge.
func expandSources(sources []registrySource) ([]SourceStatus, []string) {
	statuses := make([]SourceStatus, len(sources))
	var candidates []candidate

	for i, src := range sources {
		st := SourceStatus{
			ID:     sourceID(src.path),
			Path:   src.path,
			Origin: src.origin,
			Kind:   kindFile, // default when missing; overridden for a directory
		}
		info, err := os.Stat(src.path)
		if err != nil {
			st.Status = sourceStatusMissing
			st.Message = err.Error()
			statuses[i] = st
			continue
		}
		if info.IsDir() {
			st.Kind = kindDir
			expandDir(&st, src.path, i, &candidates)
		} else {
			expandFile(&st, src.path, i, &candidates)
		}
		statuses[i] = st
	}

	// First-wins walk across the full expanded file list in precedence order:
	// the first file to define a context name contributes it; later files that
	// also define it have that name shadowed. Attribution flows back to the
	// per-file listing (dir sources) and to every owning source.
	seen := make(map[string]bool)
	for _, c := range candidates {
		var won, shadowed []string
		for _, name := range c.contexts {
			if seen[name] {
				shadowed = append(shadowed, name)
			} else {
				seen[name] = true
				won = append(won, name)
			}
		}
		if c.fileIdx >= 0 {
			statuses[c.srcIdx].Files[c.fileIdx].Contexts = won
			statuses[c.srcIdx].Files[c.fileIdx].Shadowed = shadowed
		}
		statuses[c.srcIdx].Contexts = append(statuses[c.srcIdx].Contexts, won...)
		statuses[c.srcIdx].Shadowed = append(statuses[c.srcIdx].Shadowed, shadowed...)
	}

	files := make([]string, 0, len(candidates))
	for _, c := range candidates {
		files = append(files, c.path)
	}
	return statuses, files
}

// expandDir enumerates a directory source non-recursively in lexicographic order
// (os.ReadDir sorts by name). Subdirectories are skipped silently; hidden files,
// oversized files and unparseable files get a per-file classified status but do
// not feed the merge. Every parseable file becomes a merge candidate.
func expandDir(st *SourceStatus, dir string, srcIdx int, candidates *[]candidate) {
	entries, err := os.ReadDir(dir)
	if err != nil {
		// A readable directory is the AddSource precondition, but a source can
		// become unreadable later (permissions, unmount): report it as missing
		// rather than fail the whole listing.
		st.Status = sourceStatusMissing
		st.Message = err.Error()
		return
	}

	usable := 0
	for _, entry := range entries {
		if entry.IsDir() {
			continue // subdirectories are not part of the merge and not listed
		}
		name := entry.Name()
		full := filepath.Join(dir, name)
		fs := SourceFileStatus{Path: full}

		if strings.HasPrefix(name, ".") {
			fs.Status = fileStatusHidden
			st.Files = append(st.Files, fs)
			continue
		}
		// os.Stat (not entry.Info) so a symlink is judged by its TARGET: the
		// lstat size of a link is the link path's length, which would slip an
		// oversized target past the cap and let LoadFromFile read it unbounded.
		info, err := os.Stat(full)
		if err != nil {
			fs.Status = fileStatusUnparseable
			fs.Message = err.Error() // dangling symlink or vanished file
			st.Files = append(st.Files, fs)
			continue
		}
		if !info.Mode().IsRegular() {
			continue // symlinked dirs, devices, fifos: not kubeconfig candidates, not listed
		}
		if info.Size() > maxKubeconfigFileBytes {
			fs.Status = fileStatusTooLarge
			st.Files = append(st.Files, fs)
			continue
		}
		cfg, err := clientcmd.LoadFromFile(full)
		if err != nil {
			fs.Status = fileStatusUnparseable
			fs.Message = err.Error()
			st.Files = append(st.Files, fs)
			continue
		}
		fs.Status = fileStatusOK
		st.Files = append(st.Files, fs)
		*candidates = append(*candidates, candidate{
			path:     full,
			contexts: contextNames(cfg),
			srcIdx:   srcIdx,
			fileIdx:  len(st.Files) - 1,
		})
		usable++
	}

	// An empty (or all-unusable) directory is valid — "mount a dir, drop files
	// in later" is the point (ADR-0008).
	if usable == 0 {
		st.Status = sourceStatusEmpty
	} else {
		st.Status = sourceStatusOK
	}
}

// expandFile classifies a file source. It parses once via clientcmd.LoadFromFile:
// a parse error is status unparseable; a file that parses but declares nothing is
// status empty (still fed to the merge, which clientcmd handles); otherwise ok.
func expandFile(st *SourceStatus, path string, srcIdx int, candidates *[]candidate) {
	cfg, err := clientcmd.LoadFromFile(path)
	if err != nil {
		st.Status = sourceStatusUnparseable
		st.Message = err.Error()
		return
	}
	if len(cfg.Contexts) == 0 && len(cfg.Clusters) == 0 && len(cfg.AuthInfos) == 0 {
		st.Status = sourceStatusEmpty
	} else {
		st.Status = sourceStatusOK
	}
	*candidates = append(*candidates, candidate{
		path:     path,
		contexts: contextNames(cfg),
		srcIdx:   srcIdx,
		fileIdx:  -1,
	})
}

// contextNames returns the context names a parsed kubeconfig defines, sorted so
// the first-wins walk and the API output are deterministic (clientcmd map order
// is not).
func contextNames(cfg *clientcmdapi.Config) []string {
	names := make([]string, 0, len(cfg.Contexts))
	for name := range cfg.Contexts {
		names = append(names, name)
	}
	sort.Strings(names)
	return names
}

// NoUsableSourceError is returned when the registry resolves to zero usable
// kubeconfig files. It carries the per-source statuses so callers (setup state)
// can explain why; its Error() summarizes paths and statuses only, never file
// contents.
type NoUsableSourceError struct{ Statuses []SourceStatus }

func (e *NoUsableSourceError) Error() string {
	if len(e.Statuses) == 0 {
		return "no kubeconfig sources are configured"
	}
	parts := make([]string, len(e.Statuses))
	for i, s := range e.Statuses {
		parts[i] = fmt.Sprintf("%s (%s)", s.Path, s.Status)
	}
	return "no usable kubeconfig source among: " + strings.Join(parts, ", ")
}

// DuplicateSourceError is returned by AddSource when the path is already
// registered; handlers map it to 409.
type DuplicateSourceError struct{ Path string }

func (e *DuplicateSourceError) Error() string {
	return fmt.Sprintf("kubeconfig source %q is already registered", e.Path)
}

// UnknownSourceError is returned by RemoveSource when no registered source has
// the given id; handlers map it to 404.
type UnknownSourceError struct{ ID string }

func (e *UnknownSourceError) Error() string {
	return fmt.Sprintf("no kubeconfig source with id %q", e.ID)
}

// SourceInvisibleError is returned by AddSource when the candidate path cannot be
// stat'd — in Docker this is the "not under a mount made at container creation"
// case, so handlers map it to 422 with the mounted-directory guidance. The
// wrapped stat error carries the path only.
type SourceInvisibleError struct {
	Path string
	err  error
}

func (e *SourceInvisibleError) Error() string {
	if e.err != nil {
		return fmt.Sprintf("kubeconfig source %q is not visible to Kubescope: %v", e.Path, e.err)
	}
	return fmt.Sprintf("kubeconfig source %q is not visible to Kubescope", e.Path)
}

func (e *SourceInvisibleError) Unwrap() error { return e.err }
