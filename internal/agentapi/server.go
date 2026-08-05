package agentapi

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"net/http"
	"strconv"
	"strings"
	"time"

	"github.com/leonfox28/simplus/internal/buildinfo"
)

type ErrorResponse struct {
	Code      string `json:"code"`
	Detail    string `json:"detail"`
	Retryable bool   `json:"retryable,omitempty"`
}

func (response *ErrorResponse) Error() string {
	if response == nil {
		return "Agent request failed"
	}
	return fmt.Sprintf("agent %s: %s", response.Code, response.Detail)
}

func (response *ErrorResponse) Is(target error) bool {
	return response != nil && response.Code == "SMS_SEND_OUTCOME_UNKNOWN" && target == ErrSMSOutcomeUnknown
}

func NewHandler(monitor *Monitor, commands *CommandService, logger *slog.Logger, smsBackends ...SMSBackend) http.Handler {
	return newHandler(monitor, commands, logger, false, smsBackends...)
}

// NewReadOnlyHardwareHandler is the production V1 hardware boundary. Its
// signature deliberately provides no way to inject command or SMS backends.
func NewReadOnlyHardwareHandler(monitor *Monitor, logger *slog.Logger) http.Handler {
	return newHandler(monitor, nil, logger, true)
}

func newHandler(monitor *Monitor, commands *CommandService, logger *slog.Logger, hardwareReadOnly bool, smsBackends ...SMSBackend) http.Handler {
	if logger == nil {
		logger = slog.New(slog.NewTextHandler(io.Discard, nil))
	}
	var smsBackend SMSBackend
	if len(smsBackends) != 0 {
		smsBackend = smsBackends[0]
	}
	mux := http.NewServeMux()
	mux.HandleFunc("GET /v1/hello", func(w http.ResponseWriter, _ *http.Request) {
		features := []string{"hotplug-generation", "restart-fencing", "read-only-probe", "typed-capability-report"}
		if hardwareReadOnly {
			features = append(features, FeatureHardwareReadOnly)
		}
		if commands != nil {
			features = append(features, "durable-command-outcomes", CommandRadioEnsureOff)
		}
		if smsBackend != nil {
			features = append(features, FeatureSMS)
		}
		writeJSON(w, http.StatusOK, Hello{
			Protocol: ProtocolName, ProtocolVersion: ProtocolVersion, AgentInstanceID: monitor.InstanceID(), Agent: buildinfo.Current(),
			Features: features,
		})
	})
	mux.HandleFunc("GET /v1/snapshot", func(w http.ResponseWriter, r *http.Request) {
		refresh := r.URL.Query().Get("refresh")
		if refresh != "" && refresh != "true" && refresh != "false" {
			writeJSON(w, http.StatusBadRequest, ErrorResponse{Code: "REQUEST_INVALID", Detail: "refresh must be true or false"})
			return
		}
		snapshot := monitor.Snapshot()
		var err error
		if refresh == "true" || snapshot.Generation == 0 {
			snapshot, err = monitor.Refresh(r.Context())
		}
		if err != nil {
			logger.Error("hardware snapshot failed", "error", err)
			writeJSON(w, http.StatusServiceUnavailable, ErrorResponse{Code: "HARDWARE_SCAN_FAILED", Detail: "hardware scan failed"})
			return
		}
		writeJSON(w, http.StatusOK, snapshot)
	})
	mux.HandleFunc("GET /v1/changes", func(w http.ResponseWriter, r *http.Request) {
		instanceID := r.URL.Query().Get("instanceId")
		if !IsValidAgentInstanceID(instanceID) {
			writeJSON(w, http.StatusBadRequest, ErrorResponse{Code: "REQUEST_INVALID", Detail: "instanceId must identify the current Agent process"})
			return
		}
		after, err := strconv.ParseUint(r.URL.Query().Get("after"), 10, 64)
		if err != nil {
			writeJSON(w, http.StatusBadRequest, ErrorResponse{Code: "REQUEST_INVALID", Detail: "after must be an unsigned generation"})
			return
		}
		timeout := 25 * time.Second
		if text := r.URL.Query().Get("timeoutSeconds"); text != "" {
			seconds, parseErr := strconv.Atoi(text)
			if parseErr != nil || seconds < 1 || seconds > 30 {
				writeJSON(w, http.StatusBadRequest, ErrorResponse{Code: "REQUEST_INVALID", Detail: "timeoutSeconds must be from 1 through 30"})
				return
			}
			timeout = time.Duration(seconds) * time.Second
		}
		ctx, cancel := contextWithTimeout(r, timeout)
		defer cancel()
		snapshot, changed, err := monitor.WaitForChange(ctx, instanceID, after)
		if err != nil {
			writeJSON(w, http.StatusServiceUnavailable, ErrorResponse{Code: "HARDWARE_WATCH_FAILED", Detail: "hardware watch failed"})
			return
		}
		writeJSON(w, http.StatusOK, ChangeResponse{Changed: changed, Snapshot: snapshot})
	})
	mux.HandleFunc("POST /v1/probes/read-only", func(w http.ResponseWriter, r *http.Request) {
		defer r.Body.Close()
		decoder := json.NewDecoder(http.MaxBytesReader(w, r.Body, 16<<10))
		decoder.DisallowUnknownFields()
		var request ProbeRequest
		if err := decoder.Decode(&request); err != nil {
			writeJSON(w, http.StatusBadRequest, ErrorResponse{Code: "REQUEST_INVALID", Detail: "invalid read-only probe request"})
			return
		}
		if err := decoder.Decode(&struct{}{}); !errors.Is(err, io.EOF) {
			writeJSON(w, http.StatusBadRequest, ErrorResponse{Code: "REQUEST_INVALID", Detail: "request must contain one JSON object"})
			return
		}
		if len(request.DeviceIDs) > 64 {
			writeJSON(w, http.StatusBadRequest, ErrorResponse{Code: "REQUEST_INVALID", Detail: "deviceIds exceeds the bounded device count"})
			return
		}
		seenDeviceIDs := make(map[string]struct{}, len(request.DeviceIDs))
		for _, id := range request.DeviceIDs {
			if strings.TrimSpace(id) == "" || len(id) > 128 {
				writeJSON(w, http.StatusBadRequest, ErrorResponse{Code: "REQUEST_INVALID", Detail: "deviceIds contains an invalid id"})
				return
			}
			if _, duplicate := seenDeviceIDs[id]; duplicate {
				writeJSON(w, http.StatusBadRequest, ErrorResponse{Code: "REQUEST_INVALID", Detail: "deviceIds contains a duplicate id"})
				return
			}
			seenDeviceIDs[id] = struct{}{}
		}
		response, err := monitor.Probe(r.Context(), request.DeviceIDs)
		if err != nil {
			logger.Error("read-only hardware probe failed", "error", err)
			writeJSON(w, http.StatusServiceUnavailable, ErrorResponse{Code: "READ_ONLY_PROBE_FAILED", Detail: "read-only probe failed"})
			return
		}
		writeJSON(w, http.StatusOK, response)
	})
	if commands != nil {
		mux.HandleFunc("POST /v1/commands/radio/ensure-off", func(w http.ResponseWriter, r *http.Request) {
			defer r.Body.Close()
			decoder := json.NewDecoder(http.MaxBytesReader(w, r.Body, 32<<10))
			decoder.DisallowUnknownFields()
			var request RadioEnsureOffRequest
			if err := decoder.Decode(&request); err != nil {
				writeJSON(w, http.StatusBadRequest, ErrorResponse{Code: "REQUEST_INVALID", Detail: "invalid radio.ensure-off request"})
				return
			}
			if err := decoder.Decode(&struct{}{}); !errors.Is(err, io.EOF) {
				writeJSON(w, http.StatusBadRequest, ErrorResponse{Code: "REQUEST_INVALID", Detail: "request must contain one JSON object"})
				return
			}
			response, err := commands.EnsureRadioOff(r.Context(), request)
			if err != nil {
				status, apiError := classifyCommandError(err)
				logger.Warn("radio.ensure-off rejected", "code", apiError.Code, "error", err)
				writeJSON(w, status, apiError)
				return
			}
			writeJSON(w, http.StatusOK, response)
		})
	}
	if smsBackend != nil {
		registerSMSHandlers(mux, monitor, smsBackend, logger)
	}
	return mux
}

