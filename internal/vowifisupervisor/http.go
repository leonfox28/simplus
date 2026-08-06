package vowifisupervisor

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
	sms, _ := supervisor.(SMSAPI)
	mux.HandleFunc("GET /status", func(w http.ResponseWriter, r *http.Request) {
		statuses, err := supervisor.List(r.Context())
		if err != nil {
			writeError(w, logger, err)
			return
		}
		writeJSON(w, http.StatusOK, StatusList{Lines: statuses})
	})
	mux.HandleFunc("POST /start", func(w http.ResponseWriter, r *http.Request) {
		var request StartRequest
		if !decodeRequest(w, r, &request) {
			writeJSON(w, http.StatusBadRequest, errorResponse{Code: "REQUEST_INVALID"})
			return
		}
		status, err := supervisor.Start(r.Context(), request)
		if err != nil {
			writeError(w, logger, err)
			return
		}
		writeJSON(w, http.StatusAccepted, status)
	})
	mux.HandleFunc("POST /stop", func(w http.ResponseWriter, r *http.Request) {
		var request StopRequest
		if !decodeRequest(w, r, &request) {
			writeJSON(w, http.StatusBadRequest, errorResponse{Code: "REQUEST_INVALID"})
			return
		}
		status, err := supervisor.Stop(r.Context(), request.LineID)
		if err != nil {
			writeError(w, logger, err)
			return
		}
		writeJSON(w, http.StatusOK, status)
	})
	mux.HandleFunc("POST /sms/send", func(w http.ResponseWriter, r *http.Request) {
		var request SMSSendRequest
		if sms == nil || !decodeRequest(w, r, &request) || !validSMSSendRequest(request) {
			writeError(w, logger, chooseSMSRequestError(sms))
			return
		}
		response, err := sms.SendSMS(r.Context(), request)
		if err != nil {
			writeError(w, logger, err)
			return
		}
		writeJSON(w, http.StatusOK, response)
	})
	mux.HandleFunc("POST /sms/list", func(w http.ResponseWriter, r *http.Request) {
		var request SMSListRequest
		if sms == nil || !decodeRequest(w, r, &request) || !managedLinePattern.MatchString(request.LineID) {
			writeError(w, logger, chooseSMSRequestError(sms))
			return
		}
		messages, err := sms.ListSMS(r.Context(), request.LineID)
		if err != nil {
			writeError(w, logger, err)
			return
		}
		writeJSON(w, http.StatusOK, SMSListResponse{Messages: messages})
	})
	mux.HandleFunc("POST /sms/read", func(w http.ResponseWriter, r *http.Request) {
		var request SMSReadRequest
		if sms == nil || !decodeRequest(w, r, &request) || !validSMSMessageRequest(request.LineID, request.MessageID) {
			writeError(w, logger, chooseSMSRequestError(sms))
			return
		}
		message, err := sms.ReadSMS(r.Context(), request.LineID, request.MessageID)
		if err != nil {
			writeError(w, logger, err)
			return
		}
		writeJSON(w, http.StatusOK, message)
	})
	mux.HandleFunc("POST /sms/acknowledge", func(w http.ResponseWriter, r *http.Request) {
		var request SMSAcknowledgeRequest
		if sms == nil || !decodeRequest(w, r, &request) || !validSMSAcknowledgeRequest(request) {
			writeError(w, logger, chooseSMSRequestError(sms))
			return
		}
		if err := sms.AcknowledgeSMS(r.Context(), request); err != nil {
			writeError(w, logger, err)
			return
		}
		writeJSON(w, http.StatusOK, SMSAcknowledgeResponse{Acknowledged: true})
	})
	mux.HandleFunc("POST /sms/reports/list", func(w http.ResponseWriter, r *http.Request) {
		var request SMSListRequest
		if sms == nil || !decodeRequest(w, r, &request) || !managedLinePattern.MatchString(request.LineID) {
			writeError(w, logger, chooseSMSRequestError(sms))
			return
		}
		response, err := sms.ListSMSSubmitReports(r.Context(), request.LineID)
		if err != nil {
			writeError(w, logger, err)
			return
		}
		writeJSON(w, http.StatusOK, response)
	})
	mux.HandleFunc("POST /sms/reports/acknowledge", func(w http.ResponseWriter, r *http.Request) {
		var request SMSSubmitReportAcknowledgeRequest
		if sms == nil || !decodeRequest(w, r, &request) || !validSMSSubmitReportAcknowledgeRequest(request) {
			writeError(w, logger, chooseSMSRequestError(sms))
			return
		}
		if err := sms.AcknowledgeSMSSubmitReport(r.Context(), request); err != nil {
			writeError(w, logger, err)
			return
		}
		writeJSON(w, http.StatusOK, SMSAcknowledgeResponse{Acknowledged: true})
	})
	return mux
}

func chooseSMSRequestError(sms SMSAPI) error {
	if sms == nil {
		return ErrSMSUnavailable
	}
	return ErrRequestInvalid
}

func decodeRequest(w http.ResponseWriter, r *http.Request, target any) bool {
	defer r.Body.Close()
	decoder := json.NewDecoder(http.MaxBytesReader(w, r.Body, 8<<10))
	decoder.DisallowUnknownFields()
	if decoder.Decode(target) != nil {
		return false
	}
	return errors.Is(decoder.Decode(&struct{}{}), io.EOF)
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
	case errors.Is(err, ErrSMSMessageNotFound):
		status, code = http.StatusNotFound, "SMS_MESSAGE_NOT_FOUND"
	case errors.Is(err, ErrSMSOutcomeUnknown):
		status, code = http.StatusGatewayTimeout, "SMS_SEND_OUTCOME_UNKNOWN"
	case errors.Is(err, ErrSMSRejected):
		status, code = http.StatusUnprocessableEntity, "SMS_REJECTED"
	case errors.Is(err, ErrSMSUnavailable):
		status, code = http.StatusServiceUnavailable, "SMS_UNAVAILABLE"
	}
	if logger != nil {
		logger.Warn("Host VoWiFi supervisor request failed", "code", code, "error", err)
	}
	writeJSON(w, status, errorResponse{Code: code})
}

func writeJSON(w http.ResponseWriter, status int, value any) {
	w.Header().Set("Content-Type", "application/json")
	w.Header().Set("Cache-Control", "no-store")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(value)
}
