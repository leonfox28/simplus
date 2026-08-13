package vowifisupervisor

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net"
	"net/http"
	"path/filepath"
	"strings"
	"time"
	"unicode/utf8"

	"github.com/leonfox28/simplus/internal/smscodec"
)

const (
	maxSMSSubmitReports = 256
	maxSMSResponseBytes = 128 << 10
)

func (local *Local) SendSMS(ctx context.Context, request SMSSendRequest) (SMSSendResponse, error) {
	if !validSMSSendRequest(request) {
		return SMSSendResponse{}, ErrRequestInvalid
	}
	socket, err := local.smsWorkerSocket(request.LineID)
	if err != nil {
		return SMSSendResponse{}, err
	}
	var response SMSSendResponse
	if err := requestSMSWorker(ctx, socket, "/send", request, &response); err != nil {
		return SMSSendResponse{}, err
	}
	if !validSMSSendResponse(response) {
		return SMSSendResponse{}, ErrSMSUnavailable
	}
	return response, nil
}

func (local *Local) ListSMS(ctx context.Context, lineID string) ([]SMSMessageReference, error) {
	if !managedLinePattern.MatchString(lineID) {
		return nil, ErrRequestInvalid
	}
	socket, err := local.smsWorkerSocket(lineID)
	if err != nil {
		return nil, err
	}
	var response SMSListResponse
	if err := requestSMSWorker(ctx, socket, "/list", SMSListRequest{LineID: lineID}, &response); err != nil {
		return nil, err
	}
	if len(response.Messages) > 256 {
		return nil, ErrSMSUnavailable
	}
	if !validSMSMessageReferences(response.Messages) {
		return nil, ErrSMSUnavailable
	}
	return response.Messages, nil
}

func (local *Local) ReadSMS(ctx context.Context, lineID, messageID string) (SMSMessage, error) {
	if !validSMSMessageRequest(lineID, messageID) {
		return SMSMessage{}, ErrRequestInvalid
	}
	socket, err := local.smsWorkerSocket(lineID)
	if err != nil {
		return SMSMessage{}, err
	}
	var message SMSMessage
	if err := requestSMSWorker(ctx, socket, "/read", SMSReadRequest{LineID: lineID, MessageID: messageID}, &message); err != nil {
		return SMSMessage{}, err
	}
	if !validSMSMessagePayload(message, messageID) {
		return SMSMessage{}, ErrSMSUnavailable
	}
	return message, nil
}

func validSMSMessagePayload(message SMSMessage, expectedMessageID string) bool {
	validSegment := (message.Encoding == "gsm7" || message.Encoding == "ucs2") && message.ConcatenationReference >= 0 &&
		message.ConcatenationReference <= 255 && message.Part >= 1 && message.Total >= 1 && message.Total <= 255 &&
		message.Part <= message.Total && message.UnitCount >= 1 && message.UnitCount <= 255 && len(message.UserData) >= 1 && len(message.UserData) <= 140
	validBody := message.Total > 1 && message.Body == "" || message.Total == 1 && strings.TrimSpace(message.Body) != "" &&
		utf8.ValidString(message.Body) && utf8.RuneCountInString(message.Body) <= 1600 && len(message.Body) <= 6400
	segment := smscodec.Segment{
		Encoding: smscodec.Encoding(message.Encoding), Reference: uint16(message.ConcatenationReference),
		Part: message.Part, Total: message.Total, UnitCount: message.UnitCount, UserData: message.UserData,
	}
	decoded, decodeErr := smscodec.DecodeSegment(segment)
	validDecodedBody := message.Total > 1 || decodeErr == nil && message.Body == decoded
	return message.MessageID == expectedMessageID && message.ReceivedAt.IsZero() == false && validInboundSMSSender(message.Sender) &&
		validSegment && validBody && decodeErr == nil && validDecodedBody
}

func validSMSMessageReferences(messages []SMSMessageReference) bool {
	if len(messages) > 256 {
		return false
	}
	seen := make(map[string]struct{}, len(messages))
	for _, message := range messages {
		if !smsOperationPattern.MatchString(message.MessageID) || message.ReceivedAt.IsZero() {
			return false
		}
		if _, duplicate := seen[message.MessageID]; duplicate {
			return false
		}
		seen[message.MessageID] = struct{}{}
	}
	return true
}

func (local *Local) AcknowledgeSMS(ctx context.Context, request SMSAcknowledgeRequest) error {
	if !validSMSAcknowledgeRequest(request) {
		return ErrRequestInvalid
	}
	socket, err := local.smsWorkerSocket(request.LineID)
	if err != nil {
		return err
	}
	var response SMSAcknowledgeResponse
	if err := requestSMSWorker(ctx, socket, "/acknowledge", request, &response); err != nil {
		return err
	}
	if !response.Acknowledged {
		return ErrSMSUnavailable
	}
	return nil
}

