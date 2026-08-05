package agentapi

import (
	"encoding/json"
	"errors"
	"io"
	"log/slog"
	"net/http"
)

func registerSMSHandlers(mux *http.ServeMux, monitor *Monitor, backend SMSBackend, logger *slog.Logger) {
	mux.HandleFunc("POST /v1/sms/list", func(w http.ResponseWriter, r *http.Request) {
		var request SMSListRequest
		if !decodeSMSRequest(w, r, &request) || validateSMSListRequest(request) != nil {
			writeJSON(w, http.StatusBadRequest, ErrorResponse{Code: "REQUEST_INVALID", Detail: "invalid SMS list request"})
			return
		}
		if err := validateSMSRuntimeTarget(monitor, request.AgentInstanceID, request.DeviceID); err != nil {
			writeSMSError(w, r, logger, err, "SMS list rejected")
			return
		}
		messages, err := backend.ListSMS(r.Context(), request.DeviceID)
		if err != nil {
			writeSMSError(w, r, logger, err, "SMS list failed")
			return
		}
		response := SMSListResponse{ProtocolVersion: ProtocolVersion, AgentInstanceID: monitor.InstanceID(), Messages: messages}
		if err := validateSMSListResponse(response, request); err != nil {
			logger.Error("SMS backend returned an invalid list", "device_id", request.DeviceID, "error", err)
			writeJSON(w, http.StatusServiceUnavailable, ErrorResponse{Code: "SMS_BACKEND_INVALID", Detail: "SMS backend returned an invalid list", Retryable: true})
			return
		}
		writeJSON(w, http.StatusOK, response)
	})

	mux.HandleFunc("POST /v1/sms/read", func(w http.ResponseWriter, r *http.Request) {
		var request SMSReadRequest
		if !decodeSMSRequest(w, r, &request) || validateSMSReadRequest(request) != nil {
			writeJSON(w, http.StatusBadRequest, ErrorResponse{Code: "REQUEST_INVALID", Detail: "invalid SMS read request"})
			return
		}
		if err := validateSMSRuntimeTarget(monitor, request.AgentInstanceID, request.DeviceID); err != nil {
			writeSMSError(w, r, logger, err, "SMS read rejected")
			return
		}
		message, err := backend.ReadSMS(r.Context(), request.DeviceID, request.MessageID)
		if err != nil {
			writeSMSError(w, r, logger, err, "SMS read failed")
			return
		}
		response := SMSReadResponse{ProtocolVersion: ProtocolVersion, AgentInstanceID: monitor.InstanceID(), Message: message}
		if err := validateSMSReadResponse(response, request); err != nil {
			logger.Error("SMS backend returned an invalid message", "device_id", request.DeviceID, "message_id", request.MessageID, "error", err)
			writeJSON(w, http.StatusServiceUnavailable, ErrorResponse{Code: "SMS_BACKEND_INVALID", Detail: "SMS backend returned an invalid message", Retryable: true})
			return
		}
		writeJSON(w, http.StatusOK, response)
	})

	mux.HandleFunc("POST /v1/sms/send", func(w http.ResponseWriter, r *http.Request) {
		var request SMSSendRequest
		if !decodeSMSRequest(w, r, &request) || validateSMSSendRequest(request) != nil {
			writeJSON(w, http.StatusBadRequest, ErrorResponse{Code: "REQUEST_INVALID", Detail: "invalid SMS send request"})
			return
		}
		if err := validateSMSRuntimeTarget(monitor, request.AgentInstanceID, request.DeviceID); err != nil {
			writeSMSError(w, r, logger, err, "SMS send rejected")
			return
		}
		submission, err := backend.SendSMS(r.Context(), request)
		if err != nil {
			writeSMSError(w, r, logger, err, "SMS send failed")
			return
		}
		response := SMSSendResponse{ProtocolVersion: ProtocolVersion, AgentInstanceID: monitor.InstanceID(), Submission: submission}
		if err := validateSMSSendResponse(response, request); err != nil {
			logger.Error("SMS backend returned an invalid submission", "device_id", request.DeviceID, "operation_id", request.OperationID, "error", err)
			writeJSON(w, http.StatusServiceUnavailable, ErrorResponse{Code: "SMS_BACKEND_INVALID", Detail: "SMS backend returned an invalid submission", Retryable: true})
			return
		}
		writeJSON(w, http.StatusOK, response)
	})

	mux.HandleFunc("POST /v1/sms/acknowledge", func(w http.ResponseWriter, r *http.Request) {
		var request SMSAcknowledgeRequest
		if !decodeSMSRequest(w, r, &request) || validateSMSAcknowledgeRequest(request) != nil {
			writeJSON(w, http.StatusBadRequest, ErrorResponse{Code: "REQUEST_INVALID", Detail: "invalid SMS acknowledge request"})
			return
		}
		if err := validateSMSRuntimeTarget(monitor, request.AgentInstanceID, request.DeviceID); err != nil {
			writeSMSError(w, r, logger, err, "SMS acknowledge rejected")
			return
		}
		acknowledged, err := backend.AcknowledgeSMS(r.Context(), request)
		if err != nil {
			writeSMSError(w, r, logger, err, "SMS acknowledge failed")
			return
		}
		response := SMSAcknowledgeResponse{
			ProtocolVersion: ProtocolVersion, AgentInstanceID: monitor.InstanceID(), OperationID: request.OperationID,
			MessageID: request.MessageID, Acknowledged: acknowledged,
		}
		if err := validateSMSAcknowledgeResponse(response, request); err != nil {
			logger.Error("SMS backend returned an invalid acknowledgement", "device_id", request.DeviceID, "message_id", request.MessageID, "error", err)
			writeJSON(w, http.StatusServiceUnavailable, ErrorResponse{Code: "SMS_BACKEND_INVALID", Detail: "SMS backend returned an invalid acknowledgement", Retryable: true})
			return
		}
		writeJSON(w, http.StatusOK, response)
	})
}

