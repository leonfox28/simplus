package agentapi

import (
	"encoding/json"
	"errors"
	"io"
	"log/slog"
	"net/http"

	"github.com/leonfox28/simplus/internal/buildinfo"
)

func NewSIMAKAHILHandler(service *SIMAKAService, logger *slog.Logger) http.Handler {
	if logger == nil {
		logger = slog.New(slog.NewTextHandler(io.Discard, nil))
	}
	mux := http.NewServeMux()
	mux.HandleFunc("GET /v1/hello", func(w http.ResponseWriter, _ *http.Request) {
		instanceID := ""
		if service != nil && service.monitor != nil {
			instanceID = service.monitor.InstanceID()
		}
		writeJSON(w, http.StatusOK, Hello{
			Protocol: ProtocolName, ProtocolVersion: ProtocolVersion, AgentInstanceID: instanceID,
			Agent: buildInfo(), Features: []string{FeatureSIMAKAHIL, FeatureSIMIMSHIL},
		})
	})
	mux.HandleFunc("POST /v1/sim/ims/profile", func(w http.ResponseWriter, r *http.Request) {
		var request SIMIMSProfileRequest
		if !decodeSIMAKARequest(w, r, &request) {
			logger.WarnContext(r.Context(), "SIM IMS profile request rejected", "code", "REQUEST_INVALID")
			writeJSON(w, http.StatusBadRequest, ErrorResponse{Code: "REQUEST_INVALID", Detail: "invalid SIM IMS profile request"})
			return
		}
		response, err := service.IMSProfile(r.Context(), request)
		if err != nil {
			writeSIMAKAError(w, r, logger, err, request.DeviceID)
			return
		}
		logger.InfoContext(r.Context(), "SIM IMS profile probe completed", "device_id", request.DeviceID,
			"identity_source", response.IdentitySource)
		writeJSON(w, http.StatusOK, response)
	})
	mux.HandleFunc("POST /v1/sim/ims/identity", func(w http.ResponseWriter, r *http.Request) {
		var request SIMIMSIdentityRequest
		if !decodeSIMAKARequest(w, r, &request) {
			logger.WarnContext(r.Context(), "SIM IMS identity request rejected", "code", "REQUEST_INVALID")
			writeJSON(w, http.StatusBadRequest, ErrorResponse{Code: "REQUEST_INVALID", Detail: "invalid SIM IMS identity request"})
			return
		}
		response, err := service.IMSIdentity(r.Context(), request)
		if err != nil {
			writeSIMAKAError(w, r, logger, err, request.DeviceID)
			return
		}
		// Never log IMPI, IMPU, home domain contents or their derived values.
		logger.InfoContext(r.Context(), "SIM IMS identity read completed", "device_id", request.DeviceID,
			"identity_source", response.IdentitySource, "public_identity_count", len(response.PublicIdentities))
		writeJSON(w, http.StatusOK, response)
	})
	mux.HandleFunc("POST /v1/sim/aka/identity", func(w http.ResponseWriter, r *http.Request) {
		var request SIMAKAIdentityRequest
		if !decodeSIMAKARequest(w, r, &request) {
			logger.WarnContext(r.Context(), "SIM AKA request rejected", "code", "REQUEST_INVALID")
			writeJSON(w, http.StatusBadRequest, ErrorResponse{Code: "REQUEST_INVALID", Detail: "invalid SIM AKA identity request"})
			return
		}
		response, err := service.Identity(r.Context(), request)
		if err != nil {
			writeSIMAKAError(w, r, logger, err, request.DeviceID)
			return
		}
		writeJSON(w, http.StatusOK, response)
	})
	mux.HandleFunc("POST /v1/sim/aka/authenticate", func(w http.ResponseWriter, r *http.Request) {
		var request SIMAKAAuthenticationRequest
		if !decodeSIMAKARequest(w, r, &request) {
			logger.WarnContext(r.Context(), "SIM AKA request rejected", "code", "REQUEST_INVALID")
			writeJSON(w, http.StatusBadRequest, ErrorResponse{Code: "REQUEST_INVALID", Detail: "invalid SIM AKA authentication request"})
			return
		}
		response, err := service.Authenticate(r.Context(), request)
		if err != nil {
			writeSIMAKAError(w, r, logger, err, request.DeviceID)
			return
		}
		// Record only the bounded outcome class. Authentication material and the
		// exchange identity remain excluded so HIL can distinguish a completed
		// Agent exchange from a transport/parser failure without secret logs.
		logger.InfoContext(r.Context(), "SIM AKA request completed", "device_id", request.DeviceID, "state", response.Result.State)
		writeJSON(w, http.StatusOK, response)
	})
	return mux
}

