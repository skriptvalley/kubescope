package kube

import (
	"context"
	"crypto/x509"
	"errors"
	"net"
	"strings"
	"syscall"

	apierrors "k8s.io/apimachinery/pkg/api/errors"
)

// FailureClass is the taxonomy a connectivity failure is sorted into so the API
// and UI can attach a stable reason code and actionable remediation instead of
// a raw client-go error string (FB-6).
type FailureClass string

const (
	FailConnectionRefused FailureClass = "connection_refused"
	FailTLSCert           FailureClass = "tls_cert"
	FailExecPluginMissing FailureClass = "exec_plugin_missing"
	FailAuthExpired       FailureClass = "auth_expired"
	FailForbidden         FailureClass = "forbidden"
	FailDNS               FailureClass = "dns"
	FailTimeout           FailureClass = "timeout"
	FailAPIServer5xx      FailureClass = "apiserver_5xx"
	FailUnknown           FailureClass = "unknown"
)

// Classification is the outcome of sorting an error into the taxonomy: the
// class, a one-paragraph remediation, and an optional doc link.
type Classification struct {
	Class       FailureClass
	Remediation string // actionable fix, "" only for FailUnknown without exec hint
	DocURL      string // "" when no doc applies
}

// ClassifyHints carries context-derived facts that refine classification and
// remediation without re-parsing the kubeconfig inside ClassifyError.
type ClassifyHints struct {
	ExecCommand    string // context's exec-plugin command, "" if none
	LoopbackServer bool   // apiserver host is 127.0.0.1 / localhost / ::1
}

// docADR0004 is the auth/kubeconfig-in-Docker doc referenced by the classes
// whose fixes it covers.
const docADR0004 = "https://github.com/skriptvalley/kubescope/blob/main/docs/adr/0004-cluster-auth-and-kubeconfig-in-docker.md"

// Remediation texts are owned by this file (single source of truth); each is
// one paragraph.
const (
	remediationConnRefusedLoopback = "Nothing is listening at the API server address, which is a loopback address (127.0.0.1). " +
		"If Kubescope runs in Docker, 127.0.0.1 is the container itself — on Linux run with --network host; on macOS/Windows " +
		"point the kubeconfig server at host.docker.internal (see `deploy/testenv/testenv.sh run --docker`). If Kubescope runs " +
		"on the host, the local cluster may be stopped."
	remediationConnRefusedRemote = "Nothing is listening at the API server address — the cluster may be stopped, deleted, or the " +
		"kubeconfig server address stale. Verify the cluster is up and the address is current."
	remediationTLS = "TLS verification of the API server failed. If a local cluster's address was rewritten (e.g. to " +
		"host.docker.internal), its certificate lacks that name — pair the rewrite with insecure-skip-tls-verify: true in a copy " +
		"of the kubeconfig (local dev only), or mount the cluster CA at the path the kubeconfig names."
	remediationAuthExpired = "The cluster rejected the credentials (401) — a token or SSO session has likely expired. " +
		"Re-authenticate on the host (e.g. `aws sso login`, `gcloud auth login`), regenerate the kubeconfig token, then reload " +
		"or repoint the kubeconfig."
	remediationForbidden = "The credentials lack RBAC permission for this operation (403). This is scoped to the resource, not a " +
		"cluster outage — other views may work. Check with `kubectl auth can-i`."
	remediationDNS = "The API server hostname does not resolve. If Kubescope runs in a container, the host's DNS/VPN may not be " +
		"available inside it — verify the name resolves where Kubescope runs."
	remediationTimeout = "The API server did not respond in time — a network path issue (VPN, firewall) or an overloaded API " +
		"server. Retry, and verify the server address is reachable from where Kubescope runs."
	remediationAPIServer5xx = "The API server returned a server-side error — the cluster is reachable but unhealthy. Check " +
		"control-plane and etcd health."
)

