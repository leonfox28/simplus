package vowifisupervisor

import (
	"context"
	"encoding/json"
	"errors"
	"io"
	"net"
	"net/http"
	"os"
	"path/filepath"
	"sync"
	"time"

	"github.com/leonfox28/simplus/internal/agentapi"
	"github.com/leonfox28/simplus/internal/smscodec"
	"github.com/leonfox28/simplus/internal/vowifihil"
)

const workerSMSSocketName = "sms.sock"

type workerSMSService struct {
	session *vowifihil.IMSSession
	mu      sync.Mutex
	tpMR    byte
}

func (service *workerSMSService) SendSMS(ctx context.Context, request SMSSendRequest) (SMSSendResponse, error) {
	if service == nil || service.session == nil || !validSMSSendRequest(request) || !service.session.SMSAvailable() {
		return SMSSendResponse{}, ErrSMSUnavailable
	}
	segments, err := smscodec.Encode(request.Body)
	if err != nil {
		return SMSSendResponse{}, ErrRequestInvalid
	}
	service.mu.Lock()
	defer service.mu.Unlock()
	tpdus := make([][]byte, 0, len(segments))
	defer func() {
		for _, tpdu := range tpdus {
			zero(tpdu)
		}
	}()
	for _, segment := range segments {
		pdu, err := smscodec.EncodeSubmitPDU(request.Destination, segment, service.tpMR)
		if err != nil || len(pdu.Bytes) < 2 {
			return SMSSendResponse{}, ErrRequestInvalid
		}
		service.tpMR++
		tpdus = append(tpdus, append([]byte(nil), pdu.Bytes[1:]...))
		zero(pdu.Bytes)
	}
	dispatchCtx, cancel := context.WithTimeout(ctx, agentapi.SMSDispatchTimeout)
	defer cancel()
	submission, err := service.session.SubmitSMS(dispatchCtx, request.MessageID, tpdus)
	if err != nil {
		return SMSSendResponse{}, mapIMSSMSError(err)
	}
	return SMSSendResponse{
		ProviderMessageID: submission.ProviderMessageID, State: submission.State, ErrorCode: submission.ErrorCode,
	}, nil
}

func (service *workerSMSService) ListSMS(context.Context, string) ([]SMSMessageReference, error) {
	if service == nil || service.session == nil || !service.session.SMSAvailable() {
		return nil, ErrSMSUnavailable
	}
	references := service.session.ListSMS()
	result := make([]SMSMessageReference, 0, len(references))
	for _, reference := range references {
		result = append(result, SMSMessageReference{MessageID: reference.MessageID, ReceivedAt: reference.ReceivedAt})
	}
	return result, nil
}

func (service *workerSMSService) ReadSMS(_ context.Context, _ string, messageID string) (SMSMessage, error) {
	if service == nil || service.session == nil || !validOpaqueWorkerSMSID(messageID) {
		return SMSMessage{}, ErrSMSMessageNotFound
	}
	message, err := service.session.ReadSMS(messageID)
	if err != nil {
		return SMSMessage{}, mapIMSSMSError(err)
	}
	return SMSMessage{
		MessageID: message.MessageID, Sender: message.Sender, Body: message.Body,
		Encoding: string(message.Segment.Encoding), ConcatenationReference: int(message.Segment.Reference),
		Part: message.Segment.Part, Total: message.Segment.Total, UnitCount: message.Segment.UnitCount,
		UserData: append([]byte(nil), message.Segment.UserData...), ReceivedAt: message.ReceivedAt,
	}, nil
}

func (service *workerSMSService) AcknowledgeSMS(ctx context.Context, request SMSAcknowledgeRequest) error {
	if service == nil || service.session == nil || !validSMSAcknowledgeRequest(request) {
		return ErrRequestInvalid
	}
	return mapIMSSMSError(service.session.AcknowledgeSMS(ctx, request.MessageID, request.OperationID))
}

