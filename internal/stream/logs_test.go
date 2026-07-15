package stream

import (
	"net/url"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
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