// ClassifyError sorts err into the failure taxonomy. Detection runs in a fixed
// order: typed checks (errors.Is/As on the wrapped chain) first, lowercase
// string fallbacks second, because client-go errors can arrive deeply wrapped
// or only as opaque transport strings.
func ClassifyError(err error, hints ClassifyHints) Classification {
	if err == nil {
		return Classification{Class: FailUnknown}
	}
	msg := strings.ToLower(err.Error())

	// 1. exec_plugin_missing — the credential plugin binary isn't on PATH inside
	// the container, or client-go failed while fetching exec credentials.
	if (strings.Contains(msg, "exec: executable") && (strings.Contains(msg, "not found") || strings.Contains(msg, "no such file"))) ||
		(hints.ExecCommand != "" && strings.Contains(msg, "getting credentials:")) {
		return Classification{Class: FailExecPluginMissing, Remediation: execGuidance(hints.ExecCommand), DocURL: docADR0004}
	}

	// 2. auth_expired — credentials rejected (401).
	if apierrors.IsUnauthorized(err) || strings.Contains(msg, "unauthorized") || strings.Contains(msg, " 401") {
		return Classification{Class: FailAuthExpired, Remediation: remediationAuthExpired, DocURL: docADR0004}
	}

	// 3. forbidden — reachable and authenticated but no RBAC permission (403).
	if apierrors.IsForbidden(err) || strings.Contains(msg, "forbidden") || strings.Contains(msg, " 403") {
		return Classification{Class: FailForbidden, Remediation: remediationForbidden}
	}

	// 4. tls_cert — the server's certificate failed verification.
	var unknownAuthority x509.UnknownAuthorityError
	var hostnameErr x509.HostnameError
	var certInvalid x509.CertificateInvalidError
	if errors.As(err, &unknownAuthority) || errors.As(err, &hostnameErr) || errors.As(err, &certInvalid) ||
		strings.Contains(msg, "x509:") || strings.Contains(msg, "certificate is valid for") || strings.Contains(msg, "tls: ") {
		return Classification{Class: FailTLSCert, Remediation: remediationTLS, DocURL: docADR0004}
	}

	// 5. dns — the apiserver hostname does not resolve.
	var dnsErr *net.DNSError
	if errors.As(err, &dnsErr) || strings.Contains(msg, "no such host") {
		return Classification{Class: FailDNS, Remediation: remediationDNS, DocURL: docADR0004}
	}

	// 6. connection_refused — nothing is listening at the address.
	if errors.Is(err, syscall.ECONNREFUSED) || strings.Contains(msg, "connection refused") {
		rem := remediationConnRefusedRemote
		if hints.LoopbackServer {
			rem = remediationConnRefusedLoopback
		}
		return Classification{Class: FailConnectionRefused, Remediation: rem, DocURL: docADR0004}
	}

	// 7. timeout — no response within the deadline.
	var netErr net.Error
	if errors.Is(err, context.DeadlineExceeded) || apierrors.IsTimeout(err) || apierrors.IsServerTimeout(err) ||
		(errors.As(err, &netErr) && netErr.Timeout()) ||
		strings.Contains(msg, "i/o timeout") || strings.Contains(msg, "deadline exceeded") || strings.Contains(msg, "client.timeout exceeded") {
		return Classification{Class: FailTimeout, Remediation: remediationTimeout}
	}

	// 8. apiserver_5xx — reachable but unhealthy control plane.
	var statusErr *apierrors.StatusError
	if (errors.As(err, &statusErr) && statusErr.Status().Code >= 500) || apierrors.IsInternalError(err) || apierrors.IsServiceUnavailable(err) ||
		strings.Contains(msg, "etcdserver:") || strings.Contains(msg, "internal server error") {
		return Classification{Class: FailAPIServer5xx, Remediation: remediationAPIServer5xx}
	}

	// 9. unknown — opaque error. Attach exec guidance on exec contexts so those
	// users still get a plausible fix (preserves pre-FB-6 behavior).
	if hints.ExecCommand != "" {
		return Classification{Class: FailUnknown, Remediation: execGuidance(hints.ExecCommand)}
	}
	return Classification{Class: FailUnknown}
}
