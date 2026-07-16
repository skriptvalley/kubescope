package config

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func envMap(vars map[string]string) Option {
	return WithLookupEnv(func(key string) (string, bool) {
		v, ok := vars[key]
		return v, ok
	})
}

func noFiles() Option { return WithFileExists(func(string) bool { return false }) }
func homeDir(p string) Option {
	return WithUserHome(func() (string, error) { return p, nil })
}

func TestLoad(t *testing.T) {
	tests := []struct {
		name    string
		env     map[string]string
		exists  map[string]bool
		want    Config
		wantErr string
	}{
		{
			name: "all defaults",
			env:  map[string]string{},
			want: Config{
				ListenAddr:     "127.0.0.1:8080",
				KubeconfigPath: "/home/u/.kube/config",
				ReadOnly:       false,
				AuthMode:       "none",
			},
		},
		{
			name: "listen addr override",
			env:  map[string]string{EnvListenAddr: "0.0.0.0:9090"},
			want: Config{ListenAddr: "0.0.0.0:9090", KubeconfigPath: "/home/u/.kube/config", AuthMode: "none"},
		},
		{
			name: "port overrides port part of default listen addr",
			env:  map[string]string{EnvPort: "3000"},
			want: Config{ListenAddr: "127.0.0.1:3000", KubeconfigPath: "/home/u/.kube/config", AuthMode: "none"},
		},
		{
			name: "port overrides port part of explicit listen addr",
			env:  map[string]string{EnvListenAddr: "10.0.0.1:8080", EnvPort: "9999"},
			want: Config{ListenAddr: "10.0.0.1:9999", KubeconfigPath: "/home/u/.kube/config", AuthMode: "none"},
		},
		{
			name:    "invalid listen addr",
			env:     map[string]string{EnvListenAddr: "no-port-here"},
			wantErr: "KUBESCOPE_LISTEN_ADDR",
		},
		{
			name:    "non-numeric port",
			env:     map[string]string{EnvPort: "http"},
			wantErr: "invalid port",
		},
		{
			name:    "out of range port",
			env:     map[string]string{EnvPort: "70000"},
			wantErr: "invalid port",
		},
		{
			name:    "port zero rejected",
			env:     map[string]string{EnvPort: "0"},
			wantErr: "invalid port",
		},
		{
			name:   "explicit kubeconfig wins over everything",
			env:    map[string]string{EnvKubeconfig: "/custom/cfg", "KUBECONFIG": "/env/cfg"},
			exists: map[string]bool{"/kubeconfig": true},
			want:   Config{ListenAddr: "127.0.0.1:8080", KubeconfigPath: "/custom/cfg", AuthMode: "none"},
		},
		{
			name:   "mounted /kubeconfig wins over KUBECONFIG env",
			env:    map[string]string{"KUBECONFIG": "/env/cfg"},
			exists: map[string]bool{"/kubeconfig": true},
			want:   Config{ListenAddr: "127.0.0.1:8080", KubeconfigPath: "/kubeconfig", AuthMode: "none"},
		},
		{
			name: "KUBECONFIG env wins over home fallback",
			env:  map[string]string{"KUBECONFIG": "/env/cfg"},
			want: Config{ListenAddr: "127.0.0.1:8080", KubeconfigPath: "/env/cfg", AuthMode: "none"},
		},
		{
			name: "empty KUBECONFIG env falls through to home",
			env:  map[string]string{"KUBECONFIG": ""},
			want: Config{ListenAddr: "127.0.0.1:8080", KubeconfigPath: "/home/u/.kube/config", AuthMode: "none"},
		},
		{
			name:    "empty explicit kubeconfig rejected",
			env:     map[string]string{EnvKubeconfig: ""},
			wantErr: "KUBESCOPE_KUBECONFIG",
		},
		{
			name: "read only true",
			env:  map[string]string{EnvReadOnly: "true"},
			want: Config{ListenAddr: "127.0.0.1:8080", KubeconfigPath: "/home/u/.kube/config", ReadOnly: true, AuthMode: "none"},
		},
		{
			name:    "read only invalid",
			env:     map[string]string{EnvReadOnly: "yep"},
			wantErr: "KUBESCOPE_READ_ONLY",
		},
		{
			name: "auth mode basic with credentials accepted",
			env: map[string]string{
				EnvAuthMode:          "basic",
				EnvAuthBasicUsername: "admin",
				EnvAuthBasicPassword: "s3cret",
			},
			want: Config{
				ListenAddr: "127.0.0.1:8080", KubeconfigPath: "/home/u/.kube/config",
				AuthMode: "basic", BasicAuthUsername: "admin", BasicAuthPassword: "s3cret",
			},
		},
		{
			name:    "auth mode basic without credentials rejected",
			env:     map[string]string{EnvAuthMode: "basic"},
			wantErr: "requires both",
		},
		{
			name: "auth mode basic with only username rejected",
			env: map[string]string{
				EnvAuthMode:          "basic",
				EnvAuthBasicUsername: "admin",
			},
			wantErr: "requires both",
		},
		{
			name:    "auth mode oidc not implemented",
			env:     map[string]string{EnvAuthMode: "oidc"},
			wantErr: "not implemented",
		},
		{
			name:    "auth mode invalid",
			env:     map[string]string{EnvAuthMode: "token"},
			wantErr: "KUBESCOPE_AUTH_MODE",
		},
		{
			name: "basic credentials ignored when mode is none",
			env: map[string]string{
				EnvAuthBasicUsername: "admin",
				EnvAuthBasicPassword: "s3cret",
			},
			want: Config{ListenAddr: "127.0.0.1:8080", KubeconfigPath: "/home/u/.kube/config", AuthMode: "none"},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			opts := []Option{
				envMap(tt.env),
				homeDir("/home/u"),
				WithFileExists(func(path string) bool { return tt.exists[path] }),
			}
			got, err := Load(opts...)
			if tt.wantErr != "" {
				require.Error(t, err)
				assert.Contains(t, err.Error(), tt.wantErr)
				return
			}
			require.NoError(t, err)
			assert.Equal(t, tt.want, got)
		})
	}
}

func TestLoadHomeDirError(t *testing.T) {
	_, err := Load(
		envMap(map[string]string{}),
		noFiles(),
		WithUserHome(func() (string, error) { return "", assert.AnError }),
	)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "home directory")
}