func classifyCommandError(err error) (int, ErrorResponse) {
	status := http.StatusServiceUnavailable
	response := ErrorResponse{Code: "COMMAND_UNAVAILABLE", Detail: "hardware command is unavailable", Retryable: true}
	switch {
	case errors.Is(err, ErrCommandRequestInvalid):
		status = http.StatusBadRequest
		response = ErrorResponse{Code: "REQUEST_INVALID", Detail: "invalid radio.ensure-off request"}
	case errors.Is(err, ErrCommandAgentStale):
		status = http.StatusConflict
		response = ErrorResponse{Code: "AGENT_INSTANCE_STALE", Detail: "Agent instance changed; refresh before retrying", Retryable: true}
	case errors.Is(err, ErrCommandSnapshotStale):
		status = http.StatusConflict
		response = ErrorResponse{Code: "SNAPSHOT_STALE", Detail: "hardware snapshot changed; refresh before retrying", Retryable: true}
	case errors.Is(err, ErrCommandDeviceStale):
		status = http.StatusConflict
		response = ErrorResponse{Code: "DEVICE_GENERATION_STALE", Detail: "device generation changed; refresh before retrying", Retryable: true}
	case errors.Is(err, ErrOutcomeFenceStale):
		status = http.StatusConflict
		response = ErrorResponse{Code: "RESOURCE_FENCE_STALE", Detail: "resource fence is stale"}
	case errors.Is(err, ErrOutcomeReplayConflict):
		status = http.StatusConflict
		response = ErrorResponse{Code: "OPERATION_REPLAY_CONFLICT", Detail: "operation id was already used for different parameters"}
	case errors.Is(err, ErrCommandUnsupported):
		status = http.StatusUnprocessableEntity
		response = ErrorResponse{Code: "COMMAND_UNSUPPORTED", Detail: "radio.ensure-off is unsupported for this device"}
	case errors.Is(err, ErrOutcomePending):
		response = ErrorResponse{Code: "OUTCOME_RECONCILIATION_PENDING", Detail: "a prior outcome must be reconciled", Retryable: true}
	case errors.Is(err, ErrOutcomeLedgerFull):
		response = ErrorResponse{Code: "OUTCOME_LEDGER_FULL", Detail: "Agent outcome ledger cannot accept another command"}
	case errors.Is(err, ErrCommandPersistence):
		response = ErrorResponse{Code: "OUTCOME_PERSIST_FAILED", Detail: "Agent could not persist the command outcome", Retryable: true}
	}
	return status, response
}

func contextWithTimeout(r *http.Request, timeout time.Duration) (context.Context, context.CancelFunc) {
	return context.WithTimeout(r.Context(), timeout)
}

func writeJSON(w http.ResponseWriter, status int, value any) {
	w.Header().Set("Content-Type", "application/json")
	w.Header().Set("Cache-Control", "no-store")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(value)
}