func (local *Local) ListSMSSubmitReports(ctx context.Context, lineID string) (SMSSubmitReportListResponse, error) {
	if !managedLinePattern.MatchString(lineID) {
		return SMSSubmitReportListResponse{}, ErrRequestInvalid
	}
	socket, err := local.smsWorkerSocket(lineID)
	if err != nil {
		return SMSSubmitReportListResponse{}, err
	}
	var response SMSSubmitReportListResponse
	if err := requestSMSWorker(ctx, socket, "/reports/list", SMSListRequest{LineID: lineID}, &response); err != nil {
		return SMSSubmitReportListResponse{}, err
	}
	if len(response.Reports) > maxSMSSubmitReports || !validSMSSubmitReports(response.Reports) {
		return SMSSubmitReportListResponse{}, ErrSMSUnavailable
	}
	return response, nil
}

func (local *Local) AcknowledgeSMSSubmitReport(ctx context.Context, request SMSSubmitReportAcknowledgeRequest) error {
	if !validSMSSubmitReportAcknowledgeRequest(request) {
		return ErrRequestInvalid
	}
	socket, err := local.smsWorkerSocket(request.LineID)
	if err != nil {
		return err
	}
	var response SMSAcknowledgeResponse
	if err := requestSMSWorker(ctx, socket, "/reports/acknowledge", request, &response); err != nil {
		return err
	}
	if !response.Acknowledged {
		return ErrSMSUnavailable
	}
	return nil
}

func validSMSSubmitReports(reports []SMSSubmitReport) bool {
	if len(reports) > maxSMSSubmitReports {
		return false
	}
	seen := make(map[string]struct{}, len(reports))
	for _, report := range reports {
		if !validSMSSubmitReport(report) {
			return false
		}
		if _, duplicate := seen[report.ProviderMessageID]; duplicate {
			return false
		}
		seen[report.ProviderMessageID] = struct{}{}
	}
	return true
}

func (local *Local) smsWorkerSocket(lineID string) (string, error) {
	if local == nil {
		return "", ErrSMSUnavailable
	}
	local.mu.Lock()
	defer local.mu.Unlock()
	current := local.instances[lineID]
	if current == nil || !current.status.Online || current.status.State != StateOnline || current.runtimeDir == "" {
		return "", ErrSMSUnavailable
	}
	return filepath.Join(current.runtimeDir, workerSMSSocketName), nil
}

func requestSMSWorker(ctx context.Context, socket, path string, input, output any) error {
	encoded, err := json.Marshal(input)
	if err != nil {
		return ErrRequestInvalid
	}
	transport := &http.Transport{
		DisableCompression: true, DisableKeepAlives: true,
		DialContext: func(ctx context.Context, _, _ string) (net.Conn, error) {
			return (&net.Dialer{Timeout: 2 * time.Second}).DialContext(ctx, "unix", filepath.Clean(socket))
		},
	}
	client := &http.Client{Transport: transport, CheckRedirect: func(*http.Request, []*http.Request) error { return http.ErrUseLastResponse }}
	defer transport.CloseIdleConnections()
	request, err := http.NewRequestWithContext(ctx, http.MethodPost, "http://unix"+path, bytes.NewReader(encoded))
	if err != nil {
		return ErrSMSUnavailable
	}
	request.Header.Set("Content-Type", "application/json")
	response, err := client.Do(request)
	if err != nil {
		return ErrSMSUnavailable
	}
	defer response.Body.Close()
	if response.StatusCode < 200 || response.StatusCode >= 300 {
		var value errorResponse
		_ = json.NewDecoder(io.LimitReader(response.Body, 4<<10)).Decode(&value)
		switch value.Code {
		case "REQUEST_INVALID":
			return ErrRequestInvalid
		case "SMS_MESSAGE_NOT_FOUND":
			return ErrSMSMessageNotFound
		case "SMS_SEND_OUTCOME_UNKNOWN":
			return ErrSMSOutcomeUnknown
		case "SMS_REJECTED":
			return ErrSMSRejected
		default:
			return ErrSMSUnavailable
		}
	}
	if err := json.NewDecoder(io.LimitReader(response.Body, maxSMSResponseBytes)).Decode(output); err != nil {
		return fmt.Errorf("decode Host VoWiFi SMS response: %w", err)
	}
	return nil
}

func validInboundSMSSender(value string) bool {
	if smsAddressPattern.MatchString(value) {
		return true
	}
	if len(value) < 1 || len(value) > 20 || value[0] < 'A' || value[0] > 'Z' && value[0] < 'a' || value[0] > 'z' {
		return false
	}
	for _, current := range value[1:] {
		if current >= 'A' && current <= 'Z' || current >= 'a' && current <= 'z' || current >= '0' && current <= '9' ||
			current == ' ' || current == '.' || current == '_' || current == '-' {
			continue
		}
		return false
	}
	return true
}

var _ SMSAPI = (*Local)(nil)