func buildInfo() buildinfo.Info { return buildinfo.Current() }

func decodeSIMAKARequest(w http.ResponseWriter, r *http.Request, target any) bool {
	defer r.Body.Close()
	decoder := json.NewDecoder(http.MaxBytesReader(w, r.Body, 8<<10))
	decoder.DisallowUnknownFields()
	if err := decoder.Decode(target); err != nil {
		return false
	}
	return errors.Is(decoder.Decode(&struct{}{}), io.EOF)
}

func writeSIMAKAError(w http.ResponseWriter, r *http.Request, logger *slog.Logger, err error, deviceID string) {
	status := http.StatusServiceUnavailable
	response := ErrorResponse{Code: "SIM_AKA_UNAVAILABLE", Detail: "SIM AKA is unavailable", Retryable: true}
	switch {
	case errors.Is(err, ErrSIMAKARequestInvalid):
		status = http.StatusBadRequest
		response = ErrorResponse{Code: "REQUEST_INVALID", Detail: "invalid SIM AKA request"}
	case errors.Is(err, ErrSIMAKAAgentStale):
		status = http.StatusConflict
		response = ErrorResponse{Code: "AGENT_INSTANCE_STALE", Detail: "Agent instance changed; refresh before retrying", Retryable: true}
	case errors.Is(err, ErrSIMAKASnapshotStale):
		status = http.StatusConflict
		response = ErrorResponse{Code: "SNAPSHOT_STALE", Detail: "hardware snapshot changed; refresh before retrying", Retryable: true}
	case errors.Is(err, ErrSIMAKADeviceStale):
		status = http.StatusConflict
		response = ErrorResponse{Code: "DEVICE_GENERATION_STALE", Detail: "device generation changed; refresh before retrying", Retryable: true}
	case errors.Is(err, ErrSIMAKAIdentityChanged):
		status = http.StatusConflict
		response = ErrorResponse{Code: "SIM_IDENTITY_CHANGED", Detail: "SIM identity changed; create a new exchange"}
	case errors.Is(err, ErrSIMAKAUnsupported):
		status = http.StatusUnprocessableEntity
		response = ErrorResponse{Code: "SIM_AKA_UNSUPPORTED", Detail: "SIM AKA is unsupported for this device"}
	case errors.Is(err, ErrSIMAKASIMNotReady):
		status = http.StatusConflict
		response = ErrorResponse{Code: "SIM_NOT_READY", Detail: "SIM AKA requires a ready SIM", Retryable: true}
	case errors.Is(err, ErrSIMAKARejected):
		status = http.StatusUnauthorized
		response = ErrorResponse{Code: "SIM_AKA_REJECTED", Detail: "SIM rejected the AKA challenge"}
	}
	// Never log RAND, AUTN, IMSI, RES, CK, IK, AUTS or a wrapped error that
	// might contain them. Device IDs and stable error codes are non-secret.
	if stage, ok := SIMIMSHILStage(err); ok {
		if shape, shapeOK := SIMIMSHILShape(err); shapeOK {
			logger.WarnContext(r.Context(), "SIM AKA request failed", "device_id", deviceID, "code", response.Code,
				"stage", stage, "shape", shape)
		} else {
			logger.WarnContext(r.Context(), "SIM AKA request failed", "device_id", deviceID, "code", response.Code, "stage", stage)
		}
	} else {
		logger.WarnContext(r.Context(), "SIM AKA request failed", "device_id", deviceID, "code", response.Code)
	}
	writeJSON(w, status, response)
}
