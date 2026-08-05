package mihomosupervisor

import (
	"encoding/json"
	"errors"
	"io"
	"log/slog"
	"net/http"
)

type errorResponse struct {
	Code string `json:"code"`
}

func NewHandler(supervisor API, logger *slog.Logger) http.Handler {
	mux := http.NewServeMux()
	mux.HandleFunc("GET /v1/status", func(w http.ResponseWriter, r *http.Request) {
		status, err := supervisor.Status(r.Context())
		if err != nil {
			writeError(w, logger, err)
			return
		}
		writeJSON(w, http.StatusOK, status)
	})
	mux.HandleFunc("POST /v1/start", func(w http.ResponseWriter, r *http.Request) {
		defer r.Body.Close()
		decoder := json.NewDecoder(http.MaxBytesReader(w, r.Body, 4<<10))
		decoder.DisallowUnknownFields()
		var request StartRequest
		if decoder.Decode(&request) != nil || decoder.Decode(&struct{}{}) != io.EOF {
			writeJSON(w, http.StatusBadRequest, errorResponse{Code: "REQUEST_INVALID"})
			return
		}
		status, err := supervisor.Start(r.Context(), request)
		if err != nil {
			writeError(w, logger, err)
			return
		}
		writeJSON(w, http.StatusOK, status)
	})
	mux.HandleFunc("POST /v1/stop", func(w http.ResponseWriter, r *http.Request) {
		if err := supervisor.Stop(r.Context()); err != nil {
			writeError(w, logger, err)
			return
		}
		w.WriteHeader(http.StatusNoContent)
	})
	return mux
}

func writeError(w http.ResponseWriter, logger *slog.Logger, err error) {
	status, code := http.StatusServiceUnavailable, "SUPERVISOR_FAILED"
	switch {
	case errors.Is(err, ErrRequestInvalid):
		status, code = http.StatusBadRequest, "REQUEST_INVALID"
	case errors.Is(err, ErrAlreadyRunning):
		status, code = http.StatusConflict, "ALREADY_RUNNING"
	case errors.Is(err, ErrNotRunning):
		status, code = http.StatusConflict, "NOT_RUNNING"
	case errors.Is(err, ErrStartupFailed):
		status, code = http.StatusConflict, "STARTUP_FAILED"
	}
	if logger != nil {
		logger.Warn("Mihomo supervisor request failed", "code", code, "error", err)
	}
	writeJSON(w, status, errorResponse{Code: code})
}

func writeJSON(w http.ResponseWriter, status int, value any) {
	w.Header().Set("Content-Type", "application/json")
	w.Header().Set("Cache-Control", "no-store")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(value)
}