func (service *workerSMSService) ListSMSSubmitReports(context.Context, string) (SMSSubmitReportListResponse, error) {
	if service == nil || service.session == nil || !service.session.SMSAvailable() {
		return SMSSubmitReportListResponse{}, ErrSMSUnavailable
	}
	reports := service.session.ListSMSSubmitReports()
	result := make([]SMSSubmitReport, 0, len(reports))
	for _, report := range reports {
		result = append(result, SMSSubmitReport{
			MessageID: report.MessageID, ProviderMessageID: report.ProviderMessageID,
			State: report.State, ErrorCode: report.ErrorCode, Cause: int(report.Cause), CompletedAt: report.CompletedAt,
		})
	}
	snapshot := service.session.SMSProtocolSnapshot()
	return SMSSubmitReportListResponse{
		Reports: result,
		Diagnostics: SMSProtocolDiagnostics{
			SIPRequests: snapshot.SIPRequests, SIPParseFailures: snapshot.SIPParseFailures,
			RPParseFailures: snapshot.RPParseFailures, RPDataDeliveries: snapshot.RPDataDeliveries,
			RPACKs: snapshot.RPACKs, RPErrors: snapshot.RPErrors, CorrelationFailures: snapshot.CorrelationFailures,
			ReportTimeouts: snapshot.ReportTimeouts,
		},
	}, nil
}

func (service *workerSMSService) AcknowledgeSMSSubmitReport(_ context.Context, request SMSSubmitReportAcknowledgeRequest) error {
	if service == nil || service.session == nil || !validSMSSubmitReportAcknowledgeRequest(request) {
		return ErrRequestInvalid
	}
	return mapIMSSMSError(service.session.AcknowledgeSMSSubmitReport(request.ProviderMessageID))
}

func mapIMSSMSError(err error) error {
	switch {
	case err == nil:
		return nil
	case errors.Is(err, vowifihil.ErrIMSSMSMessageNotFound):
		return ErrSMSMessageNotFound
	case errors.Is(err, vowifihil.ErrIMSSMSOutcomeUnknown):
		return ErrSMSOutcomeUnknown
	case errors.Is(err, vowifihil.ErrIMSSMSRejected):
		return ErrSMSRejected
	default:
		return ErrSMSUnavailable
	}
}

func startWorkerSMSServer(runtimeDir string, service SMSAPI) (*http.Server, net.Listener, <-chan error, error) {
	path := filepath.Join(runtimeDir, workerSMSSocketName)
	if err := os.Remove(path); err != nil && !errors.Is(err, os.ErrNotExist) {
		return nil, nil, nil, err
	}
	listener, err := net.Listen("unix", path)
	if err != nil {
		return nil, nil, nil, err
	}
	if err := os.Chmod(path, 0o600); err != nil {
		_ = listener.Close()
		_ = os.Remove(path)
		return nil, nil, nil, err
	}
	server := &http.Server{
		Handler: workerSMSHandler(service), ReadHeaderTimeout: 3 * time.Second,
		ReadTimeout: 10 * time.Second, WriteTimeout: agentapi.SMSRequestTimeout,
		IdleTimeout: 15 * time.Second, MaxHeaderBytes: 8 << 10,
	}
	errors := make(chan error, 1)
	go func() { errors <- server.Serve(listener) }()
	return server, listener, errors, nil
}

func stopWorkerSMSServer(server *http.Server, listener net.Listener, runtimeDir string) {
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()
	if server != nil {
		_ = server.Shutdown(ctx)
	}
	if listener != nil {
		_ = listener.Close()
	}
	_ = os.Remove(filepath.Join(runtimeDir, workerSMSSocketName))
}

