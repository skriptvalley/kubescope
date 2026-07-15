package resources

import (
	"log/slog"
	"net/http"
)

// ServerConfig is the subset of runtime configuration the frontend needs to
// reflect server-enforced posture — chiefly read-only mode, so the UI can
// disable mutation controls and show a notice. The enforcement itself is
// server-side middleware (ADR-0005); this endpoint is a convenience, not the
// control.
type ServerConfig struct {
	ReadOnly bool   `json:"readOnly"`
	AuthMode string `json:"authMode"`
}

// ConfigHandler serves GET /api/v1/config: the server's read-only flag and auth
// mode. It touches no cluster, so it always succeeds regardless of kubeconfig
// state — the UI can render its read-only banner even when no cluster is
// reachable.
func ConfigHandler(cfg ServerConfig, logger *slog.Logger) http.HandlerFunc {
	return func(w http.ResponseWriter, _ *http.Request) {
		writeJSON(w, logger, http.StatusOK, cfg)
	}
}
