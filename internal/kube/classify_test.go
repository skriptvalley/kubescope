package kube

import (
	"context"
	"crypto/x509"
	"errors"
	"fmt"
	"net"
	"syscall"
	"testing"

	"github.com/stretchr/testify/assert"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime/schema"
)

func TestClassifyError(t *testing.T) {
	gr := schema.GroupResource{Resource: "pods"}

	tests := []struct {
		name       string
		err        error
		hints      ClassifyHints
		wantClass  FailureClass
		wantDoc    bool   // DocURL should be non-empty
		wantRemedy bool   // Remediation should be non-empty
		remedyHas  string // substring the remediation must contain, if set
	}{
		{
			name:       "exec plugin missing via string",
			err:        errors.New("Unable to connect to the server: getting credentials: exec: executable aws not found"),
			hints:      ClassifyHints{ExecCommand: "aws"},
			wantClass:  FailExecPluginMissing,
			wantDoc:    true,
			wantRemedy: true,
			remedyHas:  "ADR-0004",
		},
		{
			name:       "exec plugin missing via getting-credentials hint",
			err:        errors.New("getting credentials: fork/exec: permission denied"),
			hints:      ClassifyHints{ExecCommand: "gke-gcloud-auth-plugin"},
			wantClass:  FailExecPluginMissing,
			wantDoc:    true,
			wantRemedy: true,
		},
		{
			name:       "unauthorized typed",
			err:        apierrors.NewUnauthorized("bad token"),
			wantClass:  FailAuthExpired,
			wantDoc:    true,
			wantRemedy: true,
			remedyHas:  "401",
		},
		{
			name:       "unauthorized via 401 string",
			err:        errors.New("the server has asked for the client to provide credentials ( 401)"),
			wantClass:  FailAuthExpired,
			wantDoc:    true,
			wantRemedy: true,
		},
		{
			name:       "forbidden typed",
			err:        apierrors.NewForbidden(gr, "x", errors.New("nope")),
			wantClass:  FailForbidden,
			wantDoc:    false,
			wantRemedy: true,
			remedyHas:  "403",
		},
		{
			name:       "tls unknown authority typed",
			err:        fmt.Errorf("get failed: %w", x509.UnknownAuthorityError{}),
			wantClass:  FailTLSCert,
			wantDoc:    true,
			wantRemedy: true,
			remedyHas:  "insecure-skip-tls-verify",
		},
		{
			name:       "tls hostname typed",
			err:        fmt.Errorf("dial: %w", x509.HostnameError{Host: "host.docker.internal"}),
			wantClass:  FailTLSCert,
			wantDoc:    true,
			wantRemedy: true,
		},
		{
			name:       "tls via x509 string",
			err:        errors.New("x509: certificate signed by unknown authority"),
			wantClass:  FailTLSCert,
			wantDoc:    true,
			wantRemedy: true,
		},
		{
			name:       "dns typed",
			err:        fmt.Errorf("lookup: %w", &net.DNSError{Err: "no such host", Name: "api.example"}),
			wantClass:  FailDNS,
			wantDoc:    true,
			wantRemedy: true,
			remedyHas:  "resolve",
		},
		{
			name:       "connection refused typed loopback",
			err:        fmt.Errorf("dial: %w", syscall.ECONNREFUSED),
			hints:      ClassifyHints{LoopbackServer: true},
			wantClass:  FailConnectionRefused,
			wantDoc:    true,
			wantRemedy: true,
			remedyHas:  "loopback",
		},
		{
			name:       "connection refused non-loopback remediation differs",
			err:        errors.New("dial tcp 10.0.0.1:6443: connect: connection refused"),
			hints:      ClassifyHints{LoopbackServer: false},
			wantClass:  FailConnectionRefused,
			wantDoc:    true,
			wantRemedy: true,
			remedyHas:  "stopped, deleted",
		},
		{
			name:       "timeout via context deadline",
			err:        fmt.Errorf("probe: %w", context.DeadlineExceeded),
			wantClass:  FailTimeout,
			wantDoc:    false,
			wantRemedy: true,
			remedyHas:  "did not respond",
		},
		{
			name:       "timeout typed apierror",
			err:        apierrors.NewTimeoutError("request timed out", 1),
			wantClass:  FailTimeout,
			wantDoc:    false,
			wantRemedy: true,
		},
		{
			name:       "apiserver 5xx internal error",
			err:        apierrors.NewInternalError(errors.New("boom")),
			wantClass:  FailAPIServer5xx,
			wantDoc:    false,
			wantRemedy: true,
			remedyHas:  "unhealthy",
		},
		{
			name:       "apiserver 5xx generic status error",
			err:        &apierrors.StatusError{ErrStatus: metav1.Status{Code: 502, Message: "bad gateway"}},
			wantClass:  FailAPIServer5xx,
			wantDoc:    false,
			wantRemedy: true,
		},
		{
			name:       "unknown without hint has no remediation",
			err:        errors.New("some entirely opaque failure"),
			wantClass:  FailUnknown,
			wantDoc:    false,
			wantRemedy: false,
		},
		{
			name:       "unknown with exec hint falls back to exec guidance",
			err:        errors.New("some entirely opaque failure"),
			hints:      ClassifyHints{ExecCommand: "aws"},
			wantClass:  FailUnknown,
			wantDoc:    false,
			wantRemedy: true,
			remedyHas:  "ADR-0004",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := ClassifyError(tt.err, tt.hints)
			assert.Equal(t, tt.wantClass, got.Class)
			if tt.wantDoc {
				assert.NotEmpty(t, got.DocURL)
			} else {
				assert.Empty(t, got.DocURL)
			}
			if tt.wantRemedy {
				assert.NotEmpty(t, got.Remediation)
			} else {
				assert.Empty(t, got.Remediation)
			}
			if tt.remedyHas != "" {
				assert.Contains(t, got.Remediation, tt.remedyHas)
			}
		})
	}
}

// TestClassifyErrorLoopbackRemediationVaries pins that the same connection-
// refused error yields different remediation text depending on the loopback
// hint, so the UI advice matches the deployment shape.
func TestClassifyErrorLoopbackRemediationVaries(t *testing.T) {
	err := errors.New("connect: connection refused")
	loop := ClassifyError(err, ClassifyHints{LoopbackServer: true})
	remote := ClassifyError(err, ClassifyHints{LoopbackServer: false})

	assert.Equal(t, FailConnectionRefused, loop.Class)
	assert.Equal(t, FailConnectionRefused, remote.Class)
	assert.NotEqual(t, loop.Remediation, remote.Remediation)
	assert.Contains(t, loop.Remediation, "host.docker.internal")
}

func TestClassifyErrorNil(t *testing.T) {
	got := ClassifyError(nil, ClassifyHints{})
	assert.Equal(t, FailUnknown, got.Class)
	assert.Empty(t, got.Remediation)
}