func decodeSMSRequest(w http.ResponseWriter, r *http.Request, target any) bool {
	defer r.Body.Close()
	decoder := json.NewDecoder(http.MaxBytesReader(w, r.Body, 16<<10))
	decoder.DisallowUnknownFields()
	if err := decoder.Decode(target); err != nil {
		return false
	}
	return errors.Is(decoder.Decode(&struct{}{}), io.EOF)
}

func validateSMSRuntimeTarget(monitor *Monitor, instanceID, deviceID string) error {
	if monitor == nil || instanceID != monitor.InstanceID() {
		return ErrSMSAgentStale
	}
	for _, device := range monitor.Snapshot().Devices {
		if device.ID == deviceID {
			return nil
		}
	}
	return ErrSMSDeviceNotFound
}

func writeSMSError(w http.ResponseWriter, r *http.Request, logger *slog.Logger, err error, message string) {
	status := http.StatusServiceUnavailable
	response := ErrorResponse{Code: "SMS_UNAVAILABLE", Detail: "SMS backend is unavailable", Retryable: true}
	switch {
	case errors.Is(err, ErrSMSRequestInvalid):
		status = http.StatusBadRequest
		response = ErrorResponse{Code: "REQUEST_INVALID", Detail: "invalid SMS request"}
	case errors.Is(err, ErrSMSAgentStale):
		status = http.StatusConflict
		response = ErrorResponse{Code: "AGENT_INSTANCE_STALE", Detail: "Agent instance changed; refresh before retrying", Retryable: true}
	case errors.Is(err, ErrSMSDeviceNotFound):
		status = http.StatusNotFound
		response = ErrorResponse{Code: "SMS_DEVICE_NOT_FOUND", Detail: "SMS device is not present", Retryable: true}
	case errors.Is(err, ErrSMSUnsupported):
		status = http.StatusUnprocessableEntity
		response = ErrorResponse{Code: "SMS_UNSUPPORTED", Detail: "SMS is unsupported for this device"}
	case errors.Is(err, ErrSMSMessageNotFound):
		status = http.StatusNotFound
		response = ErrorResponse{Code: "SMS_MESSAGE_NOT_FOUND", Detail: "SMS message is not present"}
	case errors.Is(err, ErrSMSOperationConflict):
		status = http.StatusConflict
		response = ErrorResponse{Code: "OPERATION_REPLAY_CONFLICT", Detail: "operation id was already used for different SMS parameters"}
	case errors.Is(err, ErrSMSOutcomeUnknown):
		status = http.StatusConflict
		response = ErrorResponse{Code: "SMS_SEND_OUTCOME_UNKNOWN", Detail: "SMS may have been submitted; do not resend it automatically"}
	default:
		logger.WarnContext(r.Context(), message, "error", err)
	}
	writeJSON(w, status, response)
}
