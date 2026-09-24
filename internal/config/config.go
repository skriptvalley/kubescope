// Package config loads and validates Kubescope runtime configuration from
// KUBESCOPE_* environment variables.
package config

import (
	"fmt"
	"net"
	"os"
	"path/filepath"
	"regexp"
	"strconv"
	"strings"
)

// Canonical environment variable names. Do not invent new ones.
const (
	EnvListenAddr = "KUBESCOPE_LISTEN_ADDR"
	EnvPort       = "KUBESCOPE_PORT"
	EnvKubeconfig = "KUBESCOPE_KUBECONFIG"
	EnvReadOnly   = "KUBESCOPE_READ_ONLY"
	EnvAuthMode   = "KUBESCOPE_AUTH_MODE"
	// EnvAllowKubeconfigSet gates the runtime set-kubeconfig endpoint (ADR-0007);
	// default false, so a running Kubescope cannot be repointed at another
	// kubeconfig unless explicitly enabled.
	EnvAllowKubeconfigSet = "KUBESCOPE_ALLOW_KUBECONFIG_SET"
	// Basic-auth credential source (Sprint 8, ADR-0005). Only consulted when
	// KUBESCOPE_AUTH_MODE=basic; both are required in that mode. A single
	// operator/password pair is the v1 credential model — see ADR-0005.
	EnvAuthBasicUsername = "KUBESCOPE_AUTH_BASIC_USERNAME"
	EnvAuthBasicPassword = "KUBESCOPE_AUTH_BASIC_PASSWORD"
	// EnvBasePath is the URL sub-path Kubescope is served under behind a reverse
	// proxy (e.g. /kubescope), ADR-0012. Default "" = the host root.
	EnvBasePath = "KUBESCOPE_BASE_PATH"
)

const (
	defaultListenAddr     = "127.0.0.1:8080"
	defaultKubeconfig     = "/kubeconfig"
	defaultAuthMode       = "none"
	fallbackKubeconfigRel = ".kube/config"
)

// AuthMode values accepted by KUBESCOPE_AUTH_MODE. `none` and `basic` ship in
// v1; `oidc` is a reserved value that fails fast at startup (not implemented)
// so a config asking for it errors loudly rather than silently running open.
var validAuthModes = map[string]bool{"none": true, "basic": true, "oidc": true}

// Config is the validated runtime configuration.
type Config struct {
	// ListenAddr is the host:port the HTTP server binds to.
	ListenAddr string
	// KubeconfigSources is the ordered kubeconfig source registry the kube layer
	// loads (ADR-0008): each entry is a file or a directory, in precedence order.
	// Existence is not validated here: the server must start (and stay up)
	// without any usable source.
	KubeconfigSources []string
	// ReadOnly rejects all mutating operations server-side when true.
	ReadOnly bool
	// AuthMode is one of none|basic|oidc.
	AuthMode string
	// BasicAuthUsername/BasicAuthPassword are the credentials enforced when
	// AuthMode is "basic". Empty in every other mode. Never logged.
	BasicAuthUsername string
	BasicAuthPassword string
	// AllowKubeconfigSet enables the runtime set-kubeconfig endpoint (ADR-0007);
	// default false.
	AllowKubeconfigSet bool
	// BasePath is the normalized sub-path the UI is served under ("" = root,
	// else "/seg[/seg…]" with no trailing slash), ADR-0012.
	BasePath string
	// Warnings are non-fatal config notes for the caller to log at startup
	// (e.g. an ignored Kubernetes service-link KUBESCOPE_PORT).
	Warnings []string
}

type deps struct {
	lookupEnv  func(string) (string, bool)
	fileExists func(string) bool
	userHome   func() (string, error)
}

// Option overrides an environment dependency, for tests.
type Option func(*deps)

// WithLookupEnv replaces os.LookupEnv.
func WithLookupEnv(fn func(string) (string, bool)) Option { return func(d *deps) { d.lookupEnv = fn } }

// WithFileExists replaces the os.Stat-based existence check.
func WithFileExists(fn func(string) bool) Option { return func(d *deps) { d.fileExists = fn } }

// WithUserHome replaces os.UserHomeDir.
func WithUserHome(fn func() (string, error)) Option { return func(d *deps) { d.userHome = fn } }