func workerSMSHandler(service SMSAPI) http.Handler {
	mux := http.NewServeMux()
	mux.HandleFunc("POST /send", func(w http.ResponseWriter, r *http.Request) {
		var request SMSSendRequest
		if !decodeWorkerSMSRequest(w, r, &request) || !validSMSSendRequest(request) {
			writeWorkerSMSError(w, ErrRequestInvalid)
			return
		}
		response, err := service.SendSMS(r.Context(), request)
		if err != nil {
			writeWorkerSMSError(w, err)
			return
		}
		writeJSON(w, http.StatusOK, response)
	})
	mux.HandleFunc("POST /list", func(w http.ResponseWriter, r *http.Request) {
		var request SMSListRequest
		if !decodeWorkerSMSRequest(w, r, &request) || !managedLinePattern.MatchString(request.LineID) {
			writeWorkerSMSError(w, ErrRequestInvalid)
			return
		}
		messages, err := service.ListSMS(r.Context(), request.LineID)
		if err != nil {
			writeWorkerSMSError(w, err)
			return
		}
		writeJSON(w, http.StatusOK, SMSListResponse{Messages: messages})
	})
	mux.HandleFunc("POST /read", func(w http.ResponseWriter, r *http.Request) {
		var request SMSReadRequest
		if !decodeWorkerSMSRequest(w, r, &request) || !validSMSMessageRequest(request.LineID, request.MessageID) {
			writeWorkerSMSError(w, ErrRequestInvalid)
			return
		}
		message, err := service.ReadSMS(r.Context(), request.LineID, request.MessageID)
		if err != nil {
			writeWorkerSMSError(w, err)
			return
		}
		writeJSON(w, http.StatusOK, message)
	})
	mux.HandleFunc("POST /acknowledge", func(w http.ResponseWriter, r *http.Request) {
		var request SMSAcknowledgeRequest
		if !decodeWorkerSMSRequest(w, r, &request) || !validSMSAcknowledgeRequest(request) {
			writeWorkerSMSError(w, ErrRequestInvalid)
			return
		}
		if err := service.AcknowledgeSMS(r.Context(), request); err != nil {
			writeWorkerSMSError(w, err)
			return
		}
		writeJSON(w, http.StatusOK, SMSAcknowledgeResponse{Acknowledged: true})
	})
	mux.HandleFunc("POST /reports/list", func(w http.ResponseWriter, r *http.Request) {
		var request SMSListRequest
		if !decodeWorkerSMSRequest(w, r, &request) || !managedLinePattern.MatchString(request.LineID) {
			writeWorkerSMSError(w, ErrRequestInvalid)
			return
		}
		response, err := service.ListSMSSubmitReports(r.Context(), request.LineID)
		if err != nil {
			writeWorkerSMSError(w, err)
			return
		}
		writeJSON(w, http.StatusOK, response)
	})
	mux.HandleFunc("POST /reports/acknowledge", func(w http.ResponseWriter, r *http.Request) {
		var request SMSSubmitReportAcknowledgeRequest
		if !decodeWorkerSMSRequest(w, r, &request) || !validSMSSubmitReportAcknowledgeRequest(request) {
			writeWorkerSMSError(w, ErrRequestInvalid)
			return
		}
		if err := service.AcknowledgeSMSSubmitReport(r.Context(), request); err != nil {
			writeWorkerSMSError(w, err)
			return
		}
		writeJSON(w, http.StatusOK, SMSAcknowledgeResponse{Acknowledged: true})
	})
	return mux
}

func decodeWorkerSMSRequest(w http.ResponseWriter, r *http.Request, target any) bool {
	defer r.Body.Close()
	decoder := json.NewDecoder(http.MaxBytesReader(w, r.Body, 8<<10))
	decoder.DisallowUnknownFields()
	if decoder.Decode(target) != nil {
		return false
	}
	return errors.Is(decoder.Decode(&struct{}{}), io.EOF)
}

func writeWorkerSMSError(w http.ResponseWriter, err error) {
	status, code := http.StatusServiceUnavailable, "SMS_UNAVAILABLE"
	switch {
	case errors.Is(err, ErrRequestInvalid):
		status, code = http.StatusBadRequest, "REQUEST_INVALID"
	case errors.Is(err, ErrSMSMessageNotFound):
		status, code = http.StatusNotFound, "SMS_MESSAGE_NOT_FOUND"
	case errors.Is(err, ErrSMSOutcomeUnknown):
		status, code = http.StatusGatewayTimeout, "SMS_SEND_OUTCOME_UNKNOWN"
	case errors.Is(err, ErrSMSRejected):
		status, code = http.StatusUnprocessableEntity, "SMS_REJECTED"
	}
	writeJSON(w, status, errorResponse{Code: code})
}

func validOpaqueWorkerSMSID(value string) bool {
	return smsOperationPattern.MatchString(value)
}
