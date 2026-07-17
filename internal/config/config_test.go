package config

import (
	"os"
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
				ListenAddr:        "127.0.0.1:8080",
				KubeconfigSources: []string{"/home/u/.kube/config"},
				ReadOnly:          false,
				AuthMode:          "none",
			},
		},
		{
			name: "listen addr override",
			env:  map[string]string{EnvListenAddr: "0.0.0.0:9090"},
			want: Config{ListenAddr: "0.0.0.0:9090", KubeconfigSources: []string{"/home/u/.kube/config"}, AuthMode: "none"},
		},
		{
			name: "port overrides port part of default listen addr",
			env:  map[string]string{EnvPort: "3000"},
			want: Config{ListenAddr: "127.0.0.1:3000", KubeconfigSources: []string{"/home/u/.kube/config"}, AuthMode: "none"},
		},
		{
			name: "port overrides port part of explicit listen addr",
			env:  map[string]string{EnvListenAddr: "10.0.0.1:8080", EnvPort: "9999"},
			want: Config{ListenAddr: "10.0.0.1:9999", KubeconfigSources: []string{"/home/u/.kube/config"}, AuthMode: "none"},
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
			want:   Config{ListenAddr: "127.0.0.1:8080", KubeconfigSources: []string{"/custom/cfg"}, AuthMode: "none"},
		},
		{
			name:   "mounted /kubeconfig wins over KUBECONFIG env",
			env:    map[string]string{"KUBECONFIG": "/env/cfg"},
			exists: map[string]bool{"/kubeconfig": true},
			want:   Config{ListenAddr: "127.0.0.1:8080", KubeconfigSources: []string{"/kubeconfig"}, AuthMode: "none"},
		},
		{
			// The default mount point is accepted as a directory too (ADR-0008);
			// the existence seam abstracts file-vs-dir, so this asserts the probe
			// still resolves it to the single source.
			name:   "mounted /kubeconfig as a directory is accepted",
			env:    map[string]string{},
			exists: map[string]bool{"/kubeconfig": true},
			want:   Config{ListenAddr: "127.0.0.1:8080", KubeconfigSources: []string{"/kubeconfig"}, AuthMode: "none"},
		},
		{
			name: "KUBECONFIG env wins over home fallback",
			env:  map[string]string{"KUBECONFIG": "/env/cfg"},
			want: Config{ListenAddr: "127.0.0.1:8080", KubeconfigSources: []string{"/env/cfg"}, AuthMode: "none"},
		},
		{
			name: "explicit kubeconfig list splits on the path separator, in order",
			env:  map[string]string{EnvKubeconfig: "/a/kubeconfig:/b/dir:/c/cfg"},
			want: Config{ListenAddr: "127.0.0.1:8080", KubeconfigSources: []string{"/a/kubeconfig", "/b/dir", "/c/cfg"}, AuthMode: "none"},
		},
		{
			name: "explicit kubeconfig list drops empty segments",
			env:  map[string]string{EnvKubeconfig: ":/a::/b:"},
			want: Config{ListenAddr: "127.0.0.1:8080", KubeconfigSources: []string{"/a", "/b"}, AuthMode: "none"},
		},
		{
			name:    "explicit kubeconfig of only separators is rejected",
			env:     map[string]string{EnvKubeconfig: ":::"},
			wantErr: "KUBESCOPE_KUBECONFIG",
		},
		{
			name: "KUBECONFIG env list splits on the path separator",
			env:  map[string]string{"KUBECONFIG": "/env/a:/env/b"},
			want: Config{ListenAddr: "127.0.0.1:8080", KubeconfigSources: []string{"/env/a", "/env/b"}, AuthMode: "none"},
		},
		{
			name: "empty KUBECONFIG env falls through to home",
			env:  map[string]string{"KUBECONFIG": ""},
			want: Config{ListenAddr: "127.0.0.1:8080", KubeconfigSources: []string{"/home/u/.kube/config"}, AuthMode: "none"},
		},
		{
			name:    "empty explicit kubeconfig rejected",
			env:     map[string]string{EnvKubeconfig: ""},
			wantErr: "KUBESCOPE_KUBECONFIG",
		},
		{
			name: "read only true",
			env:  map[string]string{EnvReadOnly: "true"},
			want: Config{ListenAddr: "127.0.0.1:8080", KubeconfigSources: []string{"/home/u/.kube/config"}, ReadOnly: true, AuthMode: "none"},
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
				ListenAddr: "127.0.0.1:8080", KubeconfigSources: []string{"/home/u/.kube/config"},
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
			want: Config{ListenAddr: "127.0.0.1:8080", KubeconfigSources: []string{"/home/u/.kube/config"}, AuthMode: "none"},
		},
		{
			name: "allow kubeconfig set defaults to false",
			env:  map[string]string{},
			want: Config{
				ListenAddr: "127.0.0.1:8080", KubeconfigSources: []string{"/home/u/.kube/config"},
				AuthMode: "none", AllowKubeconfigSet: false,
			},
		},
		{
			name: "allow kubeconfig set true",
			env:  map[string]string{EnvAllowKubeconfigSet: "true"},
			want: Config{
				ListenAddr: "127.0.0.1:8080", KubeconfigSources: []string{"/home/u/.kube/config"},
				AuthMode: "none", AllowKubeconfigSet: true,
			},
		},
		{
			name:    "allow kubeconfig set invalid",
			env:     map[string]string{EnvAllowKubeconfigSet: "maybe"},
			wantErr: "KUBESCOPE_ALLOW_KUBECONFIG_SET",
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

// TestStatExistsAcceptsFileAndDirectory pins the ADR-0008 change to the default
// existence probe: the container mount point resolves whether it is a file or a
// directory, and a truly absent path still reports missing.
func TestStatExistsAcceptsFileAndDirectory(t *testing.T) {
	dir := t.TempDir()
	file := dir + "/kubeconfig"
	require.NoError(t, os.WriteFile(file, []byte("x"), 0o600))

	assert.True(t, statExists(file), "a file at the mount point exists")
	assert.True(t, statExists(dir), "a directory at the mount point exists")
	assert.False(t, statExists(dir+"/absent"), "an absent path does not exist")
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
