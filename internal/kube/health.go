package kube

import (
	"fmt"
	"strings"

	apierrors "k8s.io/apimachinery/pkg/api/errors"
	clientcmdapi "k8s.io/client-go/tools/clientcmd/api"
)

// classify turns a probe error into a ContextHealth (Name is set by the
// caller). A rejected-credentials error means the server was reachable but
// refused us; anything else (connection refused, DNS, TLS, a missing exec
// plugin) leaves the context unreachable. Exec-plugin contexts additionally
// carry the ADR-0004 guidance so users get a fix, not a raw error.
func classify(err error, usesExec bool, cmd string) ContextHealth {
	h := ContextHealth{Error: err.Error()}
	if apierrors.IsUnauthorized(err) || apierrors.IsForbidden(err) || isAuthString(err.Error()) {
		h.Reachable = true
		h.AuthOK = false
	}
	if usesExec {
		h.Guidance = execGuidance(cmd)
	}
	return h
}

// isAuthString is a fallback for auth failures that don't arrive as a typed
// Kubernetes status error (e.g. surfaced through the exec/transport layer).
func isAuthString(msg string) bool {
	l := strings.ToLower(msg)
	return strings.Contains(l, "unauthorized") ||
		strings.Contains(l, "forbidden") ||
		strings.Contains(l, " 401") ||
		strings.Contains(l, " 403")
}

// ExecGuidance returns the ADR-0004 exec-plugin guidance for the named context
// if it authenticates via an exec credential plugin, or "" otherwise. Handlers
// use it to attach guidance to errors from exec-auth contexts (e.g. overview),
// matching what the health probe already surfaces. A missing/malformed
// kubeconfig yields "" — the caller already reports that failure.
func (m *Manager) ExecGuidance(name string) string {
	raw, err := m.rawConfig()
	if err != nil {
		return ""
	}
	if !contextUsesExec(raw, name) {
		return ""
	}
	return execGuidance(execCommand(raw, name))
}

// contextUsesExec reports whether the named context authenticates via an exec
// credential plugin (EKS/GKE style).
func contextUsesExec(raw clientcmdapi.Config, name string) bool {
	c, ok := raw.Contexts[name]
	if !ok {
		return false
	}
	ai, ok := raw.AuthInfos[c.AuthInfo]
	return ok && ai.Exec != nil
}

// execCommand returns the exec plugin's command for the named context, or "".
func execCommand(raw clientcmdapi.Config, name string) string {
	c, ok := raw.Contexts[name]
	if !ok {
		return ""
	}
	ai, ok := raw.AuthInfos[c.AuthInfo]
	if !ok || ai.Exec == nil {
		return ""
	}
	return ai.Exec.Command
}

// execGuidance is the ADR-0004 fix text for a context whose exec credential
// plugin can't run inside the container.
func execGuidance(cmd string) string {
	if cmd == "" {
		cmd = "a credential plugin"
	}
	return fmt.Sprintf(
		"This context authenticates via the exec credential plugin %q, which isn't available inside the "+
			"Kubescope container. Either mount your cloud credentials and bundle the CLI into the image "+
			"(build flag), or pre-generate a token on the host (e.g. `aws eks get-token`) and mount a "+
			"token-based kubeconfig. See ADR-0004.",
		cmd,
	)
}
