package resources

import (
	"encoding/json"
	"log/slog"
	"net/http"
)

// apiError is the structured error envelope every /api handler returns on
// failure: {"error": {"code": "...", "message": "...", "guidance": "..."}}.
// Guidance is optional remediation text (e.g. ADR-0004 exec-plugin guidance).
type apiError struct {
	Code     string `json:"code"`
	Message  string `json:"message"`
	Guidance string `json:"guidance,omitempty"`
}

type errorEnvelope struct {
	Error apiError `json:"error"`
}

func writeJSON(w http.ResponseWriter, logger *slog.Logger, status int, body any) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	if err := json.NewEncoder(w).Encode(body); err != nil {
		logger.Error("encoding response", "error", err)
	}
}

func writeError(w http.ResponseWriter, logger *slog.Logger, status int, code, message string) {
	writeJSON(w, logger, status, errorEnvelope{Error: apiError{Code: code, Message: message}})
}

// writeErrorGuidance is writeError with remediation text attached.
func writeErrorGuidance(w http.ResponseWriter, logger *slog.Logger, status int, code, message, guidance string) {
	writeJSON(w, logger, status, errorEnvelope{Error: apiError{Code: code, Message: message, Guidance: guidance}})
}
