package stream

import (
	"context"
	"errors"
	"net/http"
	"net/url"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	"k8s.io/apimachinery/pkg/runtime/schema"

	"github.com/skriptvalley/kubescope/internal/kube"
)

func TestBuildPodLogOptions(t *testing.T) {
	tests := []struct {
		name          string
		query         string
		wantContainer string
		wantPrevious  bool
		wantFollow    bool
		wantTail      *int64
		wantErr       bool
	}{
		{
			name:       "defaults follow on, no container",
			query:      "",
			wantFollow: true,
		},
		{
			name:          "container selected",
			query:         "container=app",
			wantContainer: "app",
			wantFollow:    true,
		},
		{
			name:       "follow explicitly disabled",
			query:      "follow=false",
			wantFollow: false,
		},
		{
			name:         "previous forces follow off",
			query:        "previous=true&follow=true",
			wantPrevious: true,
			wantFollow:   false,
		},
		{
			name:       "tailLines parsed",
			query:      "tailLines=100",
			wantFollow: true,
			wantTail:   ptr(int64(100)),
		},
		{
			name:       "tailLines zero is valid",
			query:      "tailLines=0",
			wantFollow: true,
			wantTail:   ptr(int64(0)),
		},
		{
			name:    "tailLines negative rejected",
			query:   "tailLines=-1",
			wantErr: true,
		},
		{
			name:    "tailLines non-numeric rejected",
			query:   "tailLines=abc",
			wantErr: true,
		},
		{
			name:          "all params combined",
			query:         "container=sidecar&previous=true&tailLines=50",
			wantContainer: "sidecar",
			wantPrevious:  true,
			wantFollow:    false,
			wantTail:      ptr(int64(50)),
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			q, err := url.ParseQuery(tt.query)
			require.NoError(t, err)

			opts, err := buildPodLogOptions(q)
			if tt.wantErr {
				assert.Error(t, err)
				return
			}
			require.NoError(t, err)
			assert.Equal(t, tt.wantContainer, opts.Container)
			assert.Equal(t, tt.wantPrevious, opts.Previous)
			assert.Equal(t, tt.wantFollow, opts.Follow)
			if tt.wantTail == nil {
				assert.Nil(t, opts.TailLines)
			} else {
				require.NotNil(t, opts.TailLines)
				assert.Equal(t, *tt.wantTail, *opts.TailLines)
			}
		})
	}
}

func ptr[T any](v T) *T { return &v }

// TestLogErrorStatus covers the classified open-error mapping: a missing pod is
// 404; every other failure is classified so an outage/auth/cert error carries
// its typed reason and remediation instead of an opaque 502 (FB-6).
func TestLogErrorStatus(t *testing.T) {
	classify := func(err error) kube.Classification {
		return kube.ClassifyError(err, kube.ClassifyHints{})
	}
	podsGR := schema.GroupResource{Resource: "pods"}

	tests := []struct {
		name         string
		err          error
		wantStatus   int
		wantCode     string
		wantGuidance bool
		wantDocURL   bool
	}{
		{
			name:       "missing pod is not found",
			err:        apierrors.NewNotFound(podsGR, "web-1"),
			wantStatus: http.StatusNotFound,
			wantCode:   "not_found",
		},
		{
			name:         "unauthorized is auth_expired",
			err:          apierrors.NewUnauthorized("token expired"),
			wantStatus:   http.StatusUnauthorized,
			wantCode:     "auth_expired",
			wantGuidance: true,
			wantDocURL:   true,
		},
		{
			name:         "forbidden keeps 403",
			err:          apierrors.NewForbidden(podsGR, "web-1", errors.New("nope")),
			wantStatus:   http.StatusForbidden,
			wantCode:     "forbidden",
			wantGuidance: true,
		},
		{
			name:         "connection refused surfaces its class",
			err:          errors.New("dial tcp 127.0.0.1:6443: connect: connection refused"),
			wantStatus:   http.StatusBadGateway,
			wantCode:     "connection_refused",
			wantGuidance: true,
			wantDocURL:   true,
		},
		{
			name:         "deadline exceeded is a timeout",
			err:          context.DeadlineExceeded,
			wantStatus:   http.StatusGatewayTimeout,
			wantCode:     "timeout",
			wantGuidance: true,
		},
		{
			name:         "internal error is apiserver_5xx",
			err:          apierrors.NewInternalError(errors.New("etcd unavailable")),
			wantStatus:   http.StatusBadGateway,
			wantCode:     "apiserver_5xx",
			wantGuidance: true,
		},
		{
			name:       "opaque error is cluster_unreachable",
			err:        errors.New("some entirely opaque failure"),
			wantStatus: http.StatusBadGateway,
			wantCode:   "cluster_unreachable",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			status, code, guidance, docURL := logErrorStatus(tt.err, classify)
			assert.Equal(t, tt.wantStatus, status)
			assert.Equal(t, tt.wantCode, code)
			if tt.wantGuidance {
				assert.NotEmpty(t, guidance)
			} else {
				assert.Empty(t, guidance)
			}
			if tt.wantDocURL {
				assert.NotEmpty(t, docURL)
			} else {
				assert.Empty(t, docURL)
			}
		})
	}
}
