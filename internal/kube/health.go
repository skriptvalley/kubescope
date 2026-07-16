package kube

import (
	"fmt"

	clientcmdapi "k8s.io/client-go/tools/clientcmd/api"
)

// classify turns a probe error into a ContextHealth (Name is set by the
// caller) by sorting it through the shared failure taxonomy. A rejected-
// credentials error (auth_expired/forbidden) means the server was reachable but
// refused us; every other class leaves the context unreachable. The
// classification's reason code, remediation and doc link are surfaced on the
// ContextHealth so the UI can act on them.
func classify(err error, hints ClassifyHints) ContextHealth {
	cls := ClassifyError(err, hints)
	h := ContextHealth{
		Error:    err.Error(),
		Reason:   string(cls.Class),
		Guidance: cls.Remediation,
		DocURL:   cls.DocURL,
	}
	switch cls.Class {
	case FailAuthExpired, FailForbidden:
		h.Reachable = true
		h.AuthOK = false
	}
	return h
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