// Load reads KUBESCOPE_* environment variables, applies defaults and
// validates the result.
func Load(opts ...Option) (Config, error) {
	d := deps{
		lookupEnv:  os.LookupEnv,
		fileExists: statExists,
		userHome:   os.UserHomeDir,
	}
	for _, opt := range opts {
		opt(&d)
	}

	listenAddr, warnings, err := resolveListenAddr(d)
	if err != nil {
		return Config{}, err
	}

	kubeconfigSources, err := resolveKubeconfigSources(d)
	if err != nil {
		return Config{}, err
	}

	readOnly := false
	if raw, ok := d.lookupEnv(EnvReadOnly); ok {
		readOnly, err = strconv.ParseBool(raw)
		if err != nil {
			return Config{}, fmt.Errorf("parsing %s=%q: must be a boolean", EnvReadOnly, raw)
		}
	}

	allowKubeconfigSet := false
	if raw, ok := d.lookupEnv(EnvAllowKubeconfigSet); ok {
		allowKubeconfigSet, err = strconv.ParseBool(raw)
		if err != nil {
			return Config{}, fmt.Errorf("parsing %s=%q: must be a boolean", EnvAllowKubeconfigSet, raw)
		}
	}

	authMode := defaultAuthMode
	if raw, ok := d.lookupEnv(EnvAuthMode); ok {
		if !validAuthModes[raw] {
			return Config{}, fmt.Errorf("parsing %s=%q: must be one of none|basic|oidc", EnvAuthMode, raw)
		}
		authMode = raw
	}

	// oidc is reserved but not implemented in v1: fail fast at startup rather
	// than fall back to running open (ADR-0005).
	if authMode == "oidc" {
		return Config{}, fmt.Errorf("%s=oidc is not implemented in this release; use 'none' or 'basic'", EnvAuthMode)
	}

	var basicUser, basicPass string
	if authMode == "basic" {
		basicUser, _ = d.lookupEnv(EnvAuthBasicUsername)
		basicPass, _ = d.lookupEnv(EnvAuthBasicPassword)
		if basicUser == "" || basicPass == "" {
			return Config{}, fmt.Errorf(
				"%s=basic requires both %s and %s to be set (non-empty)",
				EnvAuthMode, EnvAuthBasicUsername, EnvAuthBasicPassword)
		}
	}

	basePath := ""
	if raw, ok := d.lookupEnv(EnvBasePath); ok {
		basePath, err = normalizeBasePath(raw)
		if err != nil {
			return Config{}, err
		}
	}

	return Config{
		Warnings:           warnings,
		BasePath:           basePath,
		ListenAddr:         listenAddr,
		KubeconfigSources:  kubeconfigSources,
		ReadOnly:           readOnly,
		AuthMode:           authMode,
		BasicAuthUsername:  basicUser,
		BasicAuthPassword:  basicPass,
		AllowKubeconfigSet: allowKubeconfigSet,
	}, nil
}

// basePathSegment is one URL path segment of a base path: unreserved characters
// only, so the value is safe to write into the served HTML <base href> as-is.
var basePathSegment = regexp.MustCompile(`^[A-Za-z0-9._~-]+$`)

// normalizeBasePath turns KUBESCOPE_BASE_PATH into "" (root) or "/a/b" (leading
// slash, no trailing slash). "", "/" mean the root. Segments are limited to URL
// unreserved characters; "." / ".." and a first segment that would shadow the
// server's own routes (api, healthz, assets) are rejected.
func normalizeBasePath(raw string) (string, error) {
	trimmed := strings.Trim(strings.TrimSpace(raw), "/")
	if trimmed == "" {
		return "", nil
	}
	if !strings.HasPrefix(strings.TrimSpace(raw), "/") {
		return "", fmt.Errorf("parsing %s=%q: must start with '/' (e.g. /kubescope)", EnvBasePath, raw)
	}
	segs := strings.Split(trimmed, "/")
	for _, seg := range segs {
		if !basePathSegment.MatchString(seg) || seg == "." || seg == ".." {
			return "", fmt.Errorf("parsing %s=%q: path segments may only use letters, digits and . _ ~ -", EnvBasePath, raw)
		}
	}
	switch segs[0] {
	case "api", "healthz", "assets":
		return "", fmt.Errorf("parsing %s=%q: /%s is a Kubescope route; pick another prefix", EnvBasePath, raw, segs[0])
	}
	return "/" + trimmed, nil
}

