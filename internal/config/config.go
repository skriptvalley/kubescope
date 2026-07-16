// Package config loads and validates Kubescope runtime configuration from
// KUBESCOPE_* environment variables.
package config

import (
	"fmt"
	"net"
	"os"
	"path/filepath"
	"strconv"
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
	// KubeconfigPath is the kubeconfig the kube layer will load. Existence is
	// not validated here: the server must start (and stay up) without one.
	KubeconfigPath string
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
		lookupEnv: os.LookupEnv,
		fileExists: func(path string) bool {
			info, err := os.Stat(path)
			return err == nil && !info.IsDir()
		},
		userHome: os.UserHomeDir,
	}
	for _, opt := range opts {
		opt(&d)
	}

	listenAddr, err := resolveListenAddr(d)
	if err != nil {
		return Config{}, err
	}

	kubeconfigPath, err := resolveKubeconfigPath(d)
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

	return Config{
		ListenAddr:         listenAddr,
		KubeconfigPath:     kubeconfigPath,
		ReadOnly:           readOnly,
		AuthMode:           authMode,
		BasicAuthUsername:  basicUser,
		BasicAuthPassword:  basicPass,
		AllowKubeconfigSet: allowKubeconfigSet,
	}, nil
}

// resolveListenAddr combines KUBESCOPE_LISTEN_ADDR (default 127.0.0.1:8080)
// with KUBESCOPE_PORT, which overrides only the port part.
func resolveListenAddr(d deps) (string, error) {
	addr := defaultListenAddr
	if raw, ok := d.lookupEnv(EnvListenAddr); ok {
		addr = raw
	}
	host, port, err := net.SplitHostPort(addr)
	if err != nil {
		return "", fmt.Errorf("parsing %s=%q: %w", EnvListenAddr, addr, err)
	}
	if raw, ok := d.lookupEnv(EnvPort); ok {
		port = raw
	}
	if err := validatePort(port); err != nil {
		return "", err
	}
	return net.JoinHostPort(host, port), nil
}

func validatePort(port string) error {
	n, err := strconv.Atoi(port)
	if err != nil || n < 1 || n > 65535 {
		return fmt.Errorf("invalid port %q: must be an integer in 1-65535", port)
	}
	return nil
}

// resolveKubeconfigPath picks the kubeconfig path with the documented
// precedence: KUBESCOPE_KUBECONFIG, then /kubeconfig if it exists (the
// container mount point), then $KUBECONFIG, then ~/.kube/config.
func resolveKubeconfigPath(d deps) (string, error) {
	if raw, ok := d.lookupEnv(EnvKubeconfig); ok {
		if raw == "" {
			return "", fmt.Errorf("parsing %s: must not be empty when set", EnvKubeconfig)
		}
		return raw, nil
	}
	if d.fileExists(defaultKubeconfig) {
		return defaultKubeconfig, nil
	}
	if raw, ok := d.lookupEnv("KUBECONFIG"); ok && raw != "" {
		return raw, nil
	}
	home, err := d.userHome()
	if err != nil {
		return "", fmt.Errorf("resolving home directory for default kubeconfig: %w", err)
	}
	return filepath.Join(home, fallbackKubeconfigRel), nil
}