// resolveListenAddr combines KUBESCOPE_LISTEN_ADDR (default 127.0.0.1:8080)
// with KUBESCOPE_PORT, which overrides only the port part.
//
// In Kubernetes a Service named "kubescope" makes the kubelet inject
// KUBESCOPE_PORT=tcp://<clusterIP>:<port> (a Docker-style service link) into
// every pod in its namespace. That value is not an operator's port override, so
// it is ignored with a warning rather than failing startup.
func resolveListenAddr(d deps) (string, []string, error) {
	addr := defaultListenAddr
	if raw, ok := d.lookupEnv(EnvListenAddr); ok {
		addr = raw
	}
	host, port, err := net.SplitHostPort(addr)
	if err != nil {
		return "", nil, fmt.Errorf("parsing %s=%q: %w", EnvListenAddr, addr, err)
	}
	var warnings []string
	if raw, ok := d.lookupEnv(EnvPort); ok {
		if isServiceLink(raw) {
			warnings = append(warnings, fmt.Sprintf(
				"ignoring %s=%q: a Kubernetes service link for a Service named kubescope, not a port (set enableServiceLinks: false on the pod to drop it)",
				EnvPort, raw))
		} else {
			port = raw
		}
	}
	if err := validatePort(port); err != nil {
		return "", nil, err
	}
	return net.JoinHostPort(host, port), warnings, nil
}

// isServiceLink reports a Kubernetes/Docker service-link value such as
// "tcp://10.43.0.12:8080".
func isServiceLink(v string) bool {
	return strings.HasPrefix(v, "tcp://") || strings.HasPrefix(v, "udp://") || strings.HasPrefix(v, "sctp://")
}

func validatePort(port string) error {
	n, err := strconv.Atoi(port)
	if err != nil || n < 1 || n > 65535 {
		return fmt.Errorf("invalid port %q: must be an integer in 1-65535", port)
	}
	return nil
}

// resolveKubeconfigSources picks the ordered kubeconfig source registry with the
// documented precedence (ADR-0008): KUBESCOPE_KUBECONFIG, then /kubeconfig if it
// exists (the container mount point, a file OR a directory), then $KUBECONFIG,
// then ~/.kube/config. KUBESCOPE_KUBECONFIG and $KUBECONFIG are split on the OS
// path-list separator (`:` on Unix) like kubectl, with empty segments dropped;
// each remaining entry is a file or a directory. Existence is not validated here.
func resolveKubeconfigSources(d deps) ([]string, error) {
	if raw, ok := d.lookupEnv(EnvKubeconfig); ok {
		sources := splitSources(raw)
		// Set-but-yielding-zero-entries (empty, or only separators) is a config
		// mistake, not a fall-through to the defaults.
		if len(sources) == 0 {
			return nil, fmt.Errorf("parsing %s: must not be empty when set", EnvKubeconfig)
		}
		return sources, nil
	}
	if d.fileExists(defaultKubeconfig) {
		return []string{defaultKubeconfig}, nil
	}
	if raw, ok := d.lookupEnv("KUBECONFIG"); ok {
		if sources := splitSources(raw); len(sources) > 0 {
			return sources, nil
		}
	}
	home, err := d.userHome()
	if err != nil {
		return nil, fmt.Errorf("resolving home directory for default kubeconfig: %w", err)
	}
	return []string{filepath.Join(home, fallbackKubeconfigRel)}, nil
}

// splitSources splits a path-list value on the OS path-list separator, dropping
// empty segments so a stray leading/trailing/doubled separator never yields a
// bogus empty source.
func splitSources(raw string) []string {
	parts := filepath.SplitList(raw)
	sources := make([]string, 0, len(parts))
	for _, p := range parts {
		if p != "" {
			sources = append(sources, p)
		}
	}
	return sources
}

// statExists reports whether a path is present, accepting a directory as well
// as a file: the default container mount point may be either (ADR-0008).
func statExists(path string) bool {
	_, err := os.Stat(path)
	return err == nil
}
